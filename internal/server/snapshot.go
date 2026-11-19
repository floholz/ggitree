package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"

	"github.com/floholz/ggitree/internal/gitx"
	"github.com/floholz/ggitree/internal/graph"
)

// maxCommits caps how much history is loaded.
const maxCommits = 100_000

// RowJSON is one row as sent to the page. Field names are short because a page
// of history holds thousands of rows.
type RowJSON struct {
	SHA     string     `json:"h"`
	Parents []string   `json:"p"`
	Subject string     `json:"s"`
	Author  string     `json:"a"`
	Email   string     `json:"m"`
	Time    int64      `json:"t"`
	Refs    []gitx.Ref `json:"r,omitempty"`
	Lane    int        `json:"l"`
	Color   int        `json:"c"`
	Width   int        `json:"w"`
	Edges   [][6]int   `json:"e"` // x1, y1, x2, y2, color, dashed (0 or 1)
	Push    int        `json:"x,omitempty"` // gitx.PushState: 1 not pushed, 2 upstream gone
}

// Snapshot is the computed history of a repository at one moment.
type Snapshot struct {
	Fingerprint string
	Head        gitx.HeadInfo
	Dirty       int // number of uncommitted paths
	Rows        []RowJSON
	index       map[string]int
}

// fingerprint summarizes the repository state cheaply, to tell whether the history
// needs to be rebuilt.
func fingerprint(ctx context.Context, repo *gitx.Repo) (string, gitx.HeadInfo, map[string][]gitx.Ref, int, error) {
	head, _ := repo.Head(ctx)
	refs, rawRefs, err := repo.Refs(ctx, head)
	if err != nil {
		return "", head, nil, 0, err
	}
	status, _ := repo.Status(ctx)
	dirty := gitx.CountStatus(status)
	h := sha256.New()
	for _, s := range []string{head.SHA, head.Branch, rawRefs, status} {
		h.Write([]byte(s))
		h.Write([]byte{0})
	}
	// Worktree markers on refs come from other checkouts.
	for sha, rs := range refs {
		for _, r := range rs {
			if r.Worktree != "" {
				h.Write([]byte(sha + r.Name + r.Worktree))
			}
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:16], head, refs, dirty, nil
}

func buildSnapshot(ctx context.Context, repo *gitx.Repo, scope gitx.Scope) (*Snapshot, error) {
	fp, head, refs, dirty, err := fingerprint(ctx, repo)
	if err != nil {
		return nil, err
	}
	commits, err := repo.Log(ctx, maxCommits, scope)
	if err != nil {
		return nil, err
	}
	if dirty > 0 {
		wt := gitx.Commit{SHA: gitx.WorktreeSHA, Subject: uncommittedLabel(dirty)}
		if head.SHA != "" {
			wt.Parents = []string{head.SHA}
		}
		commits = append([]gitx.Commit{wt}, commits...)
	}

	// Parents outside the loaded history (shallow clones, the commit cap) would
	// leave lanes open forever; the layout only follows parents it will see.
	known := make(map[string]bool, len(commits))
	for _, c := range commits {
		known[c.SHA] = true
	}
	push := repo.PushStates(ctx)
	nodes := make([]graph.Node, len(commits))
	for i, c := range commits {
		var ps []string
		for _, p := range c.Parents {
			if known[p] {
				ps = append(ps, p)
			}
		}
		// Not yet on a remote: uncommitted changes and unpushed commits.
		dashed := c.SHA == gitx.WorktreeSHA || push[c.SHA] == gitx.Unpushed
		nodes[i] = graph.Node{ID: c.SHA, Parents: ps, Dashed: dashed}
	}
	layout := graph.Layout(nodes)

	s := &Snapshot{Fingerprint: fp, Head: head, Dirty: dirty, Rows: make([]RowJSON, len(commits)), index: make(map[string]int, len(commits))}
	for i, c := range commits {
		g := layout[i]
		edges := make([][6]int, len(g.Segments))
		for j, seg := range g.Segments {
			d := 0
			if seg.Dashed {
				d = 1
			}
			edges[j] = [6]int{seg.X1, seg.Y1, seg.X2, seg.Y2, seg.Color, d}
		}
		parents := c.Parents
		if parents == nil {
			parents = []string{}
		}
		s.Rows[i] = RowJSON{
			SHA: c.SHA, Parents: parents, Subject: c.Subject, Author: c.Author, Email: c.Email, Time: c.Time,
			Refs: refs[c.SHA], Lane: g.Lane, Color: g.Color, Width: g.Width, Edges: edges,
			Push: int(push[c.SHA]),
		}
		s.index[c.SHA] = i
	}
	return s, nil
}

func uncommittedLabel(n int) string {
	if n == 1 {
		return "Uncommitted changes (1 file)"
	}
	return "Uncommitted changes (" + strconv.Itoa(n) + " files)"
}

// Locate finds the row of a full or abbreviated commit id.
func (s *Snapshot) Locate(sha string) int {
	if i, ok := s.index[sha]; ok {
		return i
	}
	if len(sha) >= 4 {
		for i, r := range s.Rows {
			if strings.HasPrefix(r.SHA, sha) {
				return i
			}
		}
	}
	return -1
}

// Search returns the rows whose subject, author, id or refs contain q (case-insensitive).
func (s *Snapshot) Search(q string, limit int) []int {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return nil
	}
	var res []int
	for i, r := range s.Rows {
		if strings.Contains(strings.ToLower(r.Subject), q) ||
			strings.Contains(strings.ToLower(r.Author), q) ||
			strings.Contains(strings.ToLower(r.Email), q) ||
			strings.HasPrefix(r.SHA, q) ||
			refMatches(r.Refs, q) {
			res = append(res, i)
			if len(res) >= limit {
				break
			}
		}
	}
	return res
}

func refMatches(refs []gitx.Ref, q string) bool {
	for _, r := range refs {
		if strings.Contains(strings.ToLower(r.Name), q) {
			return true
		}
	}
	return false
}
