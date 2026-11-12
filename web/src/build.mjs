// Type-check, then bundle src/web/ts into assets/js/app.js (one ES module, minified, with a source map).
// Usage: node src/build.mjs   (or: npm run build:web, from web/)
import { build } from "esbuild";
import { execFileSync } from "node:child_process";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const root = join(here, "..");  // web/
execFileSync(process.execPath, [join(root, "node_modules", "typescript", "bin", "tsc"), "-p", here], { stdio: "inherit" });
await build({
  entryPoints: [join(here, "ts", "main.ts")],
  outfile: join(root, "dist", "assets", "js", "app.js"),
  bundle: true, format: "esm", target: "es2022", minify: true, sourcemap: true, legalComments: "eof", logLevel: "info",
});
// The Go server's page loads this first (a classic script): it turns the server's tree into the page's data.
await build({
  entryPoints: [join(here, "ts", "v1-boot.ts")],
  outfile: join(root, "dist", "assets", "js", "v1.js"),
  bundle: true, format: "iife", target: "es2022", minify: true, sourcemap: true, legalComments: "eof", logLevel: "info",
});
