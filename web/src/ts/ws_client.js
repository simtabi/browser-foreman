// Workspaces in the preview page. src/server/serve.py embeds window.BM_WS (this workspace's id, state, head and the run's
// token) and loads this file before the page's own script, which then reads and writes its edits through BM_WS
// (see `store` in preview_template.html) instead of localStorage. Every change is saved to the workspace's history on
// disk; Undo steps back through that history; the History window lists it and rolls back to any entry.
(() => {
  const W = window.BM_WS;
  if (!W || W.v1) return;  // the Go server's page installs its own hooks (v1-boot.ts)
  const LS = "bm-preview-v2"; // the page's own localStorage prefix (STORE in preview_template.html)
  const api = (p) => `api/ws/${encodeURIComponent(W.id)}${p}`;
  const owns = k => k === "urls" || k === "refreshed" || k.startsWith("tree-");
  const lsGet = k => { try { return JSON.parse(localStorage.getItem(LS + ":" + k)); } catch { return null; } };

  // Writes need the run's token; the page's own refresh and open calls get it here, so no call site has to know.
  const nativeFetch = window.fetch.bind(window);
  window.fetch = (url, opts = {}) => {
    if (typeof url === "string" && url.startsWith("api/") && (opts.method || "GET").toUpperCase() === "POST")
      opts = {...opts, headers: {...(opts.headers || {}), "X-BM-Token": W.token}};
    return nativeFetch(url, opts);
  };
  const post = async (path, body) => {
    const r = await fetch(path, {method: "POST", headers: {"Content-Type": "application/json"}, body: JSON.stringify(body || {})});
    const j = await r.json().catch(() => ({}));
    if (!r.ok) { const e = new Error(j.error || `HTTP ${r.status}`); e.status = r.status; throw e; }
    return j;
  };

  // A workspace with no history yet still shows what this browser kept, and brings it in as its first entry.
  const legacy = W.fresh ? {tree: lsGet("tree-g"), urls: lsGet("urls") || {}, refreshed: lsGet("refreshed") || {}} : null;
  let state = W.fresh ? legacy : W.state, head = W.head, pending = false, inflight = null, timer = null, migrating = null;
  const keyOf = k => k.startsWith("tree-") ? "tree" : k;

  W.owns = owns;
  W.get = k => state[keyOf(k)] ?? null;
  // Saved at once: every change goes to the server in the same moment (edits made in one go, like a multi-select
  // move, leave as one save). Only the parts that changed are sent, so a save fits in a keepalive request.
  const dirty = new Set();
  // Set once the page has decided to reload for a newer state: it is about to be replaced, so nothing it still holds
  // may be saved. Without this, the page's save-on-close sent its old tree under the new head and undid the change
  // made elsewhere (seen 2026-11-12: a cleanup committed as #7 was put back as #8 three seconds later).
  let leaving = false;
  W.set = (k, v) => {
    if (leaving) return;
    if (W.locked) return refuseLocked(k);
    const key = keyOf(k);
    state = {...state, [key]: v ?? (k.startsWith("tree-") ? null : {})}; dirty.add(key); pending = true;
    clearTimeout(timer);
    if (document.visibilityState === "hidden") return saveNow(); // the page is going away: no waiting at all
    timer = setTimeout(flush, 0);
  };

  // A locked workspace is read-only: the page has already changed its view, so put it back and say why.
  let refusing = false;
  function refuseLocked(k) {
    if (refusing) return; refusing = true;
    setTimeout(() => {
      refusing = false;
      toast("This workspace is locked (read-only). Unlock it from the workspace menu to change it.", "warning", "Locked");
      if (k.startsWith("tree-")) { S[mode] = loadState(mode); undoStack[mode] = []; setMode(mode); }
      else location.reload(); // link updates and refresh results were applied to the items themselves
    }, 0);
  }

  // keepalive lets a request finish after the tab closes, but only up to 64 KB of body: bigger saves (a large batch
  // of refresh results) go as ordinary requests, which finish whenever the page stays open long enough.
  const KEEPALIVE_MAX = 60000;
  function send(base) {
    const keys = [...dirty]; dirty.clear(); pending = false;
    const body = JSON.stringify({changes: Object.fromEntries(keys.map(k => [k, state[k]])), base});
    status("Saving…");
    return nativeFetch(api("/commit"), {method: "POST", keepalive: body.length < KEEPALIVE_MAX, body,
      headers: {"Content-Type": "application/json", "X-BM-Token": W.token}})
      .then(async r => {
        const j = await r.json().catch(() => ({}));
        if (!r.ok) { const e = new Error(j.error || `HTTP ${r.status}`); e.status = r.status; throw e; }
        head = j.head; if (!pending) status();
      })
      .catch(e => {
        if (e.status === 409) return changedElsewhere();
        if (e.status === 423) { W.locked = true; return toast(e.message, "warning", "Locked"); }
        for (const k of keys) dirty.add(k);
        pending = true; status("Not saved: " + e.message + " (trying again)", true); setTimeout(flush, 5000);
      });
  }
  async function flush() {
    clearTimeout(timer);
    if (migrating) await migrating;
    if (inflight) { await inflight; if (!pending) return; }
    if (!pending) return;
    inflight = send(head).finally(() => { inflight = null; });
    return inflight;
  }
  // The page is being hidden or closed: send what is pending now, without waiting for a save already on its way. Its
  // head is not known yet, so this one goes without a base (it follows this page's own previous save).
  function saveNow() {
    clearTimeout(timer);
    if (!pending || migrating) return;
    if (!inflight) { inflight = send(head).finally(() => { inflight = null; }); return; }
    send(null);
  }
  W.flush = flush;

  // Factory reset (called by the page's reset flow): the workspace goes back to the generated tree with no link
  // updates or refresh results. Nothing is deleted: it is one more history entry, so History or Undo brings it all back.
  // Throws when the server refuses, so the caller can stop before reloading.
  W.factoryReset = async () => {
    await flush();
    dirty.clear(); pending = false;
    const j = await post(api("/commit"), {state: {tree: null, urls: {}, refreshed: {}}, base: head, reset: true});
    head = j.head; state = {tree: null, urls: {}, refreshed: {}};
    return j.entry;
  };

  async function changedElsewhere() {
    if (await askConfirm({title: "This workspace changed elsewhere", ok: "Reload", no: "Not now", danger: false,
      body: "Another window or the MCP tools changed it after this page loaded, so your last change was not saved. Reload to see the current version, then make the change again."})) location.reload();
  }

  // Apply a state from the server (undo, rollback, a change made by the MCP tools) without losing the view.
  function adopt(next, nextHead) {
    const same = k => JSON.stringify(next[k] || {}) === JSON.stringify(state[k] || {});
    // Saved tabs are part of the page's data, which is built at load: a change to them needs a reload too.
    const sameLinks = same("urls") && same("refreshed") && same("added");
    state = next; head = nextHead;
    if (!sameLinks) { leaving = true; dirty.clear(); pending = false; return location.reload(); } // link updates and refresh results are applied to the items at load
    S[mode] = loadState(mode); undoStack[mode] = []; setMode(mode); status();
  }

  // Undo and redo walk the workspace history; each is a new entry, so nothing is ever lost.
  const step = (verb, done, none) => async () => {
    if (W.locked) return toast("This workspace is locked (read-only).", "warning", "Locked");
    try {
      await flush();
      const j = await post(api("/" + verb));
      if (j.can) { W.can = j.can; canFor = j.head; }
      adopt(j.state, j.head); toast(j.entry.summary.replace(/^(Undid|Redid): /, ""), "success", done);
      window.dispatchEvent(new CustomEvent("bf:undo", {detail: {scope: "bookmarks"}}));
    } catch (e) { toast(e.status === 400 ? none : e.message, e.status === 400 ? "info" : "danger"); }
  };
  W.undo = step("undo", "Undone", "Nothing to undo");
  W.redo = step("redo", "Redone", "Nothing to redo");

  // What Undo and Redo would do now (labels, or null when there is nothing), re-read whenever the head moves, and
  // announced as "bf:undo" so the page enables its buttons only when there is something to undo or redo.
  W.can = {undo: null, redo: null};
  let canFor = -1;
  async function refreshCan() {
    if (canFor === head) return;
    canFor = head;
    try { const r = await nativeFetch(api("/undo-state")); if (r.ok) W.can = await r.json(); } catch {}
    window.dispatchEvent(new CustomEvent("bf:undo", {detail: {scope: "bookmarks"}}));
  }

  function status(text, bad) {
    void refreshCan();
    const e = document.querySelector("#edited"); if (!e) return;
    e.className = "small " + (bad ? "text-danger" : "text-secondary");
    e.textContent = text || (head ? `Saved · change #${head}` : "No changes yet");
    e.dataset.tip = "Kept on disk in workspaces/" + W.id + "/, with every change in its history (More › History)";
  }

  // Someone else (the MCP tools, another window) moved the head: show their change when nothing here is unsaved.
  async function poll() {
    if (document.hidden || pending || inflight || migrating) return;
    try {
      const r = await nativeFetch(api("")); if (!r.ok) return;
      const j = await r.json();
      if (j.head !== head) { const s = await (await nativeFetch(api("/state"))).json(); if (!pending && !inflight) { adopt(s.state, s.head); toast("Updated with a change made elsewhere", "info"); } }
    } catch {}
  }
  addEventListener("beforeunload", saveNow); // no "Leave site?" prompt: the save is already on its way

  // ---------- the interface ----------
  const h = (tag, attrs, ...kids) => { const n = document.createElement(tag); for (const [k, v] of Object.entries(attrs || {})) { if (k === "class") n.className = v; else if (k.startsWith("on")) n[k] = v; else if (v !== false && v != null) n.setAttribute(k, v === true ? "" : v); } n.append(...kids.flat().filter(x => x != null)); return n; };
  const ti = n => h("i", {class: `ti ${n} me-2`});
  const when = t => new Date(t * 1000).toLocaleString(undefined, {dateStyle: "medium", timeStyle: "short"});
  const nav = id => { location.href = "./?ws=" + encodeURIComponent(id); };

  function modal(id, title, size = "") {
    const body = h("div", {class: "modal-body"}), foot = h("div", {class: "modal-footer"});
    const m = h("div", {class: "modal modal-blur", id, tabindex: "-1", role: "dialog", "aria-label": title},
      h("div", {class: `modal-dialog modal-dialog-centered modal-dialog-scrollable ${size}`, role: "document"},
        h("div", {class: "modal-content"},
          h("div", {class: "modal-header"}, h("h5", {class: "modal-title"}, title), h("button", {type: "button", class: "btn-close", "aria-label": "Close", onclick: () => hideModal("#" + id)})),
          body, foot)));
    m.addEventListener("mousedown", e => { if (e.target === m) hideModal("#" + id); });
    m.addEventListener("keydown", e => { if (e.key === "Escape") { e.stopPropagation(); hideModal("#" + id); } e.stopPropagation(); });
    document.body.append(m);
    return {m, body, foot};
  }

  function menu() {
    const live = W.workspaces.filter(w => !w.archived), archived = W.workspaces.filter(w => w.archived).length;
    const item = (ic, label, fn, cls = "") => h("a", {class: "dropdown-item " + cls, href: "#", onclick: e => { e.preventDefault(); closeDropdowns(); fn(); }}, ti(ic), label);
    return h("div", {class: "dropdown", id: "wsMenu"},
      h("button", {class: "btn", "data-dd": true, "aria-label": "Workspace", "data-tip": "Workspace: one bookmark set being cleaned"},
        h("i", {class: "ti ti-stack-2 me-1"}), h("span", {class: "text-truncate", style: "max-width:14rem"}, W.name),
        W.locked ? h("span", {class: "badge bg-yellow-lt ms-2", "data-tip": "Locked: read-only until unlocked"}, h("i", {class: "ti ti-lock me-1"}), "locked") : null,
        W.archived ? h("span", {class: "badge bg-secondary-lt ms-2"}, "archived") : null,
        h("i", {class: "ti ti-chevron-down ms-1"})),
      h("div", {class: "dropdown-menu dropdown-menu-end"},
        h("h6", {class: "dropdown-header"}, "Switch workspace"),
        live.map(w => item(w.id === W.id ? "ti-check" : "ti-point", w.name, () => w.id !== W.id && nav(w.id), w.id === W.id ? "active" : "")),
        item("ti-list-details", "All workspaces" + (archived ? ` (${archived} archived)` : "") + "…", manage),
        h("div", {class: "dropdown-divider"}),
        h("h6", {class: "dropdown-header"}, "This workspace"),
        item("ti-history", "History and progress…", history),
        item("ti-pencil", "Rename…", renameWs),
        item("ti-notes", "Notes…", notesWs),
        W.locked ? item("ti-lock-open", "Unlock", () => toggleLock(false)) : item("ti-lock", "Lock (read-only)…", () => toggleLock(true)),
        item("ti-refresh-dot", "Update from source…", updateWs),
        W.archived ? item("ti-archive-off", "Bring back from the archive", () => archive(false))
          : item("ti-archive", "Archive…", () => archive(true))));
    // Import, export, backup and restore are in the Download menu (one place for bringing bookmarks in and out);
    // MCP access, Reset to the start and Delete workspace are in Settings, with the other settings and dangerous actions.
  }

  // Checked first, so a workspace with no record shows a notice instead of replacing the page with an error.
  async function duplicatesCsv() {
    try {
      const r = await nativeFetch(api("/duplicates.csv"));
      if (r.status === 404) return toast("No duplicates were merged in this workspace.", "info");
      if (!r.ok) throw new Error(`HTTP ${r.status}`);
      const a = h("a", {href: URL.createObjectURL(await r.blob()), download: `${W.id}-duplicates.csv`});
      document.body.append(a); a.click(); a.remove();
    } catch (e) { toast("Could not get the duplicates list: " + e.message, "danger"); }
  }

  function download(fmt) {
    const a = h("a", {href: api("/export?format=" + fmt), download: ""}); document.body.append(a); a.click(); a.remove();
    toast(fmt === "html" ? "Trash left out. Import it in any browser: Bookmarks › Import." : "Trash left out. A copy of a Chrome Bookmarks file, for tools and backups; Chrome itself imports the HTML file.", "success", "Export downloaded");
  }

  async function renameWs() {
    const name = await askText({title: "Rename workspace", label: "Name", value: W.name});
    if (!name || name.trim() === W.name) return;
    try { await post(api("/rename"), {name}); location.reload(); } catch (e) { toast(e.message, "danger"); }
  }
  async function notesWs() {
    let notes = "";
    try { notes = (await (await nativeFetch(api(""))).json()).notes || ""; } catch {}
    const v = await askText({title: "Workspace notes", label: "What this set is, what is left to do", value: notes, multiline: true, hint: "⌘Enter saves"});
    if (v == null || v === notes) return;
    try { await post(api("/notes"), {notes: v}); toast("Notes saved", "success"); } catch (e) { toast(e.message, "danger"); }
  }
  async function archive(on) {
    if (on && !(await askConfirm({title: `Archive “${W.name}”?`, ok: "Archive", body: "It leaves the workspace list but nothing is deleted: its bookmarks, edits and history stay on disk, and All workspaces brings it back."}))) return;
    try { await post(api("/archive"), {archived: on}); location.reload(); } catch (e) { toast(e.message, "danger"); }
  }

  // ---------- save tabs as bookmarks ----------
  // links: [{url, title}]. Resolves with the server's answer; the page then reloads so the new bookmarks join its data
  // (pass {reload: false} to reload later yourself).
  // parent: the folder path to create the new folder in (default "Saved tabs"). The server makes the last part a new
  // folder and reuses the parents, so links never land loose in an existing folder.
  async function addLinks(folderName, links, {reload = true, parent = "Saved tabs"} = {}) {
    if (W.locked) throw Object.assign(new Error("This workspace is locked (read-only)."), {status: 423});
    await flush();
    const name = String(folderName || "").trim().replaceAll("/", "∕") || "Tabs " + new Date().toLocaleString();
    const folder = String(folderName || "").startsWith("Saved tabs/") ? folderName : `${parent}/${name}`;
    const j = await post(api("/add-links"), {folder, links});
    toast(j.entry.summary, "success", "Saved as bookmarks");
    if (reload) setTimeout(() => location.reload(), 700);
    return j;
  }

  // ---------- MCP: where Claude can reach Browser Foreman, and whether it may change things ----------
  let MC;
  const TARGET_NAMES = {folder: ["Claude Code in this folder", "~/Artisan/bookmarks-cleanup/.mcp.json; sessions started here offer it once"],
    code: ["Claude Code, every folder", "user-wide, through the claude command"],
    desktop: ["Claude desktop chats", "claude_desktop_config.json; restart the app after a change"]};
  async function mcpAccess() {
    MC = MC || modal("wsMcp", "MCP access (Claude)", "modal-lg");
    MC.body.replaceChildren(h("div", {class: "text-secondary"}, "Checking…"));
    showModal("#wsMcp");
    let j; try { j = await (await nativeFetch("api/settings")).json(); } catch (e) { return MC.body.replaceChildren(h("div", {class: "text-danger"}, "Could not read the settings")); }
    const writes = h("input", {class: "form-check-input", type: "checkbox", checked: j.settings.mcp_allow_writes || false});
    writes.onchange = async () => {
      if (writes.checked && !(await askConfirm({title: "Let Claude change bookmarks?", ok: "Allow changes", danger: false,
        body: "Through MCP, Claude can then move, rename and trash bookmarks, save tabs as bookmarks, undo, roll back, export and re-check links. Every change is a history entry you can undo; trash and roll back still need an explicit confirm, and a locked workspace refuses all of it."}))) { writes.checked = false; return mcpAccess(); }
      try { await post("api/settings", {settings: {mcp_allow_writes: writes.checked}}); toast(writes.checked ? "Claude can change bookmarks (reconnect the MCP server to see the new tools)" : "Claude can only read now", "success", "MCP access"); }
      catch (e) { toast(e.message, "danger"); }
      mcpAccess();
    };
    const row = t => {
      const st = j.mcp[t] || {}, [label, where] = TARGET_NAMES[t];
      const act = async (action) => {
        hideModal("#wsMcp");
        if (!(await askConfirm({title: (action === "add" ? "Register with " : "Remove from ") + label + "?", ok: action === "add" ? "Register" : "Remove", danger: action !== "add",
          body: `This edits ${st.file || where} (a backup copy is kept beside it) and touches only the "bookmarks" entry.` + (t === "desktop" ? " Restart the Claude desktop app afterwards." : "")}))) return mcpAccess();
        try { const r = await post("api/mcp", {target: t, action}); toast(`${label}: ${r.result}`, r.result.startsWith("failed") ? "danger" : "success", "MCP"); }
        catch (e) { toast(e.message, "danger"); }
        mcpAccess();
      };
      return h("div", {class: "list-group-item d-flex align-items-center gap-2"},
        h("i", {class: "ti " + (st.registered ? "ti-circle-check text-green" : "ti-circle-dashed text-secondary")}),
        h("div", {class: "flex-fill"}, h("div", null, label), h("div", {class: "small text-secondary"}, st.error || where)),
        st.registered ? h("span", {class: "badge bg-green-lt"}, st.writes ? "registered, changes always on" : "registered") : null,
        h("button", {class: "btn btn-sm" + (st.registered ? " btn-ghost-danger" : " btn-primary"), onclick: () => act(st.registered ? "remove" : "add")},
          st.registered ? "Remove" : "Register"));
    };
    MC.body.replaceChildren(
      h("p", {class: "text-secondary"}, "Browser Foreman's MCP server lets Claude search your bookmarks, read folders and history, and read saved open tabs and windows. It runs on this Mac only and sends nothing anywhere."),
      h("h4", {class: "mb-2"}, "Where Claude can reach it"),
      h("div", {class: "list-group list-group-flush mb-3"}, ["folder", "code", "desktop"].map(row)),
      h("h4", {class: "mb-2"}, "What Claude may do"),
      h("label", {class: "form-check form-switch"}, writes, h("span", {class: "form-check-label"}, "Allow changes"),
        h("span", {class: "form-hint"}, "Off: read only. On: also move, rename, trash, put back, save tabs as bookmarks, undo, roll back, export, re-check links.")),
      h("div", {class: "small text-secondary mt-3"}, "By hand: ", h("code", null, "python3 src/mcp/mcp_register.py status")));
  }

  // ---------- backup and restore of the whole tool ----------
  function backupAll() {
    const a = h("a", {href: "api/backup", download: ""}); document.body.append(a); a.click(); a.remove();
    toast("Every workspace (and the workspace trash) and the saved tabs, with a checksummed manifest.", "success", "Backup downloaded");
  }
  function restoreAll() {
    const pick = h("input", {type: "file", accept: ".zip,application/zip", style: "display:none"});
    document.body.append(pick);
    pick.onchange = async () => {
      const f = pick.files[0]; pick.remove(); if (!f) return;
      const typed = await askText({title: "Restore from this backup?", label: "Type restore to replace all workspaces and saved tabs", ok: "Restore",
        hint: `${f.name}: the current data is first saved as a backup in backups/ and moved to _before-restore/, never deleted. The backup is checked before anything changes.`});
      if (typed == null) return;
      if (typed.trim().toLowerCase() !== "restore") return toast("Nothing was restored: the word did not match.", "info");
      try {
        await flush();
        const {data} = await read(f);
        const j = await post("api/restore", {data, confirm: "restore"});
        toast(`${j.restored} files restored from ${j.from ? new Date(j.from).toLocaleString() : "the backup"}. The previous data is in ${j.previous_data}.`, "success", "Restored");
        setTimeout(() => { location.href = "./"; }, 1500);
      } catch (e) { toast(e.message, "danger", "Not restored"); }
    };
    pick.click();
  }

  // ---------- lock, reset, update, delete ----------
  async function toggleLock(on) {
    if (on && !(await askConfirm({title: `Lock “${W.name}”?`, ok: "Lock", danger: false,
      body: "Every change is refused until you unlock it: edits here, the MCP tools, imports, reset and delete. Reading, searching and exporting still work."}))) return;
    try { await flush(); await post(api("/lock"), {locked: on}); location.reload(); } catch (e) { toast(e.message, "danger"); }
  }
  async function resetWs() {
    if (W.locked) return toast("Unlock the workspace first.", "warning", "Locked");
    if (!(await askConfirm({title: `Reset “${W.name}” to the start?`, ok: "Reset",
      body: (W.kind === "build" ? "The tree goes back to the generated one" : "It goes back to how it was imported") +
        ", and link updates and refresh results are cleared. This is one entry in the history, so Undo or History brings everything back."}))) return;
    try { await flush(); const j = await post(api("/reset")); if (j.entry) toast(j.entry.summary, "success", "Reset"); adopt(j.state, j.head); }
    catch (e) { toast(e.message, "danger"); }
  }
  async function updateWs() {
    if (W.locked) return toast("Unlock the workspace first.", "warning", "Locked");
    let pv; try { await flush(); const r = await nativeFetch(api("/update")); pv = await r.json(); if (!r.ok) throw new Error(pv.error || r.status); }
    catch (e) { return toast("Could not read the source: " + e.message, "danger"); }
    const n = k => pv[k].length, any = n("new") + n("removed") + n("changed");
    if (!any) return toast((pv.note ? pv.note + " " : "") + (pv.missing.length ? `Could not read: ${pv.missing.join(", ")}.` : "Nothing new upstream."), "info", "Up to date");
    const eg = (k, f) => pv[k].slice(0, 3).map(f).join("; ");
    const lines = [
      `${n("new")} new` + (n("new") ? ` (${eg("new", b => b.title || b.url)})` : ""),
      `${n("removed")} ${W.kind === "build" ? "no longer in the build" : "removed upstream, kept here until you trash them"}`,
      `${n("changed")} retitled upstream (your own renames still win)`];
    if (!(await askConfirm({title: W.kind === "build" ? "Update from the build?" : "Update from the source files?", ok: "Apply update", danger: false,
      body: lines.join(" · ") + ". " + (pv.missing.length ? `Could not read: ${pv.missing.join(", ")}. ` : "") + "Your edits are kept, and the update is in the history."}))) return;
    try { const j = await post(api("/update")); toast(j.entry ? j.entry.summary : "Nothing to apply", "success", "Updated"); location.reload(); }
    catch (e) { toast(e.message, "danger"); }
  }
  async function deleteWs(id, name) {
    const asked = askText({title: `Delete “${name}”?`, label: `Type the workspace name to delete it`, ok: "Delete",
      hint: "It moves to the workspace trash with its history, and All workspaces can restore it. Nothing is removed from the disk until you empty the workspace trash."});
    // The page's text dialog is shared; make its button red for this question only.
    const okBtn = document.querySelector("#textOk"), was = okBtn && okBtn.className;
    if (okBtn) okBtn.className = was.replace(/\bbtn-primary\b/, "") + " btn-danger";
    const typed = await asked;
    if (okBtn) okBtn.className = was;
    if (typed == null) return;
    if (typed.trim() !== name) return toast("The name did not match, so nothing was deleted.", "info");
    try { await flush(); await post(`api/ws/${encodeURIComponent(id)}/delete`, {confirm: id}); toast(`“${name}” is in the workspace trash`, "success", "Deleted");
      if (id === W.id) nav("main"); else manage(); }
    catch (e) { toast(e.message, "danger"); }
  }
  async function emptyWsTrash(entries) {
    hideModal("#wsManage");
    if (!(await askConfirm({title: `Permanently delete ${entries.length} workspace${entries.length === 1 ? "" : "s"}?`, ok: "Delete for ever",
      body: `${entries.map(t => t.name).join(", ")}: their bookmarks, edits, history and source copies are removed from the disk. This cannot be undone. Export anything you want to keep first.`}))) return manage();
    try { const j = await post("api/trash/empty", {confirm: "empty the workspace trash", trash_ids: entries.map(t => t.trash_id)}); toast(`${j.removed.length} removed for good`, "success", "Workspace trash emptied"); }
    catch (e) { toast(e.message, "danger"); }
    manage();
  }

  // ---------- history ----------
  let H;
  async function history() {
    H = H || modal("wsHistory", "History and progress", "modal-lg");
    H.body.replaceChildren(h("div", {class: "text-secondary"}, "Loading…"));
    showModal("#wsHistory");
    await flush();
    let j; try { j = await (await nativeFetch(api("/history?limit=500"))).json(); } catch (e) { return H.body.replaceChildren(h("div", {class: "text-danger"}, "Could not load the history")); }
    const tree = S[mode], inTree = [], inBin = []; // S and mode are shared by the page (page.ts), st() is not
    const bin = trashFolder();
    (function walk(f) { for (const x of f.c) if (typeof x === "object") walk(x); else (bin && contains(bin, x) ? inBin : inTree).push(x); })(tree.root);
    const edits = j.entries.filter(e => !["start", "import", "rollback", "undo", "notes", "rename-workspace", "archive", "unarchive"].includes(e.op)).length;
    const stat = (n, label) => h("div", {class: "col"}, h("div", {class: "h2 mb-0"}, Number(n).toLocaleString()), h("div", {class: "text-secondary small"}, label));
    const by = {page: "bg-blue-lt", mcp: "bg-purple-lt", import: "bg-green-lt"};
    H.body.replaceChildren(
      h("div", {class: "row text-center mb-3"}, stat(inTree.length, "bookmarks in the tree"), stat(inBin.length, "in the Trash"),
        stat(Object.keys(state.urls || {}).length, "links updated"), stat(Object.keys(state.refreshed || {}).length, "links re-checked"), stat(edits, "changes made")),
      h("div", {class: "small text-secondary mb-2"}, `${j.total} entries, newest first. Rolling back adds a new entry, so a rollback can itself be undone; nothing in the history is ever removed.`),
      h("div", {class: "list-group list-group-flush"}, j.entries.map(e => h("div", {class: "list-group-item d-flex align-items-start gap-2" + (e.seq === head ? " bg-primary-lt" : "")},
        h("span", {class: "text-secondary small text-nowrap", style: "min-width:3rem"}, "#" + e.seq),
        h("div", {class: "flex-fill"}, h("div", null, e.summary), h("div", {class: "small text-secondary"}, when(e.t), " · ", h("span", {class: "badge " + (by[e.by] || "bg-secondary-lt")}, e.by), " · ", e.op)),
        e.seq === head ? h("span", {class: "badge bg-primary text-primary-fg"}, "current") :
          h("button", {class: "btn btn-sm", onclick: () => rollback(e)}, h("i", {class: "ti ti-history me-1"}), "Roll back to here")))));
  }
  async function rollback(e) {
    hideModal("#wsHistory");
    if (!(await askConfirm({title: `Roll back to #${e.seq}?`, ok: "Roll back", danger: false,
      body: `The workspace goes back to how it was after “${e.summary}” (${when(e.t)}). This is added to the history as a new entry, so Undo or another rollback brings the later changes back.`}))) return history();
    try { await flush(); const j = await post(api("/rollback"), {seq: e.seq}); adopt(j.state, j.head); toast(j.entry.summary, "success", "Rolled back"); }
    catch (err) { toast(err.message, "danger"); }
  }

  // ---------- all workspaces ----------
  let M;
  async function manage() {
    M = M || modal("wsManage", "Workspaces", "modal-lg");
    let all = W.workspaces; try { all = (await (await nativeFetch("api/workspaces")).json()).workspaces; } catch {}
    M.body.replaceChildren(h("div", {class: "list-group list-group-flush"}, all.map(w => h("div", {class: "list-group-item d-flex align-items-center gap-2"},
      h("i", {class: "ti " + (w.archived ? "ti-archive text-secondary" : "ti-stack-2")}),
      h("div", {class: "flex-fill"}, h("div", null, w.name, w.id === W.id ? h("span", {class: "badge bg-primary-lt ms-2"}, "open") : null),
        h("div", {class: "small text-secondary"}, `${w.kind === "build" ? "built by src/pipeline/build.py" : (w.sources || []).map(s => s.name).join(", ")} · ${w.head} change${w.head === 1 ? "" : "s"}` + (w.last_change ? " · last " + new Date(w.last_change).toLocaleString() : ""))),
      w.locked ? h("span", {class: "badge bg-yellow-lt"}, h("i", {class: "ti ti-lock me-1"}), "locked") : null,
      w.archived && !w.locked ? h("button", {class: "btn btn-sm", onclick: async () => { await post(`api/ws/${w.id}/archive`, {archived: false}); manage(); }}, "Bring back") : null,
      w.kind === "build" || w.locked ? null : h("button", {class: "btn btn-sm btn-ghost-danger", "aria-label": "Delete " + w.name, onclick: () => { hideModal("#wsManage"); deleteWs(w.id, w.name); }}, h("i", {class: "ti ti-trash"})),
      w.id === W.id ? null : h("button", {class: "btn btn-sm btn-primary", onclick: () => nav(w.id)}, "Open")))));
    let bin = []; try { bin = (await (await nativeFetch("api/trash")).json()).trash; } catch {}
    if (bin.length) M.body.append(
      h("h4", {class: "mt-4 mb-2"}, h("i", {class: "ti ti-trash me-1"}), `Workspace trash (${bin.length})`),
      h("div", {class: "list-group list-group-flush"}, bin.map(t => h("div", {class: "list-group-item d-flex align-items-center gap-2"},
        h("div", {class: "flex-fill"}, h("div", null, t.name), h("div", {class: "small text-secondary"}, "deleted " + new Date(t.deleted).toLocaleString())),
        h("button", {class: "btn btn-sm", onclick: async () => { try { const j = await post("api/trash/restore", {trash_id: t.trash_id}); toast(`Restored as “${j.id}”`, "success"); } catch (e) { toast(e.message, "danger"); } manage(); }},
          h("i", {class: "ti ti-arrow-back-up me-1"}), "Restore")))),
      h("button", {class: "btn btn-sm btn-ghost-danger mt-2", onclick: () => emptyWsTrash(bin)}, h("i", {class: "ti ti-trash-x me-1"}), "Empty workspace trash…"));
    M.foot.replaceChildren(h("button", {class: "btn btn-primary", onclick: () => { hideModal("#wsManage"); importer(); }}, h("i", {class: "ti ti-plus me-1"}), "New workspace from bookmarks…"));
    showModal("#wsManage");
  }

  // ---------- import ----------
  let I;
  function importer() {
    I = I || modal("wsImport", "Import bookmarks", "modal-lg");
    const name = h("input", {class: "form-control", value: "Imported " + new Date().toISOString().slice(0, 10)});
    const files = h("input", {class: "form-control", type: "file", multiple: true});
    const path = h("input", {class: "form-control", placeholder: "~/Library/Application Support/Google/Chrome/Default/Bookmarks"});
    const found = h("div", {class: "list-group list-group-flush mt-2"});
    const into = h("select", {class: "form-select"}, h("option", {value: "new"}, "A new workspace"),
      W.kind === "import" ? h("option", {value: "add"}, `Add to “${W.name}”`) : null);
    const base = h("input", {class: "form-check-input", type: "checkbox"});
    const sep = h("input", {class: "form-check-input", type: "checkbox"});
    const check = (input, label, hint) => h("label", {class: "form-check"}, input, h("span", {class: "form-check-label"}, label), hint ? h("span", {class: "form-hint"}, hint) : null);
    const err = h("div", {class: "text-danger small"});
    const look = async () => {
      found.replaceChildren(h("div", {class: "text-secondary small"}, "Looking…"));
      const j = await (await nativeFetch("api/profiles")).json();
      found.replaceChildren(...(j.profiles.length ? j.profiles.map(p => h("label", {class: "list-group-item d-flex gap-2 align-items-center"},
        h("input", {type: "checkbox", class: "form-check-input", value: p.path, disabled: !p.readable}),
        h("div", null, h("div", null, `${p.browser}${p.profile ? " · " + p.profile : ""}`), h("div", {class: "small text-secondary text-break"}, p.readable ? p.path : (p.why || "macOS does not allow reading this here") + ". Export from the browser and choose that file above.")))) :
        [h("div", {class: "text-secondary small"}, "No browser profiles found here.")]));
    };
    I.body.replaceChildren(
      h("div", {class: "mb-3"}, h("label", {class: "form-label"}, "Bookmark files"), files,
        h("div", {class: "form-hint"}, "Any browser's Export bookmarks file (.html), a Chrome, Edge or Brave “Bookmarks” file, Firefox places.sqlite, or Safari Bookmarks.plist. Choose several to merge them. The files are copied into the workspace and never changed.")),
      h("div", {class: "mb-3"}, h("label", {class: "form-label"}, "Or a file in a browser profile"), path,
        h("button", {class: "btn btn-sm mt-2", onclick: look}, h("i", {class: "ti ti-search me-1"}), "Look for browser profiles"), found,
        h("div", {class: "form-hint"}, "Read from a copy, never from the profile itself, and nothing is ever written back to it: the cleaned set goes back to the browser as a file (Export as HTML, then the browser's Bookmarks › Import). Editing the browser's bookmarks in place needs the browser extension. macOS usually blocks this folder for tools; exporting from the browser always works.")),
      h("div", {class: "row mb-3"}, h("div", {class: "col"}, h("label", {class: "form-label"}, "Import into"), into),
        h("div", {class: "col"}, h("label", {class: "form-label"}, "Name of the new workspace"), name)),
      check(base, `Start from the bookmarks in “${W.name}”`, "Its current tree, with your edits, Trash left out; the files are merged into it."),
      check(sep, "Keep each file in its own top folder", "Otherwise folders with the same name are merged."),
      h("div", {class: "small text-secondary mt-2"}, "Duplicates are merged with the cleanup's rule: the same page (ignoring tracking parameters, keeping #sections) is kept once, in the deepest folder, with the earliest date. Each merged copy is listed in Merged duplicates (CSV)."),
      err);
    into.onchange = () => { name.disabled = base.disabled = into.value === "add"; };
    const go = h("button", {class: "btn btn-primary"}, h("i", {class: "ti ti-file-import me-1"}), "Import");
    go.onclick = async () => {
      err.textContent = "";
      const paths = [...found.querySelectorAll("input:checked")].map(i => i.value).concat(path.value.trim() ? [path.value.trim()] : []);
      if (!files.files.length && !paths.length && !base.checked) { err.textContent = "Choose a file first."; return; }
      go.disabled = true; go.lastChild.textContent = "Importing…";
      try {
        const payload = {files: await Promise.all([...files.files].map(read)), paths, separate: sep.checked};
        if (into.value === "add") { const j = await post(api("/sources"), payload); toast(j.entry.summary, "success", "Imported"); location.reload(); }
        else { const j = await post("api/ws", {...payload, name: name.value, base: base.checked ? W.id : null}); nav(j.id); }
      } catch (e) { err.textContent = e.message; }
      finally { go.disabled = false; go.lastChild.textContent = "Import"; }
    };
    I.foot.replaceChildren(h("button", {class: "btn", onclick: () => hideModal("#wsImport")}, "Cancel"), go);
    showModal("#wsImport");
  }
  const read = f => new Promise((ok, no) => { const r = new FileReader(); r.onload = () => ok({name: f.name, data: String(r.result).split(",", 2)[1] || ""}); r.onerror = () => no(new Error("could not read " + f.name)); r.readAsDataURL(f); });

  // ---------- start ----------
  addEventListener("DOMContentLoaded", () => {
    // Registered after the page's own pagehide/visibilitychange handlers (its script ran first), so the edit those
    // hand over is already here when this sends it.
    addEventListener("pagehide", saveNow);
    document.addEventListener("visibilitychange", () => { if (document.visibilityState === "hidden") saveNow(); });
    const edited = document.querySelector("#edited");
    edited?.before(menu());
    status();
    setInterval(poll, 4000);
    if (W.fresh && (legacy.tree || Object.keys(legacy.urls).length || Object.keys(legacy.refreshed).length)) {
      migrating = post(api("/migrate"), {state: legacy}).then(j => { head = j.head; status(); toast("The edits this browser kept are now in this workspace's history. They also stay in the browser, untouched.", "success", "Moved to the workspace"); })
        .catch(e => toast("Could not bring the browser's edits in: " + e.message, "danger")).finally(() => { migrating = null; });
    }
  });
  // The page's Download menu, Settings and the palette call these by name: keep the names stable.
  W.ui = {history, manage, importer, menu, duplicatesCsv, download, toggleLock, resetWs, updateWs, addLinks,
    backup: backupAll, restore: restoreAll, mcpAccess,
    deleteWs: () => deleteWs(W.id, W.name)};
})();
