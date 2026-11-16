// The page bridge: a content script on loopback pages (127.0.0.1, localhost; the extension's own host permissions)
// that lets Browser Foreman's page ask this browser's extension two things: "are you here, and connected to me?" and
// "connect to me". It passes only those, only from the page's own window, and the background checks the sender's real
// origin (the browser tells it, not the page). It never runs anything for the page and reads nothing from it.
//
//   page:   window.postMessage({ brofo: "ask", id: "r1", kind: "hello" }, location.origin)
//   bridge: window.postMessage({ brofo: "answer", id: "r1", ok: true, result: { … } }, location.origin)

import { ext } from "./ext";

const KINDS = new Set(["hello", "connect"]);

window.addEventListener("message", (e: MessageEvent) => {
  const m = e.data as { brofo?: string; id?: string; kind?: string } | null;
  if (e.source !== window || e.origin !== location.origin || !m || m.brofo !== "ask" || typeof m.id !== "string" || !KINDS.has(m.kind ?? "")) return;
  const answer = (body: Record<string, unknown>) => window.postMessage({ brofo: "answer", id: m.id, ...body }, location.origin);
  ext.runtime.sendMessage({ kind: "page-" + m.kind })
    .then((r: unknown) => answer(r as Record<string, unknown>))
    .catch((err: unknown) => answer({ ok: false, error: String((err as Error)?.message ?? err) }));
});
// Say so as soon as the page loads, so it need not ask: the page listens for this too.
window.postMessage({ brofo: "here" }, location.origin);
