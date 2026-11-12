// The command list's rules (src/web/ts/commands.ts) and the settings value typing (views/settings.ts), without a
// browser. Run: npm run test:web
import { build } from "esbuild";
import assert from "node:assert/strict";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const load = async f => { const o = await build({ entryPoints: [join(here, "..", "ts", f)], bundle: true, format: "esm", write: false, platform: "neutral" });
  return import("data:text/javascript;base64," + Buffer.from(o.outputFiles[0].text).toString("base64")); };
const C = await load("commands.ts"), S = await load("views/settings.ts");

const yes = () => true, no = () => false, run = () => {};
const list = [
  ["undo", "Undo", "ti-undo", "⌘Z", yes, run, "Edit"],
  ["redo", "Redo", "ti-redo", "⇧⌘Z", no, run, "Edit"],
  ["theme", "Toggle dark theme", "ti-moon", "", yes, run, "View"],
  ["dupes", "Show duplicates", "ti-copy", "D", yes, run, "Filter"],
  ["reset", "Factory reset", "ti-alert-triangle", "", yes, run, "Settings", true],
].map(C.toCommand);

let checks = 0;
const check = (name, fn) => { fn(); checks++; console.log("ok", name); };
const ids = cs => cs.map(c => c.id);

check("the tuple becomes a record, danger defaulting to false", () => {
  assert.equal(list[0].label, "Undo"); assert.equal(list[0].group, "Edit"); assert.equal(list[0].danger, false);
  assert.equal(list[4].danger, true);
});
check("an empty query lists every enabled command, in order", () => {
  assert.deepEqual(ids(C.matchCommands(list, "  ")), ["undo", "theme", "dupes", "reset"]);
});
check("a disabled command never shows", () => assert.deepEqual(ids(C.matchCommands(list, "redo")), []));
check("every word must match, in the label or the group, any case", () => {
  assert.deepEqual(ids(C.matchCommands(list, "DARK view")), ["theme"]);
  assert.deepEqual(ids(C.matchCommands(list, "edit")), ["undo"]);
  assert.deepEqual(ids(C.matchCommands(list, "dark edit")), []);
});
check("the shortcuts sheet: navigation first, keyed commands by group, Filter left out", () => {
  const cols = C.shortcutColumns(list, [["↑ ↓", "Move"]]);
  assert.deepEqual(cols.map(c => c[0]), ["Navigate", "Edit"]);
  assert.deepEqual(cols[1][1], [["⌘Z", "Undo"], ["⇧⌘Z", "Redo"]]);
});
check("a select's value is typed like the setting's default", () => {
  assert.equal(S.settingValue("25", 10), 25);
  assert.equal(S.settingValue("dark", "system"), "dark");
});
check("every setting row names a kind and selects carry options", () => {
  let n = 0;
  for (const [, rows] of S.SETTING_GROUPS) for (const r of rows) { n++; assert.ok(r[3] === "switch" || (r[3] === "select" && r[4].length > 1), r[0]); }
  assert.ok(n >= 12, `only ${n} setting rows`);
});
assert.equal(checks, 7);
console.log(`${checks} checks passed`);
