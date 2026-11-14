// A bookmark's detail panel rules (src/web/ts/views/link-panel.ts), without a browser. Run: npm run test:web
import { build } from "esbuild";
import assert from "node:assert/strict";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const load = async f => { const o = await build({ entryPoints: [join(here, "..", "ts", f)], bundle: true, format: "esm", write: false, platform: "neutral" });
  return import("data:text/javascript;base64," + Buffer.from(o.outputFiles[0].text).toString("base64")); };
const L = await load("views/link-panel.ts"), Rows = await load("views/tree-rows.ts");

const c = { readOnly: false, inTrash: false, help: { dead: "The page did not answer. More text.", unverified: "Could not tell." },
  statusName: { unverified: "Not verified", "needs-login": "Needs a login" }, statusIcon: { unverified: "ti-question-mark" } };
const link = o => ({ id: 1, u: "https://a.test/", s: "ok", ...o });
let checks = 0;
const check = (name, fn) => { fn(); checks++; console.log("ok", name); };
const acts = a => a?.actions.map(x => x[2]);

check("dead: names whether the site or the page is gone, adds the check's note, offers re-check and trash", () => {
  const a = L.linkAlert(link({ s: "dead", dk: "host", dc: true, n: "DNS failed" }), c);
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
  assert.equal(L.linkAlert(link({ s: "dead", dc: true, nu: "https://b.test/" }), c).tone, "danger");
  assert.match(L.linkAlert(link({ s: "dead", nu: "https://b.test/" }), c).title, /^Dead: /);
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
check("dead but unconfirmed: a warning that says when a check confirms it, and trash is not the red button", () => {
  const a = L.linkAlert(link({ s: "dead", dsn: "2026-11-14T09:00:00Z" }), c);
  assert.equal(a.tone, "warning");
  assert.equal(a.title, "Dead: the page is gone, the site is still up (not confirmed yet)");
  assert.match(a.body, /Found dead once so far: .* only when a check after .+ finds it dead again\./);
  assert.ok(a.body.includes(L.confirmsAfter("2026-11-14T09:00:00Z")));
  assert.equal(a.actions.find(x => x[2] === "trash")[3], "");
  assert.equal(L.linkAlert(link({ s: "dead", dc: true }), c).tone, "danger");
  assert.equal(L.confirmsAfter(""), "");
  assert.equal(L.confirmsAfter("not a date"), "");
});
check("an archived copy is offered first, also read-only, and named where the snapshot would be", () => {
  const ar = "https://web.archive.org/web/2020/https://a.test/";
  assert.deepEqual(acts(L.linkAlert(link({ s: "dead", dc: true, ar }), c)), ["archive", "recheck", "trash"]);
  assert.deepEqual(acts(L.linkAlert(link({ s: "dead", ar }), { ...c, readOnly: true })), ["archive"]);
  assert.match(L.snapshotMessage(link({ s: "dead", ar })), /Internet Archive has a copy/);
});
check("live loading stops at a site that forbids framing", () => {
  assert.equal(L.liveLoad("https://a.test/", () => false, "refused"), "refused");
  assert.equal(L.liveLoad("https://a.test/", () => false, ""), "ok");
  assert.equal(L.liveLoad("http://localhost/", () => false, "refused"), "local");
});
check("a row's dead label carries a ? until confirmed", () => {
  assert.equal(Rows.deadLabel({ dk: "host" }), "site gone?");
  assert.equal(Rows.deadLabel({ dk: "page", dc: true }), "page gone");
});
assert.equal(checks, 12);
console.log(`${checks} link-panel checks passed`);
