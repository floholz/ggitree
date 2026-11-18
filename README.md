# ggitree

A JetBrains-style commit graph for your repository, served as a local web page so it can sit in
[T3 Code](https://github.com/pingdotgg/t3code)'s right panel (or any browser tab).

- Colored branch lanes with merges, branch and tag labels, and the checked-out commit ringed
- Commit details with changed files, and syntax-highlighted diffs
- Uncommitted changes as their own row at the top
- Live: updates within a moment when you or an agent commits, checks out or edits files
- Marks branches that are checked out in other worktrees
- Local branches by default; **All** adds every remote branch and tag
- Read-only: it never changes your repository

One Go binary with the page embedded. No Node at runtime or build time.

## Install

Requires Go 1.27+ and git.

```sh
make install            # builds and copies ggitree to ~/.local/bin
make install-service    # optional: also start it at login (systemd user service)
```

## Use

```sh
ggitree open [path]   # show the repository at path (default: current directory)
ggitree status        # is it running, and what is it showing?
ggitree stop
```

`open` starts the server in the background if needed and points it at the repository; the page is at
<http://127.0.0.1:4747>. Set `GGITREE_ADDR` or pass `--addr` to use another address.

### Keys

| Key | Action |
| --- | --- |
| `↑` `↓` / `j` `k` | Previous / next commit |
| `PgUp` `PgDn` `Home` `End` | Move by page / to the ends |
| `Enter` / `]` | Open the first (next) file's diff |
| `[` | Previous file |
| `Esc` | Back from the diff to the file list |
| `/` or `Ctrl+F` | Search; `Enter` / `Shift+Enter` for next / previous match |
| `H` | Jump to the checked-out commit |

Search matches commit messages, authors, commit ids and ref names.

## In T3 Code's right panel

Add an action in T3 Code: Settings → Projects → Actions with **All projects** selected applies it to every
project; **Add Action** in a thread's top bar adds it to that project only. Projects that already have their
own action list need it added there too.

| Field | Value |
| --- | --- |
| Name | `Git tree` |
| Keybinding | e.g. `Ctrl+Alt+G` |
| Command | `ggitree open; exit` |
| Preview URL | `http://127.0.0.1:4747` |
| Stop this action when the thread settles | on |
| Open preview automatically when this action runs | on |

Pressing the keybinding opens the graph as a browser tab in the right panel. The action runs in the
thread's own checkout, so the graph shows that worktree.

- `exit` closes the terminal the action ran in. "Stop this action when the thread settles" makes T3 Code
  start a fresh terminal for it rather than reusing one of yours, so `exit` never closes your shell. The
  server detaches from the terminal and keeps running.
- T3 Code opens the preview right after starting the command. If the server is not running yet, the very
  first open can beat it; reload the tab, or run `make install-service` so it is always up.

## Develop

```sh
make build     # bundle the page (esbuild via its Go API) and build the binary
make test
make watch     # rebuild the page on change; run `go run . serve .` alongside
```

The page lives in `web/src` (plain TypeScript, no framework). `web/tsconfig.json` is only for type checking,
e.g. `npx -p typescript tsc -p web`.

Layout: `internal/gitx` reads git, `internal/graph` assigns lanes, `internal/server` serves the API and
watches the repository, `web/` is the page.
