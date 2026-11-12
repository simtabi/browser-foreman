// The right-click menu layout (src/web/ts/ctxmenu.ts), without a browser. Run: npm run test:web
import { build } from "esbuild";
import assert from "node:assert/strict";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const load = async f => { const o = await build({ entryPoints: [join(here, "..", "ts", f)], bundle: true, format: "esm", write: false, platform: "neutral" });
  return import("data:text/javascript;base64," + Buffer.from(o.outputFiles[0].text).toString("base64")); };
const C = await load("ctxmenu.ts");

let checks = 0;
const check = (name, fn) => { fn(); checks++; console.log("ok", name); };
const on = new Set();
const entry = id => on.has(id) ? { label: `Full ${id}`, short: `short ${id}`, ic: "ti-x", run: () => id } : null;
const shape = items => items.filter(Boolean).map(i => i === "-" ? "-" : "sub" in i ? { [i.l]: shape(i.sub) } : i.l);
const plan = ["open", { group: "Open in…", ic: "ti-a", items: ["tabs", "window", "-", "incog"] }, "-", { group: "Organise", ic: "ti-b", items: ["up", "down"] }, "-", "trash"];

check("a group with several entries is a submenu, with short labels inside", () => {
  ["open", "tabs", "window", "incog", "up", "down", "trash"].forEach(x => on.add(x));
  assert.deepEqual(shape(C.layoutMenu(plan, entry)),
    ["Full open", { "Open in…": ["short tabs", "short window", "-", "short incog"] }, "-", { Organise: ["short up", "short down"] }, "-", "Full trash"]);
});
check("a group with one entry shows that entry in its place, under its full label", () => {
  on.clear(); ["open", "window", "up", "down"].forEach(x => on.add(x));
  assert.deepEqual(shape(C.layoutMenu(plan, entry)), ["Full open", "Full window", "-", { Organise: ["short up", "short down"] }, "-"]);
});
check("a group with nothing that applies is left out", () => {
  on.clear(); on.add("open"); on.add("trash");
  assert.deepEqual(shape(C.layoutMenu(plan, entry)), ["Full open", "-", "-", "Full trash"]);
});
check("nested groups resolve the same way", () => {
  on.clear(); ["a", "b", "c"].forEach(x => on.add(x));
  const nested = [{ group: "Outer", ic: "ti-o", items: ["a", { group: "Inner", ic: "ti-i", items: ["b", "c"] }] }];
  assert.deepEqual(shape(C.layoutMenu(nested, entry)), [{ Outer: ["short a", { Inner: ["short b", "short c"] }] }]);
  on.delete("a"); on.delete("c");
  assert.deepEqual(shape(C.layoutMenu(nested, entry)), ["Full b"]);  // Outer holds only Inner, which holds only b
});
check("the menu item runs the command", () => {
  on.clear(); on.add("open");
  assert.equal(C.layoutMenu(["open"], entry)[0].fn(), "open");
});
assert.equal(checks, 5);
console.log(`${checks} context-menu checks passed`);
