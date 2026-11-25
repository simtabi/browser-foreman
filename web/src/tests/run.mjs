// Runs every src/tests/*.test.mjs, one at a time, so a new test file runs without being listed anywhere.
// Run: npm run test:web
import { spawnSync } from "node:child_process";
import { readdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const files = readdirSync(here).filter((f) => f.endsWith(".test.mjs")).sort();
// Measured 2026-11-25: 18 files. Fewer means the folder or the pattern moved, not that the tests are done.
const floor = 18;
if (files.length < floor) {
  console.error(`found ${files.length} test files in ${here}, fewer than ${floor}`);
  process.exit(1);
}
const failed = files.filter((f) => spawnSync(process.execPath, [join(here, f)], { stdio: "inherit" }).status !== 0);
if (failed.length) {
  console.error(`failed: ${failed.join(", ")}`);
  process.exit(1);
}
console.log(`${files.length} test files passed`);
