// Which requests show progress in the button that started them (src/web/ts/busy.ts). Run: npm run test:web
import { build } from "esbuild";
import assert from "node:assert/strict";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const o = await build({ entryPoints: [join(here, "..", "ts", "busy.ts")], bundle: true, format: "esm", write: false, platform: "neutral" });
const B = await import("data:text/javascript;base64," + Buffer.from(o.outputFiles[0].text).toString("base64"));

const cases = [
  ["POST", "api/v1/workspaces/main/merge", true],
  ["PUT", "api/v1/workspaces/main/tree", true],
  ["DELETE", "api/v1/workspaces/x", true],
  ["patch", "api/v1/workspaces/x", true],
  ["GET", "api/v1/workspaces/main/export?format=html", true],
  ["GET", "api/v1/backup", true],
  ["GET", "api/v1/workspaces/main/duplicates.csv", true],
  ["GET", "api/v1/workspaces/main", false],
  ["GET", "api/v1/tabs/snapshots?workspace=main", false],
  ["GET", "api/v1/workspaces/main/history", false],
];
for (const [m, u, want] of cases) assert.equal(B.isWork(m, u), want, `${m} ${u}`);
console.log(`ok busy: ${cases.length} requests classified`);
