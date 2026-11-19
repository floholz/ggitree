// Package server serves the ggitree page and its JSON API on localhost.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/floholz/ggitree/internal/gitx"
)

// Version is the ggitree build version, reported by /api/health.
var Version = "dev"

// HeaderName must be sent with every request that changes server state. Browsers
// cannot add it to a cross-site request without CORS approval, which ggitree never gives.
const HeaderName = "X-Ggitree"

// Server holds the repository being shown and its computed history.
type Server struct {
	addr   string
	assets fs.FS

	mu       sync.RWMutex
	repo     *gitx.Repo
	snaps    map[gitx.Scope]*Snapshot // built on first use per scope
	building sync.Mutex
	version  int
	err      string

	clientsMu sync.Mutex
	clients   map[chan string]struct{}

	refresh  chan struct{}
	watchCtl chan *gitx.Repo
	shutdown func()

	fetching    sync.Mutex // held while a fetch runs; one at a time
	fetchMu     sync.Mutex // guards the fields below
	fetchErr    string     // the last fetch's error, until one succeeds
	fetchFailAt time.Time
	lastFetch   time.Time // FETCH_HEAD time the pages were last told about
}

// autoFetchBackoff is how long automatic fetches pause after one failed, so a
// missing key or a dead network doesn't retry every few minutes. Fetching by hand
// always tries.
const autoFetchBackoff = 15 * time.Minute

// New creates a server that will listen on addr and serve the page from assets.
func New(addr string, assets fs.FS) *Server {
	return &Server{
		addr:     addr,
		assets:   assets,
		clients:  map[chan string]struct{}{},
		refresh:  make(chan struct{}, 1),
		watchCtl: make(chan *gitx.Repo, 1),
	}
}

// Run serves until ctx ends or a shutdown request arrives.
func (s *Server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.shutdown = cancel

	srv := &http.Server{Handler: s.routes(), ReadHeaderTimeout: 10 * time.Second}
	go s.watchLoop(ctx)
	go s.refreshLoop(ctx)
	go func() {
		<-ctx.Done()
		s.broadcast("bye")
		shutCtx, c := context.WithTimeout(context.Background(), 2*time.Second)
		defer c()
		_ = srv.Shutdown(shutCtx)
	}()
	log.Printf("ggitree %s listening on http://%s", Version, s.addr)
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Open switches the server to the repository containing path.
func (s *Server) Open(path string) error {
	repo, err := gitx.Open(path)
	if err != nil {
		return err
	}
	s.mu.Lock()
	same := s.repo != nil && s.repo.Root == repo.Root
	s.mu.Unlock()
	if same {
		s.broadcast("focus")
		return nil
	}
	snap, err := buildSnapshot(context.Background(), repo, gitx.ScopeLocal)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.repo, s.err = repo, ""
	s.snaps = map[gitx.Scope]*Snapshot{gitx.ScopeLocal: snap}
	s.version++
	s.mu.Unlock()
	s.fetchMu.Lock()
	s.fetchErr, s.fetchFailAt, s.lastFetch = "", time.Time{}, repo.LastFetch()
	s.fetchMu.Unlock()
	select {
	case s.watchCtl <- repo:
	default:
		<-s.watchCtl
		s.watchCtl <- repo
	}
	log.Printf("showing %s", repo.Root)
	s.broadcast("repo")
	return nil
}

// current returns the open repository and its history in the given scope,
// building that scope's history on first use.
func (s *Server) current(scope gitx.Scope) (*gitx.Repo, *Snapshot, int, string) {
	s.mu.RLock()
	repo, snap, version, errMsg := s.repo, s.snaps[scope], s.version, s.err
	s.mu.RUnlock()
	if repo == nil || snap != nil {
		return repo, snap, version, errMsg
	}
	s.building.Lock()
	defer s.building.Unlock()
	s.mu.RLock()
	snap = s.snaps[scope]
	s.mu.RUnlock()
	if snap != nil {
		return repo, snap, version, errMsg
	}
	next, err := buildSnapshot(context.Background(), repo, scope)
	if err != nil {
		return repo, nil, version, err.Error()
	}
	s.mu.Lock()
	if s.repo == repo {
		s.snaps[scope] = next
	}
	version = s.version
	s.mu.Unlock()
	return repo, next, version, errMsg
}

// requestRefresh asks the refresh loop to check the repository for changes.
func (s *Server) requestRefresh() {
	select {
	case s.refresh <- struct{}{}:
	default:
	}
}

// refreshLoop rebuilds the history when the repository changed. It runs on file
// events and, while a page is connected, every two seconds to catch edits in the
// working tree (which are not watched).
func (s *Server) refreshLoop(ctx context.Context) {
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.refresh:
		case <-tick.C:
			if s.clientCount() == 0 {
				continue
			}
		}
		s.mu.RLock()
		repo := s.repo
		var scopes []gitx.Scope
		var old *Snapshot
		for sc, sn := range s.snaps {
			scopes = append(scopes, sc)
			old = sn
		}
		s.mu.RUnlock()
		if repo == nil {
			continue
		}
		// A fetch that changed no refs still moves FETCH_HEAD; pages show its age.
		if t := repo.LastFetch(); !t.IsZero() {
			s.fetchMu.Lock()
			moved := !t.Equal(s.lastFetch)
			s.lastFetch = t
			s.fetchMu.Unlock()
			if moved {
				s.broadcast("fetched")
			}
		}
		fp, _, _, _, err := fingerprint(ctx, repo)
		if err == nil && old != nil && fp == old.Fingerprint {
			continue
		}
		next := map[gitx.Scope]*Snapshot{}
		var buildErr error
		for _, sc := range scopes {
			sn, err := buildSnapshot(ctx, repo, sc)
			if err != nil {
				buildErr = err
				break
			}
			next[sc] = sn
		}
		s.mu.Lock()
		if s.repo != repo { // switched while building
			s.mu.Unlock()
			continue
		}
		if buildErr != nil {
			s.err = buildErr.Error()
		} else {
			s.snaps, s.err = next, ""
		}
		s.version++
		s.mu.Unlock()
		s.broadcast("changed")
	}
}

func (s *Server) clientCount() int {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	return len(s.clients)
}

func (s *Server) broadcast(event string) {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	for c := range s.clients {
		select {
		case c <- event:
		default:
		}
	}
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(s.assets))
	mux.HandleFunc("GET /assets/syntax.css", serveSyntaxCSS)
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/state", s.handleState)
	mux.HandleFunc("GET /api/rows", s.handleRows)
	mux.HandleFunc("GET /api/locate", s.handleLocate)
	mux.HandleFunc("GET /api/search", s.handleSearch)
	mux.HandleFunc("GET /api/refs", s.handleRefs)
	mux.HandleFunc("GET /api/commit", s.handleCommit)
	mux.HandleFunc("GET /api/diff", s.handleDiff)
	mux.HandleFunc("GET /api/events", s.handleEvents)
	mux.HandleFunc("POST /api/open", s.requireHeader(s.handleOpen))
	mux.HandleFunc("POST /api/fetch", s.requireHeader(s.handleFetch))
	mux.HandleFunc("POST /api/shutdown", s.requireHeader(s.handleShutdown))
	return s.checkHost(mux)
}

// checkHost rejects requests addressed to any other host name, which blocks
// DNS-rebinding pages from reading the API.
func (s *Server) checkHost(next http.Handler) http.Handler {
	_, port, _ := net.SplitHostPort(s.addr)
	allowed := map[string]bool{
		"127.0.0.1:" + port: true, "localhost:" + port: true, "[::1]:" + port: true,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowed[r.Host] {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireHeader(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(HeaderName) != "1" {
			http.Error(w, "missing "+HeaderName+" header", http.StatusForbidden)
			return
		}
		h(w, r)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"app": "ggitree", "version": Version, "pid": os.Getpid()})
}

type stateJSON struct {
	Version int            `json:"version"`
	Repo    *repoJSON      `json:"repo"`
	Head    *gitx.HeadInfo `json:"head,omitempty"`
	Dirty   int            `json:"dirty"`
	Total   int            `json:"total"`
	Error   string         `json:"error,omitempty"`
	fetchJSON
}

// fetchJSON is the fetch status, part of the state and the answer to a fetch.
type fetchJSON struct {
	Fetched    int64  `json:"fetched,omitempty"`    // unix time of the last fetch by anyone; 0 for never
	FetchError string `json:"fetchError,omitempty"` // why the last fetch by ggitree failed
}

func (s *Server) fetchStatus(repo *gitx.Repo) fetchJSON {
	var f fetchJSON
	if t := repo.LastFetch(); !t.IsZero() {
		f.Fetched = t.Unix()
	}
	s.fetchMu.Lock()
	f.FetchError = s.fetchErr
	s.fetchMu.Unlock()
	return f
}

type repoJSON struct {
	Name string `json:"name"`
	Root string `json:"root"`
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	repo, snap, version, errMsg := s.current(scopeOf(r))
	st := stateJSON{Version: version, Error: errMsg}
	if repo != nil {
		st.Repo = &repoJSON{Name: repo.Name(), Root: repo.Root}
		st.fetchJSON = s.fetchStatus(repo)
	}
	if snap != nil {
		st.Head = &snap.Head
		st.Dirty = snap.Dirty
		st.Total = len(snap.Rows)
	}
	writeJSON(w, st)
}

func scopeOf(r *http.Request) gitx.Scope { return gitx.ParseScope(r.URL.Query().Get("scope")) }

func intParam(r *http.Request, name string, def int) int {
	if v, err := strconv.Atoi(r.URL.Query().Get(name)); err == nil {
		return v
	}
	return def
}

func (s *Server) handleRows(w http.ResponseWriter, r *http.Request) {
	_, snap, version, _ := s.current(scopeOf(r))
	offset := max(intParam(r, "offset", 0), 0)
	limit := min(max(intParam(r, "limit", 500), 1), 5000)
	rows := []RowJSON{}
	if snap != nil && offset < len(snap.Rows) {
		rows = snap.Rows[offset:min(offset+limit, len(snap.Rows))]
	}
	writeJSON(w, map[string]any{"version": version, "offset": offset, "rows": rows})
}

func (s *Server) handleLocate(w http.ResponseWriter, r *http.Request) {
	_, snap, version, _ := s.current(scopeOf(r))
	var idx []int
	for _, sha := range strings.Split(r.URL.Query().Get("sha"), ",") {
		i := -1
		if snap != nil && sha != "" {
			i = snap.Locate(sha)
		}
		idx = append(idx, i)
	}
	writeJSON(w, map[string]any{"version": version, "indexes": idx})
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	_, snap, version, _ := s.current(scopeOf(r))
	matches := []int{}
	if snap != nil {
		if m := snap.Search(r.URL.Query().Get("q"), 10_000); m != nil {
			matches = m
		}
	}
	writeJSON(w, map[string]any{"version": version, "matches": matches})
}

func (s *Server) handleRefs(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	repo, version := s.repo, s.version
	s.mu.RUnlock()
	if repo == nil {
		writeJSON(w, map[string]any{"version": version, "refs": gitx.RefList{}})
		return
	}
	list, err := repo.ListRefs(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{"version": version, "refs": list})
}

func (s *Server) handleCommit(w http.ResponseWriter, r *http.Request) {
	repo, snap, _, _ := s.current(scopeOf(r))
	if repo == nil {
		writeErr(w, http.StatusConflict, errors.New("no repository open"))
		return
	}
	sha := r.URL.Query().Get("sha")
	d, err := repo.Detail(r.Context(), sha)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var refs []gitx.Ref
	if snap != nil {
		if i := snap.Locate(d.SHA); i >= 0 {
			refs = snap.Rows[i].Refs
		}
	}
	writeJSON(w, map[string]any{"commit": d, "refs": refs})
}

func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	repo := s.repo
	s.mu.RUnlock()
	if repo == nil {
		writeErr(w, http.StatusConflict, errors.New("no repository open"))
		return
	}
	q := r.URL.Query()
	d, err := repo.Diff(r.Context(), q.Get("sha"), q.Get("path"), q.Get("old"), q.Get("untracked") == "1")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, d)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	c := make(chan string, 8)
	s.clientsMu.Lock()
	s.clients[c] = struct{}{}
	s.clientsMu.Unlock()
	defer func() {
		s.clientsMu.Lock()
		delete(s.clients, c)
		s.clientsMu.Unlock()
	}()
	s.requestRefresh() // a page that was away may have missed changes

	fmt.Fprint(w, "retry: 1000\n\n")
	flusher.Flush()
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-c:
			fmt.Fprintf(w, "event: %s\ndata: {}\n\n", ev)
			flusher.Flush()
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

func (s *Server) handleOpen(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil || body.Path == "" {
		writeErr(w, http.StatusBadRequest, errors.New("expected {\"path\": ...}"))
		return
	}
	if err := s.Open(body.Path); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	s.mu.RLock()
	root := s.repo.Root
	s.mu.RUnlock()
	writeJSON(w, map[string]string{"root": root})
}

// handleFetch fetches every remote. {"auto": true} marks a timed fetch, which is
// skipped while backing off from a failure.
func (s *Server) handleFetch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Auto bool `json:"auto"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&body)
	s.mu.RLock()
	repo := s.repo
	s.mu.RUnlock()
	if repo == nil {
		writeErr(w, http.StatusConflict, errors.New("no repository open"))
		return
	}
	type result struct {
		fetchJSON
		Skipped bool `json:"skipped,omitempty"` // backing off, or another fetch is running
	}
	s.fetchMu.Lock()
	backoff := body.Auto && s.fetchErr != "" && time.Since(s.fetchFailAt) < autoFetchBackoff
	s.fetchMu.Unlock()
	if backoff || !s.fetching.TryLock() {
		writeJSON(w, result{fetchJSON: s.fetchStatus(repo), Skipped: true})
		return
	}
	// Not the request's context: a page that reloads mid-fetch shouldn't kill git.
	err := repo.Fetch(context.Background())
	s.fetching.Unlock()
	s.mu.RLock()
	switched := s.repo != repo
	s.mu.RUnlock()
	if switched { // the result belongs to a repository no longer shown
		writeJSON(w, result{Skipped: true})
		return
	}
	s.fetchMu.Lock()
	if err != nil {
		s.fetchErr, s.fetchFailAt = err.Error(), time.Now()
		log.Printf("fetch: %v", err)
	} else {
		s.fetchErr = ""
	}
	s.fetchMu.Unlock()
	s.requestRefresh()
	writeJSON(w, result{fetchJSON: s.fetchStatus(repo)})
}

func (s *Server) handleShutdown(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]bool{"ok": true})
	go func() {
		time.Sleep(100 * time.Millisecond)
		s.shutdown()
	}()
}
