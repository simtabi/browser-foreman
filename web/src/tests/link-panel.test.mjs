// A bookmark's detail panel rules (src/web/ts/views/link-panel.ts), without a browser. Run: npm run test:web
import { build } from "esbuild";
import assert from "node:assert/strict";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const load = async f => { const o = await build({ entryPoints: [join(here, "..", "ts", f)], bundle: true, format: "esm", write: false, platform: "neutral" });
  return import("data:text/javascript;base64," + Buffer.from(o.outputFiles[0].text).toString("base64")); };
const L = await load("views/link-panel.ts");

const c = { readOnly: false, inTrash: false, help: { dead: "The page did not answer. More text.", unverified: "Could not tell." },
  statusName: { unverified: "Not verified", "needs-login": "Needs a login" }, statusIcon: { unverified: "ti-question-mark" } };
const link = o => ({ id: 1, u: "https://a.test/", s: "ok", ...o });
let checks = 0;
const check = (name, fn) => { fn(); checks++; console.log("ok", name); };
const acts = a => a?.actions.map(x => x[2]);

check("dead: names whether the site or the page is gone, adds the check's note, offers re-check and trash", () => {
  const a = L.linkAlert(link({ s: "dead", dk: "host", n: "DNS failed" }), c);
  assert.equal(a.title, "Dead: the whole site is gone");
  assert.equal(a.body, "The page did not answer. Check result: DNS failed.");
  assert.deepEqual(acts(a), ["recheck", "trash"]);
  assert.deepEqual(acts(L.linkAlert(link({ s: "dead" }), { ...c, inTrash: true })), ["recheck", "putBack"]);
  assert.deepEqual(acts(L.linkAlert(link({ s: "dead" }), { ...c, readOnly: true })), []);
});
check("moved: another site is purple and warns; same site is a warning", () => {
  const other = L.linkAlert(link({ s: "moved", nu: "https://b.test/", nk: "other" }), c);
  assert.equal(other.tone, "purple"); assert.match(other.body, /^Check it first/); assert.equal(other.actions[0][3], "");
  assert.equal(L.linkAlert(link({ s: "moved", nu: "https://a.test/new" }), c).tone, "warning");
});
check("an applied update can be reverted; a dead link outranks a pending move", () => {
  assert.deepEqual(acts(L.linkAlert(link({ applied: true, prev: "https://old.test/" }), c)), ["revert"]);
  assert.equal(L.linkAlert(link({ s: "dead", nu: "https://b.test/" }), c).tone, "danger");
});
check("not checked: the status's own name and help; only unverified offers Try again", () => {
  const u = L.linkAlert(link({ s: "unverified" }), c);
  assert.equal(u.title, "Not verified"); assert.equal(u.icon, "ti-question-mark"); assert.deepEqual(acts(u), ["retry"]);
  assert.equal(L.linkAlert(link({ s: "needs-login" }), c).tone, "info");
});
check("a working link says nothing, unless it may be a soft 404", () => {
  assert.equal(L.linkAlert(link({}), c), null);
  assert.deepEqual(acts(L.linkAlert(link({ sx: "Title says Page not found" }), c)), ["open"]);
});
check("badges in order", () => assert.deepEqual(L.linkBadges(link({ p: 1, m: 1 }), true).map(b => b[0]),
  ["Working", "Untouched folder", "Filed here because it matches this folder", "Your note"]));
check("the message where a snapshot would be", () => {
  assert.match(L.snapshotMessage(link({ s: "dead" })), /^Dead link/);
  assert.match(L.snapshotMessage(link({ u: "chrome://settings" })), /^Not a web page/);
  assert.match(L.snapshotMessage(link({ s: "local" })), /^Not visited on purpose/);
  assert.equal(L.snapshotMessage(link({})), L.NO_SNAPSHOT);
});
check("live loading refuses this tool and local addresses", () => {
  const self = u => u.startsWith("http://foreman.test");
  assert.equal(L.liveLoad("https://a.test/", self), "ok");
  assert.equal(L.liveLoad("http://localhost:8765/", self), "local");
  assert.equal(L.liveLoad("http://127.0.0.1/", self), "local");
  assert.equal(L.liveLoad("http://foreman.test/x", self), "local");
  assert.equal(L.liveLoad("file:///etc/hosts", self), "web");
});
assert.equal(checks, 8);
console.log(`${checks} link-panel checks passed`);
