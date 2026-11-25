// Screenshots of every feature, grouped and numbered, for the docs and the landing page: `make screenshots`.
//
// Runs the built binary on a throwaway store (a temporary config and state folder) holding demo-bookmarks.html,
// 37 public sites, and one saved tab snapshot of public pages. It never reads this computer's browsers: tab
// reading is pointed at a browser that does not exist, so no real tab, history or profile reaches a picture.
// Chrome runs headless with a throwaway profile, at 1440 x 900 and twice the pixel density.
//
//   node src/screenshots/capture.mjs [--check] [--only 02-tabs] [--out ../docs/screenshots]
//
// --check runs a link check first (it visits the 37 sites, politely, for up to 90 seconds) so link health shows.

import { spawn, execFileSync } from "node:child_process";
import { mkdtempSync, mkdirSync, readFileSync, writeFileSync, rmSync, existsSync } from "node:fs";
import { tmpdir, platform } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { createServer } from "node:net";
import puppeteer from "puppeteer-core";

const here = dirname(fileURLToPath(import.meta.url));
const root = resolve(here, "../../..");
const args = process.argv.slice(2);
const flag = n => args.includes(n);
const opt = (n, d) => { const i = args.indexOf(n); return i >= 0 ? args[i + 1] : d; };
const out = resolve(opt("--out", join(root, "docs", "screenshots")));
const only = opt("--only", "");
const osDir = { darwin: "macos", linux: "linux", win32: "windows" }[platform()];
const bin = process.env.BROFO_BIN || join(root, "build", osDir, platform() === "win32" ? "browser-foreman.exe" : "browser-foreman");
const chrome = process.env.CHROME || {
  darwin: "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
  linux: "/usr/bin/google-chrome",
  win32: "C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe",
}[platform()];
if (!existsSync(bin)) throw new Error(`no binary at ${bin}: run make build first (or set BROFO_BIN)`);
if (!existsSync(chrome)) throw new Error(`no Chrome at ${chrome}: set CHROME`);

const freePort = () => new Promise(ok => { const s = createServer(); s.listen(0, "127.0.0.1", () => { const p = s.address().port; s.close(() => ok(p)); }); });
const sleep = ms => new Promise(r => setTimeout(r, ms));

// A throwaway store.
const tmp = mkdtempSync(join(tmpdir(), "bf-shots-"));
const port = await freePort();
const base = `http://127.0.0.1:${port}`;
const env = { ...process.env, XDG_CONFIG_HOME: join(tmp, "c"), XDG_STATE_HOME: join(tmp, "s"), BROFO_SERVER_ADDR: `127.0.0.1:${port}`,
  BROFO_STORAGE_DRIVER: "json", BROFO_SERVER_OPEN: "never", NOTIFYKIT_DESKTOP: "0", BROFO_NOTIFY_DESKTOP: "0" };
const run = (...a) => execFileSync(bin, a, { env, stdio: ["ignore", "pipe", "pipe"] }).toString();
run("config", "init");
run("import", join(here, "demo-bookmarks.html"), "--workspace", "demo", "--name", "Demo");
const token = readFileSync(join(tmp, "c", "browser-foreman", ".env"), "utf8").match(/^BROFO_API_TOKEN=(.+)$/m)[1];

// One saved tab snapshot of public pages, so Open tabs and the Timeline have something to show.
const at = new Date(Date.now() - 3600e3).toISOString();
const tab = (url, title) => ({ url, title });
const state = { at, summary: { windows: 2, tabs: 7, incognito_windows: 0, incognito_tabs: 0, running: 1 }, browsers: [{
  browser: "Google Chrome", running: true, windows: [
    { index: 0, mode: "normal", active: 1, tabs: [tab("https://go.dev/doc/", "Documentation - The Go Programming Language"),
      tab("https://pkg.go.dev/std", "Standard library - Go Packages"), tab("https://developer.mozilla.org/", "MDN Web Docs"),
      tab("https://caniuse.com/", "Can I use... Support tables for HTML5, CSS3, etc")] },
    { index: 1, mode: "normal", tabs: [tab("https://en.wikivoyage.org/", "Wikivoyage"), tab("https://www.seat61.com/", "The Man in Seat 61"),
      tab("https://www.atlasobscura.com/", "Atlas Obscura")] }] }] };
const tabsDir = join(tmp, "s", "browser-foreman", "json", "tabs"); // the json driver keeps its files under json/
mkdirSync(tabsDir, { recursive: true });
writeFileSync(join(tabsDir, "demo-1.json"), JSON.stringify({ id: "demo-1", at, reason: "manual", windows: state }));

const server = spawn(bin, ["serve", "--no-open"], { env, stdio: ["ignore", "pipe", "pipe"] });
let log = ""; server.stdout.on("data", d => { log += d; }); server.stderr.on("data", d => { log += d; });
const api = async (method, path, body) => {
  const r = await fetch(`${base}/api/v1/${path}`, { method, headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json", Origin: base }, body: body ? JSON.stringify(body) : undefined });
  const j = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(`${method} ${path}: ${r.status} ${j.error ?? ""}`);
  return j;
};
const cleanup = () => { try { server.kill(); } catch {} rmSync(tmp, { recursive: true, force: true }); };
process.on("exit", cleanup);

for (let i = 0; i < 100; i++) { try { if ((await fetch(`${base}/api/v1/health`)).ok) break; } catch {} await sleep(100); }
// Never read this computer's browsers.
const prefs = await api("GET", "preferences");
await api("PUT", "preferences", { ...prefs, tracked: ["no-such-browser"], autosave: false });

if (flag("--check")) {
  const { job } = await api("POST", "linkchecks", { workspace: "demo" });
  for (let i = 0; i < 90; i++) { const v = await api("GET", `linkchecks/${job.id}`); if (v.job?.done) break; await sleep(1000); }
  await api("POST", `linkchecks/${job.id}/cancel`).catch(() => {});
  await sleep(1500);
}

const browser = await puppeteer.launch({ executablePath: chrome, headless: true, userDataDir: join(tmp, "chrome"),
  defaultViewport: { width: 1440, height: 900, deviceScaleFactor: 2 }, args: ["--no-first-run", "--no-default-browser-check"] });
const page = await browser.newPage();
// Local mode opens the page only through a one-time link (POST local/links), which signs this browser in.
const { url: signIn } = await api("POST", "local/links");
await page.goto(signIn, { waitUntil: "domcontentloaded" });
await page.evaluateOnNewDocument(() => { try { localStorage.setItem("bm-preview-v2:onboarded", "1"); } catch {} });

const ws = "?ws=demo";
let loads = 0;
const open = async (hash = "#/bookmarks", scheme = "light") => {
  await page.emulateMediaFeatures([{ name: "prefers-color-scheme", value: scheme }]);
  // Not "network idle": the page keeps its event stream open for as long as it is loaded.
  // A fresh load every time: a hash-only change would keep the last shot's dialog open.
  await page.goto(`${base}/${ws}&shot=${++loads}${hash}`, { waitUntil: "domcontentloaded" });
  const t = Date.now();
  await page.waitForSelector('[role="tree"]', { timeout: 15000 }).catch(() => console.log(`     no tree after ${Date.now() - t} ms`));
  await sleep(700);
};
// Click the first visible element of a role whose text contains label (names carry icon glyphs, so exact names
// do not match).
const clickText = async (selector, label) => {
  const ok = await page.evaluate((sel, l) => {
    const said = e => [e.textContent.replace(/\s+/g, " "), e.getAttribute("aria-label") ?? "", e.getAttribute("title") ?? ""].join(" | ");
    // The smallest match: a row, not the panel that holds it.
    const el = [...document.querySelectorAll(sel)].filter(e => e.offsetParent !== null && said(e).includes(l))
      .sort((x, y) => x.textContent.length - y.textContent.length)[0];
    if (el) el.click();
    return !!el;
  }, selector, label);
  if (!ok) throw new Error(`nothing to click: ${selector} containing "${label}"`);
  await sleep(700);
};
const click = label => clickText('button, [role="tab"], [role="menuitem"], a', label);
const key = async (k, mod) => { if (mod) await page.keyboard.down(mod); await page.keyboard.press(k); if (mod) await page.keyboard.up(mod); await sleep(700); };
const tree = name => clickText('[role="treeitem"] > *:first-child, [role="treeitem"]', name);
const cmd = platform() === "darwin" ? "Meta" : "Control";

// The shots: [group, number and name, caption, how to get there]. Grouped as the docs are.
const SHOTS = [
  ["01-bookmarks", "01-folder-overview", "A folder: its link health, where its bookmarks came from, and a picture of each page.",
    async () => { await open(); await tree("Development"); }],
  ["01-bookmarks", "02-folder-overview-dark", "The same view in dark mode.",
    async () => { await open("#/bookmarks", "dark"); await tree("Reading"); }],
  ["01-bookmarks", "03-search", "Search across titles, addresses, folders, notes and tags as you type.",
    async () => { await open(); await key("/"); await page.keyboard.type("go", { delay: 60 }); await sleep(900); }],
  ["01-bookmarks", "04-show-moved", "Show: only the bookmarks whose page moved, ready to update.",
    async () => { await open(); await click("Moved (update ready)"); }],
  ["01-bookmarks", "05-trash", "The Trash: removed and merged-away bookmarks, each with the reason, ready to put back.",
    async () => { await open(); await click("Trash"); }],
  ["02-tabs", "01-open-tabs", "Open tabs: windows and tabs from a saved snapshot, to restore after a crash.",
    async () => { await open("#/tabs"); await clickText("button, [role=\"button\"], li, a, div", "saved by you");
      await page.waitForFunction(() => /7 tabs/.test(document.querySelector("main")?.innerText ?? document.body.innerText) && !/Reading the browsers/.test(document.body.innerText), { timeout: 8000 }).catch(() => {}); await sleep(500); }],
  ["02-tabs", "02-timeline", "The Timeline: every tab snapshot over time.",
    async () => { await open("#/timeline"); }],
  ["03-workspaces", "01-workspace-menu", "The workspace menu: switch, rename, lock, archive or see the history of a workspace.",
    async () => { await open(); await click("Workspace"); }],
  ["03-workspaces", "02-import-export", "Import and export: browser bookmark files in, an import file for every browser out.",
    async () => { await open(); await click("Import and export"); }],
  ["04-browsers", "01-browsers-menu", "Browsers: which open links, which are read for tabs, and their extensions.",
    async () => { await open(); await page.click("button.nav-browsers"); await sleep(700); }],
  ["05-settings", "01-preferences", "Settings: the page's own preferences.",
    async () => { await open(); await key(","); }],
  ["05-settings", "02-server-settings-ai", "Server settings: AI provider, model and keys, written to the config files and .env. Keys are never shown back.",
    async () => { await open(); await key(","); await page.evaluate(() => document.querySelector('#settingsBody [data-group="ai"]')?.scrollIntoView({ block: "start" })); await sleep(500); }],
  ["05-settings", "03-server-settings-provider", "Server settings: a provider chosen. Only its key, its models (listed by the provider) and its effort show, saved together.",
    async () => { await open(); await key(","); await page.evaluate(() => { const s = document.querySelector('#settingsBody select[aria-label="Who answers AI jobs"]'); if (s) { s.value = "anthropic"; s.dispatchEvent(new Event("change")); } document.querySelector('#settingsBody [data-group="ai"]')?.scrollIntoView({ block: "start" }); }); await sleep(900); }],
  ["06-commands", "01-command-palette", "The command palette: every action, searchable, with its shortcut.",
    async () => { await open(); await key("k", cmd); }],
  ["06-commands", "02-keyboard-shortcuts", "Keyboard shortcuts.",
    async () => { await open(); await key("?"); }],
];

const results = [];
for (const [group, name, caption, go] of SHOTS) {
  if (only && !group.startsWith(only) && !`${group}/${name}`.startsWith(only)) continue;
  const file = join(out, group, `${name}.png`);
  const t0 = Date.now();
  try {
    await go();
    await page.evaluate(() => document.querySelectorAll(".toast").forEach(t => t.remove()));
    mkdirSync(dirname(file), { recursive: true });
    await page.screenshot({ path: file });
    results.push({ group, name, caption, ok: true });
    console.log(`ok   ${group}/${name}.png (${((Date.now() - t0) / 1000).toFixed(1)}s)`);
  } catch (e) {
    results.push({ group, name, caption, ok: false, error: String(e.message ?? e).split("\n")[0] });
    console.log(`FAIL ${group}/${name}: ${String(e.message ?? e).split("\n")[0]}`);
  }
}
await browser.close();
const failed = results.filter(r => !r.ok);
// The docs page lists every shot on disk, so a run of one group (--only) keeps the others on it.
const TITLES = { "01-bookmarks": "Bookmarks", "02-tabs": "Tabs", "03-workspaces": "Workspaces", "04-browsers": "Browsers",
  "05-settings": "Settings", "06-commands": "Commands" };
const md = ["# Screenshots", "", "Every feature, from a demo store of public sites, at 1440 x 900 and twice the pixel density. Made by",
  "`make screenshots` (`CHECK=1` checks the links first, so link health shows); see `web/src/screenshots/capture.mjs`.", ""];
let last = "";
for (const [group, name, caption] of SHOTS) {
  if (!existsSync(join(out, group, `${name}.png`))) continue;
  if (group !== last) { md.push(`## ${group.slice(0, 2)}. ${TITLES[group] ?? group}`, ""); last = group; }
  md.push(`**${group.slice(0, 2)}.${name.slice(0, 2)}** ${caption}`, "", `![${caption}](screenshots/${group}/${name}.png)`, "");
}
md.push("---", "[← Docs index](../README.md#documentation)", "");
writeFileSync(join(out, "..", "screenshots.md"), md.join("\n"));
console.log(`${results.length - failed.length} of ${results.length} shots in ${out}`);
if (failed.length) console.log(log.split("\n").slice(-5).join("\n"));
cleanup(); // the server would keep Node running
process.exit(failed.length ? 1 : 0);
