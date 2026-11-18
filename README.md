# ggitree

A JetBrains-style commit graph for your repository, served as a local web page so it can sit in
[T3 Code](https://github.com/pingdotgg/t3code)'s right panel (or any browser tab).

<img src="docs/screenshot.png" alt="ggitree showing a demo repository at side-panel width" width="420" align="right">

- Colored branch lanes with merges, branch and tag labels, and the checked-out commit ringed
- Commit details with changed files, and syntax-highlighted diffs
- Uncommitted changes as their own row at the top
- Live: updates within a moment when you or an agent commits, checks out or edits files
- Marks branches that are checked out in other worktrees
- A branches panel above the graph lists local branches (with ahead/behind counts), remote branches
  and tags; click one to jump to its commit
- Local branches by default; **All** adds every remote branch and tag
- Read-only: it never changes your repository

One Go binary with the page embedded. No Node at runtime or build time.

## Install

Requires Go 1.27+ and git.

```sh
make install            # builds, copies ggitree to ~/.local/bin and adds the T3 Code action
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
| `B` | Show or hide the branches panel |
| `Shift+B` | Filter branches and tags (`Enter` jumps to the first match) |

Search matches commit messages, authors, commit ids and ref names.

Clicking a remote branch or tag that the **Local** view doesn't include switches to **All** first. Drag
the line under the branches panel to resize it; its size and whether it is open are remembered.

The switch in the bottom-right corner fixes the layout: **Auto** puts the graph and the details side by
side when the page is wide and stacks them when it is narrow, or pick always stacked or always side by side.

## In T3 Code's right panel

`make install` sets this up: it runs `ggitree t3-install`, which adds a **GGitree** action to every project
and binds it to `Ctrl+Alt+G` (`mod+alt+g`). T3 Code picks the change up without a restart.

- **Where it goes.** T3 Code's settings page only edits one project's actions, so `t3-install` edits
  `~/.t3/userdata/settings.json` directly. It puts the action in the top-level `defaultProjectScripts` list
  that all projects share, and also into each project's own list, because a project with its own list
  ignores the shared one. The shortcut goes in `keybindings.json`.
- **Rerunning.** Running it again only fixes what is missing or out of date, so after a T3 Code update
  changes the format, fix `internal/t3setup` and run `make install` again. A project added later with its
  own action list needs a rerun too. A shortcut you changed in T3 Code is kept.
- **Backup.** The first run copies both files to `~/.t3/userdata/ggitree-backup/`. Later runs keep that copy.
- **Uninstall.** `make uninstall` (or `ggitree t3-uninstall`) puts the backup back if T3 Code hasn't changed
  the file since the last install. If it has, it removes only ggitree's entries, so your other changes stay,
  and moves the old backup to `ggitree-backup-<date>`.

`--t3-dir` points both commands at another folder (default `$T3CODE_HOME/userdata`, else
`~/.t3/userdata`). The action's fields, if you'd rather add it by hand:

| Field | Value |
| --- | --- |
| Name | `GGitree` |
| Keybinding | `Ctrl+Alt+G` |
| Command | `ggitree open; exit` |
| Preview URL | `http://127.0.0.1:4747` |
| Stop this action when the thread settles | on |
| Open preview automatically when this action runs | on |

Pressing the keybinding opens the graph as a browser tab in the right panel, not the floating preview. The
"Auto-show floating preview" browser setting only applies to previews an agent opens. The action runs in the
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
make screenshot  # rebuild docs/screenshot.png (needs Node 22+ and Chrome)
```

`make screenshot` builds a made-up repository with `tools/demo/demo-repo.sh` (merged and open
branches, tags, two remotes, a second worktree, uncommitted changes), shows it with a private server
and captures the page with headless Chrome. Set `CHROME` if Chrome isn't on your PATH.

The page lives in `web/src` (plain TypeScript, no framework). `web/tsconfig.json` is only for type checking,
e.g. `npx -p typescript tsc -p web`.

Layout: `internal/gitx` reads git, `internal/graph` assigns lanes, `internal/server` serves the API and
watches the repository, `web/` is the page.
