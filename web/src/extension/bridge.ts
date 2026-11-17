// The page bridge: a content script on loopback pages (127.0.0.1, localhost; the extension's own host permissions)
// that lets Browser Foreman's page ask this browser's extension two things: "are you here, and connected to me?" and
// "connect to me". It passes only those, only from the page's own window, and the background checks the sender's real
// origin (the browser tells it, not the page). It never runs anything for the page and reads nothing from it.
//
//   page:   window.postMessage({ brofo: "ask", id: "r1", kind: "hello" }, location.origin)
//   bridge: window.postMessage({ brofo: "answer", id: "r1", ok: true, result: { … } }, location.origin)

import { ext } from "./ext";
import { workerGone } from "./core";

const KINDS = new Set(["hello", "connect"]);
// Reloading or updating the extension leaves this script running in pages already open, cut off from it: its next
// sendMessage throws "Extension context invalidated" (at once, not as a rejected promise). From then on it answers
// "stale", so the page can say to reload, instead of an uncaught error on chrome://extensions.
let stale = false;

window.addEventListener("message", (e: MessageEvent) => {
  const m = e.data as { brofo?: string; id?: string; kind?: string } | null;
  if (e.source !== window || e.origin !== location.origin || !m || m.brofo !== "ask" || typeof m.id !== "string" || !KINDS.has(m.kind ?? "")) return;
  const answer = (body: Record<string, unknown>) => window.postMessage({ brofo: "answer", id: m.id, ...body }, location.origin);
  const cutOff = () => { stale = true; answer({ ok: false, stale: true, error: "The extension was reloaded since this page opened: reload this page to reach it." }); };
  if (stale) return cutOff();
  let sent: Promise<unknown>;
  try { sent = ext.runtime.sendMessage({ kind: "page-" + m.kind }); }
  catch (err) { return workerGone(String((err as Error)?.message ?? err)) ? cutOff() : answer({ ok: false, error: String((err as Error)?.message ?? err) }); }
  sent.then((r: unknown) => answer(r as Record<string, unknown>))
    .catch((err: unknown) => { const msg = String((err as Error)?.message ?? err); if (workerGone(msg) && /context invalidated/i.test(msg)) cutOff(); else answer({ ok: false, error: msg }); });
});
// Say so as soon as the page loads, so it need not ask: the page listens for this too.
window.postMessage({ brofo: "here" }, location.origin);
