// The one look for buttons (audits/2026-11-16-ui-consistency.md, rule 1), checked over every page source:
// no outline buttons; an icon-only button is a ghost (except in the header toolbar, whose icon buttons are a bordered
// set, and a split button's dropdown arrow). Run: npm run test:web
import assert from "node:assert/strict";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const src = join(dirname(fileURLToPath(import.meta.url)), "..");
const walk = d => readdirSync(d).flatMap(n => { const p = join(d, n); return statSync(p).isDirectory() ? walk(p) : p.endsWith(".ts") ? [p] : []; });
const files = [...walk(join(src, "ts")), join(src, "template.html")];
assert.ok(files.length >= 40, `only ${files.length} files scanned: the sweep is not reading the sources`);

let classes = 0;
const bad = [];
for (const f of files) {
  let text = readFileSync(f, "utf8");
  if (f.endsWith("template.html")) text = text.replace(/<header class="navbar app-head[\s\S]*?<\/header>/, m => m.replace(/[^\n]/g, " ")); // the header keeps its bordered set
  text.split("\n").forEach((line, i) => {
    for (const m of line.matchAll(/class(?:Name)?="([^"]*\bbtn\b[^"]*)"|"(btn [^"]*)"/g)) {
      const c = m[1] ?? m[2]; classes++;
      if (/\bbtn-outline-/.test(c)) bad.push(`${f}:${i + 1} outline button: ${c}`);
      if (/\bbtn-icon\b/.test(c) && !/\bbtn-ghost-/.test(c) && !/\bdropdown-toggle\b/.test(c)) bad.push(`${f}:${i + 1} icon-only button that is not a ghost: ${c}`);
    }
  });
}
// Rule 7: every notification says what kind it is (success, info, warning or danger), so a confirmation never looks
// like a note. Calls are read whole, across lines.
const kindless = [];
let toasts = 0;
for (const f of files) {
  const t = readFileSync(f, "utf8");
  for (let at = t.indexOf("toast("); at >= 0; at = t.indexOf("toast(", at + 6)) {
    if (/(function |\.|[A-Za-z_$])$/.test(t.slice(Math.max(0, at - 9), at)) && !/[^A-Za-z_$.]$/.test(t.slice(at - 1, at))) continue; // a definition or another name ending in toast
    let i = at + 6, depth = 1, q = null, args = 1;
    for (; i < t.length && depth; i++) {
      const c = t[i];
      if (q) { if (c === "\\") i++; else if (c === q) q = null; continue; }
      if (c === '"' || c === "'" || c === "`") q = c;
      else if (c === "(" || c === "[" || c === "{") depth++;
      else if (c === ")" || c === "]" || c === "}") depth--;
      else if (c === "," && depth === 1) args++;
    }
    toasts++;
    if (args < 2) kindless.push(`${f}:${t.slice(0, at).split("\n").length} ${t.slice(at, Math.min(i, at + 80)).replace(/\s+/g, " ")}`);
  }
}
assert.ok(toasts >= 40, `only ${toasts} notifications found: the scan no longer matches how they are written`);
assert.deepEqual(kindless, [], "notifications that do not say their kind:\n" + kindless.join("\n"));
// Rule 2: every dialog's close X has a label, and every dialog built in v1-ui.ts has a title icon (as the page's own).
const tpl = readFileSync(join(src, "template.html"), "utf8");
const closes = [...tpl.matchAll(/<button[^>]*class="btn-close"[^>]*>/g)].map(m => m[0]);
assert.ok(closes.length >= 5, `only ${closes.length} close buttons found in template.html`);
assert.deepEqual(closes.filter(c => !c.includes("aria-label")), [], "close buttons without an aria-label");
const v1 = readFileSync(join(src, "ts", "v1-ui.ts"), "utf8");
const built = [...v1.matchAll(/\bmodal\("([A-Za-z0-9]+)",[^;]*/g)];
assert.ok(built.length >= 6, `only ${built.length} built dialogs found`);
assert.deepEqual(built.filter(m => !/"ti-[a-z0-9-]+"\)\s*$|"ti-[a-z0-9-]+"\);?/.test(m[0])).map(m => m[1]), [], "dialogs built without a title icon");
// Rule 6: corners come from the theme: its two radius tokens, a pill (999px), a circle (50%), none or inherited.
const style = tpl.slice(tpl.indexOf("<style>"), tpl.indexOf("</style>"));
const radii = [...style.matchAll(/border-radius:\s*([^;}]+)/g)].map(m => m[1].trim());
assert.ok(radii.length >= 40, `only ${radii.length} border-radius rules found in the page's styles`);
assert.deepEqual(radii.filter(v => !/^(var\(--tblr-[a-z-]*radius[a-z-]*(, *var\(--tblr-[a-z-]*radius[a-z-]*\))?\)|999px|50%|0|inherit)$/.test(v)), [], "corner radii off the theme's scale");
assert.ok(classes >= 100, `only ${classes} button classes found: the pattern no longer matches how buttons are written`);
assert.deepEqual(bad, [], "buttons off the one look:\n" + bad.join("\n"));
console.log(`ui-rules: ${classes} buttons and ${toasts} notifications in ${files.length} files follow the one look`);
