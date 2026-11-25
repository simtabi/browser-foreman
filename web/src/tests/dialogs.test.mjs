// The confirm dialog's keys and turns (src/ts/dialogs.ts). Run: npm run test:web
import { build } from "esbuild";
import assert from "node:assert/strict";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const o = await build({ entryPoints: [join(here, "..", "ts", "dialogs.ts")], bundle: true, format: "esm", write: false, platform: "browser" });
globalThis.document ??= { addEventListener() {}, querySelector() { return null; }, querySelectorAll() { return []; } };
const D = await import("data:text/javascript;base64," + Buffer.from(o.outputFiles[0].text).toString("base64"));

let n = 0;
// Enter does what the focused button does: Cancel has the focus when the dialog opens.
const yes = {}, no = {};
assert.equal(D.enterMeans(no, no), false, "Enter on a focused Cancel must cancel"); n++;
assert.equal(D.enterMeans(yes, no), true); n++;
assert.equal(D.enterMeans(null, no), true, "Enter with no button focused means OK"); n++;

// A second question waits for the first to be answered; it never takes the dialog over.
const order = [];
let answerFirst;
const first = D.oneAtATime(() => { order.push("first shown"); return new Promise(r => { answerFirst = r; }); });
const second = D.oneAtATime(async () => { order.push("second shown"); return "no"; });
await new Promise(r => setTimeout(r, 10));
assert.deepEqual(order, ["first shown"], "the second question was shown over the first"); n++;
answerFirst(true);
assert.equal(await first, true); n++;
assert.equal(await second, "no"); n++;
assert.deepEqual(order, ["first shown", "second shown"]); n++;
// A question that fails does not block the ones after it.
const broken = D.oneAtATime(async () => { throw new Error("gone"); });
await assert.rejects(broken); n++;
assert.equal(await D.oneAtATime(async () => 7), 7); n++;
console.log(`dialogs: ${n} checks passed`);
