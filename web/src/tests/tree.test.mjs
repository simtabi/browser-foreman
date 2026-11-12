// The tree model's rules (src/web/ts/tree.ts), without a browser. Run: npm run test:web
import { build } from "esbuild";
import assert from "node:assert/strict";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const out = await build({ entryPoints: [join(here, "..", "ts", "tree.ts")], bundle: true, format: "esm", write: false, platform: "neutral" });
const T = await import("data:text/javascript;base64," + Buffer.from(out.outputFiles[0].text).toString("base64"));

let checks = 0;
const check = (name, fn) => { fn(); checks++; console.log("ok", name); };

check("default tree: folders even when empty, links before subfolders, Bookmarks Bar first, Trash last", () => {
  const root = T.defaultTree(["Bookmarks Bar/Empty", "Trash", "_Review"], [
    { id: 2, path: "Bookmarks Bar" }, { id: 1, path: "Bookmarks Bar/Dev" }, { id: 3, path: "Other" }]);
  assert.deepEqual(root.c.map(f => f.n), ["Bookmarks Bar", "Other", "_Review", "Trash"]);
  const bar = root.c[0];
  assert.equal(bar.c[0], 2, "the loose bookmark comes before subfolders");
  assert.deepEqual(bar.c.slice(1).map(f => f.n), ["Dev", "Empty"]);
});

check("save and restore keep names, descriptions and order", () => {
  const f = T.mkFolder("A", [1, T.mkFolder("B∕C", [2], undefined, "a note")]);
  const back = T.revive(JSON.parse(JSON.stringify(T.plain(f))));
  assert.equal(back.n, "A"); assert.equal(back.c[0], 1); assert.equal(back.c[1].d, "a note");
  assert.equal(T.nice(back.c[1].n), "B/C");
  assert.equal("fid" in T.plain(f), false, "page ids are not saved");
});

check("restore drops unknown, gone and repeated ids, and places new bookmarks", () => {
  const saved = { n: "", c: [{ n: "_Removed (not exported)", c: [5] }, { n: "X", c: [1, 1, 9, 4] }] };
  const root = T.restoreTree(saved, { valid: id => id !== 9, gone: new Set([4]),
    items: [{ id: 1, path: "X" }, { id: 7, path: "New/Deep" }] });
  assert.equal(root.c[0].n, "Trash", "the old trash name is renamed");
  assert.deepEqual(root.c[1].c, [1], "9 unknown, 4 gone, the second 1 a repeat");
  const deep = root.c.find(f => f.n === "New").c[0];
  assert.equal(deep.n, "Deep"); assert.deepEqual(deep.c, [7]);
});

check("parents, paths, containment, detach, walks", () => {
  const root = T.defaultTree([], [{ id: 1, path: "A/B" }, { id: 2, path: "A" }]);
  const parents = T.parentsOf(root), a = root.c[0], b = a.c.find(T.isFolder);
  assert.equal(T.pathIn(b, parents, root), "A/B");
  assert.equal(T.containsNode(a, 1, parents), true);
  assert.equal(T.containsNode(b, 2, parents), false);
  assert.deepEqual(T.descendants(root).sort(), [1, 2]);
  const seen = []; T.walkFolders(root, f => seen.push(f.n)); assert.deepEqual(seen, ["A", "B"]);
  T.detachNode(1, parents); assert.deepEqual(T.descendants(root), [2]);
});

assert.ok(checks >= 4, `only ${checks} checks ran`);
console.log(`${checks} tree checks passed`);
