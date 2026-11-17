// Talks to this browser's Browser Foreman extension directly, through its page bridge (src/extension/bridge.ts):
// whether it is installed and connected to this page's server, and asking it to connect. Nothing answers in a browser
// without the extension, so a quiet answer means "not installed here" (or installed after this page loaded: the
// bridge joins a page when it loads).
//
//   const h = await extensionHello();          // null when this browser has no extension (or an old one)
//   const r = await extensionConnect();        // { id, match } of the request it made, or { connected: true }

export interface ExtensionHello {
  installed: true;
  version: string;
  browser: string;   // the extension's own name for this browser ("Google Chrome")
  instance: string;  // this copy's short id: two copies of one browser on this computer differ here
  paired: boolean;   // connected to some Browser Foreman
  server: string;    // the one it uses
  here: boolean;     // connected to this page's server
  asking: string;    // the letters of a request it made to this page's server, if any
  declined: boolean; // told to stop asking by itself (denied, or disconnected)
}

let seq = 0;
/** True once the bridge in this page said it is cut off from its extension (reloaded since the page opened). */
export let bridgeStale = false;
function ask(kind: "hello" | "connect", timeoutMs: number): Promise<{ ok: boolean; result?: unknown; error?: string } | null> {
  return new Promise(resolve => {
    const id = `b${Date.now()}-${++seq}`;
    const done = (v: { ok: boolean; result?: unknown; error?: string } | null) => { window.removeEventListener("message", on); clearTimeout(t); resolve(v); };
    const on = (e: MessageEvent) => {
      const m = e.data as { brofo?: string; id?: string; ok?: boolean; result?: unknown; error?: string; stale?: boolean } | null;
      if (e.source === window && m?.brofo === "answer" && m.id === id) { if (m.stale) bridgeStale = true; else if (m.ok) bridgeStale = false; done({ ok: !!m.ok, result: m.result, error: m.error }); }
    };
    const t = setTimeout(() => done(null), timeoutMs);
    window.addEventListener("message", on);
    window.postMessage({ brofo: "ask", id, kind }, location.origin);
  });
}

/** This browser's extension, or null when none answers within half a second. */
export async function extensionHello(): Promise<ExtensionHello | null> {
  const r = await ask("hello", 500);
  return r?.ok ? (r.result as ExtensionHello) : null;
}

/** Asks this browser's extension to connect to this page's server: it makes a request, and says which. */
export async function extensionConnect(): Promise<{ id?: string; match?: string; connected?: boolean }> {
  const r = await ask("connect", 15000);
  if (!r) throw new Error("This browser's extension did not answer. Reload this page and try again.");
  if (!r.ok) throw new Error(r.error || "The extension could not connect.");
  return r.result as { id?: string; match?: string; connected?: boolean };
}

/** Calls fn when the extension's bridge announces itself (a page loaded after the extension was installed). */
export function onExtensionHere(fn: () => void): () => void {
  const on = (e: MessageEvent) => { if (e.source === window && (e.data as { brofo?: string } | null)?.brofo === "here") fn(); };
  window.addEventListener("message", on);
  return () => window.removeEventListener("message", on);
}
