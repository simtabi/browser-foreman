// The older-call translation (src/ts/v1-compat.ts), without a browser. Run: npm run test:web
import { build } from "esbuild";
import assert from "node:assert/strict";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const o = await build({ entryPoints: [join(here, "..", "ts", "v1-compat.ts")], bundle: true, format: "esm", write: false, platform: "neutral" });
const C = await import("data:text/javascript;base64," + Buffer.from(o.outputFiles[0].text).toString("base64"));

const prefs = { open_with: ["System default"], tracked: null, autosave: true, save_mode: "change", interval_seconds: 300, watch_seconds: 15, include_incognito: false, keep_days: 14, recheck_days: 7 };
const calls = [];
const call = async (method, path, body) => {
  calls.push(`${method} ${path}` + (body === undefined ? "" : " " + JSON.stringify(body)));
  if (path === "preferences") return method === "PUT" ? { ...prefs, ...body } : prefs;
  if (path === "browsers") return { installed: [{ name: "Safari", scriptable: true, path: "/Applications/Safari.app" }, { name: "Firefox", scriptable: false, path: "/x" }], default: "Safari", open_with: ["System default"], tracked: null };
  if (path === "open") return { opened: [{ browser: "Safari", ok: true }, { browser: "Arc", ok: false }] };
  if (path === "tabs/close") return { closed: 2, skipped: ["https://x/: changed since it was read"], undo_snapshot: "2026-11-12_090000" };
  if (path === "tabs/snapshots/2026-11-12_090000") return { at: "t", reason: "auto", state: { browsers: [{ browser: "Safari" }], summary: { tabs: 1 } } };
  if (path === "linkchecks") return { running: null, runs: [{ id: "lc_1", state: "done", done: true, total: 8, checked: 8, newly_broken: 2, fixed: 0, started: "s", ended: "e" }] };
  return { passthrough: path };
};
const run = (m, p, q, b) => C.legacy(m, p, q, b, call, "main");

let a = await run("GET", "tabs/browsers", "", undefined);
assert.deepEqual(a.body.installed.map(x => x.path), [undefined, undefined], "paths on this computer are not passed to the page");
assert.deepEqual(a.body.tracked, ["Safari"], "nothing chosen tracks every scriptable browser");
assert.equal(a.body.system_default, "Safari");

a = await run("POST", "open", "", { urls: ["https://go.dev/", "https://pkg.go.dev/"], how: "window", browser: "Safari" });
assert.equal(a.body.opened, 2);
assert.equal(calls.at(-1), 'POST open {"urls":["https://go.dev/","https://pkg.go.dev/"],"how":"window","browsers":["Safari"]}');

a = await run("POST", "tabs/settings", "", { open_with: "Safari", interval: 600, tracked_browsers: ["Safari"], onboarded_browsers: true });
assert.equal(calls.at(-1), 'PUT preferences {"open_with":["Safari"],"interval_seconds":600,"tracked":["Safari"]}', "older names map to v1; onboarded is dropped");
assert.equal(a.body.settings.interval, 600);

a = await run("POST", "tabs/close", "", { items: [{ browser: "Safari", url: "https://x/" }] });
assert.deepEqual([a.body.closed, a.body.skipped, a.body.details[0].why], [2, 1, "https://x/: changed since it was read"]);

a = await run("GET", "tabs/snapshots/2026-11-12_090000", "", undefined);
assert.deepEqual([a.body.reason, a.body.browsers[0].browser], ["auto", "Safari"]);

a = await run("GET", "rechecks", "", undefined);
assert.deepEqual([a.body.recheck_days, a.body.runs[0].run_id, a.body.runs[0].newly_broken, a.body.runs[0].state], [7, "lc_1", 2, "done"]);

a = await run("POST", "rechecks/run", "", {});
assert.equal(calls.at(-1), 'POST linkchecks {"workspace":"main"}');

assert.equal((await run("POST", "tabs/delete", "", {})).status, 501, "unported features answer plainly");
assert.equal(await run("GET", "ws/main/history", "", undefined), null, "unknown calls are not swallowed");
console.log("v1-compat: 13 checks passed");
