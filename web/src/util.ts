import type { Ref } from "./api";

/** Creates an element with a class and optional text. */
export function el<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  cls?: string,
  text?: string,
): HTMLElementTagNameMap[K] {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
}

const MIN = 60, HOUR = 3600, DAY = 86400;

/** Compact age: "now", "5m", "3h", "4d", then a date. */
export function shortAge(unix: number, now = Date.now() / 1000): string {
  const d = now - unix;
  if (d < MIN) return "now";
  if (d < HOUR) return `${Math.floor(d / MIN)}m ago`;
  if (d < DAY) return `${Math.floor(d / HOUR)}h ago`;
  if (d < 14 * DAY) return `${Math.floor(d / DAY)}d ago`;
  const date = new Date(unix * 1000);
  const sameYear = date.getFullYear() === new Date(now * 1000).getFullYear();
  return date.toLocaleDateString(undefined, sameYear
    ? { day: "numeric", month: "short" }
    : { day: "numeric", month: "short", year: "2-digit" });
}

export function fullDate(unix: number): string {
  return new Date(unix * 1000).toLocaleString(undefined, {
    year: "numeric", month: "short", day: "numeric", hour: "2-digit", minute: "2-digit",
  });
}

export const ICON_BRANCH = `<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"><circle cx="4.5" cy="3.5" r="1.6"/><circle cx="4.5" cy="12.5" r="1.6"/><circle cx="11.5" cy="5" r="1.6"/><path d="M4.5 5.1v5.8M11.5 6.6c0 3-7 2.2-7 4.3"/></svg>`;
export const ICON_TAG = `<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round"><path d="M2.5 2.5h5l6 6-5 5-6-6z"/><circle cx="5.5" cy="5.5" r="1" fill="currentColor"/></svg>`;
export const ICON_REMOTE = `<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round"><path d="M4.5 12.5h7a3 3 0 0 0 .4-6 4 4 0 0 0-7.7.9A2.6 2.6 0 0 0 4.5 12.5z"/></svg>`;
export const ICON_FETCH = `<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M4.5 12.5h7a3 3 0 0 0 .4-6 4 4 0 0 0-7.7.9A2.6 2.6 0 0 0 4.5 12.5z"/><path d="M8 6.5v4M6.4 9 8 10.6 9.6 9"/></svg>`;
const ICON_HEAD = `<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.8"><circle cx="8" cy="8" r="3"/><path d="M1.5 8h3.5M11 8h3.5"/></svg>`;

/**
 * Renders ref labels. A local branch and remote branches of the same name on the
 * same commit share one label ("main · origin"), as in JetBrains IDEs.
 */
export function renderRefs(refs: Ref[] | undefined | null, laneColor: string): HTMLElement[] {
  if (!refs?.length) return [];
  const out: HTMLElement[] = [];
  const locals = new Map<string, Ref>();
  for (const r of refs) if (r.k === "local") locals.set(r.n, r);
  const absorbed = new Map<string, string[]>();
  const remotes: Ref[] = [];
  for (const r of refs) {
    if (r.k !== "remote") continue;
    const slash = r.n.indexOf("/");
    const name = r.n.slice(slash + 1);
    if (slash > 0 && locals.has(name)) {
      const list = absorbed.get(name) ?? [];
      list.push(r.n.slice(0, slash));
      absorbed.set(name, list);
    } else {
      remotes.push(r);
    }
  }
  const sorted = [...refs].sort((a, b) => rank(a) - rank(b));
  for (const r of sorted) {
    if (r.k === "remote" && !remotes.includes(r)) continue;
    const also = absorbed.get(r.n);
    const e = el("span", `ref ${r.k}${r.c ? " current" : ""}${r.lo ? " local-only" : ""}`);
    if (r.k === "local" || r.k === "head") e.style.setProperty("--c", laneColor);
    const icon = r.k === "tag" ? ICON_TAG : r.k === "remote" ? ICON_REMOTE : r.k === "head" ? ICON_HEAD : ICON_BRANCH;
    // The cloud sits before the name, so it stays visible when the label is cut off.
    e.innerHTML = r.k === "local" && also ? icon + ICON_REMOTE : icon;
    e.append(document.createTextNode(r.n));
    if (r.k === "local" && also) e.append(el("span", "also", ` · ${also.join(", ")}`));
    let title = `${kindLabel(r.k)} ${r.n}`;
    if (r.c) title += " (checked out here)";
    if (r.w) title += ` (checked out in worktree ${r.w})`;
    if (r.lo) title += ", only local (not on any remote)";
    if (also) title += `, also on ${also.map((a) => `${a}/${r.n}`).join(", ")}`;
    e.title = title;
    if (r.w) e.append(el("span", "also", ` ⌂${r.w}`));
    out.push(e);
  }
  return out;
}

function rank(r: Ref): number {
  if (r.c) return 0;
  return { head: 0, local: 1, remote: 2, tag: 3 }[r.k];
}

function kindLabel(k: Ref["k"]): string {
  return { head: "Detached", local: "Branch", remote: "Remote branch", tag: "Tag" }[k];
}

export function toast(msg: string) {
  const t = el("div", "toast", msg);
  document.body.append(t);
  setTimeout(() => t.remove(), 1400);
}
