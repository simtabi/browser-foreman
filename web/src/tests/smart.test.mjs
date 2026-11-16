// Smart folders: which apply where, which one the current view is, and what adding one refuses.
import assert from "node:assert/strict";
import { build } from "esbuild";

const out = await build({ entryPoints: ["src/ts/smart.ts"], bundle: true, format: "esm", write: false, platform: "neutral" });
const m = await import("data:text/javascript;base64," + Buffer.from(out.outputFiles[0].text).toString("base64"));

const all = [
  { id: "a", name: "Dead in work", workspace: "work", filter: "dead" },
  { id: "b", name: "Go", q: "#go" },
  { id: "c", name: "Home only", workspace: "home", q: "recipes" },
];
assert.deepEqual(m.smartFor(all, "work").map(s => s.id), ["a", "b"], "a workspace sees its own and the shared ones");
assert.equal(m.smartMatching(all, "dead", "").id, "a");
assert.equal(m.smartMatching(all, "all", " #go ").id, "b", "the search is compared trimmed, the filter as all");
assert.equal(m.smartMatching(all, "moved", "#go"), undefined);

const next = m.addSmart(all, { name: "  To read ", q: "#later", filter: "all" }, "work");
const made = next.at(-1);
assert.equal(next.length, 4);
assert.equal(made.name, "To read");
assert.equal(made.filter, undefined, "all is the default, not stored");
assert.equal(made.workspace, "work");
assert.match(made.id, /^sf_[a-z0-9]+$/);
assert.throws(() => m.addSmart(all, { name: "", q: "x" }, null), /name/);
assert.throws(() => m.addSmart(all, { name: "Everything", filter: "all" }, null), /All bookmarks/);
assert.throws(() => m.addSmart(Array.from({ length: m.MAX_SMART }, (_, i) => ({ id: "x" + i, name: "x" })), { name: "y", q: "z" }, null), /At most/);

const name = k => ({ dead: "Dead links" })[k] || k;
assert.equal(m.describeSmart(all[0], name), "Dead links");
assert.equal(m.describeSmart(all[1], name), "Bookmarks tagged go (every workspace)");
assert.equal(m.describeSmart({ id: "d", name: "x", q: "rust book", workspace: "w" }, name), "Bookmarks matching “rust book”");
console.log("smart: 13 checks passed");
