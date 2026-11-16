// The browser extension's own rules (src/extension/core.ts, apply.ts), without a browser. Run: npm run test:web
import { build } from "esbuild";
import assert from "node:assert/strict";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const load = async f => { const o = await build({ entryPoints: [join(here, "..", "extension", f)], bundle: true, format: "esm", write: false, platform: "neutral" });
  return import("data:text/javascript;base64," + Buffer.from(o.outputFiles[0].text).toString("base64")); };
const C = await load("core.ts"), A = await load("apply.ts");
let n = 0; const ok = (c, m) => { assert.ok(c, m); n++; }; const eq = (a, b, m) => { assert.deepEqual(a, b, m); n++; };

// ---- core ----
eq(C.browserName("Mozilla/5.0 (Macintosh) Gecko/20100101 Firefox/133.0", [], false), "Firefox");
eq(C.browserName("Mozilla/5.0 … Chrome/131.0 Safari/537.36", ["Chromium", "Google Chrome"], false), "Google Chrome");
eq(C.browserName("Mozilla/5.0 … Chrome/131.0 Safari/537.36", ["Chromium", "Google Chrome"], true), "Brave Browser", "Brave says it is Chrome");
eq(C.browserName("Mozilla/5.0 … Chrome/131.0 Safari/537.36 Edg/131.0", ["Microsoft Edge"], false), "Microsoft Edge");
eq(C.browserName("Mozilla/5.0 (Macintosh) AppleWebKit/605 Version/18.1 Safari/605.1.15", [], false), "Safari");
eq(C.checkServer("http://127.0.0.1:8765/x"), "http://127.0.0.1:8765");
// The fingerprint a write checks: the same fixture and value as writeback/extprint_test.go, computed independently.
const vector = [{ id: "0", title: "", children: [
  { id: "1", title: "Bookmarks bar", children: [{ id: "5", title: "A  title ", url: " https://a.example/x " }, { id: "6", title: "Dev", children: [{ id: "7", title: "Café", url: "https://café.example/" }] }] },
  { id: "2", title: "Other bookmarks", children: [] }, { id: "3", title: "Mobile bookmarks", children: [] }] }];
eq(await C.extFingerprint(vector), "6c6d0bc98edc01336972cf52b62c45cecfbd5d86e77eede6c212738b9bb6a758", "the same fingerprint as the server's");
// Connecting by itself: ask when nothing waits, wait while a request is live, rest after a no or three unanswered.
const now = Date.parse("2026-11-16T10:00:00Z"), later = "2026-11-16T10:04:00Z", gone = "2026-11-16T09:59:00Z";
eq(C.connectStep({ paired: true }, now), "none", "connected: nothing to do");
eq(C.connectStep({ paired: false }, now), "ask", "installed and not connected: it asks by itself");
eq(C.connectStep({ paired: false, asking: { expires: later } }, now), "wait", "a request is waiting: it does not ask twice");
eq(C.connectStep({ paired: false, asking: { expires: gone } }, now), "ask", "an expired request: ask again");
eq(C.connectStep({ paired: false, declined: true }, now), "rest", "the owner said no (or disconnected): only Connect asks again");
eq(C.connectStep({ paired: false, unanswered: 3 }, now), "rest", "three unanswered: stop prompting");
eq(C.connectStep({ paired: false, unanswered: 2 }, now), "ask");
for (const bad of ["https://evil.example", "http://192.168.1.4:8765", "file:///x", "nonsense"]) assert.throws(() => C.checkServer(bad), bad), n++;
const rep = C.tabReport([{ id: 1, incognito: false, focused: true, type: "normal", tabs: [{ id: 9, url: "https://go.dev/", title: "Go", active: true, pinned: false, groupId: 4, windowId: 1, index: 0 }] },
  { id: 2, incognito: false, focused: false, type: "devtools", tabs: [] }], [{ id: 4, windowId: 1, title: "Work", color: "blue", collapsed: false }]);
eq([rep.windows.length, rep.windows[0].tabs[0].groupId, rep.groups[0].title], [1, 4, "Work"], "devtools windows are left out");
eq(C.groupColor("magenta"), "grey");
eq(C.webOnly(["https://a/", "file:///etc/passwd", "javascript:x"]), ["https://a/"]);
// the client sends its token and reads errors
const seen = [];
const client = new C.Client("http://127.0.0.1:8765", "brofo_x", async (url, init) => { seen.push([url, init.headers.Authorization]); return { ok: false, status: 423, json: async () => ({ error: "locked" }) }; });
await assert.rejects(client.sendBookmarks([]), /locked/); n++;
eq(seen[0], ["http://127.0.0.1:8765/api/v1/extension/bookmarks", "Bearer brofo_x"]);

// ---- apply: a fake bookmarks API that behaves like the browser's ----
function fake(roots) {
  const byId = new Map(); let next = 1000;
  const index = (n, parentId) => { n.parentId = parentId; byId.set(n.id, n); for (const c of n.children ?? []) index(c, n.id); };
  index(roots[0], undefined);
  const kids = id => byId.get(id).children;
  const detach = id => { const n = byId.get(id); const k = kids(n.parentId); k.splice(k.indexOf(n), 1); };
  const api = {
    calls: 0,
    async create(o) { api.calls++; const n = { id: String(next++), title: o.title, ...(o.url !== undefined ? { url: o.url } : { children: [] }) }; n.parentId = o.parentId; byId.set(n.id, n); kids(o.parentId).splice(o.index ?? kids(o.parentId).length, 0, n); return n; },
    async update(id, o) { api.calls++; Object.assign(byId.get(id), o); },
    async move(id, o) { api.calls++; detach(id); const n = byId.get(id); n.parentId = o.parentId; kids(o.parentId).splice(Math.min(o.index ?? 1e9, kids(o.parentId).length), 0, n); },
    async remove(id) { api.calls++; if (byId.get(id).children?.length) throw new Error("not empty"); detach(id); byId.delete(id); },
    async removeTree(id) { api.calls++; detach(id); byId.delete(id); },
    tree: () => roots,
  };
  return api;
}
const browser = () => [{ id: "0", title: "", children: [
  { id: "1", title: "Bookmarks bar", children: [
    { id: "5", title: "Go", url: "https://go.dev/" },
    { id: "6", title: "Dev", children: [{ id: "7", title: "GitHub", url: "https://github.com/" }, { id: "8", title: "MDN", url: "https://developer.mozilla.org/" }] },
    { id: "9", title: "Old", url: "https://old.example/" }] },
  { id: "2", title: "Other bookmarks", children: [{ id: "10", title: "Gone folder", children: [{ id: "11", title: "x", url: "https://x.example/" }] }] }] }];
// The workspace after editing: Go renamed and moved into Dev; Dev reordered; a new folder with a bookmark; "Old" and
// "Gone folder" deleted (or trashed); GitHub moved to Other bookmarks.
const ws = { id: "root", kind: "folder", title: "", children: [
  { id: "ext-1", kind: "folder", title: "Bookmarks bar", children: [
    { id: "ext-6", kind: "folder", title: "Dev", children: [
      { id: "ext-8", kind: "bookmark", title: "MDN", url: "https://developer.mozilla.org/" },
      { id: "ext-5", kind: "bookmark", title: "The Go site", url: "https://go.dev/doc/" }] },
    { id: "f-new", kind: "folder", title: "Reading", children: [{ id: "b-new", kind: "bookmark", title: "Blog", url: "https://blog.example/" }] }] },
  { id: "ext-2", kind: "folder", title: "Other bookmarks", children: [{ id: "ext-7", kind: "bookmark", title: "GitHub", url: "https://github.com/" },
    { id: "ext-11", kind: "bookmark", title: "x", url: "https://x.example/" }] }] }; // x leaves "Gone folder" before it is removed

const shape = nodes => nodes.map(x => x.url !== undefined ? `${x.title}<${x.url}>` : `${x.title}(${shape(x.children ?? [])})`).join(" ");
const wsShape = nodes => nodes.map(x => x.kind === "bookmark" ? `${x.title}<${x.url}>` : `${x.title}(${wsShape(x.children ?? [])})`).join(" ");

const b = fake(browser());
const p = A.plan(b.tree(), ws);
eq([p.created, p.updated, p.removedBookmarks, p.removedFolders], [2, 1, 1, 1], "2 created, 1 renamed+readdressed; Old removed, and the emptied folder");
const made = await A.run(p, b);
eq(shape(b.tree()[0].children), wsShape(ws.children), "the browser now holds exactly the workspace");
eq(A.plan(b.tree(), ws, made).steps.length, 0, "planning again after a run has nothing left to do");
ok(made.get("f-new") && made.get("b-new"), "created nodes are reported for the re-read");

// A workspace not read from this browser is refused rather than deleting everything.
assert.throws(() => A.plan(browser(), { id: "root", kind: "folder", title: "", children: [{ id: "n1", kind: "folder", title: "Bar", children: [] }] }), /not read from this browser/); n++;
// The browser's own top folders are never moved or removed, even when the workspace leaves one out.
const keepTops = A.plan(browser(), { id: "root", kind: "folder", title: "", children: [{ id: "ext-1", kind: "folder", title: "Bookmarks bar", children: [] }] });
ok(!keepTops.steps.some(s => s.id === "1" || s.id === "2"), "roots are left alone");
// The icon: a real PNG of the asked size, mostly the mark's blue with white in the middle.
const { iconPNG } = await import(join(here, "..", "extension-icon.mjs"));
for (const size of [16, 128]) {
  const png = iconPNG(size);
  eq([png.subarray(1, 4).toString(), png.readUInt32BE(16), png.readUInt32BE(20)], ["PNG", size, size], `icon ${size}`);
}
// The page bridge: only pages on this computer; a connected extension is never pulled to another server; a page can
// use only the two bridge requests, and the popup never them.
ok(C.isLoopback("http://127.0.0.1:8765") && C.isLoopback("http://localhost:9000") && C.isLoopback("http://[::1]:8765"), "loopback pages");
ok(!C.isLoopback("https://example.com") && !C.isLoopback("http://192.168.1.4:8765") && !C.isLoopback("file:///x") && !C.isLoopback("nonsense"), "anything else");
eq(C.connectRefusal("http://127.0.0.1:8765", "http://127.0.0.1:8765", true), "", "the server it uses, again");
eq(C.connectRefusal("http://127.0.0.1:8768", "http://127.0.0.1:8765", false), "", "unconnected: the asking page's server, which must still approve it");
ok(/disconnect it there/.test(C.connectRefusal("http://127.0.0.1:8768", "http://127.0.0.1:8765", true)), "connected: never taken by another port");
ok(/on this computer/.test(C.connectRefusal("https://evil.example", "http://127.0.0.1:8765", false)), "not from the web");
ok(C.messageAllowed(true, "page-hello") && C.messageAllowed(true, "page-connect"), "a page's two requests");
ok(!C.messageAllowed(true, "pair") && !C.messageAllowed(true, "apply") && !C.messageAllowed(true, "unpair") && !C.messageAllowed(true, undefined), "nothing else from a page");
ok(!C.messageAllowed(false, "page-connect") && C.messageAllowed(false, "status"), "the popup keeps its own, not the bridge's");
// A stopped or replaced service worker is not an error to show (chrome://extensions › Errors listed "No SW").
ok(C.workerGone("No SW"), "Chrome's own words");
ok(C.workerGone("Error: Extension context invalidated."), "the context is gone");
ok(!C.workerGone("fetch failed: 401"), "a real failure is still one");
// Nothing in the background is fired and forgotten: an un-awaited call that fails becomes an uncaught error. Every
// such call goes through background(), which catches. Counted on the source so a new `void report()` fails here.
{
  const { readFileSync } = await import("node:fs");
  const src = readFileSync(join(here, "..", "extension", "background.ts"), "utf8");
  const forgotten = src.split("\n").filter(l => /\bvoid [a-zA-Z]+\(/.test(l) && !l.trim().startsWith("//"));
  eq(forgotten, [], "fire-and-forget calls in background.ts");
  ok((src.match(/background\(/g) || []).length >= 6, "the guarded calls are there to count");
}
console.log(`extension: ${n} checks passed`);
