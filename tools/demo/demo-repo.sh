#!/usr/bin/env bash
# Builds "orbit", a made-up project whose history shows what ggitree draws:
# merged feature branches, open branches side by side, tags, two remotes, a
# branch ahead of and one behind its remote, a gone upstream, a second worktree
# and uncommitted changes.
#
#   tools/demo/demo-repo.sh <dir>     creates <dir>/orbit (and its remotes and worktree)
#
# Commit dates are relative to now, so the screenshot always looks recent.
set -euo pipefail

root=${1:?usage: demo-repo.sh <dir>}
rm -rf "$root"
mkdir -p "$root"
root=$(cd "$root" && pwd)

# Keep the user's git config (signing, hooks, templates) out of it.
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
now=$(date +%s)

git init -q --bare -b main "$root/origin.git"
git init -q --bare -b main "$root/fork.git"
git init -q -b main "$root/orbit"
cd "$root/orbit"
git config user.name "Demo"
git config user.email "demo@example.com"
git remote add origin "$root/origin.git"
git remote add fork "$root/fork.git"

# who <name> <hours ago>: sets author and committer for the next git command.
who() {
	local email
	email="$(echo "$1" | tr 'A-Z ' 'a-z.')@example.com"
	local ts=$((now - $2 * 3600))
	export GIT_AUTHOR_NAME="$1" GIT_AUTHOR_EMAIL="$email" GIT_AUTHOR_DATE="$ts +0100"
	export GIT_COMMITTER_NAME="$1" GIT_COMMITTER_EMAIL="$email" GIT_COMMITTER_DATE="$ts +0100"
}

# commit <name> <hours ago> <file> <message>: adds a line to file and commits it.
commit() {
	who "$1" "$2"
	mkdir -p "$(dirname "$3")"
	echo "// $4" >>"$3"
	git add "$3"
	git commit -q -m "$4"
}

# merge <name> <hours ago> <branch> [message]
merge() {
	who "$1" "$2"
	git merge -q --no-ff -m "${4:-Merge branch '$3'}" "$3"
}

tag() { who "$1" "$2"; git tag -a "$3" -m "Release $3"; }

M="Mara Lind" T="Theo Brandt" I="Ines Okafor" S="Sam Reyes"

# The first release.
commit "$M" 500 README.md "Initial commit"
commit "$M" 480 src/parse.go "Add a Markdown parser"
commit "$T" 460 src/render.go "Render pages with html/template"
commit "$M" 440 cmd/orbit/main.go "CLI: orbit build and orbit serve"
tag "$M" 440 v0.1.0

# Themes and live reload grow side by side, then both merge.
git switch -q -c feature/themes
commit "$I" 420 themes/load.go "themes: load CSS from themes/"
commit "$I" 400 themes/dusk.css "themes: add the Dusk theme"
git switch -q main
commit "$T" 410 src/frontmatter.go "Cache parsed front matter"
git switch -q -c feature/live-reload
commit "$S" 390 src/serve.go "serve: watch the content folder"
commit "$S" 370 src/serve.go "serve: push reloads over SSE"
git switch -q feature/themes
commit "$I" 380 docs/themes.md "themes: document the theme variables"
git switch -q main
merge "$M" 360 feature/themes
commit "$T" 350 src/render.go "Fix relative links in nested pages"
git switch -q feature/live-reload
commit "$S" 345 src/serve.go "serve: debounce rapid saves"
git switch -q main
merge "$M" 330 feature/live-reload
tag "$M" 330 v0.2.0

# A quick fix branch.
git switch -q -c fix/unicode-slugs
commit "$T" 310 src/slug.go "slugs: keep non-ASCII letters"
commit "$T" 300 src/slug_test.go "slugs: test umlauts and CJK titles"
git switch -q main
commit "$M" 305 CHANGELOG.md "Release notes for 0.2"
merge "$M" 290 fix/unicode-slugs
tag "$M" 290 v0.2.1
git branch -q -d feature/themes feature/live-reload fix/unicode-slugs

# Open work: search and plugins branch off while main moves on.
git switch -q -c feature/search
commit "$I" 270 src/search/index.go "search: build an index at build time"
git switch -q main
git switch -q -c feature/plugins
commit "$S" 250 src/plugins/hooks.go "plugins: a hook interface for build steps"
git switch -q main
commit "$T" 230 src/build.go "Faster incremental builds"
git switch -q feature/search
commit "$I" 240 web/search.js "search: client-side search box"
git switch -q feature/plugins
commit "$S" 210 src/plugins/load.go "plugins: load from plugins/"
git switch -q feature/search
commit "$I" 200 src/search/rank.go "search: rank title matches first"
git switch -q main
git switch -q -c experiment/wasm
commit "$S" 180 wasm/main.go "experiment: compile the renderer to wasm"
git switch -q main
commit "$M" 150 src/feed.go "Sitemap and RSS feed"
tag "$M" 150 v0.3.0

# Docs live in their own worktree.
git switch -q -c docs/getting-started
commit "$M" 120 docs/getting-started.md "docs: getting started guide"
git switch -q main
commit "$T" 90 themes/default.css "Dark mode for the default theme"
git switch -q docs/getting-started
commit "$M" 40 docs/deploy.md "docs: deploy to GitHub Pages"

# A contributor's branch that only exists on their fork.
git switch -q -c theo/windows-paths main
commit "$T" 100 src/paths.go "paths: handle Windows drive letters"
git push -q fork theo/windows-paths
git switch -q main
git branch -q -D theo/windows-paths

# Publish everything, then make the remotes differ from the local branches.
git push -q --tags -u origin main feature/search feature/plugins experiment/wasm docs/getting-started

# main is one behind: a fix landed on origin after the last pull.
commit "$M" 30 src/feed.go "Fix dates in the RSS feed"
git push -q origin main
git reset -q --hard HEAD~1

# The experiment's remote branch was deleted.
git push -q origin --delete experiment/wasm

# plugins is two ahead: local commits not pushed yet.
git switch -q feature/plugins
commit "$S" 60 src/plugins/shortcode.go "plugins: a shortcode plugin"
commit "$S" 20 src/plugins/hooks.go "plugins: pass the page context to hooks"

git worktree add -q "$root/orbit-docs" docs/getting-started

# Work in progress in the main checkout.
echo "// TODO: plugin ordering" >>src/plugins/load.go
echo "# Plugins" >docs/plugins.md

unset GIT_AUTHOR_NAME GIT_AUTHOR_EMAIL GIT_AUTHOR_DATE GIT_COMMITTER_NAME GIT_COMMITTER_EMAIL GIT_COMMITTER_DATE
echo "$root/orbit"
