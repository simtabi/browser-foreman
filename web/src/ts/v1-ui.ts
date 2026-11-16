// The workspace menu, History window and file handling when the Go server serves the page (window.BM_WS.v1, set by
// v1-boot.ts). It fills window.BM_WS.ui, the hooks the page's Download menu, palette and Settings already call, and
// talks to /api/v1 through the boot client's authenticated `call`.

import { askConfirm, askDecide, askText, hideModal, showModal, toast } from "./dialogs";
import { browserName } from "../extension/core";
import { closeDropdowns } from "./menu";
import { el, fmtDate, icon, plural } from "./ui";
import { DataTable, avatar, by, nameCell } from "./datatable";
import { RangePicker } from "./rangepick";
import { lookPicker, wsBadge } from "./wslook";
import { busy } from "./busy";
import { type DGroup, startsTicked, trashOps } from "./dupes";
import { version as EXT_VERSION } from "../../package.json";  // the extension's manifest takes the same version

interface Info { id: string; name: string; head: number; notes?: string; locked?: boolean; archived?: boolean; deleted?: string; bookmarks?: number; color?: string; icon?: string }
interface Entry { seq: number; at: string; op: string; summary: string }
interface V1 {
  v1: true; id: string; locked: boolean; workspace: Info; workspaces: Info[];
  call<T>(method: string, path: string, body?: unknown): Promise<T>;
  headers(): Record<string, string>;
  saved?(): Promise<void>;
  ui?: Record<string, unknown>;
}

const W = (window as unknown as { BM_WS?: V1 }).BM_WS;
if (W?.v1) install(W);

function install(W: V1): void {
  const ws = (p = "") => `workspaces/${encodeURIComponent(W.id)}${p}`;
  const nav = (id: string) => {
    const name = W.workspaces.find(w => w.id === id)?.name ?? id;
    void busy(`Opening “${name}”…`, () => new Promise<void>(() => { location.href = "./?ws=" + encodeURIComponent(id); }));  // until the page goes
  };
  const fail = (e: unknown) => toast(e instanceof Error ? e.message : String(e), "danger");
  const update = async (body: Record<string, unknown>, reload = true): Promise<void> => {
    try { await W.call("PATCH", ws(), body); if (reload) location.reload(); } catch (e) { fail(e); }
  };

  /** A dialog: its title carries an icon, as every dialog in the page does (template.html). */
  function modal(id: string, title: string, size: "lg" | "xl" = "lg", ic = ""): { body: HTMLElement; foot: HTMLElement; dock: HTMLElement } {
    document.getElementById(id)?.remove();
    const m = el("div", "modal modal-blur"); m.id = id; m.tabIndex = -1; m.setAttribute("role", "dialog"); m.setAttribute("aria-label", title);
    const dlg = el("div", `modal-dialog modal-dialog-top modal-dialog-scrollable modal-${size}`), box = el("div", "modal-content");
    const head = el("div", "modal-header"), close = el("button", "btn-close");
    close.type = "button"; close.setAttribute("aria-label", "Close"); close.onclick = () => hideModal("#" + id);
    const h = el("h5", "modal-title"); if (ic) h.append(icon(ic, "me-2")); h.append(title);
    head.append(h, close);
    // dock: a strip between the scrolling body and the footer buttons, for a list's pager, so it is always in view.
    const body = el("div", "modal-body"), dock = el("div", "modal-dock"), foot = el("div", "modal-footer");
    box.append(head, body, dock, foot); dlg.append(box); m.append(dlg);
    m.addEventListener("mousedown", e => { if (e.target === m) hideModal("#" + id); });
    m.addEventListener("keydown", e => { if (e.key === "Escape") { e.stopPropagation(); hideModal("#" + id); } });
    document.body.append(m);
    return { body, foot, dock };
  }

  async function history(): Promise<void> {
    const { body, dock } = modal("v1History", `History of “${W.workspace.name}”`, "xl", "ti-history");
    body.classList.add("p-0");
    const OPS: Record<string, [string, string]> = { import: ["imported", "bg-blue-lt"], merge: ["merged", "bg-blue-lt"], update: ["updated", "bg-blue-lt"],
      trash: ["to the Trash", "bg-orange-lt"], restore: ["put back", "bg-green-lt"], purge: ["deleted", "bg-red-lt"], undo: ["undo", "bg-secondary-lt"],
      redo: ["redo", "bg-secondary-lt"], rollback: ["went back", "bg-purple-lt"], reset: ["reset", "bg-purple-lt"], move: ["moved", "bg-azure-lt"],
      edit: ["edited", "bg-azure-lt"], create: ["added", "bg-green-lt"], "save-links": ["saved tabs", "bg-green-lt"],
      // Kinds the Python tool recorded, carried over by the migration.
      start: ["started", "bg-blue-lt"], migrate: ["started", "bg-blue-lt"], delete: ["deleted", "bg-red-lt"], "put-back": ["put back", "bg-green-lt"],
      refresh: ["re-checked", "bg-teal-lt"], "rename-folder": ["renamed", "bg-azure-lt"], reorder: ["reordered", "bg-azure-lt"],
      "legacy-undo": ["undo", "bg-secondary-lt"], "legacy-redo": ["redo", "bg-secondary-lt"], explode: ["split", "bg-azure-lt"] };
    const head = W.workspace.head;
    const table = new DataTable<Entry>({
      noun: "changes", size: 25, sizes: [25, 50, 100], placeholder: "Search changes…",
      search: (e, q) => e.summary.toLowerCase().includes(q) || e.op.includes(q),
      empty: { title: "No changes yet", text: "Every change made here is kept, and any of them can be gone back to.", icon: "ti-history" },
      columns: [
        { label: "#", cls: "w-1 text-secondary", sort: by(e => e.seq), cell: e => String(e.seq) },
        { label: "Change", cls: "text-wrap", cell: e => {
          const d = el("div", "d-flex align-items-center gap-2"), t = el("span", null, e.summary);
          d.append(t); if (e.seq === head) d.append(el("span", "badge bg-primary-lt", "now")); return d;
        } },
        { label: "Kind", cls: "d-none d-md-table-cell", sort: by(e => e.op), cell: e => { const [label, cls] = OPS[e.op] ?? [e.op, "bg-secondary-lt"]; return el("span", `badge ${cls}`, label); } },
        { label: "When", cls: "d-none d-sm-table-cell", sort: by(e => Date.parse(e.at)), cell: e => el("span", "text-secondary", fmtDate(Date.parse(e.at) / 1000)) },
      ],
      primary: e => e.seq < head && !W.locked ? { label: "Go back here", icon: "ti-arrow-back-up", fn: async () => {
        if (!(await askConfirm({ title: `Go back to change ${e.seq}?`, ok: "Go back", danger: false,
          body: `The bookmarks become what they were after “${e.summary}”. This is a new entry, so Undo or History brings today's state back.` }))) return;
        try { await W.call("POST", ws("/rollback"), { to: e.seq }); location.reload(); } catch (err) { fail(err); }
      } } : null,
    });
    table.element.classList.add("border-0", "rounded-0", "shadow-none");
    const note = el("div", "alert alert-info m-3 mb-0"); note.append(el("div", null, "Every change is kept. Going back makes that point the newest state, as a new entry you can undo."));
    body.append(note, table.element); dock.append(table.foot);
    // Opens at once with placeholder rows; the history fills them in when it arrives.
    table.loading("Loading every change in this workspace…");
    showModal("#v1History");
    try { table.rows([...(await W.call<{ history: Entry[] }>("GET", ws("/history"))).history].reverse()); }
    catch (e) { table.rows([]); fail(e); }
  }


  // Workspaces: the ones in use, and the workspace Trash (deleted ones, kept whole until it is emptied), as a Tabler
  // data table with tabs, search, sorting and pages.
  function manage(tab: "use" | "trash" = "use"): void {
    const { body, foot, dock } = modal("v1Workspaces", "Workspaces", "xl", "ti-stack-2");
    body.classList.add("p-0");
    let inUse: Info[] = W.workspaces, trash: Info[] = [];
    const badge = (text: string, cls: string, ic?: string) => { const b = el("span", `badge ${cls}`); if (ic) b.append(icon(ic, "me-1")); b.append(text); return b; };
    const typed = async (title: string, id: string, ok: string): Promise<boolean> => {
      const v = await askText({ title, label: `Type the workspace id, ${id}, to go ahead`, ok, hint: "" });
      if (v == null) return false;
      if (v.trim() !== id) { toast("The id did not match, so nothing happened.", "info"); return false; }
      return true;
    };
    const run = (fn: () => Promise<void>) => async () => { try { await fn(); } catch (e) { fail(e); } };
    const enc = (w: Info) => encodeURIComponent(w.id);
    let ownTabs = new Set<string>();  // workspaces that keep their own tab snapshots (Preferences › tabs_own)
    const table = new DataTable<Info>({
      noun: "workspaces", size: 10, placeholder: "Search by name or id…",
      search: (w, q) => w.name.toLowerCase().includes(q) || w.id.includes(q),
      tabs: [], tab, onTab: id => { tab = id as "use" | "trash"; paint(); },
      empty: { title: "No workspaces here", text: "Deleted workspaces wait in the Trash, history and all, until you restore them or delete them for good.", icon: "ti-stack-2" },
      columns: [
        { label: "Name", sort: by(w => w.name), cell: w => {
          const t = el("span"); t.append(w.name);
          return nameCell(t, w.id + (w.notes ? ` · ${w.notes.split("\n")[0]}` : ""), wsBadge(w, "avatar-sm"));
        } },
        { label: "Bookmarks", cls: "text-end", sort: by(w => w.bookmarks ?? 0), cell: w => w.bookmarks == null ? el("span", "placeholder col-6") : w.bookmarks.toLocaleString() },
        { label: "Changes", cls: "text-end d-none d-md-table-cell", sort: by(w => w.head), cell: w => w.head.toLocaleString() },
        { label: "Status", cls: "d-none d-sm-table-cell", cell: w => {
          const d = el("div", "d-flex gap-1 flex-wrap");
          if (w.deleted) d.append(badge(`deleted ${fmtDate(Date.parse(w.deleted) / 1000)}`, "bg-red-lt", "ti-trash"));
          if (w.id === W.id && !w.deleted) d.append(badge("open", "bg-primary-lt", "ti-eye"));
          if (w.archived) d.append(badge("archived", "bg-secondary-lt", "ti-archive"));
          if (w.locked) d.append(badge("locked", "bg-yellow-lt", "ti-lock"));
          if (!w.deleted) { const t = badge(ownTabs.has(w.id) ? "own tab snapshots" : "shared tab snapshots", ownTabs.has(w.id) ? "bg-purple-lt" : "bg-secondary-lt", "ti-browser");
            t.dataset.tip = ownTabs.has(w.id) ? "Open tabs, Timeline and History show only this workspace's tab snapshots." : "Uses the tab snapshots shared by every workspace that does not keep its own."; d.append(t); }
          if (!d.childElementCount) d.append(badge("in use", "bg-green-lt"));
          return d;
        } },
      ],
      primary: w => w.deleted
        ? { label: "Restore", icon: "ti-restore", cls: "btn-primary", fn: run(async () => {
          await W.call("POST", `workspaces/${enc(w)}/restore`);
          toast(`“${w.name}” is back.`, "success", "Workspace restored"); setTimeout(() => nav(w.id), 600);
        }) }
        : w.id === W.id ? null : { label: "Open", icon: "ti-external-link", fn: () => nav(w.id) },
      actions: w => w.deleted ? [
        { l: "Delete for good…", ic: "ti-trash-x", danger: true, fn: run(async () => {
          if (!(await askConfirm({ title: `Delete “${w.name}” for good?`, ok: "Continue", body: "Its bookmarks and every change in its history are removed from this computer. This cannot be undone. A backup made earlier still has it." }))) return;
          if (!(await typed(`Delete “${w.name}” for good`, w.id, "Delete for good"))) return;
          await W.call("POST", `workspaces/${enc(w)}/purge`, { confirm: w.id });
          toast(`“${w.name}” was removed for good.`, "success", "Deleted for good"); await load();
        }) },
      ] : [
        w.id !== W.id && { l: "Open", ic: "ti-external-link", fn: () => nav(w.id) },
        !w.locked && { l: "Rename…", ic: "ti-pencil", fn: run(async () => {
          const name = (await askText({ title: "Rename workspace", label: "Name", value: w.name }))?.trim();
          if (!name || name === w.name) return;
          await W.call("PATCH", `workspaces/${enc(w)}`, { name });
          if (w.id === W.id) { W.workspace.name = name; remount(); }
          toast(`Renamed to “${name}”.`, "success"); await load();
        }) },
        { l: "Appearance…", ic: "ti-palette", fn: run(async () => { await appearance(w, load); }) },
        !w.locked && { l: "Notes…", ic: "ti-notes", fn: run(async () => {
          const v = await askText({ title: `Notes on “${w.name}”`, label: "What this set is, what is left to do", value: w.notes ?? "", multiline: true, hint: "⌘Enter saves" });
          if (v == null || v === (w.notes ?? "")) return;
          await W.call("PATCH", `workspaces/${enc(w)}`, { notes: v }); if (w.id === W.id) W.workspace.notes = v; toast("Notes saved.", "success"); await load();
        }) },
        { l: ownTabs.has(w.id) ? "Use the shared tab snapshots" : "Keep its own tab snapshots", ic: "ti-browser", fn: run(async () => {
          const own = !ownTabs.has(w.id);
          await W.call("PUT", `workspaces/${enc(w)}/tabs`, { own });
          toast(own ? `“${w.name}” now keeps its own tab snapshots; the ones already taken stay shared.` : `“${w.name}” uses the shared tab snapshots again; its own stay kept.`, "success");
          await load();
        }) },
        !w.locked && { l: w.archived ? "Bring back from the archive" : "Archive", ic: w.archived ? "ti-archive-off" : "ti-archive", fn: run(async () => {
          await W.call("PATCH", `workspaces/${enc(w)}`, { archived: !w.archived }); toast(w.archived ? "Back in the list." : "Archived: kept, out of the way.", "success"); await load();
        }) },
        { l: w.locked ? "Unlock" : "Lock (read-only)", ic: w.locked ? "ti-lock-open" : "ti-lock", fn: run(async () => {
          await W.call("PATCH", `workspaces/${enc(w)}`, { locked: !w.locked }); await load();
        }) },
        "-",
        !w.locked && inUse.length > 1 && { l: "Delete…", ic: "ti-trash", danger: true, fn: run(async () => {
          if (!(await typed(`Delete “${w.name}”?`, w.id, "Move to the Trash"))) return;
          await W.call("POST", `workspaces/${enc(w)}/delete`, { confirm: w.id });
          toast("It is in the workspace Trash, history and all. Restore brings it back.", "success", "Workspace deleted");
          if (w.id === W.id) setTimeout(() => nav(inUse.find(x => x.id !== w.id)!.id), 700); else await load();
        }) },
      ],
    });
    const paint = () => {
      table.setTabs([{ id: "use", label: "In use", icon: "ti-stack-2", count: inUse.length }, { id: "trash", label: "Trash", icon: "ti-trash", count: trash.length }], tab);
      table.rows(tab === "use" ? inUse : trash);
    };
    const load = async () => {
      try { ownTabs = new Set((await W.call<{ tabs_own?: string[] }>("GET", "preferences")).tabs_own ?? []); } catch { ownTabs = new Set(); }
      try { inUse = (await W.call<{ workspaces: Info[] }>("GET", "workspaces")).workspaces; } catch { /* the page's own list will do */ }
      try { trash = (await W.call<{ workspaces: Info[] }>("GET", "workspaces?trash=1")).workspaces; } catch { trash = []; }
      paint();
    };
    table.element.classList.add("border-0", "rounded-0", "shadow-none");
    body.append(table.element); dock.append(table.foot);
    const add = el("button", "btn btn-primary"); add.type = "button"; add.append(icon("ti-plus", "me-1"), "New workspace");
    add.onclick = () => { hideModal("#v1Workspaces"); importer(); };
    const close = el("button", "btn me-auto"); close.type = "button"; close.textContent = "Close"; close.onclick = () => hideModal("#v1Workspaces");
    foot.append(close, add);
    table.loading("Loading workspaces and counting their bookmarks…"); void load(); showModal("#v1Workspaces");
  }

  // Reset: back to how the workspace started, as one entry (undo brings today's back).
  async function resetWs(): Promise<void> {
    if (!(await askConfirm({ title: `Reset “${W.workspace.name}” to how it started?`, ok: "Reset", danger: false,
      body: "Every change since its first import or merge is set aside as one entry in the history. Undo, or History, brings today's state back." }))) return;
    try { const j = await W.call<{ entry: Entry }>("POST", ws("/reset")); toast(j.entry.summary, "success", "Reset"); setTimeout(() => location.reload(), 700); }
    catch (e) { fail(e); }
  }

  // Appearance: a workspace's colour and icon, saved as you pick them (they apply even to a locked workspace).
  async function appearance(w: Info, after?: () => Promise<void>): Promise<void> {
    const { body, foot } = modal("v1Look", `Appearance of “${w.name}”`, "lg", "ti-palette");
    body.append(el("p", "text-secondary", "A colour and an icon tell workspaces apart in the top bar, its menu and All workspaces. The top bar takes the colour of the workspace you are in."));
    let saving: Promise<unknown> = Promise.resolve();
    body.append(lookPicker(w, look => {
      saving = saving.then(async () => {
        try {
          await W.call("PATCH", `workspaces/${encodeURIComponent(w.id)}`, look);
          w.color = look.color; w.icon = look.icon;
          const here = W.workspaces.find(x => x.id === w.id); if (here) { here.color = look.color; here.icon = look.icon; }
          if (w.id === W.id) { W.workspace.color = look.color; W.workspace.icon = look.icon; paintAccent(); remount(); }
        } catch (e) { fail(e); }
      });
    }));
    const done = el("button", "btn btn-primary ms-auto"); done.type = "button"; done.textContent = "Done";
    done.onclick = async () => { await saving; hideModal("#v1Look"); await after?.(); };
    foot.append(done);
    showModal("#v1Look");
  }
  // The top bar's strip in the colour of the workspace you are in.
  function paintAccent(): void {
    const c = W.workspace.color;
    document.documentElement.style.setProperty("--ws-accent", c ? `var(--tblr-${c})` : "transparent");
  }
  paintAccent();

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

  // Adding bookmarks: files (and optionally another workspace) merged into a new workspace (importer) or this one
  // (addFiles), through /sources: copies of the same page are folded into one, each copy going to the Trash.
  function sourcesDialog(into: "new" | "this"): void {
    const { body, foot } = modal("v1Sources", into === "new" ? "Import bookmarks as a new workspace" : `Add bookmarks to “${W.workspace.name}”`, "lg", "ti-file-import");
    body.append(el("p", "text-secondary", "Choose one or more bookmarks files: an HTML export from any browser, Chrome's Bookmarks file, Firefox's places.sqlite or Safari's Bookmarks.plist. Copies of the same page are kept once; every other copy goes to the Trash, where it can be put back, and the Merged duplicates list names them."));
    const file = el("input", "form-control mb-3"); file.type = "file"; file.multiple = true; file.accept = ".html,.htm,.json,.sqlite,.plist,application/json,text/html";
    let nameIn: HTMLInputElement | null = null;
    if (into === "new") {
      const lab = el("label", "form-label", "Name"); nameIn = el("input", "form-control mb-3"); nameIn.placeholder = "Imported bookmarks";
      lab.append(nameIn); body.append(lab);
      file.onchange = () => { if (nameIn && !nameIn.value && file.files?.[0]) nameIn.value = file.files[0].name.replace(/\.[^.]+$/, ""); };
    }
    body.append(file);
    // Browsers on this computer: each profile's own bookmarks file, read where it is (owners; Update reads it again),
    // as Tabler select-group boxes. Those the system keeps closed are named once, with the way to open them.
    const picks: HTMLInputElement[] = [];
    const found = el("div", "mb-3");
    body.append(found);
    void (async () => {
      try {
        const { profiles } = await W.call<{ profiles: { browser: string; profile: string; name?: string; path: string; readable: boolean; why?: string }[] }>("GET", "profiles");
        if (!profiles.length) return;
        found.append(el("label", "form-label", "Or read a browser on this computer"));
        const readable = profiles.filter(p => p.readable), blocked = profiles.filter(p => !p.readable);
        if (readable.length) {
          const group = el("div", "form-selectgroup form-selectgroup-boxes d-flex flex-wrap");
          for (const p of readable) {
            const lab = el("label", "form-selectgroup-item flex-fill"), box = el("input", "form-selectgroup-input"); box.type = "checkbox"; box.value = p.path;
            const inner = el("span", "form-selectgroup-label d-flex align-items-center p-3 gap-3");
            const who = p.name || (p.profile && p.profile !== p.browser && p.profile !== "Default" ? p.profile : "");
            const txt = el("span", "text-start"); txt.append(el("span", "d-block fw-medium", p.browser), el("span", "d-block text-secondary small", who || "Default profile"));
            const mark = el("span", "form-selectgroup-check");
            inner.append(mark, icon("browser:" + p.browser), txt); lab.append(box, inner); lab.title = p.path;
            group.append(lab); picks.push(box);
          }
          found.append(group);
        }
        if (blocked.length) {
          const names = [...new Set(blocked.map(p => p.browser))].join(", ");
          const fda = blocked.some(p => (p.why ?? "").includes("Full Disk Access"));
          const al = el("div", "alert alert-warning mt-2 mb-0"), d = el("div", "d-flex gap-2"), tx = el("div");
          tx.append(el("h4", "alert-title", `${names} cannot be read here`),
            el("div", "text-secondary", fda ? "macOS keeps their bookmarks behind Full Disk Access. Grant it to the app that runs Browser Foreman, or export the bookmarks from the browser and choose that file above."
              : blocked[0]!.why ?? "The system does not let this program read them."));
          d.append(icon("ti-lock", "alert-icon"), tx); al.append(d); found.append(al);
        }
      } catch { /* not an owner, or the server cannot look: the files above still work */ }
    })();
    const fromSel = el("select", "form-select mb-3");
    fromSel.append(new Option("Only the files", ""));
    for (const w of W.workspaces) if (w.id !== W.id || into === "new") fromSel.append(new Option(`Also the bookmarks of “${w.name}”`, w.id));
    const fromLab = el("label", "form-label", "Other workspace"); fromLab.append(fromSel); body.append(fromLab);
    const sepWrap = el("label", "form-check"), sep = el("input", "form-check-input"); sep.type = "checkbox";
    sepWrap.append(sep, el("span", "form-check-label", "Keep each file's folders apart, under a folder named after it"));
    body.append(sepWrap);
    const go = el("button", "btn btn-primary", into === "new" ? "Import" : "Add"); go.type = "button";
    const cancel = el("button", "btn me-auto", "Cancel"); cancel.type = "button"; cancel.onclick = () => hideModal("#v1Sources");
    foot.append(cancel, go);
    go.onclick = async () => {
      const files = [...(file.files ?? [])], paths = picks.filter(b => b.checked).map(b => b.value);
      if (!files.length && !paths.length && !fromSel.value) { toast("Choose a file, a browser, or a workspace to add from.", "warning"); return; }
      go.disabled = true;
      try {
        let id = W.id;
        if (into === "new") {
          const name = nameIn?.value.trim() || "Imported bookmarks";
          id = (name.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "").slice(0, 40) || "import") + "-" + Date.now().toString(36);
          await W.call("POST", "workspaces", { id, name });
        }
        const fd = new FormData();
        for (const f of files) fd.append("file", f, f.name);
        const q = new URLSearchParams();
        if (fromSel.value) q.set("from", fromSel.value);
        for (const p of paths) q.append("path", p);
        if (sep.checked) q.set("separate", "1");
        const h = W.headers(); delete h["Content-Type"];  // the browser sets multipart/form-data with its boundary
        const r = await fetch(`api/v1/workspaces/${encodeURIComponent(id)}/sources?${q}`, { method: "POST", headers: h, body: fd });
        const j = await r.json().catch(() => ({})) as { error?: string; added?: number; duplicates?: number };
        if (!r.ok) throw new Error(j.error ?? `the server answered ${r.status}`);
        hideModal("#v1Sources");
        toast(`${(j.added ?? 0).toLocaleString()} bookmarks added` + (j.duplicates ? `, ${j.duplicates.toLocaleString()} duplicate copies moved to the Trash` : "") + ".", "success", "Bookmarks added");
        setTimeout(() => (into === "new" ? nav(id) : location.reload()), 900);
      } catch (e) { fail(e); go.disabled = false; }
    };
    showModal("#v1Sources");
  }
  const importer = (): void => sourcesDialog("new");
  const addFiles = (): void => { if (W.locked) toast("The workspace is locked; unlock it to add bookmarks.", "warning"); else sourcesDialog("this"); };

  // Open tabs saved as bookmarks: a new folder inside `parent` (a page path), as one entry on the server.
  async function addLinks(folder: string, links: { url: string; title: string }[], opts: { reload?: boolean; parent?: string } = {}): Promise<void> {
    if (W.locked) throw new Error("the workspace is locked; unlock it to save tabs into it");
    const r = await W.call<{ path: string; saved: number }>("POST", ws("/save-links"), { folder, parent: opts.parent ?? "Saved tabs", links });
    toast(`${r.saved} saved in “${r.path}”.`, "success", "Saved as bookmarks");
    if (opts.reload) setTimeout(() => location.reload(), 900);
  }

  // Update: read the workspace's sources again; say what would change, then apply it as one entry.
  async function updateWs(): Promise<void> {
    if (W.locked) { toast("Unlock the workspace first.", "warning", "Locked"); return; }
    type C = { title: string; url: string; new_title?: string };
    let pv: { plan: { new: C[]; retitle: C[]; gone: C[] }; sources: number; missing: string[] };
    try { pv = await W.call("GET", ws("/update")); } catch (e) { fail(e); return; }
    const missing = pv.missing.length ? ` Could not read: ${pv.missing.join("; ")}.` : "";
    if (!pv.sources && !pv.missing.length) { toast("This workspace has no source to read again. Add files to it, or merge another workspace in.", "info"); return; }
    const p = pv.plan, eg = (l: C[]) => l.slice(0, 3).map(b => b.title || b.url).join("; ");
    if (!p.new.length && !p.retitle.length) {
      toast((p.gone.length ? `${p.gone.length} no longer upstream; they stay here.` : "Nothing new upstream.") + missing, "info", "Up to date");
      return;
    }
    const body = [`${p.new.length} new` + (p.new.length ? ` (${eg(p.new)})` : ""),
      `${p.retitle.length} retitled upstream (your own renames still win)`,
      `${p.gone.length} no longer upstream, kept here until you trash them`].join(" · ") + "." + missing + " Nothing is removed, and the update is one entry in the history.";
    if (!(await askConfirm({ title: "Update from the sources?", ok: "Apply update", danger: false, body }))) return;
    try {
      const j = await W.call<{ entry: Entry | null }>("POST", ws("/update"));
      toast(j.entry ? j.entry.summary : "Nothing to apply", "success", "Updated");
      if (j.entry) setTimeout(() => location.reload(), 900);
    } catch (e) { fail(e); }
  }

  // Browser history: pages visited, across browsers, read when this opens and never kept. Pages not bookmarked here
  // can be saved, as a new folder in Saved tabs.
  type Visit = { url: string; title: string; last: string; visits: number; browser: string; profile?: string; bookmarked?: boolean };
  function browserHistory(): void {
    const { body, foot, dock } = modal("v1History2", "Browser history", "xl", "ti-world-search");
    body.classList.add("p-0");
    const days = new RangePicker({ label: "Visited", heading: "Pages visited in…", value: "30", defaultKey: "30",
      options: [{ key: "1", label: "Today" }, { key: "7", label: "Last 7 days" }, { key: "30", label: "Last 30 days" },
        { key: "90", label: "Last 3 months" }, { key: "365", label: "Last year" }], onPick: () => void load(), onRange: () => void load(), end: true });
    const sw = el("label", "form-check form-switch m-0"), only = el("input", "form-check-input"); only.type = "checkbox"; only.checked = true;
    sw.append(only, el("span", "form-check-label", "Not bookmarked only"));
    const note = el("div"), status = el("div", "text-secondary small px-3 pt-3");
    let rows: Visit[] = [], blockedAll = false;
    const save = el("button", "btn btn-primary"); save.type = "button";
    const paintSave = () => { save.disabled = !table.picked.size || !!W.locked; save.replaceChildren(icon("ti-bookmark-plus", "me-1"), table.picked.size ? `Save ${table.picked.size} as bookmarks` : "Save as bookmarks"); };
    const table: DataTable<Visit> = new DataTable<Visit>({
      noun: "pages", size: 25, sizes: [25, 50, 100, 250], placeholder: "Search titles and addresses…",
      search: (r, q) => (r.title + " " + r.url).toLowerCase().includes(q), tools: [days.el, sw],
      empty: { title: "No visits here", text: "Try a longer time, or turn off Not bookmarked only.", icon: "ti-history" },
      pick: { key: r => r.url, disabled: r => !!r.bookmarked, onPick: () => paintSave() },
      columns: [
        { label: "Page", cls: "text-wrap", sort: by(r => r.title || r.url), cell: r => {
          const a = el("a", "text-reset", r.title || r.url); a.href = r.url; a.target = "_blank"; a.rel = "noopener noreferrer";
          const t = el("span"); t.append(a);
          if (r.bookmarked) { const b = el("span", "badge bg-green-lt ms-2"); b.append(icon("ti-bookmark", "me-1"), "bookmarked"); t.append(b); }
          return nameCell(t, r.url);
        } },
        { label: "Browser", cls: "d-none d-md-table-cell", sort: by(r => r.browser), cell: r => el("span", "text-secondary", r.browser + (r.profile && r.profile !== "Default" ? ` · ${r.profile}` : "")) },
        { label: "Visits", cls: "text-end d-none d-md-table-cell", sort: by(r => r.visits), cell: r => r.visits.toLocaleString() },
        { label: "Last visit", cls: "d-none d-sm-table-cell", sort: by(r => Date.parse(r.last)), cell: r => el("span", "text-secondary", fmtDate(Date.parse(r.last) / 1000)) },
      ],
    });
    table.element.classList.add("border-0", "rounded-0", "shadow-none");
    const paint = () => {
      const shown = only.checked ? rows.filter(r => !r.bookmarked) : rows;
      table.element.hidden = blockedAll;
      table.rows(shown);
      status.textContent = blockedAll ? "" : `${rows.filter(r => !r.bookmarked).length.toLocaleString()} of ${rows.length.toLocaleString()} pages not bookmarked here`;
      paintSave();
    };
    const load = async () => {
      note.replaceChildren(); table.loading("Reading your browsers' history (through copies; nothing is stored)…");
      try {
        const j = await W.call<{ visits: Visit[]; read: number; skipped: { Browser: string; Profile: string; Why: string }[] }>("GET",
          `history?${new URLSearchParams({ ...(days.range ? Object.fromEntries(Object.entries(days.range).filter(([, v]) => v)) : { days: days.key }), limit: "5000", workspace: W.id })}`);
        rows = j.visits; note.replaceChildren(); blockedAll = !j.read && j.skipped.length > 0;
        if (j.skipped.length) {
          // One line per reason, naming the browsers it applies to, and the way out where there is one.
          const byWhy = new Map<string, string[]>();
          for (const x of j.skipped) byWhy.set(x.Why, [...(byWhy.get(x.Why) ?? []), x.Browser + (x.Profile && x.Profile !== "Default" ? ` (${x.Profile})` : "")]);
          const al = el("div", "alert alert-warning m-3"), d = el("div", "d-flex gap-2"), tx = el("div", "min-w-0");
          tx.append(el("h4", "alert-title", blockedAll ? "Your browsers' history cannot be read yet" : `${j.skipped.length} browser profile${j.skipped.length === 1 ? "" : "s"} left out`));
          for (const [why, names] of byWhy) tx.append(el("div", "text-secondary", `${[...new Set(names)].join(", ")}: ${why}.`));
          const acts = el("div", "btn-list mt-2");
          const fda = el("button", "btn btn-sm btn-primary"); fda.type = "button"; fda.append(icon("ti-shield-lock", "me-1"), "Open Full Disk Access");
          fda.onclick = async () => {
            try { const r = await W.call<{ ok: boolean; error?: string }>("POST", "history/access/open-settings");
              toast(r.ok ? "Add the app that runs Browser Foreman, quit and reopen it, then press Check again." : r.error ?? "System Settings did not open", r.ok ? "info" : "danger", r.ok ? "System Settings opened" : "");
            } catch (e) { fail(e); }
          };
          const again = el("button", "btn btn-sm"); again.type = "button"; again.append(icon("ti-refresh", "me-1"), "Check again"); again.onclick = () => void load();
          if ([...byWhy.keys()].some(w => w.includes("Full Disk Access"))) acts.append(fda);
          acts.append(again); tx.append(acts);
          d.append(icon("ti-lock", "alert-icon"), tx); al.append(d); note.append(al);
        }
        if (!j.read && !j.skipped.length) note.append(el("div", "alert alert-info m-3", "No browser history was found on this computer."));
        paint();
      } catch (e) { note.replaceChildren(el("div", "alert alert-danger m-3", e instanceof Error ? e.message : String(e))); }
    };
    only.onchange = paint;
    save.onclick = async () => {
      const links = rows.filter(r => table.picked.has(r.url)).map(r => ({ url: r.url, title: r.title }));
      try { await addLinks(`From history ${new Date().toLocaleDateString()}`, links, { parent: "Saved tabs", reload: true }); hideModal("#v1History2"); }
      catch (e) { fail(e); }
    };
    const close = el("button", "btn me-auto"); close.type = "button"; close.textContent = "Close"; close.onclick = () => hideModal("#v1History2");
    body.append(note, status, table.element); dock.append(table.foot); foot.append(close, save);
    paintSave(); showModal("#v1History2"); void load();
  }

  async function duplicatesCsv(): Promise<void> {
    try {
      const r = await fetch(`api/v1/${ws("/duplicates.csv")}`, { headers: W.headers() });
      if (!r.ok) throw new Error(`the server answered ${r.status}`);
      const a = el("a"); a.href = URL.createObjectURL(await r.blob()); a.download = `${W.id}-duplicates.csv`;
      document.body.append(a); a.click(); a.remove(); setTimeout(() => URL.revokeObjectURL(a.href), 10_000);
    } catch (e) { fail(e); }
  }

  // Find duplicates: bookmarks that lead to the same page, beyond the exact copies a merge catches (GET
  // …/duplicates, dupes.Find). Each group's first bookmark is the one kept; the others go to the Trash as
  // "duplicate of <kept>", so they join the merged copies in Duplicates and Put back brings them back. Groups found
  // only by title start unticked: two pages can share a title.
  const REASON: Record<string, [string, string]> = {
    "same-address": ["Same page", "The same address once tracking parameters, “www.” and a trailing slash are set aside"],
    "same-final": ["Redirects to the same page", "A link check followed a redirect to the other bookmark's page"],
    "same-canonical": ["Same canonical page", "Both pages name the same address as their own"],
    "similar-title": ["Same page, other query", "The same page with another query or fragment, and the same title; check before trashing"],
  };
  async function findDuplicates(): Promise<void> {
    let res: { groups: DGroup[]; copies: number };
    try { res = await busy("Looking for duplicates…", () => W.call("GET", ws("/duplicates"))); } catch (e) { fail(e); return; }
    if (!res.groups.length) { toast("No two bookmarks here lead to the same page.", "success", "No duplicates"); return; }
    const { body, foot } = modal("wsDupes", "Find duplicates", "xl", "ti-files");
    const keepOf = new Map<DGroup, string>(), on = new Set<DGroup>(res.groups.filter(startsTicked));
    const go = el("button", "btn btn-danger"); go.type = "button";
    const count = () => [...on].reduce((n, g) => n + g.members.length - 1, 0);
    const paint = () => { const n = count(); go.disabled = !n || W.locked; go.replaceChildren(icon("ti-trash", "me-1"), `Move ${n.toLocaleString()} cop${n === 1 ? "y" : "ies"} to the Trash`); };
    body.append(el("p", "text-secondary", `${res.groups.length.toLocaleString()} group${res.groups.length === 1 ? "" : "s"}, ${res.copies.toLocaleString()} extra cop${res.copies === 1 ? "y" : "ies"}. ` +
      "In each group the first bookmark is kept (the most specific folder, then the earliest added); choose another to keep it instead. " +
      "The ticked groups' other copies go to the Trash, where Put back restores them. Groups matched only by title start unticked."));
    if (W.locked) body.append(el("div", "alert alert-warning", "This workspace is locked: unlock it to move copies to the Trash."));
    const host = (u: string) => { try { return new URL(u).hostname.replace(/^www\./, ""); } catch { return u; } };
    res.groups.forEach((g, gi) => {
      keepOf.set(g, g.keep);
      const card = el("div", "card mb-2"), head = el("div", "card-header py-2 gap-2 flex-wrap");
      const tick = el("input", "form-check-input m-0") as HTMLInputElement; tick.type = "checkbox"; tick.checked = on.has(g);
      tick.setAttribute("aria-label", "Move this group's other copies to the Trash");
      tick.onchange = () => { if (tick.checked) on.add(g); else on.delete(g); paint(); };
      head.append(tick);
      for (const r of g.reasons) { const b = el("span", "badge " + (r === "similar-title" ? "bg-yellow-lt" : "bg-blue-lt"), REASON[r]?.[0] ?? r); b.dataset.tip = REASON[r]?.[1] ?? ""; head.append(b); }
      head.append(el("span", "text-secondary small ms-auto", `${g.members.length} bookmarks`));
      const list = el("div", "list-group list-group-flush");
      for (const m of g.members) {
        const row = el("label", "list-group-item d-flex align-items-start gap-2 min-w-0");
        const r = el("input", "form-check-input mt-1 flex-none") as HTMLInputElement; r.type = "radio"; r.name = `dk${gi}`; r.checked = m.id === g.keep;
        r.setAttribute("aria-label", "Keep this one");
        r.onchange = () => { if (r.checked) keepOf.set(g, m.id); };
        const text = el("div", "min-w-0 flex-fill");
        const t = el("div", "text-truncate", m.title || m.url); t.dataset.tip = m.title || m.url;
        const sub = el("div", "small text-secondary text-truncate", `${host(m.url)} · ${m.path || "top level"}`); sub.dataset.tip = m.url;
        text.append(t, sub);
        row.append(r, text, el("span", "small text-secondary flex-none", m.added ? fmtDate(m.added) : ""));
        list.append(row);
      }
      card.append(head, list); body.append(card);
    });
    const cancel = el("button", "btn me-auto"); cancel.type = "button"; cancel.textContent = "Close"; cancel.onclick = () => hideModal("#wsDupes");
    go.onclick = async () => {
      const ops = trashOps([...on], keepOf);
      const n = count();
      if (!(await askConfirm({ title: `Move ${n.toLocaleString()} cop${n === 1 ? "y" : "ies"} to the Trash?`, ok: "Move to Trash",
        body: "Each goes to the Trash as a duplicate of the one kept, and joins Duplicates. It is one entry in the history, so Undo brings them all back." }))) return;
      try {
        await busy("Moving copies to the Trash…", async () => { await W.saved?.(); await W.call("POST", ws("/changes"), { ops }); });
        hideModal("#wsDupes"); toast(`${n.toLocaleString()} moved to the Trash.`, "success", "Duplicates");
        setTimeout(() => location.reload(), 700);
      } catch (e) { fail(e); }
    };
    foot.append(cancel, go); paint();
    showModal("#wsDupes");
  }

  // Sort into folders: the owner's filing rules (sort-rules.json in the data folder, sorting.Rules) say where each
  // bookmark goes. The plan shows first, folder by folder; applying makes every move as one history entry.
  type SMove = { id: string; title: string; url: string; from: string; to: string; reason: string };
  type SPlan = { moves: SMove[]; folders: Record<string, number>; new: string[] | null };
  async function sortFolders(): Promise<void> {
    if (W.locked) { toast("Unlock the workspace first.", "warning", "Locked"); return; }
    let plan: SPlan;
    try { plan = (await busy("Working out where each bookmark goes…", async () => { await W.saved?.(); return W.call<{ plan: SPlan }>("POST", ws("/sort"), {}); })).plan; }
    catch (e) { fail(e); return; }
    if (!plan.moves.length) { toast("Every bookmark is already where your rules put it.", "success", "Nothing to sort"); return; }
    const fresh = new Set(plan.new ?? []);
    const into = new Map<string, SMove[]>();
    for (const m of plan.moves) { const l = into.get(m.to); if (l) l.push(m); else into.set(m.to, [m]); }
    const order = [...into.entries()].sort((a, b) => b[1].length - a[1].length || a[0].localeCompare(b[0]));
    const { body, foot } = modal("wsSort", "Sort into folders", "xl", "ti-sort-ascending-letters");
    body.append(el("p", "text-secondary", `${plan.moves.length.toLocaleString()} bookmark${plan.moves.length === 1 ? "" : "s"} would move into ` +
      `${order.length.toLocaleString()} folder${order.length === 1 ? "" : "s"}, ${fresh.size.toLocaleString()} of them new. ` +
      "Untick a folder to leave its bookmarks where they are. Nothing changes until you apply, and applying is one entry in the history, so Undo puts everything back."));
    const ticked = new Set(order.map(([to]) => to));
    const go = el("button", "btn btn-primary"); go.type = "button";
    const paint = () => {
      const n = order.reduce((k, [to, ms]) => k + (ticked.has(to) ? ms.length : 0), 0);
      go.disabled = !n; go.replaceChildren(icon("ti-folders", "me-1"), `Move ${n.toLocaleString()} bookmark${n === 1 ? "" : "s"}`);
    };
    for (const [to, ms] of order) {
      const d = el("details", "card mb-2"), s = el("summary", "card-header py-2 d-flex gap-2 align-items-center min-w-0");
      const tick = el("input", "form-check-input m-0 flex-none") as HTMLInputElement; tick.type = "checkbox"; tick.checked = true;
      tick.setAttribute("aria-label", `Move bookmarks into ${to}`);
      tick.onclick = e => e.stopPropagation();  // ticking does not open or close the folder's list
      tick.onchange = () => { if (tick.checked) ticked.add(to); else ticked.delete(to); paint(); };
      const name = el("span", "text-truncate flex-fill", to || "(top level)"); name.dataset.tip = to;
      s.append(tick, name);
      if (fresh.has(to)) s.append(el("span", "badge bg-blue-lt flex-none", "new"));
      s.append(el("span", "badge bg-secondary-lt flex-none", ms.length.toLocaleString()));
      const list = el("div", "list-group list-group-flush");
      for (const m of ms.slice(0, 50)) {
        const row = el("div", "list-group-item min-w-0");
        const t = el("div", "text-truncate", m.title || m.url); t.dataset.tip = m.url;
        const why = el("div", "small text-secondary text-truncate", `from ${m.from || "top level"} · ${m.reason}`); why.dataset.tip = `from ${m.from || "top level"} · ${m.reason}`;
        row.append(t, why); list.append(row);
      }
      if (ms.length > 50) list.append(el("div", "list-group-item small text-secondary", `… and ${(ms.length - 50).toLocaleString()} more`));
      d.append(s, list); body.append(d);
    }
    const close = el("button", "btn me-auto"); close.type = "button"; close.textContent = "Close"; close.onclick = () => hideModal("#wsSort");
    go.onclick = async () => {
      try {
        const folders = [...ticked];
        const j = await busy("Sorting into folders…", async () => { await W.saved?.(); return W.call<{ entry: Entry | null }>("POST", ws("/sort"), { apply: true, folders }); });
        hideModal("#wsSort"); toast(j.entry ? j.entry.summary : "Nothing to move", "success", "Folders");
        if (j.entry) setTimeout(() => location.reload(), 700);
      } catch (e) { fail(e); }
    };
    foot.append(close, go); paint();
    showModal("#wsSort");
  }

  // Browser extension: the supported browsers first (the five major ones always shown, with whether each is on this
  // computer and connected, and how to install it there), then the pairing code, then the connected browsers.
  // Reached from the Browsers menu, beside Browsers and access (it was in the workspace menu until 2026-11-13).
  type Ext = { id: string; name: string; created: string; last_used?: string };
  // What the server knows (GET extension/guide): which build each installed browser takes, whether it is built, its
  // extensions page; and each extension's last reported version.
  type Guide = { browsers: { browser: string; target: "chromium" | "firefox" | "safari"; page: string; built: boolean }[]; seen: Record<string, { version: string; at: string }> };
  type Waiting = { id: string; browser: string; version: string; match: string; expires: string };
  const EXT_MAJOR = ["Google Chrome", "Microsoft Edge", "Firefox", "Safari", "Brave Browser"];
  const extSteps = (target: string | undefined, page: string): [string, string?][] => {
    if (target === "firefox") return [["Open about:debugging, then This Firefox", page], ["Click Load Temporary Add-on…"],
      ["Choose web/dist/extension/firefox/manifest.json", "web/dist/extension/firefox/manifest.json"], ["Allow it to reach 127.0.0.1 when asked. It stays until the browser restarts."]];
    if (target === "safari") return [["Build the Safari app: make extension-safari (needs Xcode)", "make extension-safari"],
      ["Build and run web/dist/extension/safari-xcode in Xcode", "web/dist/extension/safari-xcode"], ["Turn it on in Safari › Settings › Extensions"]];
    return [[`Open ${page || "chrome://extensions"}`, page || "chrome://extensions"], ["Turn on Developer mode"], ["Click Load unpacked and choose web/dist/extension/chromium", "web/dist/extension/chromium"]];
  };
  /** The last step in every browser: the extension asks to connect by itself, showing four letters on its icon. */
  /** Older than the extension built with this page? Compares dotted numbers; an unknown version is not called old. */
  const olderThan = (v: string, than: string): boolean => {
    if (!v) return false;
    const a = v.split(".").map(Number), b = than.split(".").map(Number);
    for (let k = 0; k < Math.max(a.length, b.length); k++) { const x = a[k] ?? 0, y = b[k] ?? 0; if (x !== y) return x < y; }
    return false;
  };

  // An extension asking to connect: the owner is asked wherever they are in the page, once per request, with the
  // match code the extension shows. Polled while the page is visible (the server keeps requests five minutes).
  const answered = new Set<string>();
  // The browser this page is open in, named as its extension names itself (core.ts), so a request can be matched to it.
  const pageBrowser = (() => {
    const nav = navigator as Navigator & { userAgentData?: { brands: { brand: string }[] }; brave?: unknown };
    return browserName(navigator.userAgent, (nav.userAgentData?.brands ?? []).map(b => b.brand), !!nav.brave);
  })();
  /** Settings › Browser extensions: strict approves a browser only from a page open in that same browser; loose, from any. */
  const approvableHere = (q: Waiting): boolean => document.documentElement.dataset.extApprove === "loose" || q.browser === pageBrowser;
  /** Approve or deny a request; one that already expired or was answered elsewhere says so plainly. */
  async function sendAnswer(q: Waiting, ok: boolean): Promise<void> {
    answered.add(q.id);
    try { await W.call("POST", `extension/requests/${encodeURIComponent(q.id)}/${ok ? "approve" : "deny"}`); toast(ok ? `${q.browser} is connected.` : `${q.browser} was not connected.`, ok ? "success" : "info"); }
    catch (e) {
      if ((e as { status?: number }).status === 404 || /no request waiting|has expired|already answered/.test(String((e as Error).message))) toast(`${q.browser}'s request already expired or was answered. It asks again within a minute, or press Connect in its popup.`, "warning");
      else fail(e);
    }
  }
  async function answerRequest(q: Waiting): Promise<void> {
    const a = await askDecide({ title: `Connect ${q.browser}?`, ok: "Connect", no: "Deny", later: "Not now",
      body: `Browser Foreman's extension in ${q.browser} asks to connect, showing ${q.match.split("").join(" ")} on its toolbar icon. Connect only if the letters match. Not now leaves it waiting: approve it later in Browsers › Browser extensions…` });
    if (a === "later") { answered.add(q.id); toast(`${q.browser} is still waiting: approve it in Browser extensions… before it expires.`, "info"); return; }
    await sendAnswer(q, a === "yes");
  }
  let watching = false;
  function watchRequests(): void {
    if (watching) return; watching = true;
    let asking = false;
    const tick = async () => {
      if (document.hidden || asking) return;
      let list: Waiting[];
      try { list = (await W.call<{ requests: Waiting[] }>("GET", "extension/requests")).requests; }
      catch { watching = false; clearInterval(timer); return; }  // not the owner (or the server is gone): stop asking
      const q = list.find(x => !answered.has(x.id) && approvableHere(x)); if (!q) return; // strict: only this browser's own
      answered.add(q.id); asking = true;
      try { await answerRequest(q); } finally { asking = false; }
    };
    const timer = setInterval(() => void tick(), 3000); void tick();
  }

  async function extensions(): Promise<void> {
    const { body, foot } = modal("v1Extensions", "Browser extensions", "lg", "ti-puzzle");
    let connected: Ext[] = [], installed = new Set<string>(), waiting: Waiting[] = [];
    let guide: Guide = { browsers: [], seen: {} };
    const copy = (text: string) => async () => { try { await navigator.clipboard.writeText(text); toast("Copied.", "success"); } catch { toast("Copy it by hand.", "warning"); } };
    body.append(el("p", "text-secondary mb-0", "With the extension, a browser sends its tabs and tab groups, reads its bookmarks into a workspace, and can write a workspace back. Set it up once in each browser you use: it then asks to connect by itself, and you approve it here."));
    // Asking to connect: first, and hard to miss.
    const asks = el("div", "ext-asks");
    // The browsers, one row each: connected and installed ones first; the others folded away.
    const list = el("div", "ext-list"), others = el("details", "ext-others"), othersList = el("div", "ext-list");
    others.append(el("summary", null, "Other browsers"), othersList);
    // The rows are painted again every few seconds (load() below): what is open stays open across that.
    const openSteps = new Set<string>();
    const started = new Map<string, number>(); // when Set it up was pressed, for the guide's "waiting" time and hints
    let othersOpen = false;
    others.addEventListener("toggle", () => { othersOpen = others.open; });

    const decide = (q: Waiting, ok: boolean) => async () => { await sendAnswer(q, ok); await load(); };
    const snoozed = new Set<string>(); // Not now: its card leaves the top; it stays in its browser's guide until it expires
    // Connect and Deny for a request, with Not now; in strict mode Connect waits for a page open in that browser.
    const answerButtons = (q: Waiting, small: boolean, withLater: boolean): HTMLElement[] => {
      const sz = small ? " btn-sm" : "";
      const out: HTMLElement[] = [];
      if (withLater) { const later = el("button", "btn btn-ghost-secondary" + sz); later.type = "button"; later.textContent = "Not now"; later.onclick = () => { snoozed.add(q.id); answered.add(q.id); paintAsks(); }; out.push(later); }
      const no = el("button", "btn" + sz); no.type = "button"; no.textContent = "Deny"; no.onclick = decide(q, false);
      const yes = el("button", "btn btn-primary" + sz); yes.type = "button"; yes.append(icon("ti-plug-connected", "me-1"), "Connect"); yes.onclick = decide(q, true);
      if (!approvableHere(q)) { yes.disabled = true; yes.dataset.tip = `Open Browser Foreman in ${q.browser} to approve it (Settings › Browser extensions)`; }
      out.push(no, yes); return out;
    };
    const paintAsks = () => {
      asks.replaceChildren(...waiting.filter(q => !snoozed.has(q.id)).map(q => {
        const card = el("div", "ext-ask"), txt = el("div", "ext-ask-text");
        txt.append(el("div", "fw-semibold", `${q.browser} asks to connect`),
          el("div", "small text-secondary", approvableHere(q) ? "Connect only if the extension's toolbar icon (or its popup) shows these same letters. Not you? Deny it."
            : `To approve it, open Browser Foreman in ${q.browser} (approvals are strict: Settings › Browser extensions). You can still deny it here.`));
        const letters = lettersWithCopy(q.match);
        const acts = el("div", "ext-ask-acts");
        acts.append(...answerButtons(q, true, true)); // body actions are small, as in every row
        card.append(icon("browser:" + q.browser, "ext-ico"), txt, letters, acts);
        return card;
      }));
      asks.hidden = !asks.childElementCount;
    };

    const row = (name: string): HTMLElement => {
      const c = connected.find(x => x.name.replace(/^extension:/, "") === name);
      const g = guide.browsers.find(b => b.browser === name), seen = guide.seen[name];
      const here = installed.has(name), old = !!c && !!seen && olderThan(seen.version, EXT_VERSION);
      const asking = waiting.find(q => q.browser === name);
      const r = el("div", "ext-row" + (c ? " is-connected" : ""));
      const main = el("div", "ext-row-main"), who = el("div", "ext-who");
      who.append(el("div", "fw-medium text-truncate", name));
      const heard = c?.last_used && !c.last_used.startsWith("0001") ? `heard from ${fmtDate(Date.parse(c.last_used) / 1000)}` : "";
      const pill = (cls: string, text: string) => { const p = el("span", "ext-pill " + cls); p.append(el("span", "ext-pill-dot"), text); return p; };
      const meta = el("div", "ext-meta");
      if (c) meta.append(pill(old ? "is-warn" : "is-ok", old ? `Connected · update to ${EXT_VERSION}` : "Connected"),
        ...[seen?.version ? `version ${seen.version}` : "", heard].filter(Boolean).map(t => el("span", "ext-meta-text", t)));
      else if (asking) meta.append(pill("is-ask", "Asking to connect"));
      else meta.append(pill(here ? "is-idle" : "is-off", here ? "Installed on this computer · not connected" : "Not on this computer"));
      who.append(meta);
      main.append(icon("browser:" + name, "ext-ico"), who);
      // The one next step for this browser.
      const act = el("div", "ext-row-act");
      if (c && old) {
        const b = el("button", "btn btn-sm btn-warning"); b.type = "button"; b.append(icon("ti-refresh", "me-1"), "Update");
        b.dataset.tip = "The extension reloads itself and reads the new build";
        b.onclick = async () => { try { await W.call("POST", "extension/commands", { browser: name, kind: "reload" }); toast(`${name}'s extension is reloading.`, "success"); setTimeout(() => void load(), 4000); } catch (e) { fail(e); } };
        act.append(b);
      } else if (!c && !asking && g?.built) {
        const b = el("button", "btn btn-sm btn-primary"); b.type = "button"; b.append(icon("ti-player-play", "me-1"), "Set it up");
        b.dataset.tip = g.page ? `Opens ${g.page} in ${name} and shows the extension's folder in Finder` : "Shows the extension's folder in Finder";
        b.onclick = async () => {
          openSteps.add(name); if (!started.has(name)) started.set(name, Date.now()); paintRows();
          try { await W.call("POST", "extension/setup", { browser: name }); toast("Follow the steps under the browser. It asks to connect by itself once it is loaded.", "info", `Setting up ${name}`); }
          catch (e) { fail(e); }
        };
        act.append(b);
      } else if (!c && g && !g.built) {
        act.append(el("span", "small text-secondary", "Build it first: make extension"));
      }
      if (c) {
        const b = el("button", "btn btn-sm btn-danger"); b.type = "button"; b.append(icon("ti-plug-connected-x", "me-1"), "Disconnect…");
        b.onclick = async () => {
          if (!(await askConfirm({ title: `Disconnect ${name}?`, ok: "Disconnect", no: "Keep connected",
            body: `${name}'s extension stops sending its tabs and can no longer read or write bookmarks here. Its key is revoked at once, and it does not ask to connect again until you press Connect in its popup.` }))) return;
          try { await W.call("DELETE", `tokens/${encodeURIComponent(c.id)}`); toast(`${name} is disconnected.`, "success"); } catch (e) { fail(e); }
          await load();
        };
        act.append(b);
      }
      main.append(act);
      // The setup guide: where this browser stands, step by step, with what to do now and what may be wrong.
      const steps = el("details", "ext-steps-box"); steps.open = openSteps.has(name) || (!c && (!!asking || started.has(name)));
      steps.addEventListener("toggle", () => { if (steps.open) openSteps.add(name); else openSteps.delete(name); });
      steps.append(el("summary", null, c ? "Setup check" : "Setup guide"), guideFor(name, { c, g, seen, here, old, asking }));
      r.append(main, steps);
      return r;
    };
    // The guide's checks, from what the server sees: the browser on this computer, the extension built, the extension
    // calling in (it asks to connect within a minute of being loaded), the owner's approval, its tab reports, its version.
    type Facts = { c?: Ext; g?: Guide["browsers"][number]; seen?: { version: string; at: string }; here: boolean; old: boolean; asking?: Waiting };
    // The letters, big, with a button that copies them (for `browser-foreman pair --approve --match`).
    const lettersWithCopy = (match: string, small = false): HTMLElement => {
      const w = el("span", "ext-letters-wrap");
      const l = el("span", "ext-letters font-monospace" + (small ? " ext-letters-sm" : ""), match); l.setAttribute("aria-label", "Letters: " + match.split("").join(" "));
      const b = el("button", "btn btn-sm btn-ghost-secondary btn-icon"); b.type = "button"; b.setAttribute("aria-label", "Copy the letters"); b.dataset.tip = "Copy the letters";
      b.append(icon("ti-copy")); b.onclick = copy(match);
      w.append(l, b); return w;
    };
    const guideFor = (name: string, f: Facts): HTMLElement => {
      const since = started.get(name), waited = since ? Math.round((Date.now() - since) / 1000) : 0;
      const lastReport = f.seen?.at ? Date.parse(f.seen.at) : 0, quiet = lastReport ? Date.now() - lastReport : Infinity;
      const loaded = !!(f.c || f.asking || f.seen);
      type State = "done" | "now" | "problem" | "todo";
      type Step = { state: State; title: string; detail: (Node | string)[] };
      const note = (t: string) => el("div", "ext-check-note", t);
      const copyBtn = (val: string) => { const b = el("button", "btn btn-sm btn-ghost-secondary btn-icon"); b.type = "button"; b.setAttribute("aria-label", "Copy " + val); b.dataset.tip = "Copy " + val; b.append(icon("ti-copy")); b.onclick = copy(val); return b; };

      // 1. On this computer: the browser, and the extension built for it.
      const machine: Step[] = [
        f.here ? { state: "done", title: `${name} is installed`, detail: [] }
          : { state: "todo", title: `${name} is not installed`, detail: [note("Nothing to do here unless you use it: install it first, then set the extension up.")] },
      ];
      if (f.g?.built || loaded) machine.push({ state: "done", title: "The extension is built", detail: [] });
      else { const line = el("div", "ext-check-note"); line.append("In the project folder, run ", el("code", null, "make extension"), copyBtn("make extension")); machine.push({ state: "problem", title: "The extension is not built yet", detail: [line] }); }

      // 2. In the browser: loading it, with the steps until it calls in and the usual causes if it stays silent.
      const inBrowser: Step[] = [];
      if (loaded) inBrowser.push({ state: "done", title: "The extension is loaded", detail: [] });
      else {
        const inner = el("ol", "ext-stepper");
        extSteps(f.g?.target ?? (name === "Firefox" ? "firefox" : name === "Safari" ? "safari" : "chromium"), f.g?.page ?? "").forEach(([t, val], k) => {
          const li = el("li"); li.append(el("span", "ext-step-n", String(k + 1)), el("span", "ext-step-t", t)); if (val) li.append(copyBtn(val)); inner.append(li);
        });
        const detail: (Node | string)[] = [inner];
        if (since) detail.push(note(`Waiting for it to call in · ${Math.floor(waited / 60)}:${String(waited % 60).padStart(2, "0")}. It asks to connect within a minute of being loaded.`));
        if (waited > 120) {
          const tips = el("ul", "ext-hints");
          for (const t of ["Developer mode must be on before Load unpacked appears.", "Choose the chromium folder itself: the one holding manifest.json.",
            `If the extension's card in ${name} shows an Errors button, open it: the message says what went wrong.`, "Open the extension's popup and press Connect: it asks at once, and says why if it cannot."]) tips.append(el("li", null, t));
          detail.push(el("div", "ext-check-note fw-medium", "Still nothing? The usual causes:"), tips);
        }
        inBrowser.push({ state: since ? "now" : "todo", title: since ? "Waiting for the extension to load" : `Load the extension in ${name}`, detail });
      }

      // 3. Connection: approving, with the letters here.
      const connection: Step[] = [];
      if (f.c) connection.push({ state: "done", title: "Connected", detail: [note(`Since ${fmtDate(Date.parse(f.c.created) / 1000)}.`)] });
      else if (f.asking) {
        const q = f.asking, row = el("div", "ext-inline-ask");
        row.append(lettersWithCopy(q.match, true), ...answerButtons(q, true, false));
        const expires = new Date(q.expires).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
        connection.push({ state: "problem", title: "Approve the connection", detail: [note(approvableHere(q)
          ? `Connect only if the extension's toolbar icon shows ${q.match.split("").join(" ")} too. It waits until ${expires}.`
          : `Open Browser Foreman in ${name} to approve it: approvals are strict (Settings › Browser extensions). It waits until ${expires}.`), row] });
      } else connection.push({ state: "todo", title: "Approve the connection", detail: [note("When it asks, its four letters show here and on its toolbar icon.")] });

      // 4. Working: its tab reports, and its version.
      const working: Step[] = [];
      if (f.c) {
        if (quiet < 3 * 60_000) working.push({ state: "done", title: "Sending its tabs", detail: [note(`Last report ${fmtDate(lastReport / 1000)}.`)] });
        else if (!lastReport) working.push({ state: "now", title: "Waiting for its first tab report", detail: [note("It reports within a minute while the browser is open.")] });
        else working.push({ state: "problem", title: "Gone quiet", detail: [note(`Not heard from since ${fmtDate(lastReport / 1000)}. Is ${name} open? The extension only runs while it is.`)] });
        if (f.old) {
          const b = el("button", "btn btn-sm btn-warning"); b.type = "button"; b.append(icon("ti-refresh", "me-1"), "Update now");
          b.onclick = async () => { try { await W.call("POST", "extension/commands", { browser: name, kind: "reload" }); toast(`${name}'s extension is reloading.`, "success"); } catch (e) { fail(e); } };
          working.push({ state: "problem", title: `Update available: ${f.seen?.version} → ${EXT_VERSION}`, detail: [b] });
        } else if (f.seen?.version) working.push({ state: "done", title: `Up to date (${f.seen.version})`, detail: [] });
      } else working.push({ state: "todo", title: "Sending its tabs", detail: [] });

      // The tree: four groups, each with its overall state.
      const groupState = (steps: Step[]): State => steps.some(x => x.state === "problem") ? "problem" : steps.some(x => x.state === "now") ? "now" : steps.every(x => x.state === "done") ? "done" : "todo";
      const label: Record<State, string> = { done: "Done", now: "In progress", problem: "Needs you", todo: "Not yet" };
      const node = (state: State) => {
        const m = el("span", "ext-node");
        if (state === "now") { const sp = el("span", "spinner-border ext-node-spin"); sp.setAttribute("role", "status"); sp.setAttribute("aria-label", "In progress"); m.append(sp); }
        else if (state !== "todo") m.append(icon(state === "done" ? "ti-check" : "ti-exclamation-mark")); // "not yet": a CSS dot, centred
        return m;
      };
      const tree = el("ol", "ext-tree");
      for (const [gIcon, gTitle, steps] of [["ti-device-desktop", "On this computer", machine], ["ti-puzzle", `In ${name}`, inBrowser], ["ti-plug-connected", "Connection", connection], ["ti-activity-heartbeat", "Working", working]] as [string, string, Step[]][]) {
        const st = groupState(steps), g = el("li", "ext-group is-" + st), head = el("div", "ext-group-head");
        head.append(el("span", "ext-group-ico", ""), el("span", "ext-group-title", gTitle), el("span", "ext-group-badge", label[st]));
        head.firstChild!.appendChild(icon(gIcon));
        const items = el("ol", "ext-items");
        for (const x of steps) {
          const li = el("li", "ext-item is-" + x.state), body = el("div", "ext-item-body");
          body.append(el("div", "ext-item-title", x.title), ...x.detail);
          li.append(node(x.state), body); items.append(li);
        }
        g.append(head, items); tree.append(g);
      }
      const box = el("div", "ext-guide"); box.append(tree);
      if (!loaded && location.port && location.port !== "8765") // nothing called in: it may be looking elsewhere
        box.append(el("div", "ext-diag", `Browser Foreman is on port ${location.port}, but the extension looks for 8765: in its popup, set the address to ${location.origin} and press Connect.`));
      return box;
    };
    const paintRows = () => {
      const names = [...EXT_MAJOR, ...guide.browsers.map(b => b.browser).filter(n => !EXT_MAJOR.includes(n))];
      const rank = (n: string) => connected.some(x => x.name === "extension:" + n) ? 0 : waiting.some(q => q.browser === n) ? 0 : installed.has(n) ? 1 : 2;
      const sorted = [...names].sort((a, b) => rank(a) - rank(b));
      list.replaceChildren(...sorted.filter(n => rank(n) < 2).map(row));
      othersList.replaceChildren(...sorted.filter(n => rank(n) === 2).map(row));
      others.hidden = !othersList.childElementCount; others.open = othersOpen;
    };
    body.append(asks, el("div", "ext-section", "Your browsers"), list, others,
      el("div", "text-secondary small mt-2", "Also works in Vivaldi, Opera and other Chromium browsers (set up as in Chrome); they appear here when they are on this computer."));
    // A pairing code: for when connecting by itself cannot reach this server (another address, another computer's browser).
    const codeBox = el("div", "mt-2");
    const make = el("button", "btn"); make.type = "button"; make.append(icon("ti-key", "me-1"), "Make a pairing code");
    make.dataset.tip = "For a browser that cannot ask by itself: type the code into its popup";
    make.onclick = async () => {
      try {
        const r = await W.call<{ code: string; expires: string }>("POST", "extension/pairings");
        const card = el("div", "ext-code"), code = el("div", "h1 font-monospace m-0", r.code);
        const cp = el("button", "btn btn-sm ms-auto"); cp.type = "button"; cp.append(icon("ti-copy", "me-1"), "Copy"); cp.onclick = copy(r.code);
        card.append(code, el("div", "text-secondary small", `Type it into the extension's popup with the server address ${location.origin}. Works once, until ${new Date(r.expires).toLocaleTimeString()}.`), cp);
        codeBox.replaceChildren(card);
      } catch (e) { fail(e); }
    };
    body.append(codeBox);
    const load = async () => {
      const [ext, br, gd, rq] = await Promise.allSettled([W.call<{ extensions: Ext[] }>("GET", "extension/status"), W.call<{ installed?: { name: string }[] }>("GET", "browsers"),
        W.call<Guide>("GET", "extension/guide"), W.call<{ requests: Waiting[] }>("GET", "extension/requests")]);
      connected = ext.status === "fulfilled" ? ext.value.extensions : [];
      installed = new Set(br.status === "fulfilled" ? (br.value.installed ?? []).map(b => b.name) : []);
      guide = gd.status === "fulfilled" ? gd.value : { browsers: [], seen: {} };
      // One request per browser: its newest (the popup's Connect can ask while an earlier request still waits).
      const newest = new Map<string, Waiting>();
      for (const q of rq.status === "fulfilled" ? rq.value.requests : []) { const o = newest.get(q.browser); if (!o || q.expires > o.expires) newest.set(q.browser, q); }
      waiting = [...newest.values()];
      for (const q of waiting) answered.add(q.id); // answered here, so the page-wide prompt does not ask as well
      if (ext.status === "rejected") fail(ext.reason);
      paintAsks(); paintRows();
    };
    const close = el("button", "btn me-auto"); close.type = "button"; close.textContent = "Close"; close.onclick = () => hideModal("#v1Extensions");
    foot.append(close, make);
    list.append(el("div", "text-secondary small py-2", "Asking which browsers are connected…"));
    showModal("#v1Extensions"); await load();
    // While the dialog is open, a browser being set up or asking to connect shows up by itself.
    const live = setInterval(() => { if (!document.getElementById("v1Extensions")?.classList.contains("show")) clearInterval(live); else void load(); }, 3000);
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
    const name = el("span", "text-truncate", W.workspace.name); name.dataset.tip = W.workspace.name;  // width: #hdrWs rules in template.html
    // The button as it always was: an icon and the name. The icon is the workspace's own, in its colour.
    btn.append(icon("ti-" + (W.workspace.icon || "stack-2"), "me-1" + (W.workspace.color ? ` text-${W.workspace.color}` : "")), name);
    if (W.locked) { const b = el("span", "badge bg-yellow-lt ms-2"); b.append(icon("ti-lock", "me-1"), "locked"); btn.append(b); }
    if (W.workspace.archived) btn.append(el("span", "badge bg-secondary-lt ms-2", "archived"));
    btn.append(icon("ti-chevron-down", "ms-1"));
    const dm = el("div", "dropdown-menu");
    dm.append(el("h6", "dropdown-header", "Switch workspace"));
    const live = W.workspaces.filter(w => !w.archived), archived = W.workspaces.length - live.length;
    for (const w of live) {
      const a = el("a", "dropdown-item ws-item" + (w.id === W.id ? " active" : "")); a.href = "#";
      a.append(wsBadge(w, "avatar-xs me-2"), el("span", "flex-fill text-truncate", w.name));
      if (w.id === W.id) a.append(icon("ti-check", "ms-2"));
      a.onclick = e => { e.preventDefault(); closeDropdowns(); if (w.id !== W.id) nav(w.id); };
      dm.append(a);
    }
    dm.append(item("ti-list-details", "All workspaces" + (archived ? ` (${archived} archived)` : "") + "…", manage),
      el("div", "dropdown-divider"), el("h6", "dropdown-header", "This workspace"),
      item("ti-palette", "Appearance…", () => void appearance({ ...W.workspace, id: W.id })),
      item("ti-history", "History and progress…", () => void history()),
      item("ti-refresh-dot", "Update from source…", () => void updateWs()),
      item("ti-files", "Find duplicates…", () => void findDuplicates()),
      item("ti-folders", "Sort into folders…", () => void sortFolders()));
    if (W.locked) dm.append(item("ti-lock-open", "Unlock", () => void toggleLock(false)));
    else dm.append(item("ti-pencil", "Rename…", () => void rename()), item("ti-notes", "Notes…", () => void notes()),
      item("ti-lock", "Lock (read-only)…", () => void toggleLock(true)),
      item("ti-arrow-back-up-double", "Reset to how it started…", () => void resetWs()),
      W.workspace.archived ? item("ti-archive-off", "Bring back from the archive", () => void archive(false)) : item("ti-archive", "Archive…", () => void archive(true)),
      item("ti-trash", "Delete…", () => manage("use"), "text-danger"));
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

  // Write back to a browser: the same plan and checks as `browser-foreman apply`, through the browser's extension.
  // Preview reads the browser now and plans; Apply reads it again, refuses if it changed since the preview, writes
  // (the extension saves the browser first), then reads it back and plans again to verify.
  type WbItem = { title: string; url?: string; folder: string; to?: string; was?: string; was_url?: string; is_folder?: boolean };
  type WbReport = { mode: string; added: WbItem[] | null; removed: WbItem[] | null; moved: WbItem[] | null; renamed: WbItem[] | null;
    added_since: WbItem[] | null; removed_since: WbItem[] | null; moved_since: WbItem[] | null; renamed_since: WbItem[] | null;
    conflicts: WbItem[] | null; drop_new: number; not_written: number };
  async function writeBack(): Promise<void> {
    const { body, foot } = modal("wsWriteBack", "Write back to a browser", "lg", "ti-upload");
    body.append(el("p", "text-secondary mb-3", `Writes “${W.workspace.name}” into a browser's own bookmarks, through its extension: what the browser added, moved or deleted after the export is kept (merge), or dropped once you confirm how much (replace). Preview first; nothing changes until you apply.`));
    let connected: string[] = [];
    try { connected = (await W.call<{ extensions: { name: string }[] }>("GET", "extension/status")).extensions.map(x => x.name.replace(/^extension:/, "")); } catch (e) { fail(e); }
    const field = (label: string, control: HTMLElement, hint = "") => { const f = el("div", "mb-3"); f.append(el("label", "form-label", label), control); if (hint) f.append(el("div", "form-hint", hint)); return f; };
    const browserSel = el("select", "form-select"); for (const n of connected) browserSel.append(new Option(n, n));
    const baseKey = "bf:wb-base:" + W.id;
    const others = W.workspaces.filter(w => w.id !== W.id && !w.archived);
    const baseSel = el("select", "form-select"); for (const w of others) baseSel.append(new Option(w.name, w.id));
    let remembered = ""; try { remembered = localStorage.getItem(baseKey) ?? ""; } catch { /* private window */ }
    baseSel.value = others.some(w => w.id === remembered) ? remembered : (others.find(w => /original|export/i.test(w.id + w.name))?.id ?? others[0]?.id ?? "");
    const modeSel = el("select", "form-select");
    modeSel.append(new Option("Merge: apply the cleanup, keep what the browser changed after the export", "merge"), new Option("Replace: the browser becomes the cleanup (asks before dropping what it gained)", "replace"));
    const out = el("div", "mt-3");
    if (!connected.length) {
      const b = el("button", "btn btn-sm btn-primary"); b.type = "button"; b.append(icon("ti-puzzle", "me-1"), "Browser extensions…"); b.onclick = () => { hideModal("#wsWriteBack"); void extensions(); };
      const e = el("div", "empty py-4"), ic = el("div", "empty-icon"); ic.append(icon("ti-plug-connected-x"));
      const act = el("div", "empty-action"); act.append(b);
      e.append(ic, el("p", "empty-title", "No browser is connected"), el("p", "empty-subtitle text-secondary", "Writing back goes through a browser's extension. Set it up and connect it first."), act);
      body.append(e);
    } else if (!others.length) {
      const e = el("div", "empty py-4"), ic = el("div", "empty-icon"); ic.append(icon("ti-files"));
      e.append(ic, el("p", "empty-title", "The export this cleanup started from is missing"),
        el("p", "empty-subtitle text-secondary", "Writing back compares three things: the export, this cleanup and the browser now. Import the export the cleanup started from into a workspace of its own, then come back."));
      body.append(e);
    } else body.append(field("Browser", browserSel, "Only browsers whose extension is connected."), field("The export this cleanup started from", baseSel, "Changes the browser made after it are told apart from the cleanup's."), field("Mode", modeSel), out);
    const close = el("button", "btn me-auto"); close.type = "button"; close.textContent = "Close"; close.onclick = () => hideModal("#wsWriteBack");
    const preview = el("button", "btn"); preview.type = "button"; preview.append(icon("ti-eye", "me-1"), "Preview");
    const apply = el("button", "btn btn-danger"); apply.type = "button"; apply.append(icon("ti-upload", "me-1"), "Apply…"); apply.disabled = true;
    foot.append(close, ...(connected.length && others.length ? [preview, apply] : []));
    let expect = "", report: WbReport | null = null;
    const req = () => ({ workspace: W.id, base: baseSel.value, browser: browserSel.value, mode: modeSel.value });
    const invalidate = () => { expect = ""; report = null; apply.disabled = true; out.replaceChildren(); };
    for (const c of [browserSel, baseSel, modeSel]) c.addEventListener("change", invalidate);
    const n = (xs: WbItem[] | null) => xs?.length ?? 0;
    const changes = (r: WbReport) => n(r.added) + n(r.removed) + n(r.moved) + n(r.renamed);
    const list = (title: string, xs: WbItem[] | null, line: (x: WbItem) => string) => {
      if (!n(xs)) return null;
      const d = el("details", "ext-steps-box"); d.append(el("summary", null, `${title} · ${n(xs).toLocaleString()}`));
      const ul = el("ul", "small text-secondary mt-1 mb-0");
      for (const x of xs!.slice(0, 50)) ul.append(el("li", null, line(x)));
      if (n(xs) > 50) ul.append(el("li", null, `… and ${(n(xs) - 50).toLocaleString()} more`));
      d.append(ul); return d;
    };
    const where = (x: WbItem) => `${x.title}${x.is_folder ? "/" : ""} (${x.folder})`;
    const paint = (r: WbReport) => {
      const stat = (label: string, v: number, tone: string) => { const c = el("div", "col"); const b = el("div", "card card-sm"); const bb = el("div", "card-body");
        bb.append(el("div", `h3 m-0 text-${v ? tone : "secondary"}`, v.toLocaleString()), el("div", "text-secondary small", label)); b.append(bb); c.append(b); return c; };
      const row = el("div", "row g-2 mb-2");
      row.append(stat("to add", n(r.added), "green"), stat("to remove", n(r.removed), "red"), stat("to move", n(r.moved), "primary"), stat("to rename or readdress", n(r.renamed), "yellow"));
      const since = n(r.added_since) + n(r.removed_since) + n(r.moved_since) + n(r.renamed_since);
      const notes: HTMLElement[] = [];
      if (!changes(r)) notes.push(el("div", "alert alert-success mb-2", `${browserSel.value} already holds this: there is nothing to write.`));
      notes.push(el("div", "small text-secondary mb-2", since ? `${browserSel.value} changed ${plural(since, "bookmark")} after the export: ${r.mode === "merge" ? "merge keeps them" : "replace drops what it added"}.` : `${browserSel.value} changed nothing after the export.`));
      if (n(r.conflicts)) notes.push(el("div", "small text-warning mb-2", `${plural(n(r.conflicts), "bookmark")} ${n(r.conflicts) === 1 ? "was" : "were"} changed differently on both sides: the cleanup wins.`));
      if (r.not_written) notes.push(el("div", "small text-secondary mb-2", `${plural(r.not_written, "bookmark")} ${r.not_written === 1 ? "carries" : "carry"} notes, descriptions, ratings or tags: a browser has no place for them, so they stay here.`));
      const lists = [list("Added", r.added, where), list("Removed", r.removed, where), list("Moved", r.moved, x => `${x.title} (${x.folder} → ${x.to})`),
        list("Renamed or readdressed", r.renamed, x => x.was_url ? `${x.was_url} → ${x.url}` : `${x.was} → ${x.title}`),
        list("Added in the browser after the export", r.added_since, where), list("Deleted in the browser after the export (stay deleted)", r.removed_since, where)].filter(Boolean) as HTMLElement[];
      out.replaceChildren(row, ...notes, ...lists);
    };
    preview.onclick = async () => {
      try { localStorage.setItem(baseKey, baseSel.value); } catch { /* private window */ }
      try {
        const j = await busy(`Reading ${browserSel.value}'s bookmarks and planning…`, () => W.call<{ report: WbReport; expect: string }>("POST", "writeback/preview", req()));
        report = j.report; expect = j.expect; paint(j.report); apply.disabled = !changes(j.report);
      } catch (e) { invalidate(); fail(e); }
    };
    apply.onclick = async () => {
      if (!report || !expect) return;
      const r = report, total = changes(r);
      const ok = await askConfirm({ title: `Write ${plural(total, "change")} into ${browserSel.value}?`, ok: "Write them", no: "Cancel",
        body: `Its bookmarks as they are now are saved to Browser Foreman first, so this can be undone from there. If ${browserSel.value} syncs its bookmarks, the change reaches every device signed in to it.` + (r.drop_new ? ` Replace drops ${plural(r.drop_new, "bookmark")} it gained after the export.` : "") });
      if (!ok) return;
      try {
        const j = await busy(`Writing into ${browserSel.value}… This can take a few minutes for thousands of bookmarks.`, () =>
          W.call<{ written: { created: number; moved: number; updated: number; removedBookmarks: number; removedFolders: number; backup: string } }>("POST", "writeback/apply", { ...req(), expect, drop_new: r.drop_new }));
        const w = j.written;
        toast(`${w.created.toLocaleString()} created, ${w.moved.toLocaleString()} moved, ${w.updated.toLocaleString()} renamed, ${(w.removedBookmarks + w.removedFolders).toLocaleString()} removed. Read back and checked: it holds the plan.`, "success", `Written into ${browserSel.value}`);
        invalidate(); out.replaceChildren(el("div", "alert alert-success", `${browserSel.value} now holds “${W.workspace.name}”. Its bookmarks as they were are saved: ${w.backup}`));
      } catch (e) { fail(e); }
    };
    showModal("#wsWriteBack");
  }

  W.ui = {
    history, manage, importer, addFiles, addLinks, updateWs, browserHistory, resetWs, menu, download, toggleLock, backup, restore, extensions, writeBack, duplicatesCsv, findDuplicates, sortFolders,
    rename, notes,
  };
  watchRequests();  // an extension asking to connect is answered from wherever the owner is in the page
  // The workspace switcher sits in the top bar beside the logo, so it is there in every view (Bookmarks, Open tabs,
  // Timeline): a workspace scopes all three. It was in the Bookmarks toolbar until 2026-11-13.
  function remount(): void { document.getElementById("wsMenu")?.remove(); document.querySelector("#hdrWs")?.append(menu()); }
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", remount); else remount();
}
