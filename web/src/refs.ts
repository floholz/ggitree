// The branches panel above the history: local branches, remote branches by
// remote, and tags. Clicking an entry selects its commit.

import { api, RefEntry, RefList } from "./api";
import { el, ICON_BRANCH, ICON_REMOTE, ICON_TAG, shortAge } from "./util";

// A repository can have thousands of remote branches; drawing them all would
// make every live update slow. The filter reaches the rest.
const MAX_PER_COLUMN = 300;

export type RefKindName = "branch" | "remote branch" | "tag";

export interface RefsCallbacks {
  goTo(sha: string, label: string, kind: RefKindName): void;
  onToggle(open: boolean): void;
}

export class RefsView {
  private head = el("div", "refs-head");
  private summary = el("button", "refs-summary");
  private filter = el("input", "refs-filter");
  private cols = el("div", "refs-cols");
  private list: RefList = { local: [], remotes: [], tags: [] };
  private selectedSha = "";
  private isOpen = true;
  private version = 0;

  constructor(private root: HTMLElement, private cb: RefsCallbacks) {
    this.summary.type = "button";
    this.summary.title = "Show or hide branches and tags (B)";
    this.summary.onclick = () => this.toggle();
    this.filter.type = "search";
    this.filter.placeholder = "Filter branches and tags";
    this.filter.spellcheck = false;
    this.filter.autocomplete = "off";
    this.filter.setAttribute("aria-label", "Filter branches and tags");
    this.filter.addEventListener("input", () => this.render());
    this.filter.addEventListener("keydown", (e) => {
      if (e.key === "Escape") {
        e.preventDefault();
        e.stopPropagation();
        if (this.filter.value) {
          this.filter.value = "";
          this.render();
        } else {
          this.filter.blur();
        }
      } else if (e.key === "Enter") {
        // Jump to the first match.
        e.preventDefault();
        this.cols.querySelector<HTMLElement>(".ref-item")?.click();
      }
    });
    this.head.append(this.summary, this.filter);
    root.append(this.head, this.cols);
    this.cols.addEventListener("click", (e) => {
      const item = (e.target as HTMLElement).closest<HTMLElement>(".ref-item");
      if (item?.dataset.sha) this.cb.goTo(item.dataset.sha, item.dataset.label!, item.dataset.kind as RefKindName);
    });
  }

  get open() { return this.isOpen; }

  setOpen(open: boolean) {
    this.isOpen = open;
    this.root.classList.toggle("collapsed", !open);
    this.renderSummary();
  }

  toggle() {
    this.setOpen(!this.isOpen);
    this.cb.onToggle(this.isOpen);
  }

  focusFilter() {
    if (!this.isOpen) this.toggle();
    this.filter.focus();
    this.filter.select();
  }

  async load() {
    const v = ++this.version;
    const res = await api.refs();
    if (v !== this.version) return; // a newer load is under way
    this.list = res.refs;
    this.render();
  }

  /** Marks the entries pointing at the selected commit. */
  setSelected(sha: string) {
    this.selectedSha = sha;
    for (const item of this.cols.querySelectorAll<HTMLElement>(".ref-item")) {
      item.classList.toggle("at-sel", item.dataset.sha === sha);
    }
  }

  private remoteCount() {
    return this.list.remotes.reduce((n, r) => n + r.branches.length, 0);
  }

  private renderSummary() {
    const remote = this.remoteCount();
    this.summary.replaceChildren(
      el("span", "chev", this.isOpen ? "▾" : "▸"),
      el("span", "refs-title", "Branches"),
      count(this.list.local.length, "local"),
      count(remote, "remote"),
      count(this.list.tags.length, this.list.tags.length === 1 ? "tag" : "tags"),
    );
    this.summary.setAttribute("aria-expanded", String(this.isOpen));
  }

  private render() {
    this.renderSummary();
    const q = this.filter.value.trim().toLowerCase();
    const match = (name: string) => !q || name.toLowerCase().includes(q);

    const local = this.list.local.filter((b) => match(b.n));
    const localCol = column("Local", local.length, this.list.local.length);
    const localList = el("ul", "ref-list");
    for (const b of local.slice(0, MAX_PER_COLUMN)) localList.append(this.localItem(b));
    more(localList, local.length);
    if (!this.list.local.length) localList.append(el("li", "ref-empty", "No local branches"));
    localCol.append(localList);

    let remoteShown = 0;
    let remoteMatched = 0;
    const remoteList = el("ul", "ref-list");
    for (const r of this.list.remotes) {
      const branches = r.branches.filter((b) => match(`${r.name}/${b.n}`));
      remoteMatched += branches.length;
      if (!branches.length) continue;
      if (this.list.remotes.length > 1 || q) remoteList.append(el("li", "ref-group", r.name));
      for (const b of branches) {
        if (remoteShown >= MAX_PER_COLUMN) break;
        remoteList.append(this.remoteItem(r.name, b));
        remoteShown++;
      }
    }
    more(remoteList, remoteMatched);
    if (!this.list.remotes.length) remoteList.append(el("li", "ref-empty", "No remotes"));
    const remoteCol = column(
      this.list.remotes.length === 1 ? `Remote · ${this.list.remotes[0].name}` : "Remote",
      remoteMatched, this.remoteCount(),
    );
    remoteCol.append(remoteList);

    const tags = this.list.tags.filter((t) => match(t.n));
    const tagCol = column("Tags", tags.length, this.list.tags.length);
    const tagList = el("ul", "ref-list");
    for (const t of tags.slice(0, MAX_PER_COLUMN)) tagList.append(this.tagItem(t));
    more(tagList, tags.length);
    if (!this.list.tags.length) tagList.append(el("li", "ref-empty", "No tags"));
    tagCol.append(tagList);

    this.cols.replaceChildren(localCol, remoteCol, tagCol);
    this.setSelected(this.selectedSha);
  }

  private localItem(b: RefEntry): HTMLElement {
    const li = item(b, b.n, "branch", ICON_BRANCH);
    if (b.c) li.classList.add("current");
    const extra = el("span", "ref-extra");
    if (b.w) extra.append(el("span", "wt", `⌂${b.w}`));
    if (b.ah) extra.append(el("span", "ahead", `↑${b.ah}`));
    if (b.bh) extra.append(el("span", "behind", `↓${b.bh}`));
    if (b.g) extra.append(el("span", "gone", "gone"));
    if (extra.childElementCount) li.append(extra);
    let title = `Branch ${b.n}`;
    if (b.c) title += " (checked out here)";
    if (b.w) title += ` (checked out in worktree ${b.w})`;
    if (b.u) {
      title += `\nTracks ${b.u}`;
      if (b.g) title += ", which no longer exists";
      else if (b.ah || b.bh) title += `: ${b.ah ?? 0} ahead, ${b.bh ?? 0} behind`;
      else title += ", up to date";
    }
    li.title = title + `\nLast commit ${shortAge(b.t)}`;
    return li;
  }

  private remoteItem(remote: string, b: RefEntry): HTMLElement {
    const li = item(b, `${remote}/${b.n}`, "remote branch", ICON_REMOTE, b.n);
    li.title = `Remote branch ${remote}/${b.n}\nLast commit ${shortAge(b.t)}`;
    return li;
  }

  private tagItem(t: RefEntry): HTMLElement {
    const li = item(t, t.n, "tag", ICON_TAG);
    li.append(el("span", "ref-extra age", shortAge(t.t)));
    li.title = `Tag ${t.n}\n${shortAge(t.t)}`;
    return li;
  }
}

function item(e: RefEntry, label: string, kind: RefKindName, icon: string, text = label): HTMLElement {
  const li = el("li", `ref-item k-${kind.replace(" ", "-")}`);
  li.dataset.sha = e.h;
  li.dataset.label = label;
  li.dataset.kind = kind;
  li.innerHTML = icon;
  li.append(el("span", "ref-name", text));
  return li;
}

function column(title: string, shown: number, total: number): HTMLElement {
  const col = el("div", shown === 0 && total > 0 ? "refs-col no-match" : "refs-col");
  const h = el("div", "refs-col-head");
  h.append(el("span", "", title), el("span", "n", shown === total ? String(total) : `${shown}/${total}`));
  col.append(h);
  return col;
}

function more(list: HTMLElement, matched: number) {
  if (matched > MAX_PER_COLUMN) {
    list.append(el("li", "ref-empty", `${matched - MAX_PER_COLUMN} more; filter to find them`));
  }
}

function count(n: number, label: string): HTMLElement {
  return el("span", "refs-count", `${n} ${label}`);
}
