// Builds the browser extension from src/extension into dist/extension/{chromium,firefox,safari}: one source, one
// manifest per browser. Usage: node src/build-extension.mjs   (or: npm run build:extension, from web/)
import { build } from "esbuild";
import { execFileSync } from "node:child_process";
import { mkdirSync, copyFileSync, writeFileSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { iconPNG } from "./extension-icon.mjs";

const here = dirname(fileURLToPath(import.meta.url));
const root = join(here, "..");
const src = join(here, "extension");
const version = JSON.parse(readFileSync(join(root, "package.json"), "utf8")).version;
execFileSync(process.execPath, [join(root, "node_modules", "typescript", "bin", "tsc"), "-p", src], { stdio: "inherit" });

const hosts = ["http://127.0.0.1/*", "http://localhost/*"];
const base = {
  manifest_version: 3, name: "Browser Foreman", version,
  description: "Your tabs and bookmarks, kept by Browser Foreman on this computer.",
  host_permissions: hosts,
  icons: { 16: "icons/16.png", 32: "icons/32.png", 48: "icons/48.png", 128: "icons/128.png" },
  action: { default_popup: "popup.html", default_title: "Browser Foreman", default_icon: { 16: "icons/16.png", 32: "icons/32.png" } },
};
const manifests = {
  chromium: { ...base, permissions: ["tabs", "tabGroups", "bookmarks", "storage", "alarms"], background: { service_worker: "background.js" }, minimum_chrome_version: "116" },
  firefox: { ...base, permissions: ["tabs", "bookmarks", "storage", "alarms"], background: { scripts: ["background.js"] },
    browser_specific_settings: { gecko: { id: "{8c5f2b9e-4f3a-4d0b-9a61-3e2c7b1d6f40}", strict_min_version: "121.0" } } },
  safari: { ...base, permissions: ["tabs", "bookmarks", "storage", "alarms"], background: { service_worker: "background.js" } },
};
for (const [target, manifest] of Object.entries(manifests)) {
  const out = join(root, "dist", "extension", target);
  mkdirSync(out, { recursive: true });
  for (const entry of ["background", "popup"]) {
    await build({ entryPoints: [join(src, `${entry}.ts`)], outfile: join(out, `${entry}.js`), bundle: true, format: "iife",
      target: "es2022", minify: false, legalComments: "eof", logLevel: "warning" });
  }
  copyFileSync(join(src, "popup.html"), join(out, "popup.html"));
  mkdirSync(join(out, "icons"), { recursive: true });
  for (const size of [16, 32, 48, 128]) writeFileSync(join(out, "icons", `${size}.png`), iconPNG(size));
  writeFileSync(join(out, "manifest.json"), JSON.stringify(manifest, null, 2) + "\n");
  console.log(`extension: dist/extension/${target}`);
}
