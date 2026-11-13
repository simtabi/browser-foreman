// The keyboard table (src/web/ts/keys.ts) and the import file builder (src/web/ts/exportfile.ts), without a browser.
// Run: npm run test:web
import { build } from "esbuild";
import assert from "node:assert/strict";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const load = async f => { const o = await build({ entryPoints: [join(here, "..", "ts", f)], bundle: true, format: "esm", write: false, platform: "neutral" });
  return import("data:text/javascript;base64," + Buffer.from(o.outputFiles[0].text).toString("base64")); };
const K = await load("keys.ts"), X = await load("exportfile.ts"), T = await load("tree.ts");

let checks = 0;
const check = (name, fn) => { fn(); checks++; console.log("ok", name); };
const idle = { dialogOpen: false, menuOpen: false, typing: false, inSearch: false, hasRow: true };
const act = (k, c = {}) => K.keyAction(typeof k === "string" ? { key: k } : k, { ...idle, ...c })?.action ?? null;

check("1–5 rate the selected bookmark, 0 clears, T goes to its tags; never while typing or with nothing selected", () => {
  assert.equal(act("4", { hasRow: true }), "rate4");
  assert.equal(act("0", { hasRow: true }), "rate0");
  assert.equal(act("t", { hasRow: true }), "tag");
  assert.equal(act("4", { hasRow: true, typing: true }), null);
  assert.equal(act("4", { hasRow: false }), null);
});

check("⌘K opens the palette even over a dialog or while typing", () => {
  assert.equal(act({ key: "k", meta: true }, { dialogOpen: true }), "palette");
  assert.equal(act({ key: "K", ctrl: true }, { typing: true }), "palette");
});
check("a dialog or the right-click menu takes every other key", () => {
  assert.equal(act({ key: "z", meta: true }, { dialogOpen: true }), null);
  assert.equal(act("Delete", { menuOpen: true }), null);
});
check("⌘Z / ⇧⌘Z / ⌘Y / ⌘A, but not while typing", () => {
  assert.equal(act({ key: "z", meta: true }), "undo");
  assert.equal(act({ key: "Z", meta: true, shift: true }), "redo");
  assert.equal(act({ key: "y", ctrl: true }), "redo");
  assert.equal(act({ key: "a", meta: true }), "selectAll");
  assert.equal(act({ key: "z", meta: true }, { typing: true }), null);
  assert.equal(act({ key: "c", meta: true }), null);   // ⌘C stays the browser's copy
});
check("in the search box, ↓ and Enter go to the list; Escape works anywhere and keeps the browser default", () => {
  assert.equal(act("ArrowDown", { typing: true, inSearch: true }), "toList");
  assert.equal(act("Escape", { typing: true }), "escape");
  assert.equal(K.keyAction({ key: "Escape" }, idle).prevent, false);
});
check("while typing, letters are text, not commands", () => {
  for (const k of ["m", "/", "?", "Delete", ","]) assert.equal(act(k, { typing: true }), null, k);
});
check("page keys work with no row selected; row keys need one", () => {
  assert.equal(act("/", { hasRow: false }), "search");
  assert.equal(act("?", { hasRow: false }), "shortcuts");
  assert.equal(act("F1", { hasRow: false }), "help");
  assert.equal(act(",", { hasRow: false }), "settings");
  assert.equal(act("m", { hasRow: false }), null);
});
check("row keys", () => {
  const table = { m: "move", G: "group", n: "newFolder", F2: "rename", Delete: "trash", Backspace: "trash", o: "openHere", O: "openWindow",
    i: "openIncognito", " ": "mark", Enter: "enter", "*": "expandAll", "-": "collapseAll", ".": "menu", p: "putBack", u: "update", r: "refresh", l: "live" };
  for (const [k, a] of Object.entries(table)) assert.equal(act(k), a, k);
  assert.equal(act({ key: "ArrowUp", alt: true }), "moveUp");
  assert.equal(act({ key: "ArrowDown", alt: true }), "moveDown");
  assert.equal(act("Home"), "nav");
  assert.equal(act({ key: "F10", shift: true }), "menu");
  assert.equal(act("F10"), null);
  assert.equal(act("q"), null);
});

const root = T.mkFolder(""), bar = T.mkFolder("Bookmarks Bar"), dev = T.mkFolder("Dev & <Tools>"), bin = T.mkFolder("Trash", [3]);
dev.d = 'A "quoted" note'; dev.c = [2]; bar.c = [1, dev]; root.c = [bar, bin];
const B = { 1: { u: "https://a.test/?x=1&y=2", t: "A <b>", a: 1700000000 }, 2: { u: "https://b.test/", t: "B", n: "why" }, 3: { u: "https://gone.test/", t: "Gone" } };
const out = X.netscapeFile(root, { heading: "Bookmarks (cleaned 2026-11-12, unified)", skip: bin, order: f => f.c, name: f => f.n,
  url: id => B[id].u, title: id => B[id].t, added: id => B[id].a, note: id => B[id].n });
check("the import file: Netscape header, the Trash left out, links counted", () => {
  assert.ok(out.text.startsWith("<!DOCTYPE NETSCAPE-Bookmark-file-1>"));
  assert.equal(out.links, 2);
  assert.ok(!out.text.includes("gone.test") && !out.text.includes(">Trash<"));
});
check("names, titles, addresses and notes are escaped; folder descriptions kept", () => {
  assert.ok(out.text.includes('<A HREF="https://a.test/?x=1&amp;y=2" ADD_DATE="1700000000">A &lt;b&gt;</A>'));
  assert.ok(out.text.includes("<H3>Dev &amp; &lt;Tools&gt;</H3>"));
  assert.ok(out.text.includes("<DD>A &quot;quoted&quot; note"));
  assert.ok(out.text.includes('ADD_DATE="0">B</A>') && out.text.includes("<DD>why"));
});
assert.equal(checks, 10);
console.log(`${checks} key and export checks passed`);
