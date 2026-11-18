// The commit list: a virtualized list of rows with the graph drawn on one canvas
// that covers the visible part of the list.

import { api, Row, WORKTREE } from "./api";
import { el, renderRefs, shortAge } from "./util";

const ROW_H = 24;
const LANE_W = 14;
const PAD = 8; // space before the first lane
const MAX_LANES_FOR_TEXT = 14; // text never starts further right than this
const PAGE = 500;
const OVERSCAN = 8;
const LANE_COLORS = 10;

export interface HistoryCallbacks {
  onSelect(index: number, row: Row): void;
  onActivate(row: Row): void;
}

export class HistoryView {
  private scroller = el("div", "scroller");
  private spacer = el("div", "spacer");
  private canvas = el("canvas", "graph-canvas");
  private ctx = this.canvas.getContext("2d")!;

  private total = 0;
  private generation = 0; // bumped on every reset, so stale page loads are dropped
  private pages = new Map<number, Row[]>();
  private loading = new Set<number>();
  private rendered = new Map<number, HTMLElement>();
  private selected = -1;
  private matches = new Set<number>();
  private headSha = "";
  private colors: string[] = [];
  private dimColors: string[] = [];
  private bg = "#000";
  private frame = 0;

  constructor(root: HTMLElement, private cb: HistoryCallbacks) {
    this.scroller.append(this.spacer);
    root.append(this.scroller, this.canvas);
    this.scroller.addEventListener("scroll", () => this.schedule(), { passive: true });
    new ResizeObserver(() => this.schedule()).observe(root);
    matchMedia("(prefers-color-scheme: dark)").addEventListener("change", () => {
      this.readColors();
      this.rerender();
    });
    this.readColors();

    this.spacer.addEventListener("mousedown", (e) => {
      const i = this.indexAt(e);
      if (i >= 0) this.select(i);
    });
    this.spacer.addEventListener("dblclick", (e) => {
      const i = this.indexAt(e);
      const row = this.row(i);
      if (row) this.cb.onActivate(row);
    });
  }

  private indexAt(e: MouseEvent): number {
    const target = (e.target as HTMLElement).closest<HTMLElement>(".row");
    return target ? Number(target.dataset.i) : -1;
  }

  private readColors() {
    const cs = getComputedStyle(document.documentElement);
    this.colors = Array.from({ length: LANE_COLORS }, (_, i) => cs.getPropertyValue(`--lane-${i}`).trim());
    this.bg = cs.getPropertyValue("--bg").trim();
    // Lines of other branches are mixed toward the background rather than drawn
    // translucent: overlapping translucent pieces would show darker joints.
    this.dimColors = this.colors.map((c) => mix(c, this.bg, 0.5));
  }

  laneColor(c: number): string {
    return this.colors[c % LANE_COLORS];
  }

  /** Replaces the data with a new version of the history. */
  reset(total: number, headSha: string) {
    this.total = total;
    this.generation++;
    this.headSha = headSha;
    this.pages.clear();
    this.loading.clear();
    this.spacer.style.height = `${total * ROW_H}px`;
    if (this.selected >= total) this.selected = -1;
    this.rerender();
  }

  get count() { return this.total; }
  get selectedIndex() { return this.selected; }

  row(i: number): Row | undefined {
    if (i < 0 || i >= this.total) return undefined;
    return this.pages.get(Math.floor(i / PAGE))?.[i % PAGE];
  }

  /** Loads the row at i if needed. */
  async load(i: number): Promise<Row | undefined> {
    await this.ensurePage(Math.floor(i / PAGE));
    return this.row(i);
  }

  private async ensurePage(p: number) {
    if (this.pages.has(p) || p < 0 || p * PAGE >= this.total) return;
    if (this.loading.has(p)) {
      while (this.loading.has(p)) await new Promise((r) => setTimeout(r, 16));
      return;
    }
    this.loading.add(p);
    const gen = this.generation;
    try {
      const res = await api.rows(p * PAGE, PAGE);
      if (gen !== this.generation) return; // replaced meanwhile
      this.pages.set(p, res.rows);
      this.rerender();
    } finally {
      if (gen === this.generation) this.loading.delete(p);
    }
  }

  /** Index of the first row at least partly visible. */
  topIndex(): number {
    return Math.floor(this.scroller.scrollTop / ROW_H);
  }

  /** Scrolls so that row i sits at the given pixel offset from the top. */
  scrollRowTo(i: number, offset: number) {
    this.scroller.scrollTop = i * ROW_H - offset;
  }

  rowOffset(i: number): number {
    return i * ROW_H - this.scroller.scrollTop;
  }

  setMatches(m: number[]) {
    this.matches = new Set(m);
    this.rerender();
  }

  select(i: number, opts: { scroll?: "nearest" | "center"; silent?: boolean } = {}) {
    if (i < 0 || i >= this.total) return;
    this.selected = i;
    this.ensureVisible(i, opts.scroll ?? "nearest");
    this.rerender();
    if (opts.silent) return;
    void this.load(i).then((row) => {
      if (row && this.selected === i) this.cb.onSelect(i, row);
    });
  }

  move(delta: number) {
    const base = this.selected < 0 ? this.topIndex() : this.selected;
    this.select(Math.max(0, Math.min(this.total - 1, base + delta)));
  }

  pageSize(): number {
    return Math.max(1, Math.floor(this.scroller.clientHeight / ROW_H) - 1);
  }

  ensureVisible(i: number, mode: "nearest" | "center") {
    const top = i * ROW_H;
    const h = this.scroller.clientHeight;
    const st = this.scroller.scrollTop;
    if (mode === "center") {
      if (top < st || top + ROW_H > st + h) this.scroller.scrollTop = top - h / 2 + ROW_H / 2;
    } else if (top < st) {
      this.scroller.scrollTop = top;
    } else if (top + ROW_H > st + h) {
      this.scroller.scrollTop = top + ROW_H - h;
    }
  }

  private schedule() {
    if (!this.frame) this.frame = requestAnimationFrame(() => { this.frame = 0; this.render(); });
  }

  /** Redraws every visible row, e.g. after data or selection changed. */
  rerender() {
    for (const e of this.rendered.values()) e.remove();
    this.rendered.clear();
    this.schedule();
  }

  private render() {
    const h = this.scroller.clientHeight;
    const st = this.scroller.scrollTop;
    const first = Math.max(0, Math.floor(st / ROW_H) - OVERSCAN);
    const last = Math.min(this.total - 1, Math.ceil((st + h) / ROW_H) + OVERSCAN);

    for (let p = Math.floor(first / PAGE); p <= Math.floor(last / PAGE); p++) void this.ensurePage(p);

    for (const [i, e] of this.rendered) {
      if (i < first || i > last) { e.remove(); this.rendered.delete(i); }
    }
    for (let i = first; i <= last; i++) {
      if (this.rendered.has(i)) continue;
      const e = this.renderRow(i);
      this.rendered.set(i, e);
      this.spacer.append(e);
    }
    this.draw(st, h, first, last);
  }

  private renderRow(i: number): HTMLElement {
    const row = this.row(i);
    const e = el("div", "row");
    e.dataset.i = String(i);
    e.style.top = `${i * ROW_H}px`;
    if (i === this.selected) e.classList.add("sel");
    if (this.matches.has(i)) e.classList.add("match");
    if (!row) {
      e.classList.add("placeholder");
      e.style.paddingLeft = `${PAD + LANE_W + 8}px`;
      e.append(el("span", "subj", "…"));
      return e;
    }
    const lanes = Math.min(row.w, MAX_LANES_FOR_TEXT);
    e.style.paddingLeft = `${PAD + lanes * LANE_W + 6}px`;
    if (row.h === WORKTREE) e.classList.add("wt");
    const refs = renderRefs(row.r, this.laneColor(row.c));
    if (refs.length) {
      const box = el("span", "refs");
      box.append(...refs);
      e.append(box);
    }
    const subj = el("span", "subj", row.s);
    subj.title = row.s;
    e.append(subj);
    if (row.h !== WORKTREE) {
      const author = el("span", "author", row.a);
      author.title = `${row.a} <${row.m}>`;
      e.append(author, el("span", "date", shortAge(row.t)));
    } else {
      e.append(el("span", "date", ""));
    }
    return e;
  }

  private draw(st: number, h: number, first: number, last: number) {
    const dpr = window.devicePixelRatio || 1;
    const w = PAD * 2 + this.visibleLanes(first, last) * LANE_W;
    if (this.canvas.width !== Math.ceil(w * dpr) || this.canvas.height !== Math.ceil(h * dpr)) {
      this.canvas.width = Math.ceil(w * dpr);
      this.canvas.height = Math.ceil(h * dpr);
      this.canvas.style.width = `${w}px`;
      this.canvas.style.height = `${h}px`;
    }
    const c = this.ctx;
    c.setTransform(dpr, 0, 0, dpr, 0, 0);
    c.clearRect(0, 0, w, h);
    c.lineWidth = 2;
    c.lineCap = "butt";

    const sel = this.row(this.selected);
    const focus = sel ? sel.c : -1;
    const x = (lane: number) => PAD + lane * LANE_W + LANE_W / 2;

    // Lines first, then dots on top.
    for (let i = first; i <= last; i++) {
      const row = this.row(i);
      if (!row) continue;
      const top = i * ROW_H - st;
      const ys = [top, top + ROW_H / 2, top + ROW_H];
      const dashed = row.h === WORKTREE;
      for (const [x1, y1, x2, y2, color] of row.e) {
        c.strokeStyle = focus < 0 || color === focus
          ? this.laneColor(color)
          : this.dimColors[color % LANE_COLORS];
        c.setLineDash(dashed && y1 === 1 ? [3, 3] : []);
        const ax = x(x1), ay = ys[y1], bx = x(x2), by = ys[y2];
        c.beginPath();
        c.moveTo(ax, ay);
        if (ax === bx) {
          c.lineTo(bx, by);
        } else {
          const my = (ay + by) / 2;
          c.bezierCurveTo(ax, my, bx, my, bx, by);
        }
        c.stroke();
      }
    }
    c.setLineDash([]);
    const bg = this.bg;
    for (let i = first; i <= last; i++) {
      const row = this.row(i);
      if (!row) continue;
      const cx = x(row.l), cy = i * ROW_H - st + ROW_H / 2;
      const color = this.laneColor(row.c);
      c.beginPath();
      c.arc(cx, cy, 4.5, 0, Math.PI * 2);
      if (row.h === WORKTREE) {
        c.fillStyle = bg;
        c.fill();
        c.setLineDash([2, 2]);
        c.strokeStyle = color;
        c.lineWidth = 1.5;
        c.stroke();
        c.setLineDash([]);
        c.lineWidth = 2;
        continue;
      }
      c.fillStyle = color;
      c.fill();
      c.strokeStyle = bg;
      c.lineWidth = 1.5;
      c.stroke();
      c.lineWidth = 2;
      if (row.h === this.headSha) { // ring around the checked-out commit
        c.beginPath();
        c.arc(cx, cy, 7, 0, Math.PI * 2);
        c.strokeStyle = color;
        c.lineWidth = 1.5;
        c.stroke();
        c.lineWidth = 2;
      }
    }
  }

  private visibleLanes(first: number, last: number): number {
    let n = 1;
    for (let i = first; i <= last; i++) {
      const r = this.row(i);
      if (r) n = Math.max(n, r.w);
    }
    return n;
  }
}

/** Mixes two #rrggbb colors; t is the share of the first. */
function mix(a: string, b: string, t: number): string {
  const pa = hex(a), pb = hex(b);
  if (!pa || !pb) return a;
  const ch = (i: number) => Math.round(pa[i] * t + pb[i] * (1 - t)).toString(16).padStart(2, "0");
  return `#${ch(0)}${ch(1)}${ch(2)}`;
}

function hex(c: string): number[] | null {
  const m = /^#([0-9a-f]{6})$/i.exec(c);
  if (!m) return null;
  const n = parseInt(m[1], 16);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
}
