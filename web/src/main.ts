import "./style.css";
import { api, Row, scope, Scope, setScope, State, WORKTREE } from "./api";
import { DetailsView } from "./details";
import { FetchControl } from "./fetch";
import { HistoryView } from "./history";
import { RefKindName, RefsView } from "./refs";
import { toast } from "./util";

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;

const app = $("app");
const live = $("live");
const repoName = $("repo-name");
const branch = $("branch");
const empty = $("empty");
const search = $<HTMLInputElement>("search");
const searchCount = $("search-count");
const historyEl = $("history");

let state: State | null = null;
let selectedSha = "";

// Follow mode keeps the newest change selected: the uncommitted changes when
// there are any, otherwise HEAD. Selecting anything by hand turns it off.
const FOLLOW_KEY = "ggitree.follow";
const followBtn = $("follow");
let follow = false;
try { follow = localStorage.getItem(FOLLOW_KEY) === "1"; } catch { /* private mode */ }

function setFollow(on: boolean) {
  if (on === follow) return;
  follow = on;
  followBtn.setAttribute("aria-pressed", String(on));
  try { localStorage.setItem(FOLLOW_KEY, on ? "1" : "0"); } catch { /* private mode */ }
  if (on && state) void selectNewest(state);
}

/** Selects the uncommitted changes, or HEAD when the tree is clean. */
async function selectNewest(st: State) {
  if (!st.total) return;
  let i = 0;
  if (st.dirty === 0) i = Math.max((await api.locate([st.head?.sha ?? ""])).indexes[0], 0);
  history.select(i, { silent: true, scroll: "nearest" });
  const row = await history.load(i);
  if (!row) return;
  selectedSha = row.h;
  refs.setSelected(row.h);
  void details.show(row, { force: row.h === WORKTREE });
}

const details = new DetailsView($("details"), {
  goTo: (sha) => void goTo(sha),
  laneColor: (c) => history.laneColor(c),
});

const history = new HistoryView(historyEl, {
  onSelect(_i, row) {
    selectedSha = row.h;
    refs.setSelected(row.h);
    void details.show(row);
  },
  onActivate() {
    details.stepFile(1);
  },
});

const REFS_OPEN_KEY = "ggitree.refsOpen";
const refs = new RefsView($("refs"), {
  goTo: (sha, label, kind) => void goToRef(sha, label, kind),
  onToggle(open) {
    try { localStorage.setItem(REFS_OPEN_KEY, open ? "1" : "0"); } catch { /* private mode */ }
  },
});
try { refs.setOpen(localStorage.getItem(REFS_OPEN_KEY) !== "0"); } catch { /* private mode */ }
const fetcher = new FetchControl();
refs.setRemoteControls(fetcher.root, (on) => fetcher.setAvailable(on));

// ---------- Loading and live updates ----------

async function goTo(sha: string) {
  setFollow(false);
  const { indexes } = await api.locate([sha]);
  if (indexes[0] >= 0) history.select(indexes[0], { scroll: "center" });
}

/** Selects a branch's or tag's commit, switching to All when Local hides it. */
async function goToRef(sha: string, label: string, kind: RefKindName) {
  setFollow(false);
  let { indexes } = await api.locate([sha]);
  if (indexes[0] < 0 && scope === "local") {
    chooseScope("all");
    await whenIdle();
    ({ indexes } = await api.locate([sha]));
    if (indexes[0] >= 0) toast(`Showing all branches to reach ${label}`);
  }
  if (indexes[0] < 0) {
    toast(`The ${kind} ${label} is outside the loaded history`);
    return;
  }
  history.select(indexes[0], { scroll: "center" });
  historyEl.focus();
}

async function whenIdle() {
  while (reloading) await reloading;
}

function showState(st: State) {
  state = st;
  fetcher.setState(st);
  repoName.textContent = st.repo?.name ?? "ggitree";
  document.title = st.repo ? `${st.repo.name} · ggitree` : "ggitree";
  repoName.title = st.repo?.root ?? "";
  if (st.head) {
    branch.hidden = false;
    branch.textContent = st.head.branch || `detached at ${st.head.sha.slice(0, 8)}`;
  } else {
    branch.hidden = true;
  }
  if (!st.repo) {
    empty.hidden = false;
    empty.innerHTML = `<div>No repository open.</div><div>Run <code>ggitree open</code> inside a repository.</div>`;
  } else if (st.error) {
    empty.hidden = false;
    empty.textContent = st.error;
  } else if (st.total === 0) {
    empty.hidden = false;
    empty.textContent = "This repository has no commits yet.";
  } else {
    empty.hidden = true;
  }
}

/** Loads a repository from scratch and selects its checked-out commit. */
async function loadFresh() {
  const st = await api.state();
  showState(st);
  details.clear();
  selectedSha = "";
  history.reset(st.total, st.head?.sha ?? "");
  history.scrollRowTo(0, 0);
  void refs.load();
  await runSearch();
  if (!st.total) return;
  const { indexes } = await api.locate([st.head?.sha ?? ""]);
  const target = st.dirty > 0 ? 0 : Math.max(indexes[0], 0);
  history.select(target, { scroll: "center" });
}

/** Reloads after the history changed, keeping the scroll position and selection. */
async function reloadKeep() {
  if (!state) return loadFresh();
  const top = history.topIndex();
  const anchor = history.row(top);
  const anchorOffset = history.rowOffset(top);
  const prevSelected = selectedSha;
  const wasAtTop = top === 0 && anchorOffset > -4;

  const st = await api.state();
  if (st.repo?.root !== state.repo?.root) return loadFresh();
  showState(st);
  void refs.load();
  const { indexes } = await api.locate([anchor?.h ?? "", prevSelected]);
  history.reset(st.total, st.head?.sha ?? "");
  // Stay at the top when we were there, so new commits come into view.
  if (wasAtTop) history.scrollRowTo(0, 0);
  else if (indexes[0] >= 0) history.scrollRowTo(indexes[0], -anchorOffset);
  await runSearch();
  if (follow) return selectNewest(st);

  let sel = indexes[1];
  if (sel < 0 && prevSelected === WORKTREE) sel = -1; // changes were committed: follow HEAD
  if (sel < 0) {
    const head = await api.locate([st.head?.sha ?? ""]);
    sel = Math.max(head.indexes[0], 0);
  }
  if (!st.total) return details.clear();
  history.select(sel, { silent: true });
  const row = await history.load(sel);
  if (!row) return;
  selectedSha = row.h;
  // Uncommitted changes and a moved selection need fresh details.
  void details.show(row, { force: row.h === WORKTREE });
}

let reloading: Promise<void> | null = null;
let again = false;
function queueReload(fresh = false) {
  if (reloading) { again = true; return; }
  reloading = (fresh ? loadFresh() : reloadKeep())
    .catch((err) => console.error(err))
    .finally(() => {
      reloading = null;
      if (again) { again = false; queueReload(); }
    });
}

function connect() {
  const es = new EventSource("api/events");
  let wasDown = false;
  es.onopen = () => {
    live.className = "live on";
    live.title = "Live: updates as the repository changes";
    if (wasDown) queueReload();
    wasDown = false;
  };
  es.onerror = () => {
    live.className = "live off";
    live.title = "Disconnected from ggitree. Retrying…";
    wasDown = true;
  };
  es.addEventListener("changed", () => queueReload());
  es.addEventListener("repo", () => queueReload(true));
  // A fetch that changed nothing (or one run in a terminal) only moves the "fetched" age.
  es.addEventListener("fetched", () => void api.state().then((st) => fetcher.setState(st), () => {}));
  es.addEventListener("bye", () => {
    live.className = "live off";
    live.title = "ggitree stopped";
  });
}

// ---------- Search ----------

let matches: number[] = [];
let matchPos = -1;
let searchTimer = 0;

async function runSearch() {
  const q = search.value.trim();
  if (!q) {
    matches = [];
    matchPos = -1;
    searchCount.textContent = "";
    history.setMatches([]);
    return;
  }
  const res = await api.search(q);
  if (search.value.trim() !== q) return;
  matches = res.matches;
  history.setMatches(matches);
  matchPos = -1;
  updateCount();
}

function updateCount() {
  if (!search.value.trim()) { searchCount.textContent = ""; return; }
  searchCount.textContent = matches.length
    ? `${matchPos >= 0 ? matchPos + 1 : "–"}/${matches.length}`
    : "0";
}

function stepMatch(delta: number) {
  if (!matches.length) return;
  setFollow(false);
  if (matchPos < 0) {
    // Start from the selection, so Enter finds the next match below it.
    const sel = history.selectedIndex;
    matchPos = delta > 0 ? matches.findIndex((m) => m > sel) : findLastIndex(matches, (m) => m < sel);
    if (matchPos < 0) matchPos = delta > 0 ? 0 : matches.length - 1;
  } else {
    matchPos = (matchPos + delta + matches.length) % matches.length;
  }
  history.select(matches[matchPos], { scroll: "center" });
  updateCount();
}

function findLastIndex<T>(a: T[], f: (v: T) => boolean): number {
  for (let i = a.length - 1; i >= 0; i--) if (f(a[i])) return i;
  return -1;
}

search.addEventListener("input", () => {
  clearTimeout(searchTimer);
  searchTimer = window.setTimeout(() => void runSearch(), 150);
});
search.addEventListener("keydown", (e) => {
  if (e.key === "Enter") {
    e.preventDefault();
    clearTimeout(searchTimer);
    void runSearch().then(() => stepMatch(e.shiftKey ? -1 : 1));
  } else if (e.key === "Escape") {
    e.preventDefault();
    if (search.value) {
      search.value = "";
      void runSearch();
    } else {
      historyEl.focus();
    }
  }
});
$("search-next").onclick = () => stepMatch(1);
$("search-prev").onclick = () => stepMatch(-1);
$("jump-head").onclick = () => void jumpHead();
followBtn.onclick = () => setFollow(!follow);
followBtn.setAttribute("aria-pressed", String(follow));
// A click on a row picks that commit by hand.
historyEl.addEventListener("mousedown", (e) => {
  if ((e.target as HTMLElement).closest(".row")) setFollow(false);
});

async function jumpHead() {
  setFollow(false);
  if (state?.head?.sha) await goTo(state.head.sha);
  historyEl.focus();
}

// ---------- Branch scope ----------

const SCOPE_KEY = "ggitree.scope";
const scopeButtons: Record<Scope, HTMLElement> = { local: $("scope-local"), all: $("scope-all") };

function applyScope(sc: Scope) {
  setScope(sc);
  for (const [k, b] of Object.entries(scopeButtons)) b.setAttribute("aria-pressed", String(k === sc));
}

function chooseScope(sc: Scope) {
  try { localStorage.setItem(SCOPE_KEY, sc); } catch { /* private mode */ }
  applyScope(sc);
  queueReload();
}

scopeButtons.local.onclick = () => chooseScope("local");
scopeButtons.all.onclick = () => chooseScope("all");
try { applyScope(localStorage.getItem(SCOPE_KEY) === "all" ? "all" : "local"); } catch { applyScope("local"); }

// ---------- Back ----------

// The mouse's back button needs nothing here: an open diff adds a history entry,
// and going back closes it (see DetailsView). Alt+Left gets the same meaning
// in hosts that don't navigate on it.
document.addEventListener("keydown", (e) => {
  const isBack = e.key === "BrowserBack" || (e.altKey && e.key === "ArrowLeft");
  if (isBack && details.diffOpen) {
    e.preventDefault();
    details.back();
  }
}, true);

// ---------- Keyboard ----------

document.addEventListener("keydown", (e) => {
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "f") {
    e.preventDefault();
    search.focus();
    search.select();
    return;
  }
  if (e.target instanceof HTMLInputElement || e.ctrlKey || e.metaKey || e.altKey) return;
  const byHand = (f: () => void) => () => { setFollow(false); f(); };
  const keys: Record<string, () => void> = {
    ArrowDown: byHand(() => history.move(1)),
    j: byHand(() => history.move(1)),
    ArrowUp: byHand(() => history.move(-1)),
    k: byHand(() => history.move(-1)),
    PageDown: byHand(() => history.move(history.pageSize())),
    PageUp: byHand(() => history.move(-history.pageSize())),
    Home: byHand(() => history.select(0)),
    End: byHand(() => history.select(history.count - 1)),
    f: () => setFollow(!follow),
    F: () => setFollow(!follow),
    Enter: () => details.stepFile(1),
    "]": () => details.stepFile(1),
    "[": () => details.stepFile(-1),
    Escape: () => { details.back(); },
    "/": () => { search.focus(); search.select(); },
    b: () => refs.toggle(),
    B: () => refs.focusFilter(),
    h: () => void jumpHead(),
    H: () => void jumpHead(),
  };
  const f = keys[e.key];
  if (f) {
    e.preventDefault();
    f();
  }
});

// ---------- Layout ----------

const SPLIT_KEY = "ggitree.split";
const splitter = $("splitter");
const body = $("body");

function readSplit(): Record<string, number> {
  try { return JSON.parse(localStorage.getItem(SPLIT_KEY) ?? "{}"); } catch { return {}; }
}

type LayoutMode = "auto" | "stacked" | "side";
const LAYOUT_KEY = "ggitree.layout";
const layoutButtons: Record<LayoutMode, HTMLElement> = {
  auto: $("layout-auto"), stacked: $("layout-stacked"), side: $("layout-side"),
};
let layoutMode: LayoutMode = "auto";
try {
  const saved = localStorage.getItem(LAYOUT_KEY);
  if (saved === "stacked" || saved === "side") layoutMode = saved;
} catch { /* private mode */ }

function chooseLayout(mode: LayoutMode) {
  layoutMode = mode;
  try { localStorage.setItem(LAYOUT_KEY, mode); } catch { /* private mode */ }
  applyLayout();
}
for (const [mode, b] of Object.entries(layoutButtons)) b.onclick = () => chooseLayout(mode as LayoutMode);

function applyLayout() {
  for (const [mode, b] of Object.entries(layoutButtons)) b.setAttribute("aria-pressed", String(mode === layoutMode));
  const wide = layoutMode === "side" || (layoutMode === "auto" && app.clientWidth >= 820);
  app.classList.toggle("wide", wide);
  const savedSplit = readSplit()[wide ? "wide" : "tall"];
  body.style.setProperty("--split", savedSplit ? `${savedSplit * 100}%` : wide ? "58%" : "55%");
}
new ResizeObserver(applyLayout).observe(app);

splitter.addEventListener("pointerdown", (e) => {
  e.preventDefault();
  splitter.setPointerCapture(e.pointerId);
  splitter.classList.add("drag");
  const wide = app.classList.contains("wide");
  const rect = body.getBoundingClientRect();
  let frac = 0;
  const onMove = (ev: PointerEvent) => {
    frac = wide ? (ev.clientX - rect.left) / rect.width : (ev.clientY - rect.top) / rect.height;
    frac = Math.min(0.85, Math.max(0.15, frac));
    body.style.setProperty("--split", `${frac * 100}%`);
  };
  const onUp = () => {
    splitter.classList.remove("drag");
    splitter.removeEventListener("pointermove", onMove);
    splitter.removeEventListener("pointerup", onUp);
    if (frac) {
      const s = readSplit();
      s[wide ? "wide" : "tall"] = frac;
      try { localStorage.setItem(SPLIT_KEY, JSON.stringify(s)); } catch { /* private mode */ }
    }
  };
  splitter.addEventListener("pointermove", onMove);
  splitter.addEventListener("pointerup", onUp);
});

// The branches panel's height, dragged with the line under it.
const REFS_H_KEY = "ggitree.refsHeight";
const refsEl = $("refs");
const refsSplitter = $("refs-splitter");
try {
  const h = Number(localStorage.getItem(REFS_H_KEY));
  if (h > 0) refsEl.style.setProperty("--refs-h", `${h}px`);
} catch { /* private mode */ }

refsSplitter.addEventListener("pointerdown", (e) => {
  if (!refs.open) return;
  e.preventDefault();
  refsSplitter.setPointerCapture(e.pointerId);
  refsSplitter.classList.add("drag");
  const top = refsEl.getBoundingClientRect().top;
  const maxH = refsEl.parentElement!.clientHeight - 80;
  let h = 0;
  const onMove = (ev: PointerEvent) => {
    h = Math.round(Math.min(maxH, Math.max(70, ev.clientY - top)));
    refsEl.style.setProperty("--refs-h", `${h}px`);
  };
  const onUp = () => {
    refsSplitter.classList.remove("drag");
    refsSplitter.removeEventListener("pointermove", onMove);
    refsSplitter.removeEventListener("pointerup", onUp);
    if (h) try { localStorage.setItem(REFS_H_KEY, String(h)); } catch { /* private mode */ }
  };
  refsSplitter.addEventListener("pointermove", onMove);
  refsSplitter.addEventListener("pointerup", onUp);
});

// ---------- Start ----------

applyLayout();
void loadFresh().then(() => historyEl.focus());
connect();

export type { Row };
