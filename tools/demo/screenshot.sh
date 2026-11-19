#!/usr/bin/env bash
# Rebuilds docs/screenshot.png: makes the demo repository, shows it with a
# private ggitree server and captures the page at side-panel size, with Auto
# fetch and Follow switched on.
#
#   tools/demo/screenshot.sh [ggitree binary]
#
# Needs Node 22+ and Chrome (set CHROME if it isn't on PATH).
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
top=$(cd "$here/../.." && pwd)
bin=${1:-$top/ggitree}
addr=127.0.0.1:4798
work=$(mktemp -d)
trap 'kill "$pid" 2>/dev/null || true; rm -rf "$work"' EXIT

repo=$("$here/demo-repo.sh" "$work/demo")
"$bin" serve --addr "$addr" "$repo" >/dev/null 2>&1 &
pid=$!
for _ in $(seq 50); do
	curl -sf "http://$addr/api/health" >/dev/null && break
	sleep 0.1
done

mkdir -p "$top/docs"
node "$here/screenshot.mjs" "http://$addr/" "$top/docs/screenshot.png" 600 900 dark \
	"ggitree.autoFetch=1,ggitree.follow=1"
echo "wrote docs/screenshot.png"
