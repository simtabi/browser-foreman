// The Trash window's rules (src/web/ts/views/trash.ts), without a browser. Run: npm run test:web
import { build } from "esbuild";
import assert from "node:assert/strict";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const load = async f => { const o = await build({ entryPoints: [join(here, "..", "ts", f)], bundle: true, format: "esm", write: false, platform: "neutral" });
  return import("data:text/javascript;base64," + Buffer.from(o.outputFiles[0].text).toString("base64")); };
const X = await load("views/trash.ts");

const list = [
  { node: 1, folder: false, title: "Laravel queues guide", url: "https://laravel.test/queues", from: "Development › Laravel & PHP", count: 1, reasons: { duplicate: 1 },
    dupKey: "laravel.test/queues", kept: { title: "Queues", path: "Development › Laravel & PHP › Docs", url: "https://laravel.test/queues" } },
  { node: 2, folder: true, title: "_Review", from: "Bookmarks Bar", count: 592, reasons: { dead: 520, search: 57, local: 15 } },
  { node: 3, folder: false, title: "Old shop", url: "https://shop.test/", from: "Business & life › Shopping", count: 1, reasons: { yours: 1 } },
  { node: 4, folder: false, title: "Queues (old docs)", url: "https://laravel.test/queues/", from: "Development › General", count: 1, reasons: { duplicate: 1 },
    dupKey: "laravel.test/queues", kept: { title: "Queues", path: "Development › Laravel & PHP › Docs", url: "https://laravel.test/queues" } },
];
let checks = 0;
const check = (name, fn) => { fn(); checks++; console.log("ok", name); };
check("an empty filter lists everything", () => assert.equal(X.filterTrash(list, " ").length, 4));
check("every word must match, in the title, address or origin", () => {
  assert.deepEqual(X.filterTrash(list, "laravel queues").map(e => e.node), [1, 4]);
  assert.deepEqual(X.filterTrash(list, "shopping").map(e => e.node), [3]);
  assert.deepEqual(X.filterTrash(list, "laravel shop").map(e => e.node), []);
});
check("a folder counts the bookmarks it holds", () => assert.equal(X.bookmarksIn(list), 595));
check("why: a copy of something kept comes first, then the _Review group, then the link check, else you", () => {
  assert.equal(X.reasonOf({ copies: 1, review: "Dead links", status: "dead" }).key, "duplicate");
  assert.equal(X.reasonOf({ copies: 0, review: "Google searches", status: "ok" }).key, "search");
  assert.equal(X.reasonOf({ copies: 0, review: "Dead links" }).key, "dead");
  assert.equal(X.reasonOf({ copies: 0, status: "moved-elsewhere" }).key, "redirect");
  assert.equal(X.reasonOf({ copies: 0, status: "unverified" }).key, "unverified");
  assert.equal(X.reasonOf({ copies: 0, status: "ok" }).key, "yours");
});
check("counts per reason add a folder's contents, in a fixed order", () =>
  assert.deepEqual(X.reasonCounts(list).map(([r, n]) => `${r.key}:${n}`), ["duplicate:2", "dead:520", "search:57", "local:15", "yours:1"]));
check("a reason filter keeps only entries holding that reason", () => {
  assert.deepEqual(X.filterTrash(list, "", "dead").map(e => e.node), [2]);
  assert.deepEqual(X.filterTrash(list, "", "duplicate").map(e => e.node), [1, 4]);
});
check("duplicates of one address group under the kept copy, in first-seen order", () => {
  const u = X.groupTrash(list);
  assert.deepEqual(u.map(x => x.kind), ["dup", "one", "one"]);
  assert.equal(u[0].kept.title, "Queues"); assert.deepEqual(u[0].entries.map(e => e.node), [1, 4]);
});
assert.equal(checks, 7);
console.log(`${checks} trash checks passed`);
