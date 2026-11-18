// Package gitx reads repository data by running the git CLI.
//
// It never writes to the repository: every command here is a read-only query.
package gitx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// EmptyTree is the hash of git's empty tree, used to diff root commits.
const EmptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// WorktreeSHA is the pseudo commit id for uncommitted changes.
const WorktreeSHA = "WORKTREE"

// Repo is a git working tree (the main checkout or a linked worktree).
type Repo struct {
	Root      string // top-level directory of the working tree
	GitDir    string // this worktree's git dir (HEAD, index)
	CommonDir string // shared git dir (refs, objects)
}

// Open resolves the repository that contains path.
func Open(path string) (*Repo, error) {
	out, err := run(context.Background(), path, "rev-parse", "--show-toplevel", "--absolute-git-dir", "--git-common-dir")
	if err != nil {
		return nil, fmt.Errorf("%s is not inside a git working tree: %w", path, err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 3 {
		return nil, fmt.Errorf("unexpected rev-parse output: %q", out)
	}
	common := lines[2]
	if !filepath.IsAbs(common) {
		common = filepath.Join(path, common)
	}
	return &Repo{Root: lines[0], GitDir: lines[1], CommonDir: filepath.Clean(common)}, nil
}

// Name is a short display name for the repository.
func (r *Repo) Name() string { return filepath.Base(r.Root) }

func (r *Repo) git(ctx context.Context, args ...string) (string, error) {
	return run(ctx, r.Root, args...)
}

func run(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"--no-optional-locks", "-c", "core.quotepath=off"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return stdout.String(), errors.New(msg)
	}
	return stdout.String(), nil
}

// Commit is one entry of the history log.
type Commit struct {
	SHA     string
	Parents []string
	Author  string
	Email   string
	Time    int64
	Subject string
}

// Scope selects which refs the history starts from.
type Scope string

const (
	// ScopeLocal shows local branches, the remote branches they track, and HEAD.
	ScopeLocal Scope = "local"
	// ScopeAll adds every remote branch and tag.
	ScopeAll Scope = "all"
)

// ParseScope reads a scope name, defaulting to ScopeLocal.
func ParseScope(s string) Scope {
	if s == string(ScopeAll) {
		return ScopeAll
	}
	return ScopeLocal
}

// Log lists commits reachable from the scope's refs, newest first, in date order
// that never shows a parent before its children.
func (r *Repo) Log(ctx context.Context, max int, scope Scope) ([]Commit, error) {
	args := []string{"log", "--date-order", "--max-count=" + strconv.Itoa(max),
		"--format=%H%x1f%P%x1f%an%x1f%ae%x1f%at%x1f%s%x1e"}
	if scope == ScopeAll {
		args = append(args, "--branches", "--remotes", "--tags")
	} else {
		args = append(args, "--branches")
		args = append(args, r.upstreams(ctx)...)
	}
	args = append(args, "HEAD", "--")
	out, err := r.git(ctx, args...)
	if err != nil {
		// A fresh repository has no HEAD yet.
		if strings.Contains(err.Error(), "unknown revision") || strings.Contains(err.Error(), "does not have any commits") {
			return nil, nil
		}
		return nil, err
	}
	var commits []Commit
	for _, rec := range strings.Split(out, "\x1e") {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}
		f := strings.SplitN(rec, "\x1f", 6)
		if len(f) < 6 {
			continue
		}
		t, _ := strconv.ParseInt(f[4], 10, 64)
		commits = append(commits, Commit{
			SHA: f[0], Parents: strings.Fields(f[1]), Author: f[2], Email: f[3], Time: t, Subject: f[5],
		})
	}
	return commits, nil
}

// upstreams lists the remote-tracking refs that local branches track.
func (r *Repo) upstreams(ctx context.Context) []string {
	out, err := r.git(ctx, "for-each-ref", "--format=%(upstream)", "refs/heads")
	if err != nil {
		return nil
	}
	var refs []string
	seen := map[string]bool{}
	for _, u := range strings.Fields(out) {
		// Only existing remote-tracking refs; a gone upstream would make git log fail.
		if strings.HasPrefix(u, "refs/remotes/") && !seen[u] {
			if _, err := r.git(ctx, "rev-parse", "-q", "--verify", u); err == nil {
				seen[u] = true
				refs = append(refs, u)
			}
		}
	}
	return refs
}

// RefKind classifies a ref for display.
type RefKind string

const (
	RefHead   RefKind = "head"   // the HEAD pointer itself (detached)
	RefLocal  RefKind = "local"  // refs/heads/*
	RefRemote RefKind = "remote" // refs/remotes/*
	RefTag    RefKind = "tag"    // refs/tags/*
)

// Ref is a name pointing at a commit.
type Ref struct {
	Name     string  `json:"n"`
	Kind     RefKind `json:"k"`
	Current  bool    `json:"c,omitempty"` // checked out in this worktree
	Worktree string  `json:"w,omitempty"` // checked out in another worktree (its directory name)
}

// HeadInfo describes what this worktree has checked out.
type HeadInfo struct {
	SHA    string `json:"sha"`
	Branch string `json:"branch"` // empty when detached
}

// Head returns the checked-out commit and branch.
func (r *Repo) Head(ctx context.Context) (HeadInfo, error) {
	var h HeadInfo
	sha, err := r.git(ctx, "rev-parse", "--verify", "-q", "HEAD")
	if err != nil {
		sha = ""
	}
	h.SHA = strings.TrimSpace(sha)
	if br, err := r.git(ctx, "symbolic-ref", "-q", "--short", "HEAD"); err == nil {
		h.Branch = strings.TrimSpace(br)
	}
	return h, nil
}

// Refs maps commit SHA to the refs pointing at it.
func (r *Repo) Refs(ctx context.Context, head HeadInfo) (map[string][]Ref, string, error) {
	out, err := r.git(ctx, "for-each-ref", "--format=%(objectname)%1f%(*objectname)%1f%(refname)",
		"refs/heads", "refs/remotes", "refs/tags")
	if err != nil {
		return nil, "", err
	}
	others := r.otherWorktreeBranches(ctx)
	refs := map[string][]Ref{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Split(line, "\x1f")
		if len(f) != 3 {
			continue
		}
		sha := f[0]
		if f[1] != "" { // annotated tag: point at the tagged commit
			sha = f[1]
		}
		name := f[2]
		var ref Ref
		switch {
		case strings.HasPrefix(name, "refs/heads/"):
			short := strings.TrimPrefix(name, "refs/heads/")
			ref = Ref{Name: short, Kind: RefLocal, Current: short == head.Branch, Worktree: others[short]}
		case strings.HasPrefix(name, "refs/remotes/"):
			short := strings.TrimPrefix(name, "refs/remotes/")
			if strings.HasSuffix(short, "/HEAD") {
				continue
			}
			ref = Ref{Name: short, Kind: RefRemote}
		case strings.HasPrefix(name, "refs/tags/"):
			ref = Ref{Name: strings.TrimPrefix(name, "refs/tags/"), Kind: RefTag}
		default:
			continue
		}
		refs[sha] = append(refs[sha], ref)
	}
	if head.Branch == "" && head.SHA != "" {
		refs[head.SHA] = append([]Ref{{Name: "HEAD", Kind: RefHead, Current: true}}, refs[head.SHA]...)
	}
	return refs, out, nil
}

// otherWorktreeBranches maps branch name to the directory name of another worktree
// that has it checked out.
func (r *Repo) otherWorktreeBranches(ctx context.Context) map[string]string {
	res := map[string]string{}
	out, err := r.git(ctx, "worktree", "list", "--porcelain")
	if err != nil {
		return res
	}
	for _, block := range strings.Split(out, "\n\n") {
		var path, branch string
		for _, l := range strings.Split(block, "\n") {
			if p, ok := strings.CutPrefix(l, "worktree "); ok {
				path = p
			} else if b, ok := strings.CutPrefix(l, "branch refs/heads/"); ok {
				branch = b
			}
		}
		if branch != "" && path != "" && filepath.Clean(path) != filepath.Clean(r.Root) {
			res[branch] = filepath.Base(path)
		}
	}
	return res
}

// Status returns the porcelain status output (empty when the tree is clean).
func (r *Repo) Status(ctx context.Context) (string, error) {
	return r.git(ctx, "status", "--porcelain=v1", "-z", "--untracked-files=all")
}

// CountStatus counts entries in porcelain -z output.
func CountStatus(status string) int {
	n := 0
	parts := strings.Split(status, "\x00")
	for i := 0; i < len(parts); i++ {
		p := parts[i]
		if len(p) < 4 {
			continue
		}
		n++
		if p[0] == 'R' || p[0] == 'C' { // rename/copy entries carry the old path as an extra field
			i++
		}
	}
	return n
}
