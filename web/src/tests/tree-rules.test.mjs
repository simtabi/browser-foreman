// The tree's interaction rules (src/web/ts/tree-rules.ts), without a browser. Run: npm run test:web
import { build } from "esbuild";
import assert from "node:assert/strict";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const load = async f => { const o = await build({ entryPoints: [join(here, "..", "ts", f)], bundle: true, format: "esm", write: false, platform: "neutral" });
  return import("data:text/javascript;base64," + Buffer.from(o.outputFiles[0].text).toString("base64")); };
const R = await load("tree-rules.ts"), T = await load("tree.ts");

// Bookmarks Bar (open) › [link 1, Dev (closed) › 2, link 3]; Trash
const root = T.mkFolder(""), bar = T.mkFolder("Bookmarks Bar"), dev = T.mkFolder("Dev", [2]), bin = T.mkFolder("Trash");
bar.c = [dev, 1, 3]; root.c = [bar, bin];
const rows = [
  { kind: "folder", node: bar, depth: 0, parent: root, open: true },
  { kind: "folder", node: dev, depth: 1, parent: bar, open: false },
  { kind: "link", node: 1, depth: 1, parent: bar },
  { kind: "link", node: 3, depth: 1, parent: bar },
  { kind: "folder", node: bin, depth: 0, parent: root, open: false, trash: true },
];
let checks = 0;
const check = (name, fn) => { fn(); checks++; console.log("ok", name); };

check("arrows move and stop at the ends", () => {
  assert.deepEqual(R.navigate(rows, 0, "ArrowDown", false), { kind: "select", to: 1 });
  assert.deepEqual(R.navigate(rows, 0, "ArrowUp", false), { kind: "select", to: 0 });
  assert.deepEqual(R.navigate(rows, 4, "ArrowDown", false), { kind: "select", to: 4 });
  assert.deepEqual(R.navigate(rows, 2, "End", false), { kind: "select", to: 4 });
});
check("→ opens a closed folder, steps into an open one, does nothing on a link", () => {
  assert.deepEqual(R.navigate(rows, 1, "ArrowRight", false), { kind: "toggle", open: true });
  assert.deepEqual(R.navigate(rows, 0, "ArrowRight", false), { kind: "select", to: 1 });
  assert.equal(R.navigate(rows, 2, "ArrowRight", false), null);
});
check("← closes an open folder, else goes to the parent; read-only never closes", () => {
  assert.deepEqual(R.navigate(rows, 0, "ArrowLeft", false), { kind: "toggle", open: false });
  assert.deepEqual(R.navigate(rows, 3, "ArrowLeft", false), { kind: "select", to: 0 });
  assert.equal(R.navigate(rows, 0, "ArrowLeft", true), null);  // no parent row for the top folder
});
check("ranges include both ends in either direction", () => {
  assert.deepEqual(R.rangeOf(rows, 3, 1), [dev, 1, 3]);
});
check("drop zones: a folder's top quarter before, middle into, bottom quarter after (closed only); the Trash takes any drop; a bookmark before or after", () => {
  assert.equal(R.dropZone(rows[1], 5, 40), "before");
  assert.equal(R.dropZone(rows[1], 20, 40), "into");
  assert.equal(R.dropZone(rows[1], 35, 40), "after");          // Dev is closed
  assert.equal(R.dropZone(rows[0], 35, 40), "into");           // the bar is open: below it are its own rows
  assert.equal(R.dropZone(rows[4], 1, 40), "into");            // Trash
  assert.equal(R.dropZone(rows[2], 10, 40), "before");
  assert.equal(R.dropZone(rows[2], 30, 40), "after");
});
check("drop actions: into, before, after; one level out only after a folder's last row", () => {
  assert.deepEqual(R.dropAction(rows[4], "into"), { kind: "trash" });
  assert.deepEqual(R.dropAction(rows[1], "into"), { kind: "into", folder: dev });
  assert.equal(R.dropAction(rows[2], "into"), null);           // a bookmark holds nothing
  assert.deepEqual(R.dropAction(rows[2], "before"), { kind: "before", parent: bar, before: 1 });
  assert.deepEqual(R.dropAction(rows[2], "after"), { kind: "after", parent: bar, after: 1 });
  // link 3 is the bar's last child, but the bar sits at the browser's top level: it stays inside the bar
  assert.deepEqual(R.dropAction(rows[3], "after", { outdent: true, rows }), { kind: "after", parent: bar, after: 3 });
  // with Dev open, its last child (2) at the left edge goes after Dev, inside the bar
  const open = [rows[0], { ...rows[1], open: true }, { kind: "link", node: 2, depth: 2, parent: dev }, rows[2], rows[3], rows[4]];
  assert.deepEqual(R.dropAction(open[2], "after", { outdent: true, rows: open }), { kind: "after", parent: bar, after: dev });
  // link 1 is not last: outdent does nothing
  assert.deepEqual(R.dropAction(rows[2], "after", { outdent: true, rows }), { kind: "after", parent: bar, after: 1 });
});
check("an after drop lands before the next item that is not being moved, or at the end", () => {
  assert.equal(R.afterTarget(bar, dev, []), 1);
  assert.equal(R.afterTarget(bar, dev, [1]), 3);
  assert.equal(R.afterTarget(bar, 3, []), null);
  assert.deepEqual(R.dragged(rows[2], new Set([1, 3])), [1, 3]);
  assert.deepEqual(R.dragged(rows[2], new Set([3])), [1]);
});
check("the loose-bookmarks label starts at the first link after a folder's subfolders", () => {
  assert.deepEqual(R.looseGroupAt(rows, 2), { parent: bar, count: 2 });
  assert.equal(R.looseGroupAt(rows, 3), null);
  assert.equal(R.looseGroupAt(rows, 1), null);
});
assert.ok(checks >= 8, `only ${checks} checks ran`);
console.log(`${checks} tree-rule checks passed`);
