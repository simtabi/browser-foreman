// The server settings panel's labels and summaries (src/ts/server-settings.ts). Run: npm run test:web
import { build } from "esbuild";
import assert from "node:assert/strict";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const o = await build({ entryPoints: [join(here, "..", "ts", "server-settings.ts")], bundle: true, format: "esm", write: false, platform: "browser" });
globalThis.window ??= globalThis; globalThis.document ??= { addEventListener() {}, querySelector() { return null; } };
const S = await import("data:text/javascript;base64," + Buffer.from(o.outputFiles[0].text).toString("base64"));

let n = 0;
const eq = (a, b, m) => { assert.equal(a, b, m); n++; };
eq(S.settingLabel({ key: "ai.openai_url", group: "ai" }), "OpenAI URL");
eq(S.settingLabel({ key: "OPENAI_API_KEY", group: "ai" }), "OpenAI API key");
eq(S.settingLabel({ key: "webhooks.attempts", group: "webhooks" }), "Attempts");
eq(S.shortValue({ type: "bool", value: true }), "on");
eq(S.shortValue({ type: "secret", set: false }), "not set");
eq(S.shortValue({ type: "list", value: [] }), "none");
eq(S.shortValue({ type: "string", value: "" }), "default");
eq(S.sourceText({ type: "secret", set: true, source: ".env" }), "saved in .env");
eq(S.sourceText({ type: "int", source: "config.json" }), "from config.json");
// Every provider the server accepts is offered, "none" first.
eq(S.PROVIDERS.map(p => p.id).join(","), "none,anthropic,openai,ollama");
eq(S.GROUP_ORDER.includes("ai"), false, "AI is drawn on its own, first");
console.log(`server-settings: ${n} checks passed`);
