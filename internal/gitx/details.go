package gitx

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// FileChange is one changed path in a commit.
type FileChange struct {
	Status  string `json:"status"` // A, M, D, R, C, T or ? (untracked)
	Path    string `json:"path"`
	OldPath string `json:"oldPath,omitempty"`
	Add     int    `json:"add"` // -1 for binary files
	Del     int    `json:"del"`
}

// CommitDetail is everything the details panel shows for one commit.
type CommitDetail struct {
	SHA            string       `json:"sha"`
	Parents        []string     `json:"parents"`
	Author         string       `json:"author"`
	Email          string       `json:"email"`
	AuthorTime     int64        `json:"authorTime"`
	Committer      string       `json:"committer"`
	CommitterEmail string       `json:"committerEmail"`
	CommitTime     int64        `json:"commitTime"`
	Message        string       `json:"message"`
	Files          []FileChange `json:"files"`
	Base           string       `json:"base"` // what the files are compared against
}

// validRev guards user-supplied revisions before they reach git.
func validRev(s string) bool {
	if s == WorktreeSHA {
		return true
	}
	if len(s) < 4 || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// Detail loads a commit's metadata and changed files (against its first parent).
func (r *Repo) Detail(ctx context.Context, sha string) (*CommitDetail, error) {
	if !validRev(sha) {
		return nil, fmt.Errorf("invalid commit id %q", sha)
	}
	if sha == WorktreeSHA {
		return r.worktreeDetail(ctx)
	}
	out, err := r.git(ctx, "show", "-s", "--format=%H%x1f%P%x1f%an%x1f%ae%x1f%at%x1f%cn%x1f%ce%x1f%ct%x1f%B", sha, "--")
	if err != nil {
		return nil, err
	}
	f := strings.SplitN(out, "\x1f", 9)
	if len(f) < 9 {
		return nil, fmt.Errorf("unexpected git show output")
	}
	d := &CommitDetail{
		SHA: f[0], Parents: strings.Fields(f[1]), Author: f[2], Email: f[3],
		Committer: f[5], CommitterEmail: f[6], Message: strings.TrimRight(f[8], "\n"),
	}
	d.AuthorTime, _ = strconv.ParseInt(f[4], 10, 64)
	d.CommitTime, _ = strconv.ParseInt(f[7], 10, 64)
	d.Base = EmptyTree
	if len(d.Parents) > 0 {
		d.Base = d.Parents[0]
	}
	d.Files, err = r.changedFiles(ctx, d.Base, d.SHA)
	return d, err
}

func (r *Repo) worktreeDetail(ctx context.Context) (*CommitDetail, error) {
	head, _ := r.Head(ctx)
	d := &CommitDetail{SHA: WorktreeSHA, Message: "Uncommitted changes", Base: head.SHA}
	if head.SHA != "" {
		d.Parents = []string{head.SHA}
	} else {
		d.Base = EmptyTree
	}
	files, err := r.changedFiles(ctx, d.Base, "")
	if err != nil {
		return nil, err
	}
	out, err := r.git(ctx, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	for _, p := range strings.Split(out, "\x00") {
		if p == "" {
			continue
		}
		add := countLines(filepath.Join(r.Root, p))
		files = append(files, FileChange{Status: "?", Path: p, Add: add})
	}
	d.Files = files
	return d, nil
}

// changedFiles diffs base against to (or the working tree when to is empty).
func (r *Repo) changedFiles(ctx context.Context, base, to string) ([]FileChange, error) {
	args := func(mode string) []string {
		a := []string{"diff", mode, "-z", "-M", "--no-ext-diff", base}
		if to != "" {
			a = append(a, to)
		}
		return append(a, "--")
	}
	ns, err := r.git(ctx, args("--name-status")...)
	if err != nil {
		return nil, err
	}
	num, err := r.git(ctx, args("--numstat")...)
	if err != nil {
		return nil, err
	}
	var files []FileChange
	parts := strings.Split(ns, "\x00")
	for i := 0; i+1 < len(parts); i++ {
		st := parts[i]
		if st == "" {
			continue
		}
		fc := FileChange{Status: st[:1]}
		if fc.Status == "R" || fc.Status == "C" {
			if i+2 >= len(parts) {
				break
			}
			fc.OldPath, fc.Path = parts[i+1], parts[i+2]
			i += 2
		} else {
			fc.Path = parts[i+1]
			i++
		}
		files = append(files, fc)
	}
	// numstat -z: "add\tdel\tpath\0" or, for renames, "add\tdel\t\0old\0new\0".
	counts := map[string][2]int{}
	np := strings.Split(num, "\x00")
	for i := 0; i < len(np); i++ {
		f := strings.SplitN(np[i], "\t", 3)
		if len(f) != 3 {
			continue
		}
		path := f[2]
		if path == "" && i+2 < len(np) {
			path = np[i+2]
			i += 2
		}
		add, del := -1, -1
		if f[0] != "-" {
			add, _ = strconv.Atoi(f[0])
			del, _ = strconv.Atoi(f[1])
		}
		counts[path] = [2]int{add, del}
	}
	for i := range files {
		if c, ok := counts[files[i].Path]; ok {
			files[i].Add, files[i].Del = c[0], c[1]
		}
	}
	return files, nil
}

func countLines(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	if bytes.IndexByte(b[:min(len(b), 8000)], 0) >= 0 {
		return -1
	}
	n := bytes.Count(b, []byte{'\n'})
	if len(b) > 0 && b[len(b)-1] != '\n' {
		n++
	}
	return n
}
