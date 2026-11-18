// Types and requests for the ggitree server API.

export type RefKind = "head" | "local" | "remote" | "tag";

export interface Ref {
  n: string; // name
  k: RefKind;
  c?: boolean; // checked out here
  w?: string; // checked out in another worktree
}

/** One history row. Edges are [x1, y1, x2, y2, color] with y in 0 (top), 1 (dot), 2 (bottom). */
export interface Row {
  h: string;
  p: string[];
  s: string;
  a: string;
  m: string;
  t: number;
  r?: Ref[];
  l: number;
  c: number;
  w: number;
  e: [number, number, number, number, number][];
}

export interface State {
  version: number;
  repo: { name: string; root: string } | null;
  head?: { sha: string; branch: string };
  dirty: number;
  total: number;
  error?: string;
}

export interface FileChange {
  status: string;
  path: string;
  oldPath?: string;
  add: number;
  del: number;
}

export interface CommitDetail {
  sha: string;
  parents: string[];
  author: string;
  email: string;
  authorTime: number;
  committer: string;
  committerEmail: string;
  commitTime: number;
  message: string;
  files: FileChange[] | null;
  base: string;
}

export interface DiffLine {
  k: " " | "+" | "-" | "\\";
  o?: number;
  n?: number;
  h: string;
}

export interface FileDiff {
  path: string;
  oldPath?: string;
  binary: boolean;
  truncated: boolean;
  hunks: { header: string; lines: DiffLine[] }[] | null;
}

/** A branch or tag in the branches panel. */
export interface RefEntry {
  n: string; // name (remote branches without the remote)
  h: string; // commit
  t: number; // commit date, or tag date
  c?: boolean; // checked out here
  w?: string; // checked out in another worktree
  u?: string; // upstream
  ah?: number; // ahead of upstream
  bh?: number; // behind upstream
  g?: boolean; // upstream gone
}

export interface RefList {
  local: RefEntry[];
  remotes: { name: string; branches: RefEntry[] }[];
  tags: RefEntry[];
}

export const WORKTREE = "WORKTREE";

export type Scope = "local" | "all";

/** Which branches the history shows; sent with every history request. */
export let scope: Scope = "local";
export function setScope(s: Scope) { scope = s; }

async function get<T>(path: string, params: Record<string, string | number> = {}): Promise<T> {
  const q = new URLSearchParams({ scope });
  for (const [k, v] of Object.entries(params)) q.set(k, String(v));
  const res = await fetch(`${path}?${q}`, { cache: "no-store" });
  const body = await res.json();
  if (!res.ok) throw new Error(body?.error ?? res.statusText);
  return body as T;
}

export const api = {
  state: () => get<State>("api/state"),
  rows: (offset: number, limit: number) =>
    get<{ version: number; offset: number; rows: Row[] }>("api/rows", { offset, limit }),
  locate: (shas: string[]) => get<{ version: number; indexes: number[] }>("api/locate", { sha: shas.join(",") }),
  search: (q: string) => get<{ version: number; matches: number[] }>("api/search", { q }),
  refs: () => get<{ version: number; refs: RefList }>("api/refs"),
  commit: (sha: string) => get<{ commit: CommitDetail; refs: Ref[] | null }>("api/commit", { sha }),
  diff: (sha: string, f: FileChange) =>
    get<FileDiff>("api/diff", {
      sha,
      path: f.path,
      old: f.oldPath ?? "",
      untracked: f.status === "?" ? 1 : 0,
    }),
};
