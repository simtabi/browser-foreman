// page.ts is still `// @ts-nocheck` (the TypeScript move types it as it is split up), so the compiler never told us
// when a name it uses stopped existing. On 2026-11-13 an edit deleted setFilter and paintChipCounts; the page threw
// on every load and a `try {}` hid one of them. This compiles a copy without the nocheck line and fails on any
// "Cannot find name" (TS2304 / TS2552) only; every other error stays out of scope until the file is typed.
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const src = path.dirname(path.dirname(fileURLToPath(import.meta.url)));
const page = path.join(src, "ts/page/page.ts");
const text = fs.readFileSync(page, "utf8");
assert.ok(text.split("\n").length > 1500, "page.ts is the size this check expects (it reads the real file)");

const dir = fs.mkdtempSync(path.join(os.tmpdir(), "brofo-page-names-"));
try {
  fs.writeFileSync(path.join(dir, "page.ts"), text.replace(/^\/\/ @ts-nocheck\n/, ""));
  const cfg = JSON.parse(fs.readFileSync(path.join(src, "tsconfig.json"), "utf8"));
  delete cfg.include;
  cfg.files = ["page.ts"];
  Object.assign(cfg.compilerOptions, { strict: false, noUncheckedIndexedAccess: false });
  fs.writeFileSync(path.join(dir, "tsconfig.json"), JSON.stringify(cfg));
  const tsc = path.join(src, "..", "node_modules/typescript/bin/tsc");
  let out = "";
  try { out = execFileSync(process.execPath, [tsc, "-p", dir], { encoding: "utf8" }); } catch (e) { out = String(e.stdout || "") + String(e.stderr || ""); }
  // Proof tsc ran on the copy: its relative imports cannot resolve from the temp folder, so TS2307 always appears.
  assert.match(out, /page\.ts\(\d+,\d+\): error TS2307/, "tsc did not check the copy:\n" + out.slice(0, 400));
  const missing = out.split("\n").filter(l => /TS2304|TS2552/.test(l)).map(l => l.replace(/^.*page\.ts/, "page.ts"));
  assert.deepEqual(missing, [], "page.ts uses names that no longer exist:\n" + missing.join("\n"));
  console.log("page names: every name page.ts uses exists");
} finally {
  fs.rmSync(dir, { recursive: true, force: true });
}
