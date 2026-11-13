// The workspace menu, History window and file handling when the Go server serves the page (window.BM_WS.v1, set by
// v1-boot.ts). It fills window.BM_WS.ui, the hooks the page's Download menu, palette and Settings already call, and
// talks to /api/v1 through the boot client's authenticated `call`.

import { askConfirm, askText, hideModal, showModal, toast } from "./dialogs";
import { closeDropdowns } from "./menu";
import { el, fmtDate, icon } from "./ui";
import { DataTable, avatar, by, nameCell } from "./datatable";
import { RangePicker } from "./rangepick";
import { lookPicker, wsBadge } from "./wslook";

interface Info { id: string; name: string; head: number; notes?: string; locked?: boolean; archived?: boolean; deleted?: string; bookmarks?: number; color?: string; icon?: string }
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

  function modal(id: string, title: string, size: "lg" | "xl" = "lg"): { body: HTMLElement; foot: HTMLElement; dock: HTMLElement } {
    document.getElementById(id)?.remove();
    const m = el("div", "modal modal-blur"); m.id = id; m.tabIndex = -1; m.setAttribute("role", "dialog"); m.setAttribute("aria-label", title);
    const dlg = el("div", `modal-dialog modal-dialog-top modal-dialog-scrollable modal-${size}`), box = el("div", "modal-content");
    const head = el("div", "modal-header"), close = el("button", "btn-close");
    close.type = "button"; close.setAttribute("aria-label", "Close"); close.onclick = () => hideModal("#" + id);
    head.append(el("h5", "modal-title", title), close);
    // dock: a strip between the scrolling body and the footer buttons, for a list's pager, so it is always in view.
    const body = el("div", "modal-body"), dock = el("div", "modal-dock"), foot = el("div", "modal-footer");
    box.append(head, body, dock, foot); dlg.append(box); m.append(dlg);
    m.addEventListener("mousedown", e => { if (e.target === m) hideModal("#" + id); });
    m.addEventListener("keydown", e => { if (e.key === "Escape") { e.stopPropagation(); hideModal("#" + id); } });
    document.body.append(m);
    return { body, foot, dock };
  }

  async function history(): Promise<void> {
    let h: Entry[];
    try { h = (await W.call<{ history: Entry[] }>("GET", ws("/history"))).history; } catch (e) { return fail(e); }
    const { body, dock } = modal("v1History", `History of “${W.workspace.name}”`, "xl");
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
    table.rows([...h].reverse());
    showModal("#v1History");
  }


  // Workspaces: the ones in use, and the workspace Trash (deleted ones, kept whole until it is emptied), as a Tabler
  // data table with tabs, search, sorting and pages.
  function manage(tab: "use" | "trash" = "use"): void {
    const { body, foot, dock } = modal("v1Workspaces", "Workspaces", "xl");
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
    paint(); void load(); showModal("#v1Workspaces");
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
    const { body, foot } = modal("v1Look", `Appearance of “${w.name}”`);
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
    const { body, foot } = modal("v1Sources", into === "new" ? "Import bookmarks as a new workspace" : `Add bookmarks to “${W.workspace.name}”`);
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
    const { body, foot, dock } = modal("v1History2", "Browser history", "xl");
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
      note.replaceChildren(); const sp = el("div", "p-4 text-center text-secondary"); sp.append(el("span", "spinner-border spinner-border-sm me-2"), "Reading your browsers' history…"); note.append(sp);
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
          const fda = el("button", "btn btn-sm btn-warning"); fda.type = "button"; fda.append(icon("ti-shield-lock", "me-1"), "Open Full Disk Access");
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

  // Browser extension: the supported browsers first (the five major ones always shown, with whether each is on this
  // computer and connected, and how to install it there), then the pairing code, then the connected browsers.
  // Reached from the Browsers menu, beside Browsers and access (it was in the workspace menu until 2026-11-13).
  type Ext = { id: string; name: string; created: string; last_used?: string };
  const EXT_MAJOR = ["Google Chrome", "Microsoft Edge", "Firefox", "Safari", "Brave Browser"];
  const EXT_ENGINE: Record<string, "chromium" | "firefox" | "safari"> = {
    "Google Chrome": "chromium", "Google Chrome Canary": "chromium", Chromium: "chromium", "Brave Browser": "chromium", "Microsoft Edge": "chromium",
    Vivaldi: "chromium", Opera: "chromium", Arc: "chromium", Firefox: "firefox", Zen: "firefox", Safari: "safari" };
  const EXT_PAGE: Record<string, string> = { "Microsoft Edge": "edge://extensions", "Brave Browser": "brave://extensions", Opera: "opera://extensions", Vivaldi: "vivaldi://extensions" };
  const extSteps = (name: string): [string, string?][] => {
    const engine = EXT_ENGINE[name];
    if (engine === "firefox") return [["Open about:debugging, then This Firefox", "about:debugging#/runtime/this-firefox"], ["Click Load Temporary Add-on…"],
      ["Choose web/dist/extension/firefox/manifest.json", "web/dist/extension/firefox/manifest.json"], ["Allow it to reach 127.0.0.1 when asked. It stays until the browser restarts."]];
    if (engine === "safari") return [["Build the Safari app: make extension-safari (needs Xcode)", "make extension-safari"],
      ["Build and run web/dist/extension/safari-xcode in Xcode", "web/dist/extension/safari-xcode"], ["Turn it on in Safari › Settings › Extensions"]];
    const page = EXT_PAGE[name] ?? "chrome://extensions";
    return [[`Open ${page}`, page], ["Turn on Developer mode"], ["Click Load unpacked and choose web/dist/extension/chromium", "web/dist/extension/chromium"]];
  };
  async function extensions(): Promise<void> {
    const { body, foot, dock } = modal("v1Extensions", "Browser extensions", "lg");
    let connected: Ext[] = [], installed = new Set<string>();
    const copy = (text: string) => async () => { try { await navigator.clipboard.writeText(text); toast("Copied.", "success"); } catch { toast("Copy it by hand.", "warning"); } };
    body.append(el("p", "text-secondary mb-3", "The extension sends a browser's tabs and tab groups, reads its bookmarks into a workspace, and can write a workspace back. Install it in each browser you use, then connect it with a pairing code."));
    // Supported browsers: one card each.
    const grid = el("div", "ext-grid"), more = el("div", "text-secondary small mt-2");
    const paintCards = () => {
      const byName = new Map(connected.map(x => [x.name.replace(/^extension:/, ""), x]));
      const names = [...EXT_MAJOR, ...[...installed].filter(n => EXT_ENGINE[n] && !EXT_MAJOR.includes(n))];
      grid.replaceChildren(...names.map(name => {
        const c = byName.get(name), card = el("div", "ext-card" + (c ? " is-connected" : ""));
        const head = el("div", "ext-head"), title = el("div", "min-w-0");
        title.append(el("div", "fw-medium text-truncate", name));
        const st = el("div", "ext-status small");
        if (c) st.append(el("span", "status-dot status-green me-1"), "Connected" + (c.last_used && !c.last_used.startsWith("0001") ? ` · heard from ${fmtDate(Date.parse(c.last_used) / 1000)}` : ""));
        else st.append(el("span", "status-dot status-secondary me-1"), installed.has(name) ? "On this computer · not connected" : "Not on this computer");
        title.append(st);
        head.append(icon("browser:" + name, "ext-ico"), title);
        const how = el("details", "ext-how"); const sum = el("summary", null, c ? "Install again" : "How to install");
        const ol = el("ol", "ext-steps");
        for (const [t, val] of extSteps(name)) {
          const li = el("li"); li.append(el("span", null, t));
          if (val) { const b = el("button", "btn btn-sm btn-ghost-secondary btn-icon"); b.type = "button"; b.setAttribute("aria-label", "Copy " + val); b.dataset.tip = "Copy " + val; b.append(icon("ti-copy")); b.onclick = copy(val); li.append(b); }
          ol.append(li);
        }
        how.append(sum, ol);
        card.append(head, how);
        return card;
      }));
      more.textContent = "Also works in Vivaldi, Opera and Arc (as in Chrome) and in Zen (as in Firefox); those appear here when they are on this computer.";
    };
    body.append(el("div", "ext-section", "Supported browsers"), grid, more);
    // Connect: the pairing code.
    const codeBox = el("div", "mt-2");
    const make = el("button", "btn btn-primary"); make.type = "button"; make.append(icon("ti-key", "me-1"), "Make a pairing code");
    make.onclick = async () => {
      try {
        const r = await W.call<{ code: string; expires: string }>("POST", "extension/pairings");
        const card = el("div", "ext-code"), code = el("div", "h1 font-monospace m-0", r.code);
        const cp = el("button", "btn btn-sm ms-auto"); cp.type = "button"; cp.append(icon("ti-copy", "me-1"), "Copy"); cp.onclick = copy(r.code);
        card.append(code, el("div", "text-secondary small", `Type it into the extension's popup with the server address ${location.origin}. Works once, until ${new Date(r.expires).toLocaleTimeString()}.`), cp);
        codeBox.replaceChildren(card);
      } catch (e) { fail(e); }
    };
    const connect = el("div", "ext-connect");
    connect.append(el("div", "small text-secondary", "Installed it? Make a code, then type it into the extension's popup with the server address " + location.origin + "."));
    body.append(el("div", "ext-section", "Connect a browser"), connect, codeBox);
    // Connected browsers, each with Disconnect.
    const table = new DataTable<Ext>({
      noun: "browsers", size: 10,
      empty: { title: "No browser is connected yet", text: "Install the extension, make a pairing code, then type it into the extension.", icon: "ti-puzzle" },
      columns: [
        { label: "Browser", cell: x => nameCell(x.name.replace(/^extension:/, ""), `connected ${fmtDate(Date.parse(x.created) / 1000)}`, icon("browser:" + x.name.replace(/^extension:/, ""))) },
        { label: "Last heard from", cell: x => el("span", "text-secondary", x.last_used && !x.last_used.startsWith("0001") ? fmtDate(Date.parse(x.last_used) / 1000) : "not yet") },
      ],
      actions: x => [{ l: "Disconnect…", ic: "ti-plug-connected-x", danger: true, fn: async () => {
        if (!(await askConfirm({ title: "Disconnect this browser?", ok: "Disconnect", body: "Its extension stops being able to reach Browser Foreman until it is connected again with a new code." }))) return;
        try { await W.call("DELETE", `tokens/${encodeURIComponent(x.id)}`); toast("Disconnected.", "success"); await load(); } catch (e) { fail(e); }
      } }],
    });
    const tableWrap = el("div"); tableWrap.append(el("div", "ext-section", "Connected"), table.element);
    const load = async () => {
      const [ext, br] = await Promise.allSettled([W.call<{ extensions: Ext[] }>("GET", "extension/status"), W.call<{ installed?: { name: string }[] }>("GET", "browsers")]);
      connected = ext.status === "fulfilled" ? ext.value.extensions : [];
      installed = new Set(br.status === "fulfilled" ? (br.value.installed ?? []).map(b => b.name) : []);
      if (ext.status === "rejected") fail(ext.reason);
      table.rows(connected); tableWrap.hidden = !connected.length; paintCards();
    };
    body.append(tableWrap); dock.append(table.foot);
    const close = el("button", "btn me-auto"); close.type = "button"; close.textContent = "Close"; close.onclick = () => hideModal("#v1Extensions");
    foot.append(close, make);
    await load(); showModal("#v1Extensions");
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
      item("ti-refresh-dot", "Update from source…", () => void updateWs()));
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

  W.ui = {
    history, manage, importer, addFiles, addLinks, updateWs, browserHistory, resetWs, menu, download, toggleLock, backup, restore, extensions, duplicatesCsv,
    rename, notes,
  };
  // The workspace switcher sits in the top bar beside the logo, so it is there in every view (Bookmarks, Open tabs,
  // Timeline): a workspace scopes all three. It was in the Bookmarks toolbar until 2026-11-13.
  function remount(): void { document.getElementById("wsMenu")?.remove(); document.querySelector("#hdrWs")?.append(menu()); }
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", remount); else remount();
}
