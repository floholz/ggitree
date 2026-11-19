// Fetching from the Branches header: a button that fetches now and shows how long
// ago the last fetch was, and an Auto toggle that fetches every few minutes while
// the page is visible. Auto is off by default, so ggitree stays read-only unless
// asked. A fetch only updates remote-tracking branches.

import { api, FetchResult, State } from "./api";
import { el, fullDate, ICON_FETCH, shortAge, toast } from "./util";

const AUTO_KEY = "ggitree.autoFetch";
const INTERVAL = 5 * 60; // seconds between automatic fetches
const TICK = 30_000;

export class FetchControl {
  readonly root = el("div", "fetch");
  private now = el("button", "fetch-now");
  private age = el("span", "age");
  private auto = el("button", "fetch-auto", "Auto");
  private fetched = 0; // unix seconds; 0 for never
  private error = "";
  private running = false;
  private autoOn = false;
  private lastToast = "";
  private lastAuto = 0; // when an automatic fetch last started, so failures don't retry every tick

  constructor() {
    this.now.type = this.auto.type = "button";
    this.now.innerHTML = ICON_FETCH;
    this.now.append(this.age);
    this.now.setAttribute("aria-label", "Fetch all remotes now");
    this.now.onclick = () => void this.run(false);
    this.auto.onclick = () => this.setAuto(!this.autoOn);
    this.root.append(this.now, this.auto);
    try { this.autoOn = localStorage.getItem(AUTO_KEY) === "1"; } catch { /* private mode */ }
    this.renderAuto();
    this.render();
    setInterval(() => this.tick(), TICK);
    document.addEventListener("visibilitychange", () => this.tick());
  }

  /** Takes the fetch status from a fresh state, then fetches if one is due. */
  setState(st: State) {
    this.apply(st);
    this.tick();
  }

  private apply(r: FetchResult | State) {
    this.fetched = r.fetched ?? 0;
    this.error = r.fetchError ?? "";
    this.render();
  }

  /** Shows or hides the control; the branches panel hides it without remotes. */
  setAvailable(on: boolean) {
    if (this.root.hidden === !on) return;
    this.root.hidden = !on;
    this.tick();
  }

  private setAuto(on: boolean) {
    this.autoOn = on;
    try { localStorage.setItem(AUTO_KEY, on ? "1" : "0"); } catch { /* private mode */ }
    this.renderAuto();
    this.tick();
  }

  private tick() {
    this.render();
    // Hidden means no repository or no remotes: nothing to fetch.
    if (!this.autoOn || this.running || this.root.hidden || document.visibilityState !== "visible") return;
    const now = Date.now() / 1000;
    if (now - Math.max(this.fetched, this.lastAuto) >= INTERVAL) {
      this.lastAuto = now;
      void this.run(true);
    }
  }

  private async run(auto: boolean) {
    if (this.running) return;
    this.running = true;
    this.render();
    try {
      const r = await api.fetch(auto);
      this.apply(r);
      if (r.fetchError && !r.skipped && (!auto || r.fetchError !== this.lastToast)) {
        toast(`Fetch failed: ${r.fetchError}`);
      }
      this.lastToast = r.fetchError ?? "";
      if (!auto && r.skipped && !r.fetchError) toast("A fetch is already running");
    } catch (err) {
      if (!auto) toast(`Fetch failed: ${(err as Error).message}`);
    } finally {
      this.running = false;
      this.render();
    }
  }

  private render() {
    this.now.classList.toggle("running", this.running);
    this.now.classList.toggle("failed", !!this.error && !this.running);
    this.now.disabled = this.running;
    this.age.textContent = this.running ? "fetching" : this.fetched ? shortAge(this.fetched) : "never";
    let title = "Fetch all remotes now: updates remote-tracking branches only";
    title += this.fetched ? `\nLast fetch: ${fullDate(this.fetched)}` : "\nNever fetched";
    if (this.error) title += `\nLast fetch failed: ${this.error}`;
    this.now.title = title;
  }

  private renderAuto() {
    this.auto.setAttribute("aria-pressed", String(this.autoOn));
    this.auto.title = this.autoOn
      ? "Auto-fetch is on: fetches every 5 minutes while this page is visible. Click to turn off."
      : "Auto-fetch is off: click to fetch every 5 minutes while this page is visible. Only remote-tracking branches change, never your branches or files.";
  }
}
