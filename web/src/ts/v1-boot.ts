// Runs before the page script when the Go server serves the page (it sets window.BF_V1). It turns the server's tree
// into the page's data (window.BF_PAGE, read by the page as DATA) and installs window.BM_WS, the workspace hooks the
// page already uses: the page's saves become PUT /api/v1/workspaces/{id}/tree, undo and redo go to the server.

import { type Media, toPage, fromPage, linkFields, type V1Tree, type V1Link, type PageState } from "./v1";
import { legacy } from "./v1-compat";

export interface WorkspaceInfo { id: string; name: string; head: number; notes?: string; locked?: boolean; archived?: boolean }
interface Boot {
  workspace: WorkspaceInfo;
  workspaces: WorkspaceInfo[];
  tree: V1Tree;
  built: string;
  can: { undo: string | null; redo: string | null };
  auth: { token?: string; csrf?: string };
  links: Record<string, V1Link>;
  media?: Media;
}
interface CheckView { id: string; state: string; total: number; checked: number; started: string; done: boolean }

declare global { interface Window { BF_V1?: Boot; BF_PAGE?: unknown; BM_WS?: unknown } }

const boot = window.BF_V1;
if (boot) {
  const page = toPage(boot.tree, boot.built, boot.links ?? {}, boot.media);
  window.BF_PAGE = page.data;
  let state: PageState = page.state, urls: Record<string, string> = {}, head = boot.workspace.head;
  const base = `api/v1/workspaces/${encodeURIComponent(boot.workspace.id)}`;
  const headers = (): Record<string, string> => {
    const h: Record<string, string> = { "Content-Type": "application/json" };
    if (boot.auth.token) h.Authorization = `Bearer ${boot.auth.token}`;
    if (boot.auth.csrf) h["X-CSRF-Token"] = boot.auth.csrf;
    return h;
  };
  // This is the workspace in use now: background tab snapshots are saved into its pool (its own, or the shared one).
  // A viewer may not change that, and the page works the same either way.
  void fetch(`${base}/open`, { method: "POST", headers: headers() }).catch(() => undefined);
  const newId = (): string => "f" + Array.from(crypto.getRandomValues(new Uint8Array(6)), b => b.toString(16).padStart(2, "0")).join("");

  // A save the server refused stays on screen until it is dealt with: a Tabler alert with what happened and what to
  // do, never a toast that times out over an unsaved edit.
  type Act = { label: string; run: () => void; primary?: boolean };
  const banner = (title: string, text: string, kind: "danger" | "warning", acts: Act[] = []): void => {
    document.getElementById("bfSaveBanner")?.remove();
    const el = document.createElement("div");
    el.id = "bfSaveBanner"; el.setAttribute("role", "alert");
    el.className = `alert alert-${kind} alert-dismissible bg-surface shadow-lg mb-0 bf-save-banner`;
    const row = document.createElement("div"); row.className = "d-flex gap-3";
    const ic = document.createElement("i"); ic.className = `ti ${kind === "danger" ? "ti-alert-circle" : "ti-alert-triangle"} alert-icon fs-2`;
    const box = document.createElement("div"); box.className = "min-w-0";
    const h = document.createElement("h4"); h.className = "alert-title"; h.textContent = title;
    const d = document.createElement("div"); d.className = "text-secondary"; d.textContent = text;
    box.append(h, d);
    if (acts.length) {
      const list = document.createElement("div"); list.className = "btn-list mt-3";
      for (const a of acts) {
        const b = document.createElement("button"); b.type = "button";
        b.className = `btn btn-sm ${a.primary ? `btn-${kind}` : ""}`; b.textContent = a.label; b.onclick = a.run;
        list.append(b);
      }
      box.append(list);
    }
    const x = document.createElement("button"); x.type = "button"; x.className = "btn-close"; x.setAttribute("aria-label", "Close"); x.onclick = () => el.remove();
    row.append(ic, box); el.append(row, x);
    document.body.append(el);
  };
  // Keep my change: the other change stays in the history (undo or History bring it back); this edit goes on top.
  const keepMine = async (): Promise<void> => {
    try {
      const r = await fetch(base, { headers: headers() });
      const j = await r.json() as { workspace?: { head: number } };
      if (!r.ok || !j.workspace) throw new Error(`the server answered ${r.status}`);
      head = j.workspace.head; document.getElementById("bfSaveBanner")?.remove(); flush();
    } catch (e) { banner("Not saved", String(e), "danger"); }
  };

  let timer: ReturnType<typeof setTimeout> | undefined, sending: Promise<void> | null = null, dirty = false;
  const send = async (): Promise<void> => {
    dirty = false;
    const body = JSON.stringify(fromPage(state, urls, page, newId));
    const r = await fetch(`${base}/tree`, { method: "PUT", headers: { ...headers(), "If-Match": `"${head}"` }, body, keepalive: body.length < 60_000 });
    if (r.status === 412) return banner("Changed somewhere else", "Another window, the extension or the API changed these bookmarks after this page loaded, so your last edit was not saved yet.", "warning",
      [{ label: "Reload", run: () => location.reload(), primary: true }, { label: "Keep my change", run: () => void keepMine() }]);
    const j = await r.json().catch(() => ({})) as { error?: string; entry?: { seq: number; summary: string } | null };
    if (!r.ok) return banner("Not saved", j.error ?? `The server answered ${r.status}.`, "danger", [{ label: "Try again", run: () => flush(), primary: true }]);
    if (j.entry) { head = j.entry.seq; ws.can = { undo: j.entry.summary, redo: null }; }
    document.getElementById("bfSaveBanner")?.remove();
  };
  const flush = (): void => {
    clearTimeout(timer);
    if (sending) { dirty = true; return; }
    sending = send().catch(e => banner("Not saved", `The server could not be reached (${e}). Your edit is still on this page.`, "danger", [{ label: "Try again", run: () => flush(), primary: true }])).finally(() => { sending = null; if (dirty) flush(); });
  };
  const schedule = (): void => { clearTimeout(timer); timer = setTimeout(flush, 400); };
  document.addEventListener("visibilitychange", () => { if (document.visibilityState === "hidden" && timer) flush(); });

  const step = (verb: "undo" | "redo") => async (): Promise<void> => {
    if (sending) await sending;
    const r = await fetch(`${base}/${verb}`, { method: "POST", headers: headers() });
    if (r.ok) return location.reload();
    const j = await r.json().catch(() => ({})) as { error?: string };
    banner(verb === "undo" ? "Nothing undone" : "Nothing redone", j.error ?? `The server answered ${r.status}.`, "danger");
  };

  const call = async <T>(method: string, path: string, body?: unknown): Promise<T> => {
    const r = await fetch(`api/v1/${path}`, { method, headers: headers(), body: body === undefined ? undefined : JSON.stringify(body) });
    const j = await r.json().catch(() => ({})) as T & { error?: string };
    if (!r.ok) throw new Error(j.error ?? `the server answered ${r.status}`);
    return j;
  };
  // The page's older calls (api/tabs, api/open, api/rechecks…) are answered through /api/v1 (v1-compat.ts).
  const nativeFetch = window.fetch.bind(window);
  const callStatus = async (method: string, path: string, body?: unknown): Promise<any> => {
    const r = await nativeFetch(`api/v1/${path}`, { method, headers: headers(), body: body === undefined ? undefined : JSON.stringify(body) });
    const j = await r.json().catch(() => ({}));
    if (!r.ok) throw Object.assign(new Error(j.error ?? `the server answered ${r.status}`), { status: r.status });
    return j;
  };
  window.fetch = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const url = typeof input === "string" ? input : input instanceof URL ? input.href : input.url;
    const rel = url.replace(location.origin + "/", "").replace(/^\.?\//, "");
    if (!rel.startsWith("api/") || rel.startsWith("api/v1/")) return nativeFetch(input, init);
    const [path, query = ""] = rel.slice(4).split("?");
    let body: unknown;
    try { body = typeof init?.body === "string" ? JSON.parse(init.body) : undefined; } catch { body = undefined; }
    try {
      const a = await legacy(init?.method ?? "GET", path ?? "", query, body, callStatus, boot.workspace.id);
      if (!a) return new Response(JSON.stringify({ error: "not available with this server" }), { status: 404, headers: { "Content-Type": "application/json" } });
      return new Response(JSON.stringify(a.body), { status: a.status, headers: { "Content-Type": "application/json" } });
    } catch (e) {
      const err = e as Error & { status?: number };
      return new Response(JSON.stringify({ error: err.message }), { status: err.status ?? 502, headers: { "Content-Type": "application/json" } });
    }
  };

  // Link checks, in the shape the page's activity panel reads (it was written for the Python server's jobs).
  const pageIdsByURL = new Map<string, number[]>();
  for (const i of page.data.keep) pageIdsByURL.set(i.u, [...(pageIdsByURL.get(i.u) ?? []), i.id]);
  // A refresh re-checks links and takes snapshots: of the chosen pages again, or (for very many, Refresh all) of those
  // with none yet, since each snapshot starts a browser. No Chrome installed: checking alone.
  type SnapView = { id: string; total: number; done: number; failed: number; skipped: number; state: string };
  const snapOf = new Map<string, { id: string; urls: string[] }>();
  const refresh = async (ids: number[]): Promise<{ job: string; total: number }> => {
    const nodes = ids.map(id => page.ids[id - 1]).filter((x): x is string => !!x);
    const { job } = await call<{ job: CheckView }>("POST", "linkchecks", { workspace: boot.workspace.id, nodes });
    try {
      const s = await call<{ job: SnapView }>("POST", "snapshots", { workspace: boot.workspace.id, nodes, force: nodes.length <= 300 });
      snapOf.set(job.id, { id: s.job.id, urls: ids.map(id => page.data.keep[id - 1]?.u ?? "").filter(Boolean) });
    } catch { /* no browser to take snapshots with, or snapshots already running */ }
    return { job: job.id, total: job.total };
  };
  const thumbKey = async (u: string): Promise<string> =>
    Array.from(new Uint8Array(await crypto.subtle.digest("SHA-1", new TextEncoder().encode(u))), b => b.toString(16).padStart(2, "0")).join("").slice(0, 16);
  const job = async (id: string): Promise<Record<string, unknown>> => {
    let j: { job: CheckView; results?: Record<string, V1Link> };
    try { j = await call("GET", `linkchecks/${encodeURIComponent(id)}`); } catch { return { done: true, state: "lost", results: {}, rendered: 0 }; }
    const v = j.job;
    const results: Record<string, unknown> = {};
    if (v.done) {
      j = await call("GET", `linkchecks/${encodeURIComponent(id)}?results=1`);
      for (const [url, l] of Object.entries(j.results ?? {})) for (const pid of pageIdsByURL.get(url) ?? []) results[pid] = linkFields(l);
    }
    const base = { state: v.state === "done" ? "done" : v.state, checked: v.checked, total: v.total, started: Date.parse(v.started) / 1000, results };
    const snap = snapOf.get(id);
    if (!v.done || !snap) return { ...base, phase: "checking", done: v.done, rendered: 0, render_total: 0 };
    let sv: SnapView;
    try { sv = (await call<{ job: SnapView }>("GET", `snapshots/${encodeURIComponent(snap.id)}`)).job; } catch { return { ...base, phase: "rendering", done: true, rendered: 0, render_total: 0 }; }
    const rendered = sv.done + sv.failed + sv.skipped;
    if (sv.state === "running") return { ...base, phase: "rendering", done: false, rendered, render_total: sv.total };
    for (const u of snap.urls) {  // only beside a check result: a snapshot alone must not clear a page's status
      const pids = (pageIdsByURL.get(u) ?? []).filter(pid => results[pid]);
      if (!pids.length) continue;
      const h = await thumbKey(u);
      for (const pid of pids) results[pid] = { ...(results[pid] as object), h };
    }
    snapOf.delete(id);
    return { ...base, state: v.state === "done" && sv.state === "cancelled" ? "cancelled" : base.state, phase: "rendering", done: true, rendered: sv.done, render_total: sv.total };
  };
  const ctl = (id: string, verb: string): Promise<unknown> => {
    const snap = snapOf.get(id);
    if (verb === "cancel" && snap) void call("POST", `snapshots/${encodeURIComponent(snap.id)}/cancel`).catch(() => null);
    return call("POST", `linkchecks/${encodeURIComponent(id)}/${verb}`).catch(() => null);
  };

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
