// The Find duplicates dialog's rules (src/ts/dupes.ts), without a browser. Run: npm run test:web
import { build } from "esbuild";
import assert from "node:assert/strict";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const load = async f => { const o = await build({ entryPoints: [join(here, "..", "ts", f)], bundle: true, format: "esm", write: false, platform: "neutral" });
  return import("data:text/javascript;base64," + Buffer.from(o.outputFiles[0].text).toString("base64")); };
const D = await load("dupes.ts");

const m = id => ({ id, title: id, url: "https://x.test/" + id, path: "Bar" });
const strong = { reasons: ["same-final", "similar-title"], keep: "a", members: [m("a"), m("b"), m("c")] };
const weak = { reasons: ["similar-title"], keep: "d", members: [m("d"), m("e")] };

assert.equal(D.startsTicked(strong), true);
assert.equal(D.startsTicked(weak), false, "a group found only by title starts unticked");
assert.deepEqual(D.trashOps([strong], new Map()), [{ op: "trash", ids: ["b", "c"], reason: "duplicate of a" }]);
assert.deepEqual(D.trashOps([strong, weak], new Map([[strong, "c"]])),
  [{ op: "trash", ids: ["a", "b"], reason: "duplicate of c" }, { op: "trash", ids: ["e"], reason: "duplicate of d" }], "the owner's pick wins");
assert.deepEqual(D.trashOps([{ reasons: ["same-address"], keep: "z", members: [m("z")] }], new Map()), [], "nothing to move, no change");
console.log("dupes: 5 checks passed");
