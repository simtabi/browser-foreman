// The server-tree bridge (src/ts/v1.ts), without a browser. Run: npm run test:web
import { build } from "esbuild";
import assert from "node:assert/strict";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const load = async f => { const o = await build({ entryPoints: [join(here, "..", "ts", f)], bundle: true, format: "esm", write: false, platform: "neutral" });
  return import("data:text/javascript;base64," + Buffer.from(o.outputFiles[0].text).toString("base64")); };
const V = await load("v1.ts"), T = await load("tree.ts");

const tree = () => ({
  root: { id: "root", kind: "folder", title: "", children: [
    { id: "bar", kind: "folder", title: "Bookmarks Bar", children: [
      { id: "go", kind: "bookmark", title: "Go", url: "https://go.dev/", added: 1700000000, note: "start here" },
      { id: "dev", kind: "folder", title: "Dev / Ops", note: "tools", children: [
        { id: "gh", kind: "bookmark", title: "GitHub", url: "https://github.com/" }] }] },
    { id: "other", kind: "folder", title: "Other bookmarks", children: [] }] },
  trash: [{ node: { id: "old", kind: "bookmark", title: "Old", url: "https://old.example/" }, from: "dev", index: 1, at: 1600000000, reason: "dead" }],
});
let n = 0; const newId = () => `new${++n}`;

// Round trip: what the page reads, saved back unchanged, is the same tree.
const page = V.toPage(tree(), "2026-11-12T00:00:00Z");
assert.equal(page.data.keep.length, 3);
assert.equal(page.data.total, 2, "the Trash is not counted as live");
assert.deepEqual(page.data.keep.map(i => [i.t, i.g]), [["Go", "Bookmarks Bar"], ["GitHub", "Bookmarks Bar/Dev ∕ Ops"], ["Old", "Bookmarks Bar/Dev ∕ Ops"]]);
assert.deepEqual(page.data.folders, ["Bookmarks Bar", "Bookmarks Bar/Dev ∕ Ops", "Other bookmarks"]);
assert.equal(page.state.notes[1], "start here");
assert.deepEqual(page.state.trashFrom[3], { path: "Bookmarks Bar/Dev ∕ Ops", index: 1 });
assert.deepEqual(page.data.dupes, [], "a Trash entry that is not a duplicate is not listed as one");

// A copy merged away is listed in dupes, with the folder of the copy kept, and stays in the Trash.
const merged = tree();
merged.trash.push({ node: { id: "go2", kind: "bookmark", title: "Go again", url: "https://www.go.dev", added: 1500000000 }, from: "other", index: 0, at: 1, reason: "duplicate of go" });
const mp = V.toPage(merged);
assert.deepEqual(mp.data.dupes.map(d => [d.id, d.t, d.o, d.k, d.a]), [[5, "Go again", "Other bookmarks", "Bookmarks Bar", 1500000000]]);
assert.equal(mp.data.keep.length, 4, "the copy is also in the Trash, where it can be put back");
assert.ok(!mp.data.keep.some(i => i.id === 5), "dupes ids come after keep's");
assert.deepEqual(V.fromPage(page.state, {}, page, newId), tree());

// The page's own tree functions keep the server ids through a revive/plain cycle (what loadState and persist do).
const revived = T.plain(T.revive(page.state.root));
assert.deepEqual(revived, page.state.root);

// Edits: rename (titles), new address (urls), new folder, move, delete for good (gone), put back from the Trash.
const s = structuredClone(page.state);
s.titles[1] = "The Go site";
const bar = s.root.c[0];
bar.c.push({ n: "Reading", c: [2] });                              // a new folder, GitHub moved into it
bar.c[1].c = [];                                                   // …out of Dev / Ops
const bin = s.root.c.find(x => typeof x === "object" && x.n === "Trash");
bin.c = []; s.gone = [3];                                          // "Old" deleted for good
const out = V.fromPage(s, { 1: "https://go.dev/doc/" }, page, newId);
const b = out.root.children[0];
assert.equal(b.children[0].title, "The Go site");
assert.equal(b.children[0].url, "https://go.dev/doc/");
assert.equal(b.children[2].id, "new1");
assert.equal(b.children[2].title, "Reading");
assert.deepEqual(b.children[2].children.map(x => x.id), ["gh"]);
assert.equal(b.children[1].title, "Dev / Ops", "∕ goes back to /");
assert.equal(out.trash, undefined, "an emptied Trash is left out");

// Trashing from the page keeps the folder it came from, by server id.
const t2 = structuredClone(page.state);
const goId = t2.root.c[0].c.shift();
t2.root.c.find(x => x.n === "Trash").c.push(goId);
t2.trashFrom[goId] = { path: "Bookmarks Bar", index: 0 };
const out2 = V.fromPage(t2, {}, page, newId, 1800000000);
assert.deepEqual(out2.trash.map(x => [x.node.id, x.from, x.at]), [["old", "dev", 1600000000], ["go", "bar", 1800000000]]);
assert.equal(out2.trash[0].reason, "dead", "the reason survives a save");
// Stored link results reach the page's fields; a moved link offers its new address.
const linked = V.toPage(tree(), "", {
  "https://go.dev/": { url: "https://go.dev/", status: "moved", final: "https://go.dev/home", note: "redirects", checked: "2026-11-12T00:00:00Z" },
  "https://github.com/": { url: "https://github.com/", status: "dead", kind: "host", note: "the domain does not exist", checked: "2026-11-12T00:00:00Z" },
});
assert.deepEqual([linked.data.keep[0].s, linked.data.keep[0].nu, linked.data.keep[0].n], ["moved", "https://go.dev/home", "redirects"]);
assert.deepEqual([linked.data.keep[1].s, linked.data.keep[1].dk, linked.data.keep[1].nu], ["dead", "host", undefined]);
assert.equal(linked.data.keep[2].s, "unchecked");
console.log("v1: 20 checks passed");
