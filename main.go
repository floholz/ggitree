// Command ggitree shows a repository's commit graph as a local web page, made to
// sit in T3 Code's right panel.
//
//	ggitree open [path]   start the server if needed and show the repository at path
//	ggitree serve [path]  run the server in the foreground
//	ggitree stop          stop the running server
//	ggitree status        print whether a server is running
//	ggitree t3-install    add the action to T3 Code for every project
//	ggitree t3-uninstall  take it out again
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/floholz/ggitree/internal/server"
	"github.com/floholz/ggitree/internal/t3setup"
	"github.com/floholz/ggitree/web"
)

const defaultAddr = "127.0.0.1:4747"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	addr := fs.String("addr", envOr("GGITREE_ADDR", defaultAddr), "address to listen on / connect to")
	t3Dir := fs.String("t3-dir", t3setup.DefaultDir(), "T3 Code's userdata folder (t3-install, t3-uninstall)")
	_ = fs.Parse(args)
	path := fs.Arg(0)
	if path == "" {
		path, _ = os.Getwd()
	}

	var err error
	switch cmd {
	case "open":
		err = open(*addr, path)
	case "serve":
		err = serve(*addr, fs.Arg(0))
	case "stop":
		err = stop(*addr)
	case "status":
		err = status(*addr)
	case "t3-install":
		err = t3setup.Install(t3setup.Config{Dir: *t3Dir, PreviewURL: "http://" + *addr, Log: os.Stdout})
	case "t3-uninstall":
		err = t3setup.Uninstall(t3setup.Config{Dir: *t3Dir, Log: os.Stdout})
	case "version", "--version", "-v":
		fmt.Println("ggitree", server.Version)
	case "help", "--help", "-h":
		usage()
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "ggitree:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `usage: ggitree <command> [--addr host:port] [path]

commands:
  open [path]   show the repository at path (default: current directory),
                starting the server in the background if it is not running
  serve [path]  run the server in the foreground
  stop          stop the running server
  status        report whether the server is running
  t3-install    add a "GGitree" action and the Ctrl+Alt+G shortcut to T3 Code
                for every project; safe to run again (--t3-dir to change the folder)
  t3-uninstall  remove them again, restoring T3 Code's files from the backup
                the first t3-install made
  version       print the version

The address defaults to `+defaultAddr+` or $GGITREE_ADDR.
`)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func serve(addr, path string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	s := server.New(addr, web.FS())
	if path != "" {
		if err := s.Open(path); err != nil {
			return err
		}
	}
	return s.Run(ctx)
}

var client = &http.Client{Timeout: 5 * time.Second}

func healthy(addr string) bool {
	c := &http.Client{Timeout: 400 * time.Millisecond}
	resp, err := c.Get("http://" + addr + "/api/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var h struct{ App string }
	return json.NewDecoder(resp.Body).Decode(&h) == nil && h.App == "ggitree"
}

func post(addr, path string, body any) ([]byte, error) {
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, "http://"+addr+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(server.HeaderName, "1")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		var e struct{ Error string }
		if json.Unmarshal(out, &e) == nil && e.Error != "" {
			return nil, errors.New(e.Error)
		}
		return nil, fmt.Errorf("%s: %s", resp.Status, bytes.TrimSpace(out))
	}
	return out, nil
}

func open(addr, path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if !healthy(addr) {
		if err := startBackground(addr); err != nil {
			return err
		}
	}
	out, err := post(addr, "/api/open", map[string]string{"path": abs})
	if err != nil {
		return err
	}
	var res struct{ Root string }
	_ = json.Unmarshal(out, &res)
	fmt.Printf("ggitree: showing %s at http://%s\n", res.Root, addr)
	return nil
}

// startBackground launches "ggitree serve" detached from this terminal, so it keeps
// running after the shell (or T3 Code terminal) that started it closes.
func startBackground(addr string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	logPath := logFile()
	lf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer lf.Close()
	cmd := exec.Command(exe, "serve", "--addr", addr)
	cmd.Stdout, cmd.Stderr = lf, lf
	cmd.SysProcAttr = detachAttr()
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Process.Release()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if healthy(addr) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("server did not start; see %s", logPath)
}

func logFile() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	dir = filepath.Join(dir, "ggitree")
	_ = os.MkdirAll(dir, 0o755)
	return filepath.Join(dir, "server.log")
}

func stop(addr string) error {
	if !healthy(addr) {
		fmt.Println("ggitree: not running")
		return nil
	}
	if _, err := post(addr, "/api/shutdown", map[string]string{}); err != nil {
		return err
	}
	// Wait until it is gone, so a following "open" starts a fresh server.
	deadline := time.Now().Add(5 * time.Second)
	for healthy(addr) {
		if time.Now().After(deadline) {
			return errors.New("server did not stop")
		}
		time.Sleep(50 * time.Millisecond)
	}
	fmt.Println("ggitree: stopped")
	return nil
}

func status(addr string) error {
	if !healthy(addr) {
		fmt.Printf("ggitree: not running on %s\n", addr)
		return nil
	}
	resp, err := client.Get("http://" + addr + "/api/state")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var st struct {
		Repo *struct{ Root string }
	}
	_ = json.NewDecoder(resp.Body).Decode(&st)
	root := "(no repository)"
	if st.Repo != nil {
		root = st.Repo.Root
	}
	fmt.Printf("ggitree: running on http://%s, showing %s\n", addr, root)
	return nil
}
