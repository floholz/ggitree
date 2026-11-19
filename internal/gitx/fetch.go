package gitx

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// FetchTimeout bounds one fetch of every remote.
const FetchTimeout = 2 * time.Minute

// Fetch updates the remote-tracking branches from every remote. It is the only
// command in this package that writes to the repository, and it writes only what
// git fetch writes: objects, remote-tracking refs, fetched tags and FETCH_HEAD.
// Local branches, HEAD, the index and the working tree are never touched.
//
// It never asks for credentials (a key that needs a passphrase fails instead of
// prompting), starts no gc or maintenance, skips submodules, and follows the
// repository's own fetch.prune setting.
func (r *Repo) Fetch(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, FetchTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git",
		"-c", "gc.auto=0", "-c", "maintenance.auto=false",
		"fetch", "--all", "--quiet", "--recurse-submodules=no")
	cmd.Dir = r.Root
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never", "LC_ALL=C")
	if ssh := r.batchSSH(ctx); ssh != "" {
		cmd.Env = append(cmd.Env, "GIT_SSH_COMMAND="+ssh)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return errors.New("fetch timed out")
		}
		if msg := fetchError(stderr.String()); msg != "" {
			return errors.New(msg)
		}
		return err
	}
	return nil
}

// batchSSH returns an ssh command that fails instead of prompting, or "" to leave
// the user's setup alone (a custom GIT_SSH_COMMAND, or an sshCommand that isn't ssh).
func (r *Repo) batchSSH(ctx context.Context) string {
	if os.Getenv("GIT_SSH_COMMAND") != "" || os.Getenv("GIT_SSH") != "" {
		return ""
	}
	base := "ssh"
	if out, err := r.git(ctx, "config", "--get", "core.sshCommand"); err == nil && strings.TrimSpace(out) != "" {
		base = strings.TrimSpace(out)
		if f := strings.Fields(base); len(f) == 0 || filepath.Base(f[0]) != "ssh" {
			return ""
		}
	}
	return base + " -o BatchMode=yes"
}

// fetchError keeps the lines of git's stderr that say what went wrong.
func fetchError(stderr string) string {
	var lines []string
	for _, l := range strings.Split(stderr, "\n") {
		l = strings.TrimSpace(l)
		l = strings.TrimPrefix(l, "fatal: ")
		l = strings.TrimPrefix(l, "error: ")
		if l == "" || strings.HasPrefix(l, "Please make sure you have the correct access rights") ||
			strings.HasPrefix(l, "and the repository exists") {
			continue
		}
		lines = append(lines, l)
	}
	if len(lines) > 3 {
		lines = lines[len(lines)-3:]
	}
	return strings.Join(lines, "; ")
}

// LastFetch is when any fetch last ran here, by ggitree or anyone else, from
// FETCH_HEAD's modification time. It is zero when the repository was never fetched.
func (r *Repo) LastFetch() time.Time {
	var t time.Time
	for _, dir := range []string{r.GitDir, r.CommonDir} {
		if fi, err := os.Stat(filepath.Join(dir, "FETCH_HEAD")); err == nil && fi.ModTime().After(t) {
			t = fi.ModTime()
		}
	}
	return t
}
