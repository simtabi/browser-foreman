// Opening tabs and the Open tabs menus (src/web/ts/tabs.ts planOpen, views/tab-menus.ts), without a browser.
// Run: npm run test:web
import { build } from "esbuild";
import assert from "node:assert/strict";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const load = async f => { const o = await build({ entryPoints: [join(here, "..", "ts", f)], bundle: true, format: "esm", write: false, platform: "neutral" });
  return import("data:text/javascript;base64," + Buffer.from(o.outputFiles[0].text).toString("base64")); };
const T = await load("tabs.ts"), M = await load("views/tab-menus.ts");

const win = (browser, n, mode, urls) => ({ browser, n, inc: mode === "incognito", w: { index: n, mode, tabs: urls.map(u => ({ url: u, title: u })) } });
const A = win("Brave", 1, "normal", ["https://a.test/", "https://b.test/", "chrome://settings"]);
const B = win("Brave", 2, "normal", ["https://c.test/"]);
const I = win("Brave", 3, "incognito", ["https://secret.test/"]);
const all = [A, B, I].flatMap(T.tabsIn);

let checks = 0;
const check = (name, fn) => { fn(); checks++; console.log("ok", name); };
const plan = (...a) => { const p = T.planOpen(...a); return { ...p, batches: p.batches.map(b => `${b.how}:${b.urls.length}`) }; };

check("grouped: one window per original window, incognito stays incognito, non-web links left out", () =>
  assert.deepEqual(plan(all, "window", true), { batches: ["window:2", "window:1", "incognito:1"], total: 4, skipped: 0 }));
check("not grouped: one window per mode", () =>
  assert.deepEqual(plan(all, "window", false), { batches: ["window:3", "incognito:1"], total: 4, skipped: 0 }));
check("crossMode lets incognito tabs open in a normal window", () =>
  assert.deepEqual(plan(T.tabsIn(I), "window", true, true), { batches: ["window:1"], total: 1, skipped: 0 }));
check("as tabs: incognito ones are never put in the current window, and are counted", () =>
  assert.deepEqual(plan(all, "tabs", false), { batches: ["tabs:3"], total: 3, skipped: 1 }));
check("only incognito, as tabs: nothing opens", () =>
  assert.deepEqual(plan(T.tabsIn(I), "tabs", false), { batches: [], total: 0, skipped: 1 }));
check("incognito asked for: everything goes incognito", () =>
  assert.deepEqual(plan(all, "incognito", false), { batches: ["incognito:4"], total: 4, skipped: 0 }));

const calls = [], acts = new Proxy({}, { get: (_, k) => (...a) => calls.push([k, ...a]) });
const labels = items => items.filter(Boolean).map(i => i === "-" ? "-" : i.h ? `#${i.h}` : i.l);
check("mode items: moves only for live tabs; saved incognito tabs can still open in a normal window", () => {
  assert.deepEqual(labels(M.modeMenuItems(all, true, acts)), ["Move to a normal window", "Move 4 to an incognito window", "Open in a normal window"]);
  assert.deepEqual(labels(M.modeMenuItems(all, false, acts)), ["Open in a normal window"]);
});
check("a saved tab's menu has no close or move; a live one ends with Close", () => {
  const i = T.tabsIn(A)[0], o = { picked: false, title: "a.test", window: T.tabsIn(A) };
  const saved = labels(M.tabMenuItems(i, [i], { ...o, live: false }, acts)), live = labels(M.tabMenuItems(i, [i], { ...o, live: true }, acts));
  assert.ok(!saved.some(l => /Close|Move to bookmarks/.test(l)));
  assert.equal(live.at(-1), "Close this tab");
});
check("several picked: the menu names how many and acts on all of them", () => {
  const sel = T.tabsIn(A).slice(0, 2), items = M.tabMenuItems(sel[0], sel, { live: true, picked: true, title: "x", window: T.tabsIn(A) }, acts);
  assert.equal(items[0].h, "2 selected tabs");
  items.find(i => i && i.l === "Close 2 tabs").fn();
  assert.deepEqual(calls.at(-1), ["close", sel]);
});
check("a window menu is headed by its title and suggests its name", () => {
  const items = M.windowMenuItems(T.tabsIn(A), { live: false, title: "Brave · window 1", name: "Brave window 1", folded: false, fold() {}, saveFile() {} }, acts);
  assert.equal(items[0].h, "Brave · window 1");
  items.find(i => i && i.l === "Save as a bookmark folder…").fn();
  assert.deepEqual(calls.at(-1).slice(2), ["Brave window 1"]);
});
check("the View menu ticks the current choice", () => {
  const v = M.viewMenuItems({ incFirst: true, winSort: "most", tabSort: "original", compact: false }, () => {}, () => {});
  const ticked = v.filter(i => i && i.ic === "ti-check").map(i => i.l);
  assert.deepEqual(ticked, ["Incognito first", "Most tabs first", "As in the window"]);
});

// ---- the view's state rules ----
const read = { browsers: [
  { browser: "Brave", windows: [{ mode: "normal", tabs: [{}, {}] }, { mode: "incognito", tab_count: 5 }] },
  { browser: "Safari", windows: [{ mode: "normal", tabs: [{}] }] },
] };
check("summary counts only the browsers shown, incognito apart, kept counts included", () => {
  assert.deepEqual(T.scopedSummary(read, b => b === "Brave"), { windows: 1, tabs: 2, incognito_windows: 1, incognito_tabs: 5 });
  assert.deepEqual(T.scopedSummary(null, () => true), { windows: 0, tabs: 0, incognito_windows: 0, incognito_tabs: 0 });
});
check("ago: minutes, hours, days", () => {
  const now = Date.parse("2026-11-12T12:00:00Z"), at = s => new Date(now - s * 1000).toISOString();
  assert.deepEqual([at(10), at(300), at(7200), at(3 * 86400)].map(i => T.ago(i, now)), ["just now", "5 min ago", "2 h ago", "3 d ago"]);
  assert.equal(T.when("not a date"), "");
});
check("list rows: Open now and a snapshot's line", () => {
  assert.equal(T.liveRowSub(null), "reading the browsers…");
  assert.equal(T.liveRowSub({ windows: 1, tabs: 4, incognito_windows: 1, incognito_tabs: 2 }), "1 window · 4 tabs · 2 incognito");
  const r = T.snapshotRow({ id: "x", at: "2026-11-12T10:14:00", reason: "manual", browsers: ["Brave", "Safari"], summary: { windows: 2, tabs: 9, incognito_tabs: 3 } });
  assert.equal(r.icon, "ti-camera"); assert.match(r.title, /saved by you$/);
  assert.equal(r.sub, "2 windows · 9 tabs · 3 incognito (count only) · Brave, Safari");
});
check("autosave line", () => {
  assert.equal(T.autosaveText({ autosave: false }), "Tab autosave off");
  assert.equal(T.autosaveText({ autosave: true, save_mode: "interval", interval: 600 }), "Tabs saved every 10 min");
  assert.equal(T.autosaveText({ autosave: true, save_mode: "change" }), "Tabs saved as they change");
});
{
  let reads = 0, t = 1000, release;
  const r = new T.SingleRead(() => { reads++; return new Promise(ok => { release = () => ok({ n: reads }); }); }, e => ({ error: String(e) }), () => t);
  const a = r.get(), b = r.get(); release();
  const [x, y] = await Promise.all([a, b]);
  check("single read: two callers share one read; a fresh result is reused; a stale one reads again", () => {
    assert.equal(reads, 1); assert.equal(x, y); assert.equal(x._got, 1000);
  });
  t = 3000; const c = await r.get(5000);
  check("single read: within maxAge no new read", () => { assert.equal(reads, 1); assert.equal(c, x); assert.ok(r.fresh(5000)); assert.ok(!r.fresh(0)); });
  const d = r.get(0); release(); await d;
  check("single read: maxAge 0 always reads", () => assert.equal(reads, 2));
  const bad = new T.SingleRead(() => Promise.reject(new Error("down")), e => ({ error: e.message }), () => 1);
  const e = await bad.get();
  check("single read: a failed read becomes the error value", () => assert.deepEqual(e, { error: "down", _got: 1 }));
}
// A selection follows its tab, not a position: a re-read that moves tabs along never selects the tab that moved in.
{
  const a = { browser: "Safari", windowN: 1, pos: 2, url: "https://a.example/" };
  const b = { browser: "Safari", windowN: 1, pos: 2, url: "https://b.example/" }; // a closed, b slid into its place
  assert.notEqual(T.keyOf(a), T.keyOf(b), "by position alone, the next tab would take over the selection"); checks++;
  const c = { browser: "Chrome", windowId: 9, tabId: 41, windowN: 1, pos: 3, url: "https://a.example/" };
  assert.equal(T.keyOf(c), T.keyOf({ ...c, pos: 1, url: "https://a.example/#moved" }), "with its own id, a tab keeps its key"); checks++;
}
assert.equal(checks, 21);
console.log(`${checks} checks passed`);
