package gitx

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t",
		"GIT_COMMITTER_EMAIL=t@t", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestFetchUpdatesOnlyRemoteTrackingRefs(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	root := t.TempDir()
	bare, a, b := filepath.Join(root, "remote.git"), filepath.Join(root, "a"), filepath.Join(root, "b")
	gitIn(t, root, "init", "-q", "--bare", "-b", "main", bare)
	gitIn(t, root, "clone", "-q", bare, a)
	gitIn(t, a, "commit", "-q", "--allow-empty", "-m", "one")
	gitIn(t, a, "push", "-q", "origin", "HEAD:main")
	gitIn(t, root, "clone", "-q", bare, b)
	gitIn(t, a, "commit", "-q", "--allow-empty", "-m", "two")
	gitIn(t, a, "push", "-q", "origin", "HEAD:main")
	want := gitIn(t, a, "rev-parse", "HEAD")

	repo, err := Open(b)
	if err != nil {
		t.Fatal(err)
	}
	localBefore := gitIn(t, b, "rev-parse", "main")
	if !repo.LastFetch().IsZero() {
		t.Fatal("fresh clone already has FETCH_HEAD")
	}
	if err := repo.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := gitIn(t, b, "rev-parse", "origin/main"); got != want {
		t.Errorf("origin/main = %s, want %s", got, want)
	}
	if got := gitIn(t, b, "rev-parse", "main"); got != localBefore {
		t.Errorf("local main moved to %s", got)
	}
	if repo.LastFetch().IsZero() {
		t.Error("LastFetch is zero after a fetch")
	}
}

func TestFetchError(t *testing.T) {
	in := "ERROR: Permission to x denied.\nfatal: Could not read from remote repository.\n\nPlease make sure you have the correct access rights\nand the repository exists.\nerror: could not fetch origin\n"
	want := "ERROR: Permission to x denied.; Could not read from remote repository.; could not fetch origin"
	if got := fetchError(in); got != want {
		t.Errorf("fetchError = %q, want %q", got, want)
	}
}
