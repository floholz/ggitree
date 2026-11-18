package server

import (
	"bytes"
	"context"
	"io/fs"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/fsnotify/fsnotify"

	"github.com/floholz/ggitree/internal/gitx"
)

// watchLoop watches the open repository's git directories (HEAD, index, refs) and
// asks for a refresh shortly after they change.
func (s *Server) watchLoop(ctx context.Context) {
	var w *fsnotify.Watcher
	var events <-chan fsnotify.Event
	var errs <-chan error
	var debounce *time.Timer
	var debounceC <-chan time.Time
	defer func() {
		if w != nil {
			w.Close()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case repo := <-s.watchCtl:
			if w != nil {
				w.Close()
			}
			nw, err := fsnotify.NewWatcher()
			if err != nil {
				log.Printf("watch: %v (live updates fall back to polling)", err)
				w, events, errs = nil, nil, nil
				continue
			}
			w, events, errs = nw, nw.Events, nw.Errors
			addRepoWatches(w, repo)
		case ev := <-events:
			if strings.HasSuffix(ev.Name, ".lock") {
				continue
			}
			// New directories under refs/ (e.g. refs/heads/feature/) need their own watch.
			if ev.Has(fsnotify.Create) && strings.Contains(ev.Name, string(filepath.Separator)+"refs") {
				addTree(w, ev.Name)
			}
			if debounce == nil {
				debounce = time.NewTimer(150 * time.Millisecond)
			} else {
				debounce.Reset(150 * time.Millisecond)
			}
			debounceC = debounce.C
		case err := <-errs:
			log.Printf("watch: %v", err)
		case <-debounceC:
			debounceC = nil
			s.requestRefresh()
		}
	}
}

func addRepoWatches(w *fsnotify.Watcher, repo *gitx.Repo) {
	_ = w.Add(repo.GitDir)
	if repo.CommonDir != repo.GitDir {
		_ = w.Add(repo.CommonDir)
		// Other worktrees' HEADs live here; their checkouts move worktree markers.
		addTree(w, filepath.Join(repo.CommonDir, "worktrees"))
	}
	addTree(w, filepath.Join(repo.CommonDir, "refs"))
}

func addTree(w *fsnotify.Watcher, root string) {
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		_ = w.Add(p)
		return nil
	})
}

var (
	syntaxOnce sync.Once
	syntaxCSS  []byte
)

// serveSyntaxCSS serves token colors for highlighted diffs, light and dark.
func serveSyntaxCSS(w http.ResponseWriter, r *http.Request) {
	syntaxOnce.Do(func() {
		var b bytes.Buffer
		b.WriteString(themeCSS("github"))
		b.WriteString("@media (prefers-color-scheme: dark) {\n")
		b.WriteString(themeCSS("github-dark"))
		b.WriteString("}\n")
		syntaxCSS = b.Bytes()
	})
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	_, _ = w.Write(syntaxCSS)
}

// themeCSS keeps only per-token colors and styles; backgrounds come from the page.
func themeCSS(name string) string {
	var b bytes.Buffer
	f := html.New(html.WithClasses(true))
	_ = f.WriteCSS(&b, styles.Get(name))
	var out strings.Builder
	for _, line := range strings.Split(b.String(), "\n") {
		i := strings.Index(line, ".chroma .")
		if i < 0 {
			continue
		}
		rule := line[i:]
		open, end := strings.Index(rule, "{"), strings.LastIndex(rule, "}")
		if open < 0 || end < open {
			continue
		}
		var props []string
		for _, p := range strings.Split(rule[open+1:end], ";") {
			p = strings.TrimSpace(p)
			if p != "" && !strings.HasPrefix(p, "background") {
				props = append(props, p)
			}
		}
		if len(props) > 0 {
			out.WriteString(strings.TrimSpace(rule[:open]) + " { " + strings.Join(props, "; ") + " }\n")
		}
	}
	return out.String()
}
