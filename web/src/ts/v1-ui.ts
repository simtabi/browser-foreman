// The workspace menu, History window and file handling when the Go server serves the page (window.BM_WS.v1, set by
// v1-boot.ts). It fills window.BM_WS.ui, the hooks the page's Download menu, palette and Settings already call, and
// talks to /api/v1 through the boot client's authenticated `call`.

import { askConfirm, askText, hideModal, showModal, toast } from "./dialogs";
import { closeDropdowns } from "./menu";
import { el, fmtDate, icon } from "./ui";

interface Info { id: string; name: string; head: number; notes?: string; locked?: boolean; archived?: boolean; deleted?: string; bookmarks?: number }
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

  // Workspaces: the ones in use, and the workspace Trash (deleted ones, kept whole until it is emptied).
  function manage(tab: "use" | "trash" = "use"): void {
    const { body, foot } = modal("v1Workspaces", "Workspaces");
    const nav2 = el("ul", "nav nav-tabs mb-3"); nav2.setAttribute("role", "tablist");
    const pane = el("div");
    const tabBtn = (id: "use" | "trash", label: string, ic: string) => {
      const li = el("li", "nav-item"), a = el("a", "nav-link" + (tab === id ? " active" : "")); a.href = "#"; a.setAttribute("role", "tab");
      a.append(icon(ic, "me-1"), label); a.onclick = e => { e.preventDefault(); tab = id; void paint(); };
      li.append(a); return li;
    };
    const badge = (text: string, cls: string, ic?: string) => { const b = el("span", `badge ${cls}`); if (ic) b.append(icon(ic, "me-1")); b.append(text); return b; };
    const typed = async (title: string, id: string, ok: string): Promise<boolean> => {
      const v = await askText({ title, label: `Type the workspace id, ${id}, to go ahead`, ok, hint: "" });
      if (v == null) return false;
      if (v.trim() !== id) { toast("The id did not match, so nothing happened.", "info"); return false; }
      return true;
    };
    const paint = async (): Promise<void> => {
      let trash: Info[] = [], inUse: Info[] = W.workspaces;
      try { inUse = (await W.call<{ workspaces: Info[] }>("GET", "workspaces")).workspaces; } catch { /* the page's own list will do */ }
      try { trash = (await W.call<{ workspaces: Info[] }>("GET", "workspaces?trash=1")).workspaces; } catch { /* not an owner: no Trash tab */ }
      nav2.replaceChildren(tabBtn("use", `In use (${inUse.length})`, "ti-stack-2"), tabBtn("trash", `Trash (${trash.length})`, "ti-trash"));
      const list = el("div", "list-group list-group-flush border rounded");
      const rows = tab === "use" ? inUse : trash;
      if (!rows.length) {
        const e = el("div", "empty py-4"); e.append(el("p", "empty-title", tab === "use" ? "No workspaces" : "The workspace Trash is empty"),
          el("p", "empty-subtitle text-secondary", tab === "use" ? "Import bookmarks to make one." : "Deleted workspaces wait here, history and all, until you restore them or delete them for good."));
        list.append(e);
      }
      for (const w of rows) {
        const row = el("div", "list-group-item d-flex align-items-center gap-3");
        const text = el("div", "flex-fill min-w-0"), title = el("div", "d-flex align-items-center gap-2 flex-wrap");
        title.append(el("span", "fw-medium text-truncate", w.name));
        if (w.id === W.id && tab === "use") title.append(badge("open", "bg-primary-lt"));
        if (w.archived) title.append(badge("archived", "bg-secondary-lt", "ti-archive"));
        if (w.locked) title.append(badge("locked", "bg-yellow-lt", "ti-lock"));
        const facts = [w.bookmarks != null ? `${w.bookmarks.toLocaleString()} bookmarks` : "", `${w.head} change${w.head === 1 ? "" : "s"}`,
          w.deleted ? `deleted ${fmtDate(Date.parse(w.deleted) / 1000)}` : ""].filter(Boolean).join(" · ");
        text.append(title, el("div", "text-secondary small", `${w.id} · ${facts}`));
        const acts = el("div", "btn-list flex-nowrap");
        const btn = (label: string, ic: string, cls: string, fn: () => Promise<void> | void) => {
          const b = el("button", `btn btn-sm ${cls}`); b.type = "button"; b.append(icon(ic, "me-1"), label);
          b.onclick = async () => { b.disabled = true; try { await fn(); } catch (e) { fail(e); } finally { b.disabled = false; } };
          acts.append(b);
        };
        const enc = encodeURIComponent(w.id);
        if (tab === "use") {
          if (w.id !== W.id) btn("Open", "ti-external-link", "", () => nav(w.id));
          if (!w.locked) btn(w.archived ? "Bring back" : "Archive", w.archived ? "ti-archive-off" : "ti-archive", "btn-ghost-secondary", async () => {
            await W.call("PATCH", `workspaces/${enc}`, { archived: !w.archived }); location.reload();
          });
          if (!w.locked && inUse.length > 1) btn("Delete…", "ti-trash", "btn-ghost-danger", async () => {
            if (!(await typed(`Delete “${w.name}”?`, w.id, "Move to the Trash"))) return;
            await W.call("POST", `workspaces/${enc}/delete`, { confirm: w.id });
            toast("It is in the workspace Trash, history and all. Restore brings it back.", "success", "Workspace deleted");
            setTimeout(() => (w.id === W.id ? nav(inUse.find(x => x.id !== w.id)!.id) : location.reload()), 700);
          });
        } else {
          btn("Restore", "ti-restore", "btn-primary", async () => {
            await W.call("POST", `workspaces/${enc}/restore`);
            toast(`“${w.name}” is back.`, "success", "Workspace restored"); setTimeout(() => nav(w.id), 600);
          });
          btn("Delete for good…", "ti-trash-x", "btn-ghost-danger", async () => {
            if (!(await askConfirm({ title: `Delete “${w.name}” for good?`, ok: "Continue", body: "Its bookmarks and every change in its history are removed from this computer. This cannot be undone. A backup made earlier still has it." }))) return;
            if (!(await typed(`Delete “${w.name}” for good`, w.id, "Delete for good"))) return;
            await W.call("POST", `workspaces/${enc}/purge`, { confirm: w.id });
            toast(`“${w.name}” was removed for good.`, "success", "Deleted for good"); void paint();
          });
        }
        row.append(text, acts); list.append(row);
      }
      pane.replaceChildren(list);
    };
    body.append(nav2, pane);
    const add = el("button", "btn btn-primary"); add.type = "button"; add.append(icon("ti-plus", "me-1"), "New workspace from bookmarks…");
    add.onclick = () => { hideModal("#v1Workspaces"); importer(); };
    const close = el("button", "btn"); close.type = "button"; close.textContent = "Close"; close.onclick = () => hideModal("#v1Workspaces");
    foot.append(close, add);
    void paint(); showModal("#v1Workspaces");
  }

  // Reset: back to how the workspace started, as one entry (undo brings today's back).
  async function resetWs(): Promise<void> {
    if (!(await askConfirm({ title: `Reset “${W.workspace.name}” to how it started?`, ok: "Reset", danger: false,
      body: "Every change since its first import or merge is set aside as one entry in the history. Undo, or History, brings today's state back." }))) return;
    try { const j = await W.call<{ entry: Entry }>("POST", ws("/reset")); toast(j.entry.summary, "success", "Reset"); setTimeout(() => location.reload(), 700); }
    catch (e) { fail(e); }
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
    // Browsers on this computer: each profile's own bookmarks file, read where it is (owners; Update reads it again).
    const picks: HTMLInputElement[] = [];
    const found = el("div", "mb-3");
    body.append(found);
    void (async () => {
      try {
        const { profiles } = await W.call<{ profiles: { browser: string; profile: string; name?: string; path: string; readable: boolean; why?: string }[] }>("GET", "profiles");
        if (!profiles.length) return;
        found.append(el("div", "form-label", "Or read a browser on this computer"));
        for (const p of profiles) {
          const row = el("label", "form-check"), box = el("input", "form-check-input"); box.type = "checkbox"; box.value = p.path; box.disabled = !p.readable;
          const label = p.browser + (p.name || p.profile ? ` · ${p.name || p.profile}` : "");
          row.append(box, el("span", "form-check-label", label));
          if (!p.readable) row.append(el("div", "text-secondary small", p.why ?? "cannot be read"));
          row.title = p.path; found.append(row); picks.push(box);
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
    const cancel = el("button", "btn", "Cancel"); cancel.type = "button"; cancel.onclick = () => hideModal("#v1Sources");
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
    const { body, foot } = modal("v1History2", "Browser history");
    const bar = el("div", "row g-2 align-items-center mb-3");
    const qWrap = el("div", "col-12 col-md"), qIcon = el("div", "input-icon");
    const q = el("input", "form-control"); q.type = "search"; q.placeholder = "Search titles and addresses…"; q.setAttribute("aria-label", "Search history");
    const qi = el("span", "input-icon-addon"); qi.append(icon("ti-search"));
    qIcon.append(qi, q); qWrap.append(qIcon);
    const days = el("select", "form-select"); days.setAttribute("aria-label", "How far back");
    for (const [v, t] of [["1", "Today"], ["7", "Last 7 days"], ["30", "Last 30 days"], ["90", "Last 3 months"], ["365", "Last year"]]) days.append(new Option(t, v));
    days.value = "30";
    const dWrap = el("div", "col-6 col-md-auto"); dWrap.append(days);
    const sw = el("label", "form-check form-switch mb-0"), only = el("input", "form-check-input"); only.type = "checkbox"; only.checked = true;
    sw.append(only, el("span", "form-check-label", "Not bookmarked only"));
    const sWrap = el("div", "col-6 col-md-auto"); sWrap.append(sw);
    bar.append(qWrap, dWrap, sWrap);
    const note = el("div"), list = el("div", "list-group list-group-flush border rounded"), status = el("div", "text-secondary small me-auto");
    body.append(bar, note, list);
    const save = el("button", "btn btn-primary"); save.type = "button"; save.disabled = true;
    const close = el("button", "btn"); close.type = "button"; close.textContent = "Close"; close.onclick = () => hideModal("#v1History2");
    foot.append(status, close, save);
    const picked = new Set<string>();
    let rows: Visit[] = [], timer: ReturnType<typeof setTimeout> | undefined, blockedAll = false;
    const paintSave = () => { save.disabled = !picked.size || !!W.locked; save.replaceChildren(icon("ti-bookmark-plus", "me-1"), picked.size ? `Save ${picked.size} as bookmarks` : "Save as bookmarks"); };
    const paint = () => {
      const shown = only.checked ? rows.filter(r => !r.bookmarked) : rows;
      list.replaceChildren();
      if (!shown.length && blockedAll) { list.replaceChildren(); list.hidden = true; status.textContent = ""; return; }
      list.hidden = false;
      if (!shown.length) {
        const e = el("div", "empty py-4"); e.append(el("div", "empty-icon"), el("p", "empty-title", rows.length ? "Every page here is bookmarked" : "No visits match"),
          el("p", "empty-subtitle text-secondary", rows.length ? "Turn off Not bookmarked only to see them." : "Try other words or a longer time."));
        e.querySelector(".empty-icon")?.append(icon("ti-history"));
        list.append(e);
      }
      for (const r of shown.slice(0, 500)) {
        const row = el("label", "list-group-item d-flex align-items-center gap-3");
        const box = el("input", "form-check-input m-0 flex-none"); box.type = "checkbox"; box.checked = picked.has(r.url); box.disabled = !!r.bookmarked;
        box.onchange = () => { if (box.checked) picked.add(r.url); else picked.delete(r.url); paintSave(); };
        const txt = el("div", "min-w-0 flex-fill"), a = el("a", "text-reset d-block text-truncate", r.title || r.url);
        a.href = r.url; a.target = "_blank"; a.rel = "noopener noreferrer";
        txt.append(a, el("div", "text-secondary small text-truncate", r.url));
        const meta = el("div", "text-end flex-none small");
        meta.append(el("div", "text-secondary", fmtDate(Date.parse(r.last) / 1000)), el("div", "text-secondary", `${r.browser} · ${r.visits} visit${r.visits === 1 ? "" : "s"}`));
        if (r.bookmarked) { const b = el("span", "badge bg-green-lt flex-none"); b.append(icon("ti-bookmark", "me-1"), "Bookmarked"); row.append(box, txt, b, meta); }
        else row.append(box, txt, meta);
        list.append(row);
      }
      status.textContent = `${shown.length.toLocaleString()} page${shown.length === 1 ? "" : "s"}` + (shown.length > 500 ? " (first 500 shown)" : "");
    };
    const load = async () => {
      list.replaceChildren(); const sp = el("div", "p-4 text-center text-secondary"); sp.append(el("span", "spinner-border spinner-border-sm me-2"), "Reading your browsers' history…"); list.append(sp);
      try {
        const p = new URLSearchParams({ days: days.value, limit: "2000", workspace: W.id }); if (q.value.trim()) p.set("q", q.value.trim());
        const j = await W.call<{ visits: Visit[]; read: number; skipped: { Browser: string; Profile: string; Why: string }[] }>("GET", `history?${p}`);
        rows = j.visits; note.replaceChildren();
        blockedAll = !j.read && j.skipped.length > 0;
        if (j.skipped.length) {
          // One line per reason, naming the browsers it applies to, and the way out where there is one.
          const byWhy = new Map<string, string[]>();
          for (const x of j.skipped) byWhy.set(x.Why, [...(byWhy.get(x.Why) ?? []), x.Browser + (x.Profile && x.Profile !== "Default" ? ` (${x.Profile})` : "")]);
          const al = el("div", "alert alert-warning mb-3"), d = el("div", "d-flex gap-2"), tx = el("div", "min-w-0");
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
        if (!j.read && !j.skipped.length) note.append(el("div", "alert alert-info mb-3", "No browser history was found on this computer."));
        paint();
      } catch (e) { list.replaceChildren(el("div", "p-3 text-danger", e instanceof Error ? e.message : String(e))); }
    };
    q.oninput = () => { clearTimeout(timer); timer = setTimeout(() => void load(), 350); };
    days.onchange = () => void load(); only.onchange = paint;
    save.onclick = async () => {
      const links = rows.filter(r => picked.has(r.url)).map(r => ({ url: r.url, title: r.title }));
      try { await addLinks(`From history ${new Date().toLocaleDateString()}`, links, { parent: "Saved tabs", reload: true }); hideModal("#v1History2"); }
      catch (e) { fail(e); }
    };
    paintSave(); showModal("#v1History2"); void load(); q.focus();
  }

  async function duplicatesCsv(): Promise<void> {
    try {
      const r = await fetch(`api/v1/${ws("/duplicates.csv")}`, { headers: W.headers() });
      if (!r.ok) throw new Error(`the server answered ${r.status}`);
      const a = el("a"); a.href = URL.createObjectURL(await r.blob()); a.download = `${W.id}-duplicates.csv`;
      document.body.append(a); a.click(); a.remove(); setTimeout(() => URL.revokeObjectURL(a.href), 10_000);
    } catch (e) { fail(e); }
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
  };
  const mount = () => { document.getElementById("wsMenu")?.remove(); document.querySelector("#edited")?.before(menu()); };
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", mount); else mount();
}
