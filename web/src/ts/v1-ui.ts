// The workspace menu, History window and file handling when the Go server serves the page (window.BM_WS.v1, set by
// v1-boot.ts). It fills window.BM_WS.ui, the hooks the page's Download menu, palette and Settings already call, and
// talks to /api/v1 through the boot client's authenticated `call`.

import { askConfirm, askText, hideModal, showModal, toast } from "./dialogs";
import { closeDropdowns } from "./menu";
import { el, fmtDate, icon } from "./ui";

interface Info { id: string; name: string; head: number; notes?: string; locked?: boolean; archived?: boolean }
interface Entry { seq: number; at: string; op: string; summary: string }
interface V1 {
  v1: true; id: string; locked: boolean; workspace: Info; workspaces: Info[];
  call<T>(method: string, path: string, body?: unknown): Promise<T>;
  headers(): Record<string, string>;
  ui?: Record<string, unknown>;
}

const W = (window as unknown as { BM_WS?: V1 }).BM_WS;
if (W?.v1) install(W);

function install(W: V1): void {
  const ws = (p = "") => `workspaces/${encodeURIComponent(W.id)}${p}`;
  const nav = (id: string) => { location.href = "./?ws=" + encodeURIComponent(id); };
  const fail = (e: unknown) => toast(e instanceof Error ? e.message : String(e), "danger");
  const update = async (body: Record<string, unknown>, reload = true): Promise<void> => {
    try { await W.call("PATCH", ws(), body); if (reload) location.reload(); } catch (e) { fail(e); }
  };

  function modal(id: string, title: string): { body: HTMLElement; foot: HTMLElement } {
    document.getElementById(id)?.remove();
    const m = el("div", "modal modal-blur"); m.id = id; m.tabIndex = -1; m.setAttribute("role", "dialog"); m.setAttribute("aria-label", title);
    const dlg = el("div", "modal-dialog modal-dialog-centered modal-dialog-scrollable modal-lg"), box = el("div", "modal-content");
    const head = el("div", "modal-header"), close = el("button", "btn-close");
    close.type = "button"; close.setAttribute("aria-label", "Close"); close.onclick = () => hideModal("#" + id);
    head.append(el("h5", "modal-title", title), close);
    const body = el("div", "modal-body"), foot = el("div", "modal-footer");
    box.append(head, body, foot); dlg.append(box); m.append(dlg);
    m.addEventListener("mousedown", e => { if (e.target === m) hideModal("#" + id); });
    m.addEventListener("keydown", e => { if (e.key === "Escape") { e.stopPropagation(); hideModal("#" + id); } });
    document.body.append(m);
    return { body, foot };
  }

  async function history(): Promise<void> {
    let h: Entry[];
    try { h = (await W.call<{ history: Entry[] }>("GET", ws("/history"))).history; } catch (e) { return fail(e); }
    const { body } = modal("v1History", `History of “${W.workspace.name}”`);
    body.append(el("p", "text-secondary", "Every change is kept. Going back makes that point the newest state, as a new entry you can undo."));
    const list = el("div", "list-group list-group-flush");
    for (const e of [...h].reverse()) {
      const row = el("div", "list-group-item d-flex align-items-center gap-2");
      const text = el("div", "flex-fill min-w-0");
      text.append(el("div", "text-truncate", `${e.seq}. ${e.summary}`), el("div", "text-secondary small", `${fmtDate(Date.parse(e.at) / 1000)} · ${e.op}`));
      row.append(text);
      if (e.seq < W.workspace.head && !W.locked) {
        const b = el("button", "btn btn-sm", "Go back to this"); b.type = "button";
        b.onclick = async () => {
          if (!(await askConfirm({ title: `Go back to ${e.seq}?`, ok: "Go back", danger: false,
            body: `The bookmarks become what they were after “${e.summary}”. This is a new entry, so Undo or History brings today's state back.` }))) return;
          try { await W.call("POST", ws("/rollback"), { to: e.seq }); location.reload(); } catch (err) { fail(err); }
        };
        row.append(b);
      }
      list.append(row);
    }
    body.append(list);
    showModal("#v1History");
  }

  function manage(): void {
    const { body } = modal("v1Workspaces", "Workspaces");
    const list = el("div", "list-group list-group-flush");
    for (const w of W.workspaces) {
      const row = el("div", "list-group-item d-flex align-items-center gap-2");
      const text = el("div", "flex-fill min-w-0");
      text.append(el("div", "fw-medium text-truncate", w.name + (w.id === W.id ? " (open)" : "")),
        el("div", "text-secondary small", [w.archived ? "archived" : "", w.locked ? "locked" : "", `${w.head} entries`].filter(Boolean).join(" · ")));
      row.append(text);
      if (w.id !== W.id) { const o = el("button", "btn btn-sm", "Open"); o.type = "button"; o.onclick = () => nav(w.id); row.append(o); }
      if (!w.locked) {
        const a = el("button", "btn btn-sm btn-ghost-secondary", w.archived ? "Bring back" : "Archive"); a.type = "button";
        a.onclick = async () => {
          try { await W.call("PATCH", `workspaces/${encodeURIComponent(w.id)}`, { archived: !w.archived }); location.reload(); } catch (e) { fail(e); }
        };
        row.append(a);
      }
      list.append(row);
    }
    body.append(list);
    showModal("#v1Workspaces");
  }

  async function rename(): Promise<void> {
    const name = await askText({ title: "Rename workspace", label: "Name", value: W.workspace.name });
    if (name && name.trim() !== W.workspace.name) await update({ name: name.trim() });
  }
  async function notes(): Promise<void> {
    const v = await askText({ title: "Workspace notes", label: "What this set is, what is left to do", value: W.workspace.notes ?? "", multiline: true, hint: "⌘Enter saves" });
    if (v == null || v === (W.workspace.notes ?? "")) return;
    await update({ notes: v }, false);
    W.workspace.notes = v;
    toast("Notes saved", "success");
  }
  async function toggleLock(on: boolean): Promise<void> {
    if (on && !(await askConfirm({ title: `Lock “${W.workspace.name}”?`, ok: "Lock", danger: false,
      body: "Nothing in it can be changed until it is unlocked: edits, undo, imports and going back in History are all refused." }))) return;
    await update({ locked: on });
  }
  async function archive(on: boolean): Promise<void> {
    if (on && !(await askConfirm({ title: `Archive “${W.workspace.name}”?`, ok: "Archive",
      body: "It leaves the list of workspaces, but nothing is deleted: its bookmarks and history stay, and Workspaces… brings it back." }))) return;
    await update({ archived: on });
  }

  async function download(fmt: "html" | "chrome"): Promise<void> {
    try {
      const r = await fetch(`api/v1/${ws("/export")}?format=${fmt}`, { headers: W.headers() });
      if (!r.ok) throw new Error(`the server answered ${r.status}`);
      const a = el("a"); a.href = URL.createObjectURL(await r.blob()); a.download = `${W.id}.${fmt === "html" ? "html" : "json"}`;
      document.body.append(a); a.click(); a.remove(); setTimeout(() => URL.revokeObjectURL(a.href), 10_000);
      toast(fmt === "html" ? "Trash left out. Import it in any browser: Bookmarks › Import." : "Trash left out. Chrome's own format, for tools and backups.", "success", "Export downloaded");
    } catch (e) { fail(e); }
  }

  // Import: a bookmarks file becomes a new workspace (the open one is never replaced from here).
  function importer(): void {
    const input = el("input"); input.type = "file"; input.accept = ".html,.htm,.json,application/json,text/html";
    input.onchange = async () => {
      const f = input.files?.[0]; if (!f) return;
      const name = await askText({ title: "Import as a new workspace", label: "Name", value: f.name.replace(/\.[^.]+$/, "") });
      if (!name) return;
      const id = (name.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "").slice(0, 40) || "import") + "-" + Date.now().toString(36);
      try {
        await W.call("POST", "workspaces", { id, name });
        const r = await fetch(`api/v1/workspaces/${encodeURIComponent(id)}/import?name=${encodeURIComponent(f.name)}`, { method: "POST", headers: W.headers(), body: f });
        const j = await r.json().catch(() => ({})) as { error?: string };
        if (!r.ok) throw new Error(j.error ?? `the server answered ${r.status}`);
        nav(id);
      } catch (e) { fail(e); }
    };
    input.click();
  }

  // Browser extensions: a one-time pairing code, and the browsers already paired (each disconnects on its own).
  async function extensions(): Promise<void> {
    const { body } = modal("v1Extensions", "Browser extension");
    body.append(el("p", "text-secondary", "The extension sends this browser's tabs and tab groups, reads its bookmarks into a workspace, and can write a workspace back. Install it from web/dist/extension (see docs › Browser extension), then connect it with a code."));
    const codeBox = el("div", "mb-3");
    const make = el("button", "btn btn-primary", "Make a pairing code"); make.type = "button";
    make.onclick = async () => {
      try {
        const r = await W.call<{ code: string; expires: string }>("POST", "extension/pairings");
        codeBox.replaceChildren(el("div", "h2 font-monospace mb-1", r.code),
          el("div", "text-secondary small", `Type it into the extension's popup with the server ${location.origin}. It works once, until ${new Date(r.expires).toLocaleTimeString()}.`));
      } catch (e) { fail(e); }
    };
    body.append(make, codeBox);
    const list = el("div", "list-group list-group-flush");
    try {
      const { extensions } = await W.call<{ extensions: { id: string; name: string; created: string; last_used?: string }[] }>("GET", "extension/status");
      if (!extensions.length) list.append(el("div", "text-secondary", "No browser is connected yet."));
      for (const x of extensions) {
        const row = el("div", "list-group-item d-flex align-items-center gap-2");
        const text = el("div", "flex-fill");
        text.append(el("div", "fw-medium", x.name.replace(/^extension:/, "")),
          el("div", "text-secondary small", x.last_used && !x.last_used.startsWith("0001") ? `last heard from ${fmtDate(Date.parse(x.last_used) / 1000)}` : "not heard from yet"));
        const off = el("button", "btn btn-sm btn-ghost-danger", "Disconnect"); off.type = "button";
        off.onclick = async () => {
          if (!(await askConfirm({ title: "Disconnect this browser?", ok: "Disconnect", body: "Its extension stops being able to reach Browser Foreman until it is connected again with a new code." }))) return;
          try { await W.call("DELETE", `tokens/${encodeURIComponent(x.id)}`); row.remove(); } catch (e) { fail(e); }
        };
        row.append(text, off); list.append(row);
      }
    } catch (e) { list.append(el("div", "text-danger", String((e as Error).message))); }
    body.append(el("h4", "mt-2", "Connected browsers"), list);
    showModal("#v1Extensions");
  }

  function menu(): HTMLElement {
    const item = (ic: string, label: string, fn: () => void, cls = ""): HTMLElement => {
      const a = el("a", "dropdown-item " + cls); a.href = "#"; a.append(icon(ic, "me-2"), label);
      a.onclick = e => { e.preventDefault(); closeDropdowns(); fn(); };
      return a;
    };
    const root = el("div", "dropdown"); root.id = "wsMenu";
    const btn = el("button", "btn"); btn.type = "button"; btn.dataset.dd = ""; btn.setAttribute("aria-label", "Workspace");
    btn.dataset.tip = "Workspace: one bookmark set, with its history";
    const name = el("span", "text-truncate", W.workspace.name); name.style.maxWidth = "14rem";
    btn.append(icon("ti-stack-2", "me-1"), name);
    if (W.locked) { const b = el("span", "badge bg-yellow-lt ms-2"); b.append(icon("ti-lock", "me-1"), "locked"); btn.append(b); }
    if (W.workspace.archived) btn.append(el("span", "badge bg-secondary-lt ms-2", "archived"));
    btn.append(icon("ti-chevron-down", "ms-1"));
    const dm = el("div", "dropdown-menu dropdown-menu-end");
    dm.append(el("h6", "dropdown-header", "Switch workspace"));
    const live = W.workspaces.filter(w => !w.archived), archived = W.workspaces.length - live.length;
    for (const w of live) dm.append(item(w.id === W.id ? "ti-check" : "ti-point", w.name, () => { if (w.id !== W.id) nav(w.id); }, w.id === W.id ? "active" : ""));
    dm.append(item("ti-list-details", "All workspaces" + (archived ? ` (${archived} archived)` : "") + "…", manage),
      item("ti-puzzle", "Browser extension…", () => void extensions()),
      el("div", "dropdown-divider"), el("h6", "dropdown-header", "This workspace"),
      item("ti-history", "History and progress…", () => void history()));
    if (W.locked) dm.append(item("ti-lock-open", "Unlock", () => void toggleLock(false)));
    else dm.append(item("ti-pencil", "Rename…", () => void rename()), item("ti-notes", "Notes…", () => void notes()),
      item("ti-lock", "Lock (read-only)…", () => void toggleLock(true)),
      W.workspace.archived ? item("ti-archive-off", "Bring back from the archive", () => void archive(false)) : item("ti-archive", "Archive…", () => void archive(true)));
    root.append(btn, dm);
    return root;
  }

  async function backup(): Promise<void> {
    try {
      const r = await fetch("api/v1/backup", { headers: W.headers() });
      if (!r.ok) throw new Error(r.status === 403 ? "Backups need the owner role" : `the server answered ${r.status}`);
      const name = /filename="([^"]+)"/.exec(r.headers.get("Content-Disposition") ?? "")?.[1] ?? "browser-foreman-backup.json.gz";
      const a = el("a"); a.href = URL.createObjectURL(await r.blob()); a.download = name;
      document.body.append(a); a.click(); a.remove(); setTimeout(() => URL.revokeObjectURL(a.href), 10_000);
      toast("Every workspace with its history, and the link results. Accounts and secrets are not in it.", "success", "Backup downloaded");
    } catch (e) { fail(e); }
  }
  function restore(): void {
    const input = el("input"); input.type = "file"; input.accept = ".gz,.json,application/gzip,application/json";
    input.onchange = async () => {
      const f = input.files?.[0]; if (!f) return;
      if (!(await askConfirm({ title: `Restore ${f.name}?`, ok: "Restore", danger: false,
        body: "Its workspaces are added beside yours; nothing you have is replaced. A workspace whose name is taken comes back under a new one." }))) return;
      try {
        const r = await fetch("api/v1/restore", { method: "POST", headers: W.headers(), body: f });
        const j = await r.json().catch(() => ({})) as { error?: string; workspaces?: Record<string, string> };
        if (!r.ok) throw new Error(j.error ?? `the server answered ${r.status}`);
        toast(`${Object.keys(j.workspaces ?? {}).length} workspace(s) restored. Workspaces… lists them.`, "success", "Restored");
        setTimeout(() => location.reload(), 1200);
      } catch (e) { fail(e); }
    };
    input.click();
  }

  W.ui = {
    history, manage, importer, menu, download, toggleLock, backup, restore, extensions,
    duplicatesCsv: () => toast("Duplicates are merged by the cleanup the Python tool runs; this workspace has no list of them.", "info"),
  };
  const mount = () => { document.getElementById("wsMenu")?.remove(); document.querySelector("#edited")?.before(menu()); };
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", mount); else mount();
}
