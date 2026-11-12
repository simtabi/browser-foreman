// Runs before the page script when the Go server serves the page (it sets window.BF_V1). It turns the server's tree
// into the page's data (window.BF_PAGE, read by the page as DATA) and installs window.BM_WS, the workspace hooks the
// page already uses: the page's saves become PUT /api/v1/workspaces/{id}/tree, undo and redo go to the server.

import { toPage, fromPage, linkFields, type V1Tree, type V1Link, type PageState } from "./v1";

export interface WorkspaceInfo { id: string; name: string; head: number; notes?: string; locked?: boolean; archived?: boolean }
interface Boot {
  workspace: WorkspaceInfo;
  workspaces: WorkspaceInfo[];
  tree: V1Tree;
  built: string;
  can: { undo: string | null; redo: string | null };
  auth: { token?: string; csrf?: string };
  links: Record<string, V1Link>;
}
interface CheckView { id: string; state: string; total: number; checked: number; started: string; done: boolean }

declare global { interface Window { BF_V1?: Boot; BF_PAGE?: unknown; BM_WS?: unknown } }

const boot = window.BF_V1;
if (boot) {
  const page = toPage(boot.tree, boot.built, boot.links ?? {});
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

  const call = async <T>(method: string, path: string, body?: unknown): Promise<T> => {
    const r = await fetch(`api/v1/${path}`, { method, headers: headers(), body: body === undefined ? undefined : JSON.stringify(body) });
    const j = await r.json().catch(() => ({})) as T & { error?: string };
    if (!r.ok) throw new Error(j.error ?? `the server answered ${r.status}`);
    return j;
  };
  // Link checks, in the shape the page's activity panel reads (it was written for the Python server's jobs).
  const pageIdsByURL = new Map<string, number[]>();
  for (const i of page.data.keep) pageIdsByURL.set(i.u, [...(pageIdsByURL.get(i.u) ?? []), i.id]);
  const refresh = async (ids: number[]): Promise<{ job: string; total: number }> => {
    const nodes = ids.map(id => page.ids[id - 1]).filter((x): x is string => !!x);
    const { job } = await call<{ job: CheckView }>("POST", "linkchecks", { workspace: boot.workspace.id, nodes });
    return { job: job.id, total: job.total };
  };
  const job = async (id: string): Promise<Record<string, unknown>> => {
    let j: { job: CheckView; results?: Record<string, V1Link> };
    try { j = await call("GET", `linkchecks/${encodeURIComponent(id)}`); } catch { return { done: true, state: "lost", results: {}, rendered: 0 }; }
    const v = j.job;
    const results: Record<string, unknown> = {};
    if (v.done) {
      j = await call("GET", `linkchecks/${encodeURIComponent(id)}?results=1`);
      for (const [url, l] of Object.entries(j.results ?? {})) for (const pid of pageIdsByURL.get(url) ?? []) results[pid] = linkFields(l);
    }
    return { state: v.state === "done" ? "done" : v.state, phase: "checking", checked: v.checked, total: v.total, done: v.done,
      started: Date.parse(v.started) / 1000, results, rendered: 0, render_total: 0 };
  };
  const ctl = (id: string, verb: string): Promise<unknown> => call("POST", `linkchecks/${encodeURIComponent(id)}/${verb}`).catch(() => null);

  const ws = {
    refresh, job, ctl,
    v1: true,
    call,
    headers,
    workspace: boot.workspace,
    workspaces: boot.workspaces ?? [],
    locked: !!boot.workspace.locked,
    id: boot.workspace.id,
    can: boot.can,
    // "refreshed" (link results the page kept itself) is claimed and ignored: the server keeps results by address and
    // sends them with the page, while the page's numbers for bookmarks change from one load to the next.
    owns: (k: string): boolean => k === "urls" || k === "refreshed" || k.startsWith("tree-"),
    get: (k: string): unknown => k === "urls" ? urls : k.startsWith("tree-") ? state : null,
    set: (k: string, v: unknown): void => {
      if (k === "refreshed") return;
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
