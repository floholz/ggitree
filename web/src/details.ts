// The details panel: commit metadata and changed files, and the diff of one file.

import { api, CommitDetail, FileChange, FileDiff, Ref, Row, WORKTREE } from "./api";
import { el, fullDate, renderRefs, toast } from "./util";

export interface DetailsCallbacks {
  /** Selects a commit in the history by id. */
  goTo(sha: string): void;
  laneColor(c: number): string;
}

export class DetailsView {
  private row: Row | null = null;
  private detail: CommitDetail | null = null;
  private refs: Ref[] | null = null;
  private fileIndex = -1; // file whose diff is open, or -1 for the file list
  private token = 0;
  private cache = new Map<string, { commit: CommitDetail; refs: Ref[] | null }>();
  private listScroll = 0;

  constructor(private root: HTMLElement, private cb: DetailsCallbacks) {
    this.placeholder("Select a commit");
  }

  get diffOpen() { return this.fileIndex >= 0; }

  private placeholder(msg: string) {
    this.root.replaceChildren(el("div", "placeholder-msg", msg));
  }

  /** Shows a commit. With keepFile, an open diff of the same path stays open. */
  async show(row: Row, opts: { force?: boolean } = {}) {
    const same = this.row?.h === row.h;
    if (same && !opts.force && this.detail) return;
    const openPath = same && this.fileIndex >= 0 ? this.detail?.files?.[this.fileIndex]?.path : undefined;
    this.row = row;
    const token = ++this.token;
    let data = row.h !== WORKTREE && !opts.force ? this.cache.get(row.h) : undefined;
    if (!data) {
      if (!same) this.placeholder("Loading…");
      try {
        data = await api.commit(row.h);
      } catch (err) {
        if (token === this.token) this.placeholder(String((err as Error).message));
        return;
      }
      if (row.h !== WORKTREE) {
        this.cache.set(row.h, data);
        if (this.cache.size > 200) this.cache.delete(this.cache.keys().next().value!);
      }
    }
    if (token !== this.token) return;
    this.detail = data.commit;
    this.refs = data.refs;
    const files = this.detail.files ?? [];
    const keep = openPath ? files.findIndex((f) => f.path === openPath) : -1;
    if (keep >= 0) {
      await this.openFile(keep);
    } else {
      this.fileIndex = -1;
      this.listScroll = 0;
      this.renderCommit();
    }
  }

  clear() {
    this.row = this.detail = null;
    this.fileIndex = -1;
    this.placeholder("Select a commit");
  }

  /** Closes the diff and returns to the file list. Returns false if none was open. */
  back(): boolean {
    if (this.fileIndex < 0) return false;
    const prev = this.fileIndex;
    this.fileIndex = -1;
    this.renderCommit();
    this.root.scrollTop = this.listScroll;
    this.root.querySelector<HTMLElement>(`.file[data-i="${prev}"]`)?.classList.add("active");
    return true;
  }

  /** Opens the first file's diff, or steps to the next/previous file when one is open. */
  stepFile(delta: number) {
    const files = this.detail?.files ?? [];
    if (!files.length) return;
    const next = this.fileIndex < 0 ? (delta > 0 ? 0 : files.length - 1) : this.fileIndex + delta;
    if (next >= 0 && next < files.length) void this.openFile(next);
  }

  private renderCommit() {
    const d = this.detail!;
    const row = this.row!;
    const head = el("div", "d-head");
    const lines = d.message.split("\n");
    head.append(el("h2", "d-subject", lines[0] || "(no message)"));

    const meta = el("div", "d-meta");
    if (d.sha !== WORKTREE) {
      const sha = el("span", "sha", d.sha.slice(0, 10));
      sha.title = "Copy full commit id";
      sha.onclick = () => {
        void navigator.clipboard?.writeText(d.sha).then(() => toast("Commit id copied"), () => toast(d.sha));
      };
      meta.append(sha);
      meta.append(el("span", "", `${d.author}`));
      const when = el("span", "", fullDate(d.authorTime));
      meta.append(when);
      if (d.committer && (d.committer !== d.author || Math.abs(d.commitTime - d.authorTime) > 60)) {
        meta.append(el("span", "", `committed by ${d.committer}, ${fullDate(d.commitTime)}`));
      }
    } else {
      meta.append(el("span", "", "Working tree compared with HEAD"));
    }
    if (d.parents.length) {
      const ps = el("span", "", d.parents.length > 1 ? "parents " : "parent ");
      d.parents.forEach((p, i) => {
        if (i) ps.append(", ");
        const a = el("a", "", p.slice(0, 8));
        a.onclick = () => this.cb.goTo(p);
        ps.append(a);
      });
      meta.append(ps);
    }
    head.append(meta);

    const refs = renderRefs(this.refs, this.cb.laneColor(row.c));
    if (refs.length) {
      const box = el("div", "d-refs");
      box.append(...refs);
      head.append(box);
    }
    const body = lines.slice(1).join("\n").trim();
    if (body) head.append(el("pre", "d-body", body));

    const files = d.files ?? [];
    const fh = el("div", "d-files-head");
    let add = 0, del = 0;
    for (const f of files) { if (f.add > 0) add += f.add; if (f.del > 0) del += f.del; }
    fh.append(el("span", "", files.length === 1 ? "1 file changed" : `${files.length} files changed`));
    const stat = el("span", "stat");
    stat.append(el("span", "a", `+${add}`), el("span", "d", `−${del}`));
    fh.append(stat);
    if (d.parents.length > 1) fh.title = "Changes compared with the first parent";

    const list = el("ul", "files");
    files.forEach((f, i) => list.append(this.renderFile(f, i)));
    if (!files.length) list.append(el("li", "diff-note", "No changed files"));
    this.root.replaceChildren(head, fh, list);
  }

  private renderFile(f: FileChange, i: number): HTMLElement {
    const li = el("li", "file");
    li.dataset.i = String(i);
    const slash = f.path.lastIndexOf("/");
    const name = f.path.slice(slash + 1);
    const dir = slash >= 0 ? f.path.slice(0, slash) : "";
    li.append(el("span", `st st-${f.status}`, f.status === "?" ? "U" : f.status));
    li.append(el("span", "name", name));
    const dirText = f.oldPath ? `${dir} ← ${f.oldPath}` : dir;
    li.append(el("span", "dir", dirText));
    const stat = el("span", "stat");
    if (f.add < 0) stat.append(el("span", "", "bin"));
    else {
      if (f.add) stat.append(el("span", "a", `+${f.add}`));
      if (f.del) stat.append(el("span", "d", `−${f.del}`));
    }
    li.append(stat);
    li.title = f.oldPath ? `${f.oldPath} → ${f.path}` : f.path;
    li.onclick = () => void this.openFile(i);
    return li;
  }

  async openFile(i: number) {
    const d = this.detail;
    const f = d?.files?.[i];
    if (!d || !f) return;
    if (this.fileIndex < 0) this.listScroll = this.root.scrollTop;
    this.fileIndex = i;
    const token = ++this.token;
    const header = this.diffHeader(f, i, d.files!.length);
    this.root.replaceChildren(header, el("div", "diff-note", "Loading…"));
    let diff: FileDiff;
    try {
      diff = await api.diff(d.sha, f);
    } catch (err) {
      if (token === this.token) this.root.replaceChildren(header, el("div", "diff-note", (err as Error).message));
      return;
    }
    if (token !== this.token) return;
    this.root.replaceChildren(header, renderDiff(diff));
    this.root.scrollTop = 0;
  }

  private diffHeader(f: FileChange, i: number, n: number): HTMLElement {
    const h = el("div", "diff-head");
    const back = el("button", "icon-btn", "←");
    back.title = "Back to files (Esc)";
    back.onclick = () => this.back();
    const prev = el("button", "icon-btn", "‹");
    prev.title = "Previous file ([)";
    prev.disabled = i === 0;
    prev.onclick = () => this.stepFile(-1);
    const next = el("button", "icon-btn", "›");
    next.title = "Next file (])";
    next.disabled = i === n - 1;
    next.onclick = () => this.stepFile(1);
    const path = el("span", "path");
    path.append(el("bdi", "", f.oldPath ? `${f.oldPath} → ${f.path}` : f.path));
    path.title = f.path;
    h.append(back, path, el("span", "search-count", `${i + 1}/${n}`), prev, next);
    return h;
  }
}

function renderDiff(d: FileDiff): HTMLElement {
  const box = el("div", "diff chroma");
  if (d.binary) {
    box.append(el("div", "diff-note", "Binary file"));
    return box;
  }
  const hunks = d.hunks ?? [];
  if (!hunks.length) {
    box.append(el("div", "diff-note", "No content changes (mode or rename only)"));
    return box;
  }
  const parts: string[] = [];
  for (const h of hunks) {
    parts.push(`<div class="hunk-head">${escapeHTML(h.header)}</div>`);
    for (const l of h.lines) {
      const cls = l.k === "+" ? "add" : l.k === "-" ? "del" : l.k === "\\" ? "note" : "ctx";
      parts.push(
        `<div class="dl ${cls}"><span class="no">${l.o ?? ""}</span><span class="no">${l.n ?? ""}</span><span class="tx">${l.h || " "}</span></div>`,
      );
    }
  }
  box.innerHTML = parts.join("");
  if (d.truncated) box.append(el("div", "diff-note", "Diff truncated: the file has too many changed lines to show here."));
  return box;
}

function escapeHTML(s: string): string {
  return s.replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]!);
}
