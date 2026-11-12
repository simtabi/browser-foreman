// Runs before the page script when the Go server serves the page (it sets window.BF_V1). It turns the server's tree
// into the page's data (window.BF_PAGE, read by the page as DATA) and installs window.BM_WS, the workspace hooks the
// page already uses: the page's saves become PUT /api/v1/workspaces/{id}/tree, undo and redo go to the server.

import { toPage, fromPage, type V1Tree, type PageState } from "./v1";

interface Boot {
  workspace: { id: string; name: string; head: number };
  tree: V1Tree;
  built: string;
  can: { undo: string | null; redo: string | null };
  auth: { token?: string; csrf?: string };
}

declare global { interface Window { BF_V1?: Boot; BF_PAGE?: unknown; BM_WS?: unknown } }

const boot = window.BF_V1;
if (boot) {
  const page = toPage(boot.tree, boot.built);
  window.BF_PAGE = page.data;
  let state: PageState = page.state, urls: Record<string, string> = {}, head = boot.workspace.head;
  const base = `api/v1/workspaces/${encodeURIComponent(boot.workspace.id)}`;
  const headers = (): Record<string, string> => {
    const h: Record<string, string> = { "Content-Type": "application/json" };
    if (boot.auth.token) h.Authorization = `Bearer ${boot.auth.token}`;
    if (boot.auth.csrf) h["X-CSRF-Token"] = boot.auth.csrf;
    return h;
  };
  const newId = (): string => "f" + Array.from(crypto.getRandomValues(new Uint8Array(6)), b => b.toString(16).padStart(2, "0")).join("");

  const banner = (text: string, kind: "danger" | "warning"): void => {
    let el = document.getElementById("bfSaveBanner");
    if (!el) {
      el = document.createElement("div");
      el.id = "bfSaveBanner";
      el.setAttribute("role", "alert");
      el.style.cssText = "position:fixed;left:50%;bottom:16px;transform:translateX(-50%);z-index:3000;max-width:min(560px,calc(100vw - 32px))";
      document.body.append(el);
    }
    el.className = `alert alert-${kind} shadow mb-0`;
    el.textContent = text;
    if (kind === "warning") {
      const b = document.createElement("button");
      b.className = "btn btn-sm ms-2"; b.textContent = "Reload"; b.onclick = () => location.reload();
      el.append(b);
    }
  };

  let timer: ReturnType<typeof setTimeout> | undefined, sending: Promise<void> | null = null, dirty = false;
  const send = async (): Promise<void> => {
    dirty = false;
    const body = JSON.stringify(fromPage(state, urls, page, newId));
    const r = await fetch(`${base}/tree`, { method: "PUT", headers: { ...headers(), "If-Match": `"${head}"` }, body, keepalive: body.length < 60_000 });
    if (r.status === 412) return banner("These bookmarks were changed somewhere else since this page loaded. Reload to see them; your last edit was not saved.", "warning");
    const j = await r.json().catch(() => ({})) as { error?: string; entry?: { seq: number; summary: string } | null };
    if (!r.ok) return banner(`Not saved: ${j.error ?? `the server answered ${r.status}`}`, "danger");
    if (j.entry) { head = j.entry.seq; ws.can = { undo: j.entry.summary, redo: null }; }
    document.getElementById("bfSaveBanner")?.remove();
  };
  const flush = (): void => {
    clearTimeout(timer);
    if (sending) { dirty = true; return; }
    sending = send().catch(e => banner(`Not saved: ${e}`, "danger")).finally(() => { sending = null; if (dirty) flush(); });
  };
  const schedule = (): void => { clearTimeout(timer); timer = setTimeout(flush, 400); };
  document.addEventListener("visibilitychange", () => { if (document.visibilityState === "hidden" && timer) flush(); });

  const step = (verb: "undo" | "redo") => async (): Promise<void> => {
    if (sending) await sending;
    const r = await fetch(`${base}/${verb}`, { method: "POST", headers: headers() });
    if (r.ok) return location.reload();
    const j = await r.json().catch(() => ({})) as { error?: string };
    banner(j.error ?? `The server answered ${r.status}`, "danger");
  };

  const ws = {
    v1: true,
    id: boot.workspace.id,
    can: boot.can,
    owns: (k: string): boolean => k === "urls" || k.startsWith("tree-"),
    get: (k: string): unknown => k === "urls" ? urls : k.startsWith("tree-") ? state : null,
    set: (k: string, v: unknown): void => {
      if (k === "urls") urls = (v as Record<string, string>) ?? {};
      else if (k.startsWith("tree-")) state = (v as PageState) ?? page.state;
      else return;
      schedule();
    },
    undo: step("undo"),
    redo: step("redo"),
  };
  window.BM_WS = ws;
}
