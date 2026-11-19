// Captures a page with headless Chrome through the DevTools protocol. A plain
// `chrome --screenshot` never finishes on ggitree, because the page keeps its
// live-update connection open.
//
//   node screenshot.mjs <url> <out.png> [width] [height] [light|dark] [key=value,...]
//
// The last argument presets the page's localStorage, e.g. "ggitree.follow=1" to
// show a toggle switched on.
//
// Chrome is $CHROME, or the first of a few usual names on PATH.

import { spawn, execFileSync } from "node:child_process";
import { writeFileSync } from "node:fs";
import { setTimeout as sleep } from "node:timers/promises";

const [url, out, w = "600", h = "900", scheme = "dark", storage = ""] = process.argv.slice(2);
if (!url || !out) {
  console.error("usage: node screenshot.mjs <url> <out.png> [width] [height] [light|dark] [key=value,...]");
  process.exit(2);
}

function findChrome() {
  if (process.env.CHROME) return process.env.CHROME;
  for (const name of ["chrome-headless-shell", "google-chrome-stable", "google-chrome", "chromium", "chromium-browser"]) {
    try {
      return execFileSync("which", [name], { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] }).trim();
    } catch { /* not installed */ }
  }
  console.error("No Chrome found; set CHROME to its path.");
  process.exit(1);
}

const port = 9300 + Math.floor(Math.random() * 500);
const chrome = spawn(findChrome(), [
  "--headless", "--no-sandbox", "--hide-scrollbars", "--no-first-run",
  `--remote-debugging-port=${port}`, `--window-size=${w},${h}`, "about:blank",
], { stdio: "ignore" });

try {
  let target;
  for (let i = 0; i < 100 && !target; i++) {
    await sleep(100);
    try {
      const list = await (await fetch(`http://127.0.0.1:${port}/json`)).json();
      target = list.find((t) => t.type === "page");
    } catch { /* not up yet */ }
  }
  if (!target) throw new Error("Chrome did not start");

  const ws = new WebSocket(target.webSocketDebuggerUrl);
  await new Promise((r) => (ws.onopen = r));
  let id = 0;
  const pending = new Map();
  ws.onmessage = (m) => {
    const msg = JSON.parse(m.data);
    if (msg.id && pending.has(msg.id)) { pending.get(msg.id)(msg); pending.delete(msg.id); }
    if (msg.method === "Runtime.exceptionThrown") console.error("page error:", msg.params.exceptionDetails.exception?.description);
  };
  const send = (method, params = {}) => new Promise((r) => {
    const i = ++id;
    pending.set(i, r);
    ws.send(JSON.stringify({ id: i, method, params }));
  });

  await send("Runtime.enable");
  await send("Page.enable");
  if (storage) {
    const entries = storage.split(",").map((kv) => kv.split("="));
    await send("Page.addScriptToEvaluateOnNewDocument", {
      source: `for (const [k, v] of ${JSON.stringify(entries)}) localStorage.setItem(k, v);`,
    });
  }
  await send("Emulation.setEmulatedMedia", { features: [{ name: "prefers-color-scheme", value: scheme }] });
  await send("Emulation.setDeviceMetricsOverride", { width: +w, height: +h, deviceScaleFactor: 2, mobile: false });
  await send("Page.navigate", { url });
  await sleep(2000);
  const shot = await send("Page.captureScreenshot", { format: "png" });
  writeFileSync(out, Buffer.from(shot.result.data, "base64"));
  ws.close();
} finally {
  chrome.kill();
}
