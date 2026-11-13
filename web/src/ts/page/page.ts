// @ts-nocheck
// The page's main script, moved verbatim from preview_template.html on 2026-11-12 (step 1 of the TypeScript move).
// It is type-checked as it is split into typed modules (step 2): see .claude/design/bookmarks/bookmarks.md, "Code
// structure". The data still arrives inline, because the server swaps that line in the page (ws_api.page_html).
import { Dom } from "../dom";
import { PlaceMemory } from "../place";
import { HistoryAccessPanel } from "../history";
import { SnapshotSettingsPanel } from "../snapshot-settings";
import { Pager } from "../pager";
import { RangePicker } from "../rangepick";
import { FoldCenter } from "../fold";
import { cleanTags, starRating, tagEditor } from "../labels";
import { api } from "../api";
import { ViewSearch } from "../search";
import { UndoCenter } from "../undo";
import { Poller } from "../poller";
import { BrowserScope } from "../scope";
import { tabHistory } from "../components";
import { $, badge, el, fmtDate, fmtDay, icon, kbd } from "../ui";
import { looseLabel, pendingUpdate, rowElement } from "../views/tree-rows";
import { redactedNote, tabRowElement, windowCard as windowCardOf } from "../views/tab-cards";
import { DetailsPanel, STATUS_BG, STATUS_NAME, STATUS_SHORT, alertBox as alertBoxOf, countFolders, datagrid, folderSummary as folderSummaryOf, sectionCard } from "../views/details";
import { SingleRead, ago as agoOf, autosaveText, liveRowSub, planOpen, scopedSummary as scopedSummaryOf, snapshotRow, sortTabs as sortTabsBy, when as whenOf, sortWindows, tabCount, tabsIn, windowsOf } from "../tabs";
import { TimelineView } from "../views/timeline";
import { NO_SNAPSHOT, linkAlert, linkBadges, liveLoad, snapshotMessage } from "../views/link-panel";
import { CommandPalette } from "../views/palette";
import { actionRow, renderSettings, settingsGroup } from "../views/settings";
import { TrashDialog, reasonOf } from "../views/trash";
import { netscapeFile } from "../exportfile";
import { keyAction } from "../keys";
import { toCommand } from "../commands";
import { TabsSettingsPanel } from "../views/tabs-settings";
import { buildMenu, closeDropdowns, closePopMenu, installMenus, installPopMenus, placeDropdown, popMenu as _popMenu } from "../menu";
import { modeMenuItems, openMenuItems, sectionMenuItems, tabMenuItems, viewMenuItems, windowMenuItems } from "../views/tab-menus";
import { layoutMenu } from "../ctxmenu";
import { askConfirm, askText, hideModal, installDialogs, modalOpen, showModal, toast, visibleModals } from "../dialogs";
import { hideTip, installTips, showPop, tipsFromTitles } from "../tips";
import { afterTarget, dragged, dropAction, dropZone, looseGroupAt, navigate, rangeOf } from "../tree-rules";
import { OLD_TRASH, TRASH, byName, containsNode, defaultTree as defaultTreeOf, descendants, detachNode, isFolder, mkFolder, nice, parentsOf, pathIn, plain, restoreTree, revive, walkFolders } from "../tree";
const DATA = window.BF_DATA;
const LABEL = {dead: "dead link", moved: "moved — new address available", "moved-elsewhere": "redirects to another site",
  unverified: "could not be verified", "needs-login": "requires login", local: "local or staging site", skipped: "not checked (action link)"};
// What each status means, for tooltips and popovers.
const HELP = {
  ok: "The link answered normally when it was last checked.",
  dead: "The page is gone (404/410, or a page that says “not found”), or the whole site is gone (domain does not resolve, refuses connections, is parked or for sale). Dead links sit in _Review/Dead links; nothing is deleted.",
  moved: "The link redirects to a new address on the same site. Update it to the new address with one click, or keep the old one.",
  "moved-elsewhere": "The link now goes to a different site. That can be a rebrand, or a parked or taken-over domain, so check it before updating.",
  unverified: "The site blocked the automated check, timed out, or asked us to slow down. It may be fine; Refresh tries again, politely.",
  "needs-login": "The page sends visitors to a sign-in screen. The link works; it needs your login. The checker never signs in.",
  local: "A local or staging address (localhost, .test). It only works on the machine that served it, so it is not checked.",
  skipped: "Looks like an action link (log out, unsubscribe, confirm, reset). It is never visited, so nothing is triggered by accident.",
  prot: "Folders you marked untouched: kept exactly as they were, in the same order, never de-duplicated or rewritten.",
  dupes: "Copies removed because another bookmark points to the same page. The kept copy took the earliest date added. Nothing is lost: each removed copy is listed here and in duplicates.csv.",
};
const STATUS_ICON = {dead: "ti-alert-triangle", moved: "ti-arrow-forward-up", "moved-elsewhere": "ti-arrows-split",
  unverified: "ti-help-circle", "needs-login": "ti-lock", local: "ti-device-laptop", skipped: "ti-player-skip-forward", ok: "ti-circle-check"};
const FILTERS = [["all", "All bookmarks", "ti-list"], ["prot", "Untouched folders", "ti-lock"], ["dead", "Dead links", "ti-alert-triangle"],
  ["moved", "Moved (update ready)", "ti-arrow-forward-up"], ["moved-elsewhere", "Redirects elsewhere", "ti-arrows-split"],
  ["unverified", "Could not verify", "ti-help-circle"], ["needs-login", "Requires login", "ti-lock-open"], ["dupes", "Removed duplicates", "ti-copy"]];
// The Show control's segment labels; the full names above stay in tooltips, the palette and the details pane.
const FILTER_SHORT = {all: "All", prot: "Untouched", dead: "Dead", moved: "Moved", "moved-elsewhere": "Redirects", unverified: "Unverified", "needs-login": "Login", dupes: "Duplicates"};
const STORE = "bm-preview-v2";  // TRASH and OLD_TRASH come from tree.ts

// $, el, icon and kbd come from src/web/ts/ui.ts (imported above), shared with the typed view modules.
const list = $("#list"), frame = $("#frame"), info = $("#info"), shot = $("#shot"), grid = $("#grid");
const ITEM = new Map(); for (const i of DATA.keep) ITEM.set(i.id, i); for (const i of DATA.dupes) ITEM.set(i.id, i);
const DUPE = new Set(DATA.dupes.map(d => d.id));
const newMine = new Set(DATA.newMine);
const thumbVer = new Map();
let mode = "g", filter = "all", rows = [], sel = 0, anchor = null;
const marked = new Set();

// ---------- storage (a per-viewer convenience; the page works without it) ----------
// Served by browser-foreman serve, the edits (tree, link updates, refresh results) belong to a workspace kept on disk with its
// history (src/web/ts/ws_client.js); opened as a plain file, they stay in this browser as before.
const WS = window.BM_WS && window.BM_WS.get ? window.BM_WS : null;
const store = {
  get(k) { if (WS && WS.owns(k)) return WS.get(k); try { return JSON.parse(localStorage.getItem(STORE + ":" + k)); } catch { return null; } },
  set(k, v) { if (WS && WS.owns(k)) return WS.set(k, v); try { localStorage.setItem(STORE + ":" + k, JSON.stringify(v)); } catch {} },
  del(k) { if (WS && WS.owns(k)) return WS.set(k, null); try { localStorage.removeItem(STORE + ":" + k); } catch {} },
};
// ---------- settings: preferences for this tool, kept in this browser (never in a workspace, never exported) ----------
const SETTINGS_DEFAULT = {theme: "system", tooltips: true, tipDelay: 400, toastSecs: 4.5, openWarn: 20,
  treeThumbs: true, treeDesc: true, treeDates: true, tileLimit: 120, rememberPlace: true, pageSize: 25, liveMs: 5000};
const SETTINGS_KEY = STORE + ":settings";
let SET = (() => { try { return Object.assign({}, SETTINGS_DEFAULT, JSON.parse(localStorage.getItem(SETTINGS_KEY)) || {}); } catch { return {...SETTINGS_DEFAULT}; } })();
function saveSettings() { try { localStorage.setItem(SETTINGS_KEY, JSON.stringify(SET)); } catch {} applySettings(); window.dispatchEvent(new CustomEvent("bf:settings")); }
// Where you were (view, filter, search, selection, tab snapshot): in the address and this browser, restored on load.
const PLACE = new PlaceMemory({key: STORE + ":place"}).enabledWhen(() => SET.rememberPlace !== false);
let restoringPlace = true;  // nothing is written until restorePlace (end of this script) has read where you were
const nodeKey = r => r.kind === "folder" ? "f:" + pathOf(r.node) : "b:" + r.node;
// Page size is per list: a size picked in one list's footer changes that list only (kept in this browser). Every
// other list uses Settings › Rows per page.
const PAGE_SIZES = (() => { try { return JSON.parse(localStorage.getItem(STORE + ":page-sizes")) || {}; } catch { return {}; } })();
const listSize = k => PAGE_SIZES[k] ?? SET.pageSize;
const tilePage = new Map();  // folder path → page of its card overview
const setListSize = (k, n) => { PAGE_SIZES[k] = n; try { localStorage.setItem(STORE + ":page-sizes", JSON.stringify(PAGE_SIZES)); } catch {} };
const sysDark = matchMedia("(prefers-color-scheme: dark)");
function applySettings() {
  const t = SET.theme === "system" ? (sysDark.matches ? "dark" : "light") : SET.theme;
  document.documentElement.setAttribute("data-bs-theme", t);
  const b = document.body.classList;
  b.toggle("no-tree-thumbs", !SET.treeThumbs); b.toggle("no-tree-desc", !SET.treeDesc); b.toggle("no-tree-dates", !SET.treeDates);
}
sysDark.addEventListener?.("change", () => { if (SET.theme === "system") applySettings(); });
// The menu's Dark / light toggle sets an explicit theme; Settings can put it back to following the system.
function applyTheme(t) { SET.theme = t; saveSettings(); }
{ const old = store.get("theme"); if (old && !localStorage.getItem(SETTINGS_KEY)) SET.theme = old; }
applySettings();

for (const [id, v] of Object.entries(store.get("refreshed") || {})) { const i = ITEM.get(+id); if (i) Object.assign(i, v); }
// Moved links: the new address is suggested (i.nu); applying it is your choice, kept in this browser.
const URLS = store.get("urls") || {};
for (const [id, u] of Object.entries(URLS)) { const i = ITEM.get(+id); if (i && u !== i.u) { i.prev = i.u; i.u = u; i.applied = true; } }
// pendingUpdate comes from src/web/ts/views/tree-rows.ts.

// ---------- the editable tree: folder = {fid, n, c:[folder | bookmark id], from?, d? (description)} ----------
// The tree's model (node types, building, saving, walking, parents and paths): src/web/ts/tree.ts.

const defaultTree = m => defaultTreeOf(DATA.folders || [], DATA.keep.map(i => ({id: i.id, path: i[m]})));

// Saved edits are reconciled with the current data: ids that no longer exist drop out, new ones go where they belong,
// and anything emptied from the trash stays gone.
function loadState(m) {
  const saved = store.get("tree-" + m), openSaved = store.get("open-" + m);
  const base = {titles: {}, notes: {}, ratings: {}, tags: {}, open: new Set(openSaved || ["Bookmarks Bar"]), gone: new Set(), trashFrom: {}, edited: false};
  if (!saved) return {...base, root: defaultTree(m)};
  const gone = new Set(saved.gone || []);
  const root = restoreTree(saved.root, {valid: id => ITEM.has(id) && !DUPE.has(id), gone, items: DATA.keep.map(i => ({id: i.id, path: i[m]}))});
  return {...base, root, titles: saved.titles || {}, notes: saved.notes || {}, ratings: saved.ratings || {}, tags: saved.tags || {}, open: new Set(openSaved || saved.open || ["Bookmarks Bar"]), gone, trashFrom: saved.trashFrom || {}, edited: true};
}
const S = {g: loadState("g")};
const undoStack = {g: []}, redoStack = {g: []};  // used only when no workspace is attached (the page opened as a file)
const st = () => S[mode];

let PARENT = new Map();
function indexParents(root = st().root) { PARENT = parentsOf(root); }
const pathOf = f => pathIn(f, PARENT, st().root);
const isOpen = f => st().open.has(pathOf(f));
function setOpen(f, open) { const p = pathOf(f); if (open) st().open.add(p); else st().open.delete(p); }
const trashFolder = () => st().root.c.find(x => isFolder(x) && x.n === TRASH);
const inTrash = x => { const t = trashFolder(); for (let p = PARENT.get(x); p; p = PARENT.get(p)) if (p === t) return true; return false; };

let saveTimer;
function persist() {
  clearTimeout(saveTimer);
  saveTimer = setTimeout(() => {
    const s = st();
    if (s.edited) store.set("tree-" + mode, {root: plain(s.root), titles: s.titles, notes: s.notes, ratings: s.ratings, tags: s.tags, gone: [...s.gone], trashFrom: s.trashFrom});
  }, 300);
}
// Never lose the last half-second of edits: when the page is hidden or closed, write at once.
function persistNow() { clearTimeout(saveTimer); const s = st(); if (s.edited) store.set("tree-" + mode, {root: plain(s.root), titles: s.titles, notes: s.notes, ratings: s.ratings, tags: s.tags, gone: [...s.gone], trashFrom: s.trashFrom}); }
addEventListener("pagehide", persistNow);
document.addEventListener("visibilitychange", () => { if (document.visibilityState === "hidden") persistNow(); });
// With a server the edits are saved in the workspace (with its history); opened as a file, they stay in this browser.
const EDITED = () => WS ? "Edited · saved in this workspace, with its history" : "Edited · kept in this browser";
function save() { st().edited = true; persist(); $("#edited").textContent = EDITED(); }
// Which folders are open is a view preference, saved on its own: it must never freeze the tree against a rebuild.
const saveOpen = () => { store.set("open-" + mode, [...st().open]); };

function snapshot() {
  const s = st();
  undoStack[mode].push(JSON.stringify({root: plain(s.root), titles: s.titles, notes: s.notes, ratings: s.ratings, tags: s.tags, gone: [...s.gone], trashFrom: s.trashFrom}));
  if (undoStack[mode].length > 60) undoStack[mode].shift();
  redoStack[mode] = [];  // a new edit ends what could be redone
  setTimeout(() => UNDO.refresh(), 0);
}
// Bookmarks: with a workspace, its history is the undo stack (server-side, redo included); opened as a file, the
// page keeps its own two stacks.
const pageState = () => { const s = st(); return JSON.stringify({root: plain(s.root), titles: s.titles, notes: s.notes, ratings: s.ratings, tags: s.tags, gone: [...s.gone], trashFrom: s.trashFrom}); };
function applyPageState(u) { const o = JSON.parse(u), s = st(); s.root = revive(o.root); s.titles = o.titles; s.notes = o.notes || {}; s.ratings = o.ratings || {}; s.tags = o.tags || {}; s.gone = new Set(o.gone); s.trashFrom = o.trashFrom; save(); build(); }
function undo() {
  if (WS) return WS.undo();
  const u = undoStack[mode].pop(); if (!u) return toast("Nothing to undo");
  redoStack[mode].push(pageState()); applyPageState(u); toast("Undone", "success"); UNDO.refresh();
}
function redo() {
  if (WS) return WS.redo?.();
  const u = redoStack[mode].pop(); if (!u) return toast("Nothing to redo");
  undoStack[mode].push(pageState()); applyPageState(u); toast("Redone", "success"); UNDO.refresh();
}
// Undo and redo act on the view you are in: bookmark edits in Bookmarks, closed or moved tabs in Open tabs. The
// Timeline has nothing to undo, so its view shows no buttons and ⌘Z does nothing there.
const UNDO = new UndoCenter()
  .register("bookmarks", {
    undoLabel: () => readOnly?.() ? null : WS ? (WS.can?.undo ?? null) : undoStack[mode].length ? "your last edit" : null,
    redoLabel: () => readOnly?.() ? null : WS ? (WS.can?.redo ?? null) : redoStack[mode].length ? "the last undone edit" : null,
    undo, redo})
  .register("tabs", {undoLabel: () => tabHistory.undoLabel, redoLabel: () => tabHistory.redoLabel, undo: () => tabHistory.undo(), redo: () => tabHistory.redo()});
const UNDO_LABEL = k => UNDO.label(k);
// Expand / collapse all: one header button for every view (ts/fold.ts). Open tabs registers itself in TABS.
let FOLD = null;
function setupFold() {
  FOLD = new FoldCenter($("#foldBtn")).register("bookmarks", {
    anyOpen: () => rows.some(r => r.kind === "folder") ? rows.some(r => r.kind === "folder" && r.open) : null,
    expand: () => { expandAllFolders(); list.focus(); }, collapse: () => { collapseAllFolders(); list.focus(); }})
    .register("tabs", TABS.fold).show(view_);
}
addEventListener("bf:undo", () => UNDO.refresh());
document.addEventListener("click", e => {
  const b = e.target.closest?.("[data-brofo-undo] [data-brofo-action]"); if (!b) return;
  e.preventDefault(); closeDropdowns(); Dom.read(b, "action") === "redo" ? UNDO.redo() : UNDO.undo();
});

// ---------- items ----------
const titleOf = id => st().titles[id] ?? ITEM.get(id).t;
const noteOf = id => st().notes[id];
// Last opened: from the browsers' own history, read when the page opens (owner only; never saved). Keyed by the
// bookmark's address as the server holds it. OPENED_READ is how many history files could be read (0: none).
let OPENED = new Map(), OPENED_READ = 0, OPENED_BLOCKED = 0;
const openedOf = u => OPENED.get(u);
async function loadOpened() {
  if (!WS || !window.BM_WS?.id) return;
  try {
    const j = await api.path("opened").get();  // v1-compat: GET /api/v1/workspaces/{id}/opened
    OPENED = new Map(Object.entries(j.opened || {}).map(([u, v]) => [u, {t: Math.floor(Date.parse(v.last) / 1000), n: v.visits}]));
    OPENED_READ = j.read || 0; OPENED_BLOCKED = (j.skipped || []).length;
    build(true); if (rows[sel]?.kind === "link") show();
  } catch { /* not the owner, or the server has no history access: the column stays empty */ }
}
function openedBucket(t) {
  if (!t) return OPENED_READ ? "Never opened" : "Unknown (history not readable)";
  const days = (Date.now() / 1000 - t) / 86400;
  return days < 1 ? "Today" : days < 7 ? "This week" : days < 31 ? "This month" : days < 365 ? "This year" : "Over a year ago";
}
const ratingOf = id => st().ratings?.[id] || 0;
const tagsOf = id => st().tags?.[id] || [];
// Every tag in use, most used first (the tag field suggests these).
function allTags() {
  const n = new Map();
  for (const ts of Object.values(st().tags || {})) for (const t of ts) n.set(t, (n.get(t) || 0) + 1);
  return [...n.keys()].sort((a, b) => n.get(b) - n.get(a) || a.localeCompare(b));
}
const descOf = id => noteOf(id) ?? ITEM.get(id).ds ?? "";
function host(u) { try { return new URL(u).hostname.replace(/^www\./, ""); } catch { return ""; } }
function hue(s) { let h = 0; for (const c of s) h = (h * 31 + c.charCodeAt(0)) % 360; return h; }
function favicon(i) {
  const h = host(i.u) || "?";
  const letter = () => { const d = el("span", "fvd", (h[0] || "?").toUpperCase()); d.style.background = `hsl(${hue(h)} 45% 45%)`; return d; };
  if (!i.fv) return letter();
  const img = el("img", "fv"); img.alt = ""; img.loading = "lazy"; img.src = `favicons/${i.fv}`;
  img.onerror = () => img.replaceWith(letter());
  return img;
}
// A link as people read it: site and path, no query string or fragment, cut in the middle when still long.
function shortUrl(u, max = 64) {
  let t;
  try { const x = new URL(u); t = x.hostname.replace(/^www\./, "") + decodeURI(x.pathname).replace(/\/$/, ""); } catch { t = String(u); }
  if (t.length <= max) return t;
  const keep = max - 1, head = Math.ceil(keep * 0.6);
  return t.slice(0, head) + "…" + t.slice(t.length - (keep - head));
}
function thumb(i, cls) {
  const h = host(i.u) || i.u || "?";
  const ph = () => {
    if (i.fv) { const d = el("div", cls + " ph fav"); const im = el("img"); im.alt = ""; im.src = `favicons/${i.fv}`; d.append(im, el("span", null, h)); return d; }
    // No snapshot and no favicon: a soft tint of the site's colour with its initial and name, never a loud block.
    const d = el("div", cls + " ph letter"), k = hue(h);
    d.style.setProperty("--h", String(k));  // the tint is in the stylesheet, light and dark
    d.append(el("b", null, (h[0] || "?").toUpperCase()), el("span", null, h)); return d;
  };
  if (!i.h) return ph();
  const img = el("img", cls); img.loading = "lazy"; img.decoding = "async"; img.alt = "";
  img.src = `thumbs/${i.h}.jpg` + (thumbVer.has(i.h) ? `?v=${thumbVer.get(i.h)}` : "");
  img.onerror = () => img.replaceWith(ph());
  return img;
}
// fmtDate and fmtDay come from src/web/ts/ui.ts.

// ---------- view: grouping, sorting, folder position, date range — what you see is what the download contains ----------
const VIEW_DEFAULT = {group: "folder", sort: "name", folders: "first", from: "", to: ""};
let view = Object.assign({}, VIEW_DEFAULT, store.get("view") || {});
// Read-only: the duplicates view, and a locked workspace (the server refuses its changes anyway).
const readOnly = () => filter === "dupes" || !!WS?.locked;
const grouped = () => !readOnly() && view.group !== "folder";
const vopen = new Set();
const VIEW_OPTS = [
  ["group", "Group by", [["folder", "Folders"], ["site", "Site"], ["status", "Status"], ["year", "Year added"], ["month", "Month added"], ["rating", "Rating"], ["tag", "Tag"], ["opened", "Last opened"], ["flat", "One list"]]],
  ["sort", "Sort", [["manual", "Manual"], ["name", "Name A–Z"], ["name-desc", "Name Z–A"], ["new", "Newest first"], ["old", "Oldest first"], ["site", "Site"], ["status", "Status"], ["rating", "Highest rated"], ["opened", "Recently opened"]]],
  ["folders", "Folders", [["first", "First"], ["last", "Last"], ["mixed", "Mixed with links"]]],
];
function setView(k, v) { view[k] = v; store.set("view", view); const s = $("#v-" + k); if (s) s.value = v; }
const STATUS_ORDER = {dead: 0, "moved-elsewhere": 1, unverified: 2, "needs-login": 3, moved: 4, ok: 5};
function cmpLinks(a, b) {
  const A = ITEM.get(a), B = ITEM.get(b), byTitle = () => titleOf(a).localeCompare(titleOf(b));
  switch (view.sort) {
    case "name-desc": return titleOf(b).localeCompare(titleOf(a));
    case "new": return (B.a || 0) - (A.a || 0) || byTitle();
    case "old": return (A.a || 0) - (B.a || 0) || byTitle();
    case "site": return host(A.u).localeCompare(host(B.u)) || byTitle();
    case "status": return (STATUS_ORDER[A.s] ?? 9) - (STATUS_ORDER[B.s] ?? 9) || byTitle();
    case "rating": return ratingOf(b) - ratingOf(a) || byTitle();
    case "opened": return (openedOf(B.u)?.t || 0) - (openedOf(A.u)?.t || 0) || byTitle();
    default: return byTitle();
  }
}
function ordered(f) {
  const c = f.c;
  if (view.sort === "manual" && view.folders === "mixed") return c;
  const fs = c.filter(isFolder), ls = c.filter(x => !isFolder(x));
  const sf = view.sort === "manual" ? fs : [...fs].sort((a, b) => view.sort === "name-desc" ? byName(b.n, a.n) : byName(a.n, b.n));
  const sl = view.sort === "manual" ? ls : [...ls].sort(cmpLinks);
  if (view.folders === "first") return [...sf, ...sl];
  if (view.folders === "last") return [...sl, ...sf];
  if (view.sort === "manual") return c;
  const name = x => isFolder(x) ? x.n : titleOf(x);
  return [...c].sort((a, b) => name(a).localeCompare(name(b)) * (view.sort === "name-desc" ? -1 : 1));
}
function materialize() {
  if (view.sort === "manual") return;
  const w = f => { f.c = ordered(f); f.c.filter(isFolder).forEach(w); };
  w(st().root);
  setView("sort", "manual"); toast("Sort is now Manual, starting from the order you saw");
}
function virtualTree() {
  const ids = descendants(st().root).filter(id => !inTrash(id)), root = mkFolder("");
  if (view.group === "flat") { root.c = ids; return root; }
  const key = id => {
    const i = ITEM.get(id);
    if (view.group === "site") return host(i.u) || "(no site)";
    if (view.group === "status") return i.s === "ok" ? "Working" : i.s === "dead" ? (i.dk === "host" ? "Dead: whole site gone" : "Dead: page gone") : (LABEL[i.s] || i.s);
    if (view.group === "month") return i.a ? new Date(i.a * 1000).toISOString().slice(0, 7) : "Unknown date";
    // One folder per bookmark: a bookmark with several tags sits under its first (they are sorted).
    if (view.group === "tag") return tagsOf(id)[0] || "(no tags)";
    if (view.group === "rating") { const r = ratingOf(id); return r ? `${"★".repeat(r)} ${r} star${r === 1 ? "" : "s"}` : "Not rated"; }
    if (view.group === "opened") return openedBucket(openedOf(i.u)?.t);
    return i.a ? String(new Date(i.a * 1000).getFullYear()) : "Unknown year";
  };
  const by = new Map();
  for (const id of ids) { const k = key(id); if (!by.has(k)) { const f = mkFolder(k); f.virtual = true; by.set(k, f); root.c.push(f); } by.get(k).c.push(id); }
  return root;
}
// Date range: datetime-local values, compared with each bookmark's date added.
const toEpoch = v => v ? new Date(v).getTime() / 1000 : null;
function matcher() {
  const q = $("#q").value.trim().toLowerCase(), from = toEpoch(view.from), to = toEpoch(view.to);
  return id => {
    const i = ITEM.get(id);
    if (filter === "prot" && !(i.p || i.m)) return false;
    if (!["all", "prot", "dupes"].includes(filter) && i.s !== filter) return false;
    if (filter === "moved" && !pendingUpdate(i)) return false;  // "Moved (update ready)": not those already updated
    if (from != null && !(i.a && i.a >= from)) return false;
    if (to != null && !(i.a && i.a <= to)) return false;
    if (!q) return true;
    // "#work" matches the tag exactly; any other words also search the tags.
    if (q.startsWith("#") && !q.includes(" ")) return tagsOf(id).some(t => t.toLowerCase() === q.slice(1));
    return (titleOf(id) + " " + i.u + " " + i.ou + " " + nice(i.o) + " " + descOf(id) + " " + tagsOf(id).join(" ")).toLowerCase().includes(q);
  };
}
const filtering = () => filter !== "all" || $("#q").value.trim() || view.from || view.to;

function buildToolbar() {
  // Each chip carries its count (these were the stat cards above the bar; one control now, not two).
  const n = k => k === "all" ? DATA.keep.length : k === "dupes" ? DATA.dupes.length : k === "prot" ? DATA.keep.filter(i => i.p).length : DATA.keep.filter(i => i.s === k).length;
  const extra = {all: `${DATA.total.toLocaleString()} before cleanup.`, dead: `${DATA.keep.filter(i => i.s === "dead" && i.dk === "host").length} whole site gone, ${DATA.keep.filter(i => i.s === "dead" && i.dk !== "host").length} page gone.`};
  for (const [k, l, ic] of FILTERS) {
    const b = el("button", "nav-link"); b.type = "button"; b.setAttribute("role", "tab"); b.dataset.f = k; b.setAttribute("aria-label", l); b.append(icon(ic), FILTER_SHORT[k] || l, el("span", "chip-n", n(k).toLocaleString()));  // refreshed live by paintChipCounts
    b.dataset.tip = [l + ".", HELP[k], extra[k]].filter(Boolean).join(" ");
    b.onclick = () => setFilter(k);
    $("#chips").append(b);
  }
  for (const [k, l, opts] of VIEW_OPTS) {
    const s = el("select", "form-select form-select-sm"); s.id = "v-" + k;
    for (const [v, t] of opts) { const o = el("option", null, t); o.value = v; s.append(o); }
    s.value = view[k];
    // A compact picker: the label, the chosen value, a chevron; the real select lies over it, invisible.
    const lab = el("label", "btn btn-sm tb-pick"), val = el("b", null, s.selectedOptions[0]?.text || "");
    const paint = () => { val.textContent = s.selectedOptions[0]?.text || ""; lab.classList.toggle("is-set", s.value !== VIEW_DEFAULT[k]); };
    s.onchange = () => { setView(k, s.value); paint(); marked.clear(); sel = 0; build(); list.focus(); };
    s.setAttribute("aria-label", l);
    lab.append(el("span", "tb-pick-label", l), val, icon("ti-chevron-down", "text-secondary"), s); paint();
    $("#view").append(lab);
  }
  DATES = new RangePicker({label: "Added", heading: "Bookmarks added…", defaultKey: "Any time", value: "Any time",
    options: DATE_PRESETS.map(([label]) => ({key: label, label})), columns: 1,
    onPick: key => { const p = DATE_PRESETS.find(x => x[0] === key); if (p) setDates(...p[1](), key); },
    onRange: (from, to) => { datePreset = ""; setView("from", from); setView("to", to); sel = 0; build(); }});
  $("#dates").replaceWith(DATES.el); DATES.el.id = "dates";
  $("#clearFilters").onclick = clearFilters;
  paintChips();
}
const iso = d => d ? new Date(d.getTime() - d.getTimezoneOffset() * 60000).toISOString().slice(0, 16) : "";
const DATE_PRESETS = [
  ["Today", () => { const d = new Date(); d.setHours(0, 0, 0, 0); return [d, null]; }],
  ["Last 7 days", () => [new Date(Date.now() - 7 * 864e5), null]],
  ["Last 30 days", () => [new Date(Date.now() - 30 * 864e5), null]],
  ["Last 12 months", () => [new Date(Date.now() - 365 * 864e5), null]],
  ["This year", () => [new Date(new Date().getFullYear(), 0, 1), null]],
  ["Last year", () => { const y = new Date().getFullYear() - 1; return [new Date(y, 0, 1), new Date(y, 11, 31, 23, 59)]; }],
  ["Older than 3 years", () => [null, new Date(Date.now() - 3 * 365 * 864e5)]],
  ["Before 2020", () => [null, new Date(2019, 11, 31, 23, 59)]],
  ["Any time", () => [null, null]],
];
function setDates(from, to, preset = "") { datePreset = from || to ? preset : ""; setView("from", iso(from)); setView("to", iso(to)); sel = 0; build(); }
// The Added picker's label: the preset chosen, else the range typed, else "Any time".
let datePreset = "", DATES = null;
function paintDates() {
  if (!DATES) return;
  if (datePreset) DATES.set(datePreset); else DATES.setRange(view.from, view.to);
}
function setFilter(k) { filter = k; paintChips(); marked.clear(); sel = 0; build(); list.focus(); if (!restoringPlace) PLACE.update({filter: k === "all" ? "" : k}); }
// The chips count what the tree holds now, outside the Trash (moved, renamed, trashed or deleted bookmarks included);
// they used to count the original set, so "Dead links 470" stayed after every dead link had been deleted.
function paintChipCounts() {
  const bin = trashFolder(), by = new Map(); let all = 0, prot = 0;
  const walk = f => { for (const x of f.c) { if (isFolder(x)) { if (x !== bin) walk(x); continue; } const i = ITEM.get(x); if (!i) continue; all++; if (i.p) prot++;
    if (i.s !== "moved" || pendingUpdate(i)) by.set(i.s, (by.get(i.s) || 0) + 1); } };
  walk(st().root);
  for (const b of $("#chips").children) {
    const k = b.dataset.f, v = k === "all" ? all : k === "dupes" ? DATA.dupes.length : k === "prot" ? prot : by.get(k) || 0;
    const n = b.querySelector(".chip-n"); if (n) n.textContent = v.toLocaleString();
  }
}
function paintChips() {
  for (const b of $("#chips").children) {
    const on = b.dataset.f === filter; b.classList.toggle("active", on); b.setAttribute("aria-selected", String(on));
    if (on) {  // keep the chosen one in view when the row scrolls sideways (never scrolls the page itself)
      const row = b.parentElement!, l = b.offsetLeft - row.offsetLeft, r = l + b.offsetWidth;
      if (l < row.scrollLeft) row.scrollLeft = l; else if (r > row.scrollLeft + row.clientWidth) row.scrollLeft = r - row.clientWidth;
    }
  }
  for (const c of document.querySelectorAll(".stat")) c.classList.toggle("active", c.dataset.f === filter);
}
function clearFilters() { $("#q").value = ""; SEARCH.sync(); setDates(null, null); setFilter("all"); }

// ---------- header: stat cards that filter when clicked ----------
function summary() {
  const n = s => DATA.keep.filter(i => i.s === s).length;
  const cards = [
    ["all", "Bookmarks", DATA.keep.length, `${DATA.total.toLocaleString()} before cleanup`, "ti-bookmarks", "primary"],
    ["moved", "Moved", n("moved"), "new address ready to apply", "ti-arrow-forward-up", "warning"],
    ["moved-elsewhere", "Redirect elsewhere", n("moved-elsewhere"), "check each one first", "ti-arrows-split", "purple"],
    ["dead", "Dead links", n("dead"), `${DATA.keep.filter(i => i.s === "dead" && i.dk === "host").length} whole site gone, ${DATA.keep.filter(i => i.s === "dead" && i.dk !== "host").length} page gone`, "ti-alert-triangle", "danger"],
    ["unverified", "Could not verify", n("unverified"), "site blocked the check or timed out", "ti-help-circle", "secondary"],
    ["dupes", "Duplicates removed", DATA.dupes.length, "merged, nothing lost", "ti-copy", "info"],
  ];
  $("#stats").replaceChildren(...cards.map(([f, t, v, sub, ic, col]) => {
    const col_ = el("div", "col-6 col-md-4 col-xl-2"), c = el("div", "card card-sm stat h-100 position-relative"); c.dataset.f = f;
    c.innerHTML = `<div class="card-body d-flex align-items-center gap-3 pe-4"><span class="avatar bg-${col}-lt flex-none"><i class="ti ${ic}"></i></span>
      <div class="min-w-0"><div class="h3 mb-0 lh-1">${v.toLocaleString()}</div><div class="fw-medium small mt-1">${t}</div><div class="text-secondary small text-truncate">${sub}</div></div></div>`;
    c.onclick = () => setFilter(f);
    c.dataset.tip = "Click to show only these.";
    const help = el("button", "btn btn-ghost-secondary btn-icon btn-sm position-absolute top-0 end-0 m-1"); help.type = "button"; help.setAttribute("data-pop", ""); help.setAttribute("aria-label", "What does this mean?");
    help.append(icon("ti-info-circle"));
    help.onclick = e => { e.stopPropagation(); showPop(help, t, HELP[f] || "Every bookmark in the cleaned structure, after duplicates were merged."); };
    c.append(help);
    col_.append(c); return col_;
  }));
  paintChips();
}

function setMode(m) {
  mode = m; marked.clear(); sel = 0;
  $("#edited").textContent = st().edited ? EDITED() : "";
  build(); list.focus();
}
let qt; $("#q").oninput = () => { clearTimeout(qt); qt = setTimeout(() => { sel = 0; build(); PLACE.update({q: $("#q").value.trim()}); }, 150); };
const on = (id, fn) => { $(id).onclick = e => { e.preventDefault(); closeDropdowns(); fn(); list.focus(); }; };
// While a filter, search or the duplicates view forces folders open, these act on that view's own closed set.
const forcedView = () => filtering() || readOnly();
// Expand / collapse every folder: the toggle in the Folders card header, the command palette and the "-" key.
// In a filtered view the folders' open state is the view's own (viewClosed); the Trash keeps its saved state in every
// view, so both buttons set it too. They used to skip it: Collapse all left the Trash open.
function expandAllFolders() {
  if (forcedView()) { viewClosed.clear(); const bin = trashFolder(); if (bin) { setOpen(bin, true); saveOpen(); } build(); return; }
  walkFolders(st().root, f => setOpen(f, true)); saveOpen(); build();
}
function collapseAllFolders() {
  if (forcedView()) {
    for (const r of rows) if (r.kind === "folder") { if (r.trash) setOpen(r.node, false); else viewClosed.add(r.key); }
    saveOpen(); sel = 0; build(); return;
  }
  st().open.clear(); saveOpen(); sel = 0; build();
}
// Open a folder and everything inside it, in the saved tree or the forced view.
function expandInside(r) {
  if (forcedView()) { for (const k of [...viewClosed]) if (k === r.key || k.startsWith(r.key + "/")) viewClosed.delete(k); build(true); return; }
  setOpen(r.node, true); walkFolders(r.node, f => setOpen(f, true)); saveOpen(); build();
}
// #undo and #redo (the ⋮ menu) are data-brofo-action controls: the one click handler beside UNDO runs them.
// Reset edits: in Settings › Danger zone (and the command palette), not in a menu.
async function resetEdits() {
  if (!(await askConfirm({title: "Reset all your edits?", ok: "Reset everything",
    body: "Every move, rename, note, trash and permanent deletion made in this preview is thrown away, and the tree goes back to the generated one. This cannot be undone."}))) return;
  store.del("tree-" + mode); S[mode] = loadState(mode); undoStack[mode] = []; setMode(mode); toast("Edits reset");
}
on("#download", downloadFile);
on("#refreshAll", () => refresh(true));
on("#paletteBtn", openPalette);
on("#shortcutsBtn", openShortcuts);
on("#settingsBtn", openSettings);
// The ⋮ menu's Workspace section (hidden when the page runs without a server, where there are no workspaces).
on("#wsAllBtn", () => wsUI()?.manage?.());
on("#wsRenameBtn", () => wsUI()?.rename?.());
on("#wsNotesBtn", () => wsUI()?.notes?.());
for (const x of document.querySelectorAll(".ws-only")) x.hidden = !WS;
// ---------- onboarding: a welcome on first open, then a guided tour that can be replayed any time ----------
const TOUR = [
  [".hdr-views", "Bookmarks, open tabs and the timeline", "Switch between your bookmarks, the tabs open in your browsers, and a timeline of everything that happened. Open tabs are saved every few minutes, so after a crash you can see what was open and reopen anything that was lost."],
  ["#chips", "Your bookmarks at a glance, one kind at a time", "Each chip counts a kind (all, dead, moved, redirecting elsewhere, could not verify, needs a login, untouched folders, merged duplicates) and shows only those when clicked."],
  [".toolbar .tb-row + .tb-row", "Group, sort and date", "Group by folder, site, status or the date added; sort; put folders first or last; limit to bookmarks added between two dates."],
  ["#hq", "Search the view you are in", "In Bookmarks: titles, links, descriptions and folder names. In Open tabs: titles and addresses. In Timeline: the events. Each view keeps its own search. Press / to jump here from anywhere."],
  [".app-main > section:first-child", "Your bookmarks", "One structure: your own folders, with the cleanup's additions inside them. Arrow keys move, → opens a folder, Space selects, ⇧ or ⌘ selects several. Right-click (or .) for everything you can do."],
  ["#infoHead", "Details of what is selected", "A folder shows its link health, where its bookmarks came from and a card for each; a bookmark shows its status, details and a snapshot of the page."],
  ["#infoActs", "Actions", "Open links (new tabs, a new window, one window per folder, incognito) or move them to tabs, refresh them, rename, describe, move into any folder through the menus, or move to the Trash."],
  ["#paletteBtn", "Every command, by name", "⌘K opens the command palette. ? shows every keyboard shortcut, F1 opens help."],
  ["#refreshAll", "Re-check links", "Visits the links again and retakes snapshots, politely and a few at a time. Pause or stop it whenever you like; what was checked is kept."],
  ["#wsMenu", "Workspaces", "Each bookmark set you clean is a workspace, saved on this computer with its full history. Switch, rename, add notes, or archive one here."],
  ["#ioGroup", "Import and export", "The arrow holds everything that moves bookmarks in or out: import from any browser, export HTML or Chrome JSON, the merged-duplicates list, and history with roll back."],
  ["#download", "Take it back to your browser", "Writes a standard bookmarks file. In Chrome: Bookmarks → Import bookmarks and settings → Bookmarks HTML file. It arrives as one new folder, so nothing you already have is overwritten."],
];
let tourAt = -1;
function tour(start = 0) { closeDropdowns(); hideTip(); tourAt = start; $("#tourShade").hidden = false; $("#tourCard").hidden = false; paintTour(); }
function endTour(done) {
  tourAt = -1; $("#tourShade").hidden = true; $("#tourCard").hidden = true; document.querySelector(".tour-target")?.classList.remove("tour-target");
  try { localStorage.setItem(STORE + ":onboarded", "1"); } catch {}
  if (done) toast("Replay it any time from Help (F1) or the command palette.", "success", "Tour finished");
  list.focus();
}
function paintTour() {
  const [sel_, title, text] = TOUR[tourAt]; const t = document.querySelector(sel_);
  document.querySelector(".tour-target")?.classList.remove("tour-target");
  if (!t) return tourAt < TOUR.length - 1 ? (tourAt++, paintTour()) : endTour(true);
  t.scrollIntoView({block: "nearest"}); t.classList.add("tour-target");
  $("#tourStep").textContent = `${tourAt + 1} of ${TOUR.length}`; $("#tourTitle").textContent = title; $("#tourText").textContent = text;
  $("#tourBack").disabled = tourAt === 0; $("#tourNext").replaceChildren(tourAt === TOUR.length - 1 ? "Finish" : "Next", icon(tourAt === TOUR.length - 1 ? "ti-check" : "ti-arrow-right", "ms-1"));
  const r = t.getBoundingClientRect(), shade = $("#tourShade"), pad = 6;
  Object.assign(shade.style, {left: r.left - pad + "px", top: r.top - pad + "px", width: r.width + 2 * pad + "px", height: r.height + 2 * pad + "px"});
  // The card goes where there is most room: below, above, right or left of the highlighted part.
  const card = $("#tourCard"), cw = card.offsetWidth, ch = card.offsetHeight, gap = 14;
  const tall = r.height > innerHeight * 0.45;  // a tall area (the tree) gets the card beside it, not over the toolbar
  const opts = tall ? [["right", innerWidth - r.right], ["left", r.left], ["below", innerHeight - r.bottom], ["above", r.top]]
    : [["below", innerHeight - r.bottom], ["above", r.top], ["right", innerWidth - r.right], ["left", r.left]];
  const fits = ([side, room]) => room >= (side === "below" || side === "above" ? ch : cw) + gap + 8;
  const side = (opts.find(fits) || opts.sort((a, b) => b[1] - a[1])[0])[0];
  let x = side === "right" ? r.right + gap : side === "left" ? r.left - cw - gap : r.left + r.width / 2 - cw / 2;
  let y = side === "below" ? r.bottom + gap : side === "above" ? r.top - ch - gap : r.top + r.height / 2 - ch / 2;
  x = Math.max(12, Math.min(x, innerWidth - cw - 12)); y = Math.max(12, Math.min(y, innerHeight - ch - 12));
  card.style.left = x + "px"; card.style.top = y + "px"; card.dataset.side = side;
  $("#tourNext").focus();
}
on("#tourNext", () => tourAt < TOUR.length - 1 ? (tourAt++, paintTour()) : endTour(true));
on("#tourBack", () => { if (tourAt > 0) { tourAt--; paintTour(); } });
on("#tourSkip", () => endTour(false)); on("#tourClose", () => endTour(false));
document.addEventListener("keydown", e => {
  if (tourAt < 0) return;
  if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); endTour(false); }
  else if (e.key === "ArrowRight" || e.key === "Enter") { e.preventDefault(); e.stopPropagation(); $("#tourNext").click(); }
  else if (e.key === "ArrowLeft") { e.preventDefault(); e.stopPropagation(); $("#tourBack").click(); }
  else e.stopPropagation();
}, true);
addEventListener("resize", () => { if (tourAt >= 0) paintTour(); });
// Welcome, shown once: what this is, how to get bookmarks in and out, and an offer of the tour.
async function welcome() {
  const go = await askConfirm({title: `Welcome to ${APP.name}`, ok: "Take the 1-minute tour", no: "Not now", danger: false,
    body: `${APP.name} keeps your bookmarks clean and your tabs safe. This build holds ${DATA.keep.length.toLocaleString()} of your bookmarks (from ${DATA.total.toLocaleString()}), checked and sorted into one structure built on your own folders, and Open tabs saves your browser windows so a crash never loses them. ` +
      "Review and edit here; nothing changes in your browser until you download the import file and import it, or write it back with the browser extension (Browsers › Browser extensions…)."});
  try { localStorage.setItem(STORE + ":onboarded", "1"); } catch {}
  if (go) tour(0);
}
try { if (!localStorage.getItem(STORE + ":onboarded") && !sessionStorage.getItem(STORE + ":reset-done")) setTimeout(welcome, 600); } catch {}
on("#aboutBtn", () => openAbout("about"));
// ---------- views: Bookmarks and Open tabs ----------
let view_ = "bookmarks";
// The header search types into the view you are in (each view keeps its own query and its own hidden input).
const SEARCH = new ViewSearch($("#hq")).view("bookmarks", $("#q"), "Search bookmarks…").view("tabs", $("#tabsQ"), "Filter open tabs…")
  .view("timeline", Dom.one("timeline", "query"), "Search the timeline…").show("bookmarks");
function setAppView(v, {restoring = false} = {}) {
  view_ = v; closeDropdowns(); hideTip();
  if (!restoring) PLACE.update({view: v}, {push: true});
  SEARCH.show(v); UNDO.show(v); FOLD?.show(v);
  for (const b of Dom.all("view")) { const on = Dom.read(b, "view") === v; b.classList.toggle("active", on); b.setAttribute("aria-selected", on); }
  document.body.classList.toggle("view-tabs", v === "tabs");
  $("#tabsView").hidden = v !== "tabs"; $("#timelineView").hidden = v !== "timeline";
  for (const e of document.querySelectorAll(".bm-only")) e.hidden = v !== "bookmarks";
  if (v === "tabs") TABS.open(); else if (v === "timeline") TL.open(); else list.focus();
}
for (const b of Dom.all("view")) b.onclick = () => setAppView(Dom.read(b, "view"));

// ---------- open tabs: live windows, saved snapshots, reopen what a crash closed ----------
const TABS = (() => {
  let snaps = [], cur = "live", data = null, live = null, cfg = null, lastAuto = null, loading = false, stat = {};
  let noteMap = new Map();  // "tab|<url>" / "window|<browser>|<id>" → {note, tags}
  const loadNotes = async () => { try { noteMap = new Map((await api("/notes")).notes.map(n => [n.kind + "|" + n.key, n])); } catch { noteMap = new Map(); } };
  const editNote = (kind, key, label) => { const n = noteMap.get(kind + "|" + key) || {}; window.BF?.editNote(kind, key, label, n.note || "", n.tags || [], async () => { await loadNotes(); paint(); }); };
  // The open-tab records the TypeScript side closes: browser ids where the browser gives them.
  const toOpenTab = i => ({browser: i.x.browser, windowId: i.x.w.id ?? null, windowN: i.x.n, tabId: i.t.id ?? null, pos: i.pos, url: i.t.url, title: i.t.title, mode: i.x.inc ? "incognito" : "normal"});
  const closeItems = async items => { const list = items.filter(i => isWeb(i.t.url)).map(toOpenTab); if (list.length) await window.BF?.closeTabs(list); };
  // Incognito ⇄ normal: live tabs move (open in the other mode, then close here); saved ones can only be opened.
  const moveMode = (items, to) => window.BF?.moveToMode(items.filter(i => isWeb(i.t.url)).map(toOpenTab), to);
  // Tabs to bookmarks: pick where (a new folder in "Saved tabs", or inside any folder of yours), name the new
  // folder, save. "Move" then closes the tabs (a snapshot is saved before they close); "Save" leaves them open.
  const tabsToBookmarks = async (items, {suggested, close}) => {
    if (!wsUI()?.addLinks) return needServer(close ? "Moving tabs to bookmarks" : "Saving tabs as bookmarks");
    const links = items.filter(i => isWeb(i.t.url)).map(i => ({url: i.t.url, title: i.t.title || ""}));
    if (!links.length) return toast(close ? "No web links to move" : "No web links to save");
    const n = `${links.length} tab${links.length === 1 ? "" : "s"}`;
    const where = await chooseFolder({title: `${close ? "Move" : "Save"} ${n} to bookmarks: where?`, extra: [["saved", "A new folder in “Saved tabs”", "ti-folder-plus"]]});
    if (!where) return;
    const parent = where === "saved" ? "Saved tabs" : pathOf(where);
    const name = await askText({title: `${close ? "Move" : "Save"} ${n} to bookmarks`, label: `New folder inside “${nice(parent)}”`, value: suggested || n, ok: close ? "Save, then close" : "Save"});
    if (name == null) return;
    try { await wsUI().addLinks(name, links, {reload: !close, parent}); }
    catch (e) { return toast(String(e.message || e), "danger", close ? "Not saved, nothing closed" : "Not saved"); }
    if (close) { await closeItems(items); setTimeout(() => location.reload(), 900); }  // show the new folder in the tree
  };
  const moveToBookmarks = (items, suggested) => tabsToBookmarks(items, {suggested, close: true});
  const saveAsFolder = (items, suggested) => tabsToBookmarks(items, {suggested, close: false});
  const api = (p, opt) => fetch("api/tabs" + p, opt).then(r => r.ok ? r.json() : r.json().then(j => Promise.reject(new Error(j.error || r.status)), () => Promise.reject(new Error(r.status))));
  const post = (p, body) => tokenPost("api/tabs" + p, body);
  // when / ago, the summary counts, the list rows and the single live read: typed in src/web/ts/tabs.ts.
  const when = whenOf, ago = iso => agoOf(iso);
  async function refreshList() {
    try { snaps = (await api("/snapshots" + (SCOPE.query() ? "?" + SCOPE.query() : ""))).snapshots; const st = await api("/status"); cfg = st.settings; lastAuto = st.last_auto; stat = st; } catch (e) { snaps = []; cfg = null; }
    paintSettings(); paintTimeline(); paintNavSide();
  }
  const READ = new SingleRead(() => api(""), e => ({error: String(e.message || e), browsers: [], summary: null}));
  const scopedSummary = l => scopedSummaryOf(l, inScope);
  // Why a filtered Open now is empty, browser by browser, and the ways out: the browsers that do have windows, or all.
  function scopedEmpty(d) {
    const box = el("div", "empty py-4");
    box.append(el("div", "empty-icon"), el("p", "empty-title", `Nothing open in ${listNames(SCOPE.shown)}`));
    box.firstChild.append(icon("ti-world-off"));
    const ul = el("div", "d-inline-flex flex-column gap-1 text-start my-2");
    for (const n of SCOPE.shown) ul.append(el("div", "text-secondary", `${n}: ${browserState(d, n)}`));
    box.append(ul);
    const acts = el("div", "empty-action d-flex gap-2 justify-content-center flex-wrap");
    const withWindows = (d.browsers || []).filter(b => (b.windows || []).length && !inScope(b.browser)).map(b => b.browser);
    if (withWindows.length) { const b = el("button", "btn btn-primary"); b.append(icon("ti-eye", "me-1"), `Show ${listNames(withWindows)}`); b.onclick = () => { for (const n of withWindows) SCOPE.toggle(n, true); }; acts.append(b); }
    const all = el("button", "btn"); all.append(icon("ti-world", "me-1"), "Show all browsers"); all.onclick = () => SCOPE.setAll(true); acts.append(all);
    box.append(acts);
    return box;
  }
  function paintCount() {
    const s = live?.summary ? scopedSummary(live) : null;
    $("#navTabCount").textContent = s ? (s.tabs + s.incognito_tabs).toLocaleString() : "";
  }
  // The browser choice changed: every list here re-reads with it; the open view repaints at once.
  addEventListener("bf:scope", async () => {
    pageOf.clear(); paintCount();
    if (view_ === "tabs") await open(); else { await refreshList(); paintTimeline(); }
  });
  async function loadLive(maxAge = 0) {
    if (READ.fresh(maxAge)) return live;
    live = await READ.get(maxAge);  // one read at a time: a second caller waits for the first
    paintCount();
    if (!live.error) { LIVE_STATE = live; paintScope(); }
    return live;
  }
  function placeholder() {
    $("#tabsHead").replaceChildren(el("h3", "card-title mb-0", "Open now"), el("div", "text-secondary small", "Reading the browsers…"));
    const n = el("div", "pane-note"); n.append(el("span", "spinner-border spinner-border-sm"), el("span", null, "Asking your browsers which windows and tabs are open. The first time, macOS may ask you to allow this."));
    $("#tabsBody").replaceChildren(n); $("#tabsActs").replaceChildren();
  }
  async function open() {
    if (loading) return; loading = true;
    loadNotes();
    if (!$("#tabsBody").childElementCount || (cur === "live" && !READ.current?._got)) placeholder();
    try { await Promise.all([refreshList(), loadLive()]); await select(cur); } finally { loading = false; }
  }
  async function select(id) {
    if (id !== "live" && snaps.length && !snaps.some(x => x.id === id)) id = "live";  // a remembered snapshot since deleted
    if (id !== cur) pageOf.clear();
    cur = id; paintTimeline();
    if (!restoringPlace) PLACE.update({snapshot: id === "live" ? "" : id});
    if (id === "live") { if (!READ.fresh(5000)) placeholder(); data = await loadLive(5000); }
    else { try { data = await api("/snapshots/" + encodeURIComponent(id)); } catch (e) { data = {error: String(e.message || e), browsers: []}; } if (!live) await loadLive(); }
    paint();
  }
  function paintNavSide() {
    const s = $("#navSide"); s.replaceChildren();
    if (!cfg) return;
    const d = el("span", "nav-autosave" + (cfg.autosave ? " on" : ""));
    d.append(el("span", "status-dot " + (cfg.autosave ? "on" : "")), autosaveText(cfg));
    if (snaps[0]) d.append(el("span", "text-secondary ms-1", `· last ${ago(snaps[0].at)}`));
    d.dataset.tip = "Open tabs are saved on this computer so a crash never loses them. Change it in Open tabs.";
    d.onclick = () => setAppView("tabs");
    s.append(d);
  }
  // The Snapshots settings panel: src/web/ts/views/tabs-settings.ts (typed). paintSettings() keeps its name for callers.
  let settingsPanel = null;
  function paintSettings() {
    settingsPanel ??= new TabsSettingsPanel($("#tabsSettings"), {
      post, confirm: askConfirm, toast,
      saved: next => { cfg = next; paintNavSide(); },
      purged: async () => { await refreshList(); await select(cur); },
      clearHistory: () => window.BF?.clearHistory(),
    });
    settingsPanel.paint(cfg, stat);
  }
  function paintTimeline() {
    const t = $("#tabsTimeline"); t.replaceChildren();
    const row = (id, title, sub, ic) => { const a = el("button", "tl-item" + (id === cur ? " active" : "")); a.type = "button"; a.setAttribute("role", "option");
      a.append(icon(ic), el("span", "tl-text"), ); a.lastChild.append(el("span", "tl-title", title), el("span", "tl-sub", sub)); a.onclick = () => select(id); t.append(a); };
    const ls = live && live.summary && scopedSummary(live); row("live", "Open now", liveRowSub(ls || null), "ti-broadcast");
    let day = "";
    for (const s of snaps) {
      const r = snapshotRow(s);
      if (r.day !== day) { day = r.day; t.append(el("div", "tl-day", r.day)); }
      row(r.id, r.title, r.sub, r.icon);
    }
    if (!snaps.length) t.append(el("div", "text-secondary small p-3", SCOPE.isAll ? "No snapshots yet. They appear here as soon as one is saved."
      : SCOPE.isNone ? "No browsers chosen in the Browsers menu." : `No saved snapshot has a window from ${listNames(SCOPE.shown)}.`));
  }
  const openUrls = () => new Set((live?.browsers || []).flatMap(b => (b.windows || []).flatMap(w => (w.tabs || []).map(x => x.url))));
  // ---- view state: selection, collapsed windows, compact rows, order (kept in this browser) ----
  const pref = (k, d) => { try { const v = localStorage.getItem(STORE + ":tabs-" + k); return v == null ? d : JSON.parse(v); } catch { return d; } };
  const setPref = (k, v) => { try { localStorage.setItem(STORE + ":tabs-" + k, JSON.stringify(v)); } catch {} };
  const V = {incFirst: pref("incFirst", false), winSort: pref("winSort", "original"), tabSort: pref("tabSort", "original"), compact: pref("compact", false)};
  const pageOf = new Map();  // window key → page shown; a new search or snapshot starts every window at page 1
  const collapsed = new Set(pref("collapsed", [])), picked = new Set();
  const wkey = x => `${x.browser}|${x.n}`, tkey = (x, pos) => `${wkey(x)}|${pos}`;
  let shown = [];  // the window records currently drawn, for selection and menus

  // Everything in the selected moment, as flat window records, so it can be grouped any way.
  // Window records, tabs in a window, counts and sort rules: typed in src/web/ts/tabs.ts.
  const sortTabs = list => sortTabsBy(list, V.tabSort), sortWins = xs => sortWindows(xs, V.winSort), cnt = tabCount;

  // ---- menus: built on demand beside a button or at the pointer (menu.ts popMenu); items from views/tab-menus.ts ----
  const popMenu = (items, at, y) => _popMenu(items, at, y, hideTip);
  const closePop = closePopMenu;
  installPopMenus();

  // ---- opening: some or all, grouped (one window per original window) or not (one window), normal or incognito ----
  // Incognito tabs never land in a normal window: "not grouped" still keeps them apart from the normal ones.
  // crossMode: the caller asked for this exact mode, so incognito tabs may open in a normal window (Open in a normal
  // window). Without it, incognito tabs never land in a normal window by accident.
  async function openSet(items, how, grouped, {crossMode = false} = {}) {
    if (!items.some(i => isWeb(i.t.url))) return toast("No web links to open");
    // Which windows and modes: planOpen in src/web/ts/tabs.ts (typed, tested).
    const {batches: real, total, skipped} = planOpen(items, how, grouped, crossMode);
    if (!real.length) return toast("Incognito tabs are not opened as tabs in a normal window.", "warning", "Nothing opened");
    if (total > SET.openWarn && !(await askConfirm({title: `Open ${total} tabs?`, ok: `Open ${total}`, danger: false,
      body: how === "tabs" ? `They open as tabs in your current ${BROWSER_NAME} window.` : `They open in ${real.length} new ${BROWSER_NAME} window${real.length === 1 ? "" : "s"}` + (real.some(b => b.how === "incognito") ? ", incognito ones in incognito windows." : ".")}))) return;
    // Each batch opens in every browser chosen under Browsers › opens links, all at the same time (the server's job).
    let opened = 0; const used = new Set(), failed = new Map();
    for (const b of real) { try { const r = await tokenPost("api/open", {urls: b.urls, how: b.how}); opened += r.opened || b.urls.length;
      for (const n of r.browsers || [r.browser]) if (n) used.add(n); for (const f of r.failed || []) failed.set(f.browser, f.error); }
      catch (e) { return toast(String(e.message || e), "danger", "Could not open them"); } }
    const where = used.size > 1 ? ` in ${[...used].join(", ")}` : "";
    toast(`${opened} tab${opened === 1 ? "" : "s"} opened` + (how === "tabs" ? " in your current window" : ` in ${real.length} window${real.length === 1 ? "" : "s"}`) + where + (skipped ? `; ${skipped} incognito left out` : ""), "success");
    if (failed.size) toast([...failed].map(([n, e]) => `${n}: ${e}`).join(" · "), "warning", "Some browsers did not open them");
  }
  // What each menu offers: src/web/ts/views/tab-menus.ts. ACTS is what they can do here.
  const ACTS = {
    open: (items, how, grouped, opts) => openSet(items, how, grouped, opts), openOne: u => openTab(u),
    moveMode: (items, to) => moveMode(items, to), copy: items => copyItems(items),
    saveAsFolder: (items, s) => saveAsFolder(items, s), moveToBookmarks: (items, s) => moveToBookmarks(items, s),
    close: items => closeItems(items), select: (items, on) => toggle(items.map(i => tkey(i.x, i.pos)), on),
  };
  const openItems = (items, label) => openMenuItems(items, label, ACTS);
  const modeItems = (items, isLive) => modeMenuItems(items, isLive, ACTS);
  const copyItems = items => { const u = items.map(i => i.t.url).filter(Boolean); navigator.clipboard?.writeText(u.join("\n")).then(() => toast(`Copied ${u.length} link${u.length === 1 ? "" : "s"}`, "success")); };

  // ---- selection ----
  const pickedItems = () => shown.flatMap(x => tabsIn(x).filter(i => picked.has(tkey(x, i.pos))));
  function toggle(keys, on) { for (const k of keys) on ? picked.add(k) : picked.delete(k); paint(); }
  function selBar() {
    const items = pickedItems(); if (!items.length) return null;
    const bar = el("div", "tabs-selbar");
    const inc = items.filter(i => i.x.inc).length;
    bar.append(el("b", null, `${items.length} selected`), inc ? el("span", "badge tabs-inc-pill", `${inc} incognito`) : "");
    const open = el("div", "dropdown"), ob = el("button", "btn btn-sm btn-primary"); ob.setAttribute("data-dd", ""); ob.append(icon("ti-external-link", "me-1"), "Open", icon("ti-chevron-down", "ms-1"));
    open.append(ob, buildMenu(openItems(items, `Open ${items.length} tab${items.length === 1 ? "" : "s"}`)));
    const cp = el("button", "btn btn-sm"); cp.append(icon("ti-copy", "me-1"), "Copy links"); cp.onclick = () => copyItems(items);
    const ex = el("button", "btn btn-sm"); ex.append(icon("ti-bookmark-plus", "me-1"), "Save as bookmarks"); ex.onclick = () => exportWindow({tabs: items.map(i => i.t)}, `${items.length} selected tabs`);
    const cl = el("button", "btn btn-sm btn-ghost-secondary"); cl.append(icon("ti-x", "me-1"), "Clear"); cl.onclick = () => toggle([...picked], false);
    const sv = el("button", "btn btn-sm"); sv.append(icon("ti-folder-plus", "me-1"), "Save as bookmark folder"); sv.onclick = () => saveAsFolder(items, `${items.length} tabs`);
    const extra = [sv];
    const modes = modeItems(items, cur === "live").filter(Boolean);
    if (modes.length) { const md = el("div", "dropdown"), mb = el("button", "btn btn-sm"); mb.setAttribute("data-dd", ""); mb.append(icon("ti-switch-horizontal", "me-1"), "Incognito ⇄ normal", icon("ti-chevron-down", "ms-1"));
      md.append(mb, buildMenu([{h: `${items.length} selected`}, ...modes])); extra.push(md); }
    if (cur === "live") { const mv = el("button", "btn btn-sm"); mv.append(icon("ti-arrow-bar-to-right", "me-1"), "Move to bookmarks"); mv.dataset.tip = "Save them as a bookmark folder, then close the tabs"; mv.onclick = () => moveToBookmarks(items); extra.push(mv); }
    if (cur === "live") { const cb = el("button", "btn btn-sm btn-danger"); cb.append(icon("ti-square-x", "me-1"), "Close tabs"); cb.onclick = async () => { await closeItems(items); toggle([...picked], false); }; extra.push(cb); }
    bar.append(el("span", "ms-auto"), open, ...extra, cp, ex, cl);
    return bar;
  }

  function paint() {
    const body = $("#tabsBody"), head = $("#tabsHead"), acts = $("#tabsActs"); body.replaceChildren(); head.replaceChildren(); acts.replaceChildren(); closePop();
    body.classList.toggle("tabs-compact", !!V.compact); body.classList.toggle("tabs-has-pick", picked.size > 0);
    const isLive = cur === "live", q = $("#tabsQ").value.trim().toLowerCase(), group = $("#tabsGroup").value;
    head.append(el("h3", "card-title mb-0", isLive ? "Open now" : `Saved ${when(data.at)}`),
      el("div", "text-secondary small", isLive ? "Read from the browsers just now. Nothing is changed in them." : `${ago(data.at)} · ${data.reason === "manual" ? "saved by you" : "saved automatically"}`));
    if (data.error) { body.append(el("div", "pane-note", data.error)); return; }
    if (SCOPE.isNone) { body.append(noneChosen("tabs")); return; }
    for (const b of data.browsers || []) if (b.error) { const n = el("div", "pane-note mb-3"); n.append(icon("ti-lock"), el("span", null, `${b.browser}: ${b.error === "needs-permission" ? "allow this tool to read its tabs." : b.error}`), b.hint ? el("span", "text-secondary small ms-2", b.hint) : ""); body.append(n); }
    const now = openUrls(), every = windowsOf(data), wins = every.filter(x => inScope(x.browser)), missingAll = [];
    const hidden = every.length - wins.length;
    const match = t => !q || (t.title + " " + t.url).toLowerCase().includes(q);
    for (const x of wins) { x.missing = isLive ? [] : (x.w.tabs || []).filter(t => /^https?:/i.test(t.url) && !now.has(t.url)); if (x.missing.length) missingAll.push(x); }
    shown = wins; queueMicrotask(() => FOLD?.refresh());
    const norm = sortWins(wins.filter(x => !x.inc)), inc = sortWins(wins.filter(x => x.inc)), count = xs => xs.reduce((n, x) => n + cnt(x), 0);
    // Summary strip: what is in this moment, normal and incognito apart.
    const strip = el("div", "tabs-strip");
    const chip = (ic, label, val, cls = "") => { const c = el("div", "tabs-chip " + cls); c.append(icon(ic), el("span", "text-secondary", label), el("b", null, val)); strip.append(c); };
    chip("ti-app-window", "Windows", `${norm.length} · ${count(norm)} tabs`);
    chip("ti-spy", "Incognito", `${inc.length} · ${count(inc)} tabs`, inc.length ? "inc" : "");
    chip("ti-world", "Browsers", [...new Set(wins.map(x => x.browser))].join(", ") || "none");
    if (hidden) chip("ti-filter", "Hidden by Show tabs from", `${hidden} window${hidden === 1 ? "" : "s"}`, "warn");
    const nMissing = missingAll.reduce((n, x) => n + x.missing.length, 0);
    if (!isLive) chip("ti-alert-triangle", "Not open now", nMissing.toLocaleString(), nMissing ? "warn" : "");
    body.append(strip);
    const bar = selBar(); if (bar) body.append(bar);
    const sections = [["Windows", norm, false], ["Incognito", inc, true]];
    if (V.incFirst) sections.reverse();
    for (const [title, xs, isInc] of sections) {
      if (!xs.length) continue;
      const sec = el("section", "tabs-sec" + (isInc ? " inc" : ""));
      const h = el("div", "tabs-sec-head"), all = xs.flatMap(tabsIn);
      const ck = checkbox(all.map(i => tkey(i.x, i.pos)));
      h.append(ck, el("span", isInc ? "tabs-inc-pill" : "tabs-norm-pill"), el("span", null, title), el("span", "text-secondary", `${xs.length} window${xs.length === 1 ? "" : "s"} · ${count(xs)} tabs`));
      h.children[1].append(icon(isInc ? "ti-spy" : "ti-app-window"));
      const more = el("button", "btn btn-sm btn-ghost-secondary btn-icon ms-auto"); more.append(icon("ti-dots")); more.dataset.tip = "Open, select or fold this section";
      const foldAll = on => { xs.forEach(x => on ? collapsed.add(wkey(x)) : collapsed.delete(wkey(x))); setPref("collapsed", [...collapsed]); paint(); };
      const secMenu = e => { e.preventDefault(); popMenu(sectionMenuItems(all, title, foldAll, ACTS),
        e.type === "contextmenu" ? e.clientX : more, e.type === "contextmenu" ? e.clientY : undefined); };
      more.onclick = secMenu; h.oncontextmenu = secMenu; h.append(more); sec.append(h);
      if (group === "site") paintBySite(sec, xs, match, isLive);
      else if (group === "browser") { for (const br of [...new Set(xs.map(x => x.browser))]) { sec.append(el("div", "tabs-sub-head", br)); for (const x of xs.filter(y => y.browser === br)) sec.append(windowCard(x, match, isLive)); } }
      else for (const x of xs) sec.append(windowCard(x, match, isLive));
      body.append(sec);
    }
    if (!wins.length) body.append(hidden && isLive ? scopedEmpty(data) : el("div", "pane-note", isLive ? "No browser windows are open, or none could be read." : hidden ? `This snapshot has no windows from ${listNames(SCOPE.shown)}.` : "This snapshot has no windows."));
    // Header actions: reopen what is missing, view options, read again.
    if (!isLive && nMissing) {
      const b = el("button", "btn btn-sm btn-warning"); b.append(icon("ti-restore", "me-1"), `Reopen ${nMissing} missing`);
      b.onclick = () => openSet(missingAll.flatMap(x => x.missing.map(t => ({t, x, pos: (x.w.tabs || []).indexOf(t)}))), "window", true); acts.append(b);
    }
    if (isLive) { const cu = el("button", "btn btn-sm"); cu.append(icon("ti-wand", "me-1"), "Clean up"); cu.dataset.tip = "Find tabs open twice, untouched for days, already bookmarked, or incognito, and close them"; cu.onclick = () => window.BF?.cleanup(); acts.append(cu); }
    else if (wins.some(x => (x.w.tabs || []).length)) {
      const rs = el("button", "btn btn-sm btn-primary"); rs.append(icon("ti-history", "me-1"), "Restore session");
      rs.dataset.tip = "Reopen every window of this snapshot, each as it was (incognito ones in incognito windows)";
      rs.onclick = () => openSet(wins.flatMap(tabsIn), "window", true); acts.append(rs);
    }
    acts.append(viewMenu());
    const rf = el("button", "btn btn-sm btn-icon"); rf.append(icon("ti-refresh")); rf.dataset.tip = "Read again"; rf.onclick = () => select(cur); acts.append(rf);
  }
  function viewMenu() {
    return menuButtonEl("View", "ti-adjustments-horizontal", viewMenuItems(V, (k, v) => { V[k] = v; setPref(k, v); paint(); },
      on => { if (on) shown.forEach(x => collapsed.add(wkey(x))); else collapsed.clear(); setPref("collapsed", [...collapsed]); paint(); }));
  }
  function checkbox(keys) {
    const c = el("input", "form-check-input m-0 tabs-check"); c.type = "checkbox";
    const n = keys.filter(k => picked.has(k)).length; c.checked = n > 0 && n === keys.length; c.indeterminate = n > 0 && n < keys.length;
    c.onclick = e => { e.stopPropagation(); toggle(keys, c.checked); }; c.setAttribute("aria-label", "Select");
    return c;
  }
  function tabRow(i, isLive, extra) {
    const {t, x, pos} = i, k = tkey(x, pos);
    const hst = host(t.url) || t.url.split(":")[0];
    // The row's markup: src/web/ts/views/tab-cards.ts. Selection and the menu stay here.
    const r = tabRowElement(i, {live: isLive, picked: picked.has(k), checkbox: checkbox([k]), note: noteMap.get("tab|" + t.url), extra},
      () => x.inc ? openSet([i], "incognito", false) : openTab(t.url), CARD_DEPS);
    r.onclick = e => { if (e.target.closest("button, input, a")) return; toggle([k], !picked.has(k)); };
    r.oncontextmenu = e => { e.preventDefault();
      const sel = picked.has(k) && picked.size > 1 ? pickedItems() : [i];
      popMenu(tabMenuItems(i, sel, {live: isLive, picked: picked.has(k), title: t.title || hst, window: tabsIn(x),
        note: () => editNote("tab", t.url, t.title || hst)}, ACTS), e.clientX, e.clientY); };
    return r;
  }
  function windowCard(x, match, isLive) {
    const {w, inc, missing} = x, list = sortTabs(tabsIn(x)).filter(i => match(i.t)), k = wkey(x), folded = collapsed.has(k);
    const all = tabsIn(x);
    // The card and its header: src/web/ts/views/tab-cards.ts. The menus and actions stay here.
    const menu = (e, more) => { e.preventDefault();
      popMenu(windowMenuItems(all, {live: isLive, title: `${x.browser} · window ${x.n}`, name: `${x.browser} window ${x.n}`, folded, fold: foldIt,
        saveFile: () => exportWindow(w, `${x.browser} window ${x.n}`),
        note: w.id != null ? () => editNote("window", `${x.browser}|${w.id}`, `${x.browser} window ${x.n}`) : undefined}, ACTS),
        e.type === "contextmenu" ? e.clientX : more, e.type === "contextmenu" ? e.clientY : undefined); };
    const foldIt = () => { folded ? collapsed.delete(k) : collapsed.add(k); setPref("collapsed", [...collapsed]); paint(); };
    const card = windowCardOf(x, {folded, checkbox: checkbox(all.map(i => tkey(x, i.pos))), count: cnt(x), missing}, {
      fold: foldIt,
      reopenMissing: () => openSet(missing.map(t => ({t, x, pos: w.tabs.indexOf(t)})), inc ? "incognito" : "window", false),
      openAll: ob => popMenu(openItems(all, `Open ${all.length} tabs`), ob),
      menu,
    });
    if (folded) return card;
    if (w.redacted) { card.append(redactedNote(w.tab_count)); return card; }
    // One page of tabs at a time; a page or size change redraws this card only.
    const pager = new Pager({size: listSize(k), noun: "tabs", onChange: p => {
      if (p.pageSize !== listSize(k)) setListSize(k, p.pageSize);
      pageOf.set(k, p.page); const next = windowCard(x, match, isLive); card.replaceWith(next);
      next.scrollIntoView({block: "nearest"}); }}).total(list.length).goto(pageOf.get(k) || 1);
    const ul = el("div", "list-group list-group-flush tab-list");
    pager.slice(list).forEach(i => { const r = tabRow(i, isLive); if (w.active === i.pos + 1) r.classList.add("active-tab"); ul.append(r); });
    if (!list.length) ul.append(el("div", "list-group-item text-secondary small", "No tab here matches the filter."));
    card.append(ul);
    if (pager.needed) card.append(pager.render());
    return card;
  }
  function paintBySite(sec, xs, match, isLive) {
    const by = new Map();
    for (const x of xs) for (const i of tabsIn(x).filter(i => match(i.t))) { const h = host(i.t.url) || "(not a web page)"; (by.get(h) || by.set(h, []).get(h)).push(i); }
    const redacted = xs.filter(x => x.w.redacted).reduce((n, x) => n + (x.w.tab_count || 0), 0);
    if (redacted) { const n = el("div", "pane-note mb-2"); n.append(icon("ti-eye-off"), el("span", null, `${redacted} incognito tabs whose pages are not kept`)); sec.append(n); }
    for (const [h, items] of [...by.entries()].sort((a, b) => b[1].length - a[1].length || a[0].localeCompare(b[0]))) {
      const card = el("div", "card tab-win" + (items[0].x.inc ? " incognito" : "")), hd = el("div", "card-header tab-win-head");
      const fv = el("span", "fvd", (h[0] || "?").toUpperCase()); fv.style.background = `hsl(${hue(h)} 45% 45%)`;
      const t = el("div", "min-w-0 flex-fill"); t.append(el("div", "tab-win-title", h), el("div", "tab-win-meta", `${items.length} tab${items.length === 1 ? "" : "s"} · ${new Set(items.map(i => wkey(i.x))).size} window(s)`));
      const ob = el("button", "btn btn-sm"); ob.append(icon("ti-external-link", "me-1"), "Open", icon("ti-chevron-down", "ms-1")); ob.onclick = () => popMenu(openItems(items, `${h}: ${items.length} tabs`), ob);
      hd.append(checkbox(items.map(i => tkey(i.x, i.pos))), fv, t, ob); hd.oncontextmenu = e => { e.preventDefault(); popMenu(openItems(items, `${h}: ${items.length} tabs`), e.clientX, e.clientY); };
      card.append(hd);
      const ul = el("div", "list-group list-group-flush tab-list");
      for (const i of sortTabs(items)) ul.append(tabRow(i, isLive, `${i.x.browser.split(" ").pop()} ${i.x.n}`));
      card.append(ul); sec.append(card);
    }
  }
  async function reopen(urls, incognito, confirmed = false) {
    urls = urls.filter(u => /^https?:/i.test(u)); if (!urls.length) return;
    if (!confirmed && urls.length > SET.openWarn && !(await askConfirm({title: `Open ${urls.length} tabs?`, ok: `Open ${urls.length}`, danger: false, body: `They open in a new ${BROWSER_NAME} ${incognito ? "incognito " : ""}window.`}))) return;
    try { const r = await tokenPost("api/open", {urls, how: incognito ? "incognito" : "window"}); toast(`Opened ${r.opened || urls.length} in a new ${incognito ? "incognito " : ""}window`, "success"); }
    catch (e) { toast(String(e.message || e), "danger", "Could not open them"); }
  }
  function exportWindow(w, name) {
    const e = s => String(s).replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
    const t = Math.floor(Date.now() / 1000);
    const lines = ["<!DOCTYPE NETSCAPE-Bookmark-file-1>", '<META HTTP-EQUIV="Content-Type" CONTENT="text/html; charset=UTF-8">', "<TITLE>Bookmarks</TITLE>", "<H1>Bookmarks</H1>", "<DL><p>",
      `    <DT><H3 ADD_DATE="${t}">${e(name)} (${new Date().toLocaleString()})</H3>`, "    <DL><p>",
      ...(w.tabs || []).filter(x => /^https?:/i.test(x.url)).map(x => `        <DT><A HREF="${e(x.url)}" ADD_DATE="${t}">${e(x.title || x.url)}</A>`), "    </DL><p>", "</DL><p>"];
    const a = el("a"); a.href = URL.createObjectURL(new Blob([lines.join("\n")], {type: "text/html"})); a.download = `tabs-${name.replace(/[^a-z0-9]+/gi, "-").toLowerCase()}.html`; a.click();
    setTimeout(() => URL.revokeObjectURL(a.href), 2000);
    toast("Import it in any browser: Bookmarks › Import.", "success", "Window saved as bookmarks");
  }
  async function snapNow() {
    const b = $("#tabsSnap"); b.disabled = true;
    try { const r = await post("/snapshot", {}); toast(r.stored ? "Every open window and tab is saved." : r.why, r.stored ? "success" : "info", r.stored ? "Snapshot saved" : "Nothing new to save"); await refreshList(); if (r.id) await select(r.id); }
    catch (e) { toast(String(e.message || e), "danger", "Could not save"); } finally { b.disabled = false; }
  }
  $("#tabsSnap").onclick = snapNow;
  $("#tabsQ").oninput = () => { pageOf.clear(); paint(); };
  try { $("#tabsGroup").value = localStorage.getItem(STORE + ":tabs-group") || "window"; } catch {}
  $("#tabsGroup").onchange = () => { try { localStorage.setItem(STORE + ":tabs-group", $("#tabsGroup").value); } catch {} paint(); };
  // Keep the header count and the autosave line current even while the bookmarks view is showing.
  const tick = async () => { try { const st = await api("/status"); cfg = st.settings; lastAuto = st.last_auto; snaps = (await api("/snapshots" + (SCOPE.query() ? "?" + SCOPE.query() : ""))).snapshots; paintNavSide(); if (view_ === "tabs") paintTimeline(); } catch {} setTimeout(tick, 60000); };
  setTimeout(() => { tick(); loadLive(); }, 1500);
  // Background refresh: every Settings › Refresh from the browsers ms (while this page is visible), read the
  // browsers again; when anything changed (a tab opened, closed, navigated, a window came or went) the open-tabs
  // count updates and, if you are looking at "Open now", the list repaints where you were. Unchanged: nothing moves.
  const signature = l => JSON.stringify((l?.browsers || []).map(b => [b.browser, b.error || "", (b.windows || []).map(w => [w.id, w.mode, w.active, (w.tabs || []).map(t => t.url + "\u0001" + t.title)])]));
  let lastSig = "";
  const LIVE = new Poller(async () => {
    if (loading) return;
    const before = lastSig;
    await loadLive(0);
    lastSig = signature(live);
    if (before && lastSig !== before) {
      window.dispatchEvent(new CustomEvent("bf:tabs-changed"));
      if (view_ === "tabs" && cur === "live" && !document.querySelector(".pop-menu.show, .modal.show[style*='block']")) {
        const pane = $("#tabsBody").closest(".pane-scroll"), top = pane?.scrollTop || 0;
        data = live; paint(); if (pane) pane.scrollTop = top;
      }
    }
  }).every(SET.liveMs).start();
  addEventListener("bf:settings", () => LIVE.every(SET.liveMs));
  // The snapshot to show when the view next opens (a restored place); select() falls back to live if it is gone.
  const prefer = id => { cur = id; };
  // For the header's Expand / collapse all (ts/fold.ts): the windows drawn now.
  const fold = {
    anyOpen: () => shown.length ? shown.some(x => !collapsed.has(wkey(x))) : null,
    expand: () => { shown.forEach(x => collapsed.delete(wkey(x))); setPref("collapsed", [...collapsed]); paint(); },
    collapse: () => { shown.forEach(x => collapsed.add(wkey(x))); setPref("collapsed", [...collapsed]); paint(); },
  };
  return {open, snapNow, select, prefer, fold};
})();
// ---------- timeline: tab snapshots, edits and bookmarks added, as one feed and two charts ----------
// The Timeline view lives in src/web/ts/views/timeline.ts (typed); this hands it what it needs from the page.
const TL = new TimelineView({
  scope: () => SCOPE, bookmarks: () => DATA.keep, titleOf, host, shortUrl,
  workspaceId: () => (WS ? window.BM_WS.id : null),
  pageSize: () => listSize("timeline"), setPageSize: n => setListSize("timeline", n),
  showSnapshot: id => { setAppView("tabs"); TABS.select(id); },
  showAddedBetween: (from, to) => { setAppView("bookmarks"); setDates(from, to); },
  showHistory: () => IO.history(),
  noneChosen: () => noneChosen("timeline"),
  isOpen: () => view_ === "timeline",
  storeKey: STORE + ":tl-days",
});

// The bridge the TypeScript modules (assets/js/app.js) use to reach this script. Keep it small and typed there
// (src/web/ts/legacy.ts): when a piece moves to TypeScript, it comes out of here.
window.BF_LEGACY = {
  data: DATA, titleOf, setAppView,
  selectSnapshot: id => TABS.select(id),
  refreshTabs: () => TABS.open(),
  openWindows: async batches => { for (const b of batches) await tokenPost("api/open", {urls: b.urls.filter(isWeb), how: b.incognito ? "incognito" : "window", ...(b.browser ? {browser: b.browser} : {})}); },
};
// POST with the run's token, as every write to the local server must.
async function tokenPost(url, body) {
  const tok = (window.BM_WS && window.BM_WS.token) || (await fetch("api/token").then(r => r.ok ? r.json() : {}).catch(() => ({}))).token || "";
  const r = await fetch(url, {method: "POST", headers: {"Content-Type": "application/json", "X-BM-Token": tok}, body: JSON.stringify(body)});
  const j = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(j.error || `server answered ${r.status}`);
  return j;
}
on("#tourBtn", () => tour(0));
// ---------- import, export, history, workspaces: one place for everything that moves bookmarks in or out ----------
const wsUI = () => (WS && WS.ui) || (window.BM_WS && window.BM_WS.ui) || null;
const needServer = what => toast(`${what} needs the Browser Foreman server. Start it with: browser-foreman serve`, "warning", "Not available in this copy");
const IO = {
  importer: () => wsUI() ? wsUI().importer() : needServer("Importing"),
  exportHtml: () => wsUI()?.download ? wsUI().download("html") : wsUI() ? wsDownload("html") : downloadFile(),
  exportChrome: () => wsUI()?.download ? wsUI().download("chrome") : wsUI() ? wsDownload("chrome") : needServer("Chrome JSON export"),
  duplicates: () => wsUI()?.duplicatesCsv ? wsUI().duplicatesCsv() : wsUI() ? (location.href = `api/ws/${encodeURIComponent(window.BM_WS.id)}/duplicates.csv`) : needServer("The duplicates list"),
  history: () => wsUI() ? wsUI().history() : needServer("History"),
  workspaces: () => wsUI() ? wsUI().manage() : needServer("Workspaces"),
};
// The workspace layer owns its download; ask it through its own menu entry so its toast and format stay one implementation.
function wsDownload(fmt) {
  const want = fmt === "html" ? "Export as HTML" : "Export as Chrome JSON";
  const a = [...document.querySelectorAll("#wsMenu .dropdown-item")].find(x => x.textContent.includes(want));
  if (a) a.click(); else downloadFile();
}
function paintIoMenu() {
  const m = $("#ioMenu"); m.replaceChildren();
  const ws = !!wsUI();
  const item = (ic, label, fn, sub) => { const a = el("a", "dropdown-item"); a.href = "#"; a.append(icon(ic, "me-2"));
    const t = el("div", "min-w-0"); t.append(el("div", null, label)); if (sub) t.append(el("div", "text-secondary small", sub)); a.append(t);
    a.onclick = e => { e.preventDefault(); closeDropdowns(); fn(); }; m.append(a); };
  m.append(el("h6", "dropdown-header", "Bring bookmarks in"));
  item("ti-file-import", "Import bookmarks…", IO.importer, "An HTML export from any browser, or a browser's own bookmarks file");
  if (wsUI()?.addFiles) item("ti-file-plus", "Add to this workspace…", () => wsUI().addFiles(), "Merge more files in; copies of a page you have go to the Trash");
  if (wsUI()?.browserHistory) item("ti-history-toggle", "From browser history…", () => wsUI().browserHistory(), "Pages you visited but never bookmarked");
  m.append(el("div", "dropdown-divider"), el("h6", "dropdown-header", "Take them out"));
  item("ti-download", "Import file for any browser (HTML)", IO.exportHtml, "Chrome, Edge, Firefox, Safari: Bookmarks › Import");
  item("ti-braces", "Chrome bookmarks JSON", IO.exportChrome, "A copy in Chrome's own format, for tools and backups");
  item("ti-file-spreadsheet", "Merged duplicates (CSV)", IO.duplicates, "Every copy that was merged, and which one was kept");
  m.append(el("div", "dropdown-divider"), el("h6", "dropdown-header", "Progress"));
  item("ti-history", "History and progress…", IO.history, "Every change, with roll back to any point");
  item("ti-stack-2", "Workspaces…", IO.workspaces, "Separate bookmark sets, each with its own history");
  m.append(el("div", "dropdown-divider"), el("h6", "dropdown-header", "Backup"));
  item("ti-file-zip", "Back up everything…", () => wsUI()?.backup ? wsUI().backup() : needServer("Backup"), "Workspaces, tab history and settings in one file");
  item("ti-restore", "Restore from a backup…", () => wsUI()?.restore ? wsUI().restore() : needServer("Restore"), "Your current state is kept aside first");
  // MCP access (Claude) is in Settings › Assistants.
  if (!ws) m.append(el("div", "dropdown-item-text text-secondary small px-3 pb-2", "Import, history and workspaces need the Browser Foreman server (browser-foreman serve)."));
}
setTimeout(paintIoMenu, 0);
// ---------- footer: what this is, whether the local server is answering, where edits are kept, and the links ----------
function paintFooter() {
  $("#navBmCount").textContent = liveItems().length.toLocaleString();  // the Trash is counted on its own button
  $("#footName").textContent = APP.name; $("#footVer").textContent = "v" + APP.version;
  $("#footCopy").textContent = `© ${APP.year} ${APP.org} · MIT`;
  $("#footBuilt").textContent = DATA.built ? `Built ${fmtDate(Date.parse(DATA.built) / 1000)}` : "";
  $("#footBuilt").dataset.tip = `${DATA.keep.length.toLocaleString()} bookmarks in this build, from ${DATA.total.toLocaleString()}`;

}
for (const a of document.querySelectorAll(".foot-links [data-about]")) a.onclick = e => { e.preventDefault(); openAbout(a.dataset.about); };
on("#footKeys", e => { e?.preventDefault?.(); openShortcuts(); });
let serverUp = null;
async function pingServer() {
  let up = false; try { up = (await fetch("api/v1/health", {cache: "no-store"})).ok; } catch {}
  if (up !== serverUp) {
    serverUp = up; const f = $("#footServer");
    f.querySelector(".status-dot").className = "status-dot " + (up ? "on" : "off");
    f.lastElementChild.textContent = up ? "Local server running" : "Local server off";
    f.dataset.tip = up ? "Refresh, opening links in your browsers and saving work. It answers only this computer." : "Start browser-foreman serve to check links, open them in your browsers and save your changes. Browsing works without it.";
  }
  setTimeout(pingServer, up ? 30000 : 10000);
}
setTimeout(() => { paintFooter(); pingServer(); }, 0);  // after the whole script has run (APP is defined further down)
on("#helpBtn", () => openAbout("help"));
// ---------- About, help, privacy and licences ----------
const APP = {name: "Browser Foreman", version: "0.1.0", year: 2026, licence: "MIT",
  tagline: "Clean bookmarks, safe tabs.",
  description: "Keeps what your browser should never lose. For bookmarks: finds duplicates, checks every link, separates dead pages from dead sites, sorts them into one structure built on your own folders, and writes a standard file your browser imports. For tabs: saves every open window and tab, incognito included if you choose, so after a crash you can see what was open and reopen it. Everything runs on this computer.",
  company: "Simtabi LLC", site: "https://simtabi.com", oss: "https://opensource.simtabi.com", github: "https://github.com/simtabi",
  maintainer: "Imani Manyara", maintainerEmail: "imani@simtabi.com", contact: "opensource@simtabi.com", security: "security@simtabi.com",
  author: "Imani Manyara", org: "Simtabi LLC"};
const CREDITS = [
  ["Go", "1.26", "BSD-3-Clause", "https://go.dev", "The language the server is written in"],
  ["SQLite (modernc.org/sqlite)", "1.62.0", "BSD-3-Clause", "https://gitlab.com/cznic/sqlite", "Stores workspaces, history and tab snapshots"],
  ["MCP Go SDK", "1.8.0", "Apache-2.0 and MIT", "https://github.com/modelcontextprotocol/go-sdk", "Serves bookmarks to AI assistants"],
  ["Cobra", "1.10.2", "Apache-2.0", "https://cobra.dev", "The command line"],
  ["howett.net/plist", "1.0.1", "BSD-3-Clause", "https://howett.net/plist", "Reads Safari's bookmarks"],
  ["golang.org/x/net", "0.62.0", "BSD-3-Clause", "https://pkg.go.dev/golang.org/x/net", "Reads exported bookmarks files"],
  ["Tabler", "1.7.3", "MIT", "https://tabler.io", "Interface components and styles"],
  ["Tabler Icons", "3.50.0", "MIT", "https://tabler.io/icons", "Icons"],
  ["Alpine.js", "3.17", "MIT", "https://alpinejs.dev", "Parts of the interface"],
  ["Google Chrome", "installed copy", "Google's terms", "https://www.google.com/chrome/", "Takes page snapshots, headless, in a throwaway profile with no sign-ins"],
];
const MIT_TEXT = `MIT License

Copyright (c) ${APP.year} ${APP.org}

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.`;
function openAbout(tab = "about") {
  const tabs = [["about", "About", "ti-info-circle"], ["help", "Help", "ti-help-circle"], ["privacy", "Privacy & safety", "ti-shield-check"], ["legal", "Licences", "ti-license"]];
  const nav = $("#aboutTabs"); nav.replaceChildren();
  $("#aboutName").textContent = APP.name; $("#aboutVer").textContent = "v" + APP.version; $("#aboutTag").textContent = APP.tagline;
  const paint = t => {
    for (const a of nav.querySelectorAll("button")) { a.classList.toggle("active", a.dataset.t === t); a.setAttribute("aria-selected", a.dataset.t === t); }
    const b = $("#aboutBody"); b.replaceChildren(); b.scrollTop = 0;
    const p = (txt, cls = "") => b.appendChild(el("p", cls, txt));
    const h = txt => b.appendChild(el("h4", "about-h", txt));
    const dl = pairs => { const g = el("div", "datagrid"); for (const [k, v] of pairs) { const it = el("div", "datagrid-item"); it.append(el("div", "datagrid-title", k)); const c = el("div", "datagrid-content"); c.append(v); it.append(c); g.append(it); } b.append(g); };
    const link = (href, text) => { const a = el("a", null, text || href); a.href = href; a.target = "_blank"; a.rel = "noopener noreferrer"; return a; };
    const mail = addr => { const a = el("a", null, addr); a.href = "mailto:" + addr; return a; };
    if (t === "about") {
      p(APP.description, "about-lead");
      // Facts about this build, as three even tiles.
      const tiles = el("div", "about-tiles");
      const tile = (label, value, sub) => { const d = el("div", "about-tile"); d.append(el("div", "about-tile-label", label), el("div", "about-tile-value", value)); if (sub) d.append(el("div", "about-tile-sub", sub)); tiles.append(d); };
      tile("Version", APP.version, "first release");
      tile("Bookmarks", DATA.keep.length.toLocaleString(), `from ${DATA.total.toLocaleString()} before cleanup`);
      tile("Built", DATA.built ? fmtDay(Date.parse(DATA.built) / 1000) : "—", DATA.built ? new Date(DATA.built).toLocaleTimeString(undefined, {hour: "numeric", minute: "2-digit"}) : "");
      b.append(tiles);
      // Who makes it.
      const maker = el("div", "about-card");
      const mh = el("div", "about-maker"), mlogo = el("span", "about-maker-logo", "S");
      const mt = el("div", "min-w-0"); mt.append(el("div", "about-maker-name", APP.company), el("div", "text-secondary small", "Designs, builds and maintains this tool"));
      const ml = el("div", "about-maker-links ms-auto");
      for (const [href, ic, label] of [[APP.site, "ti-world", "simtabi.com"], [APP.oss, "ti-code", "Open source"], [APP.github, "ti-brand-github", "GitHub"]]) {
        const a = el("a", "btn btn-sm"); a.href = href; a.target = "_blank"; a.rel = "noopener noreferrer"; a.append(icon(ic, "me-1"), label); ml.append(a);
      }
      mh.append(mlogo, mt, ml); maker.append(mh);
      const rows = el("div", "about-contacts");
      const contact = (ic, label, value, note) => { const r = el("div", "about-contact"); r.append(icon(ic), el("span", "about-contact-label", label)); const v = el("span", "about-contact-value"); v.append(value); r.append(v); if (note) r.append(el("span", "about-contact-note", note)); rows.append(r); };
      contact("ti-user", "Maintainer", APP.maintainer + " · ", null); rows.lastChild.querySelector(".about-contact-value").append(mail(APP.maintainerEmail));
      contact("ti-messages", "Questions & ideas", mail(APP.contact));
      contact("ti-shield-lock", "Security reports", mail(APP.security), "private; please don't post details publicly");
      contact("ti-license", "Licence", `${APP.licence} · © ${APP.year} ${APP.company}`);
      maker.append(rows); b.append(maker);
    } else if (t === "help") {
      h("Getting around");
      p("The tree on the left is your bookmarks; the pane on the right shows whatever is selected. Arrow keys move through the tree, → opens a folder, ← closes it or goes up, Enter opens a link. Right-click (or .) anywhere for the menu of everything you can do to the selection.");
      h("Finding things");
      p("Type in the search box (/) to search titles, links, descriptions and folders. The Show row narrows to one kind of link (dead, moved, needs login…); the View row groups and sorts, and limits by the date a bookmark was added. ⌘K opens every command by name.");
      h("Fixing links");
      p("Dead links are split into a page that is gone and a whole site that is gone. A moved link has its new address ready: Update link applies it, one at a time or all at once; links that now redirect to a different site are left for you to check first. Refresh re-checks links and retakes snapshots; you can pause or stop it.");
      h("Organising");
      p("Move (M), group into a new folder (G), rename (F2), describe (D), reorder (⌥↑ ⌥↓). Trash keeps everything until you empty it, and the Trash is left out of the import file. ⌘Z undoes.");
      h("Getting the result into Chrome");
      p("Download import file writes a standard bookmarks file. In Chrome: Bookmarks → Import bookmarks and settings → Bookmarks HTML file. It arrives as one folder, so nothing you already have is overwritten.");
      const btns = el("div", "d-flex flex-wrap gap-2 mt-2");
      const kb = el("button", "btn btn-sm"); kb.append(icon("ti-keyboard", "me-1"), "All keyboard shortcuts"); kb.onclick = () => { hideModal("#aboutModal"); openShortcuts(); };
      const tb = el("button", "btn btn-sm btn-primary"); tb.append(icon("ti-route", "me-1"), "Take the guided tour"); tb.onclick = () => { hideModal("#aboutModal"); tour(0); };
      btns.append(tb, kb); b.append(btns);
      h("Where to find everything");
      const idx = [
        ["Import bookmarks from any browser", "Arrow beside Download import file › Import bookmarks…"],
        ["Export (HTML for any browser, Chrome JSON, duplicates CSV)", "Arrow beside Download import file"],
        ["History, progress and roll back", "Arrow beside Download › History and progress…, or the workspace button"],
        ["Workspaces: switch, rename, notes, archive", "The workspace button in the header"],
        ["Dead, moved, redirecting, login-only, unverified links", "Show row, or click a card at the top"],
        ["Update moved links (one, some, all)", "Update moved in the header, or Actions › Update link"],
        ["Re-check links and retake snapshots (pause, stop)", "Refresh all, or Actions › Refresh on any folder"],
        ["Open links: tabs, a new window, one window per folder, incognito; or move them to tabs", "Open menu, right-click, or O / ⇧O / I"],
        ["Move, group, rename, describe, reorder", "Actions menu, right-click, or M / G / F2 / D / ⌥↑↓"],
        ["Trash, put back, empty", "Actions › Move to Trash; the Trash folder at the bottom of the tree"],
        ["Group, sort, folders first, date range", "View row under the Show row"],
        ["Removed duplicates and where each went", "Show › Removed duplicates"],
        ["Full folder details", "All details on a folder's Link health panel"],
        ["Open tabs per window, incognito, snapshots, reopen after a crash", "Open tabs, next to Bookmarks under the header"],
        ["Timeline and history: tabs opened/closed, edits, bookmarks added, charts", "Timeline, next to Open tabs"],
        ["Settings and factory reset", "⋯ menu › Settings (or ,)"],
        ["Every command by name", "Commands (⌘K)"],
      ];
      const it = el("table", "table table-sm table-vcenter mb-0 help-index"), itb = el("tbody");
      for (const [w, where] of idx) { const tr = el("tr"); tr.append(el("td", "fw-medium", w), el("td", "text-secondary", where)); itb.append(tr); }
      it.append(itb); b.append(it);
      h("Feature guide");
      const fg = el("div", "list-group list-group-flush feature-guide");
      TOUR.forEach(([, title, text], k) => { const a = el("a", "list-group-item list-group-item-action"); a.href = "#";
        const tx = el("div", "min-w-0"); tx.append(el("div", "fw-medium", title), el("div", "text-secondary small", text)); a.append(tx, el("span", "badge bg-primary-lt ms-auto flex-none", "Show me"));
        a.onclick = e => { e.preventDefault(); hideModal("#aboutModal"); tour(k); }; fg.append(a); });
      b.append(fg);
    } else if (t === "privacy") {
      h("Your data stays on this computer");
      p("Your bookmarks, their history, link checks and tab snapshots are kept by the Browser Foreman server on this computer (in ~/.local/state/browser-foreman unless you chose another place), and a few view settings in this browser's storage. Nothing is uploaded, and there is no tracking or analytics.");
      h("What the link checker does");
      p("To check a link it visits that page, as a browser would, a few at a time and politely, with no cookies and no sign-ins. Sign-out, delete and unsubscribe links are never visited. The sites you bookmarked see an ordinary visit from your internet address.");
      h("Open tabs");
      p("To keep track of your tabs, the server asks your browsers which windows and tabs are open: on macOS through its Automation permission, which you grant once per browser, or through the Browser Foreman extension once you pair it. It opens or closes a tab only when you ask. Snapshots are kept with your other data on this computer.");
      p("Private windows: you choose whether to keep their pages or only their counts. Unless you keep them, only the number of private windows and tabs is stored.");
      h("The server");
      p("browser-foreman serve answers only on this computer (127.0.0.1) unless you set it up for several people, and every change needs this page's token or a signed-in person. It reads your browsers' bookmarks files only when you ask it to, and only files named like bookmarks.");
      h("Backups and safety");
      p("Every change is an entry in the workspace's history, so undo and History bring back anything. Removed bookmarks go to the workspace's Trash first. Back up everything… writes one file with every workspace, its history, link checks, tab snapshots and settings, and never passwords or tokens.");
    } else {
      h(`${APP.name} ${APP.version}`);
      p(`Copyright © ${APP.year} ${APP.org}. Released under the MIT licence.`);
      const pre = el("pre", "about-licence", MIT_TEXT); b.append(pre);
      h("Built with");
      const t2 = el("table", "table table-sm table-vcenter"), tb = el("tbody");
      for (const [n, v, lic, url, what] of CREDITS) { const tr = el("tr"), nm = el("td", "text-nowrap"); nm.append(link(url, n)); tr.append(nm, el("td", "text-secondary text-nowrap", v), el("td", "text-nowrap", lic), el("td", "text-secondary small", what)); tb.append(tr); }
      t2.append(tb); b.append(t2);
      p("Site names, logos and favicons shown here belong to their owners and are shown only to help you recognise your own bookmarks. Snapshots are private pictures of public pages for your own use.", "text-secondary small");
      p("No warranty: link checks can be wrong (a site may block checks or be down for a moment). Review before deleting anything.", "text-secondary small");
    }
  };
  for (const [k, l, ic] of tabs) { const a = el("button", "about-tab"); a.type = "button"; a.setAttribute("role", "tab"); a.dataset.t = k; a.append(icon(ic), el("span", null, l)); a.onclick = () => paint(k); nav.append(a); }
  $("#aboutFoot").textContent = `${APP.name} ${APP.version} · © ${APP.year} ${APP.company} · ${APP.licence} licence · Made by Simtabi`;
  paint(tab); showModal("#aboutModal");
}
on("#settingsDefaults", () => { SET = {...SETTINGS_DEFAULT}; saveSettings(); openSettings(); build(); toast("Settings back to their defaults", "success"); });
// Settings: grouped rows, each a label, a line saying what it does, and its control. Changes apply at once.
function openSettings() {
  // The preference rows are typed (views/settings.ts); the page keeps the values and what each change sets off.
  const store = {
    get: k => SET[k], defaults: SETTINGS_DEFAULT,
    set: (k, v) => {
      SET[k] = v; saveSettings();
      if (k.startsWith("tree")) draw();
      if (k === "rememberPlace") v ? PLACE.sync() : PLACE.clear();
      if (k === "tileLimit") show();
      if (k === "pageSize" && view_ === "tabs") TABS.open();
    },
  };
  // A button here closes the dialog before it acts (Factory reset asks inside it, so it does not).
  const closing = fn => () => { hideModal("#settingsModal"); fn(); };
  // Browser history: Full Disk Access status and the way to grant it.
  const ha = Dom.write(el("div", "set-group"), "panel", "history-access"); new HistoryAccessPanel(ha).refresh();
  const extra = [ha];
  // Page snapshots: window size, wait and folder (server preferences; owner only).
  if (WS) extra.push(new SnapshotSettingsPanel().element);
  // Assistants: where Claude can reach Browser Foreman, and whether it may change things.
  if (wsUI()?.mcpAccess) extra.push(settingsGroup("Assistants", [
    actionRow("MCP access (Claude)", "Where Claude can reach Browser Foreman, and whether it may change things. Off by default.", "Open…", "ti-plug-connected", "", closing(() => wsUI().mcpAccess()))]));
  // Danger zone: everything that throws work away or deletes it, kept out of the menus.
  const ws = window.BM_WS, danger = [
    actionRow("Reset edits", "Throw away moves, renames, notes and the trash. Settings stay.", "Reset edits…", "ti-restore", "btn-danger", closing(resetEdits))];
  if (wsUI()?.resetWs) danger.push(actionRow("Reset this workspace to its start", "The workspace goes back to how it was when it was created, as one history entry you can undo.", "Reset workspace…", "ti-history-off", "btn-danger", closing(() => wsUI().resetWs())));
  if (wsUI()?.deleteWs && ws && ws.id !== "main") danger.push(actionRow("Delete this workspace", "Moves it to the workspace trash; you can bring it back from All workspaces.", "Delete workspace…", "ti-trash", "btn-danger", closing(() => wsUI().deleteWs())));
  danger.push(actionRow("Factory reset", "Everything this tool has saved goes back to how it was on first open: edits, link updates, refresh results, view, open folders and settings. Your original bookmarks, the backup and the checked data on disk are not touched.", "Factory reset…", "ti-alert-triangle", "btn-danger", factoryReset));
  extra.push(settingsGroup("Danger zone", danger, "set-danger"));
  renderSettings($("#settingsBody"), store, extra);
  showModal("#settingsModal");
}
async function factoryReset() {
  hideModal("#settingsModal");
  const keys = []; try { for (let k = 0; k < localStorage.length; k++) { const n = localStorage.key(k); if (n && n.startsWith(STORE + ":")) keys.push(n); } } catch {}
  const edited = !!(st().edited || Object.keys(URLS).length || Object.keys(store.get("refreshed") || {}).length);
  if (!(await askConfirm({title: "Reset the tool to factory settings?", ok: edited ? "Download, then reset" : "Reset everything", no: "Keep my work",
    body: (edited ? "Your edited bookmarks are downloaded first as an import file, so nothing is lost. " : "") +
      (WS ? "Then the workspace goes back to the generated tree (Undo or History brings your edits back), " : "") +
      `${WS ? "and " : "Then "}${keys.length} saved item${keys.length === 1 ? "" : "s"} in this browser are cleared (view, open folders, settings${WS ? "" : ", edits, link updates, refresh results"}) and the page reloads as on first open. ` +
      "Your original bookmarks, the backup and the checked data on disk are not touched."}))) return;
  if (edited) downloadFile();
  if (WS && typeof WS.factoryReset === "function") { try { await WS.factoryReset(); } catch (e) { return toast(String(e), "danger", "The workspace could not be reset"); } }
  for (const k of keys) { try { localStorage.removeItem(k); } catch {} }
  PLACE.forget();  // the address too, or the reload would land back where you were
  try { sessionStorage.setItem(STORE + ":reset-done", "1"); } catch {}
  setTimeout(() => location.reload(), edited ? 600 : 0);
}
try { if (sessionStorage.getItem(STORE + ":reset-done")) { sessionStorage.removeItem(STORE + ":reset-done"); setTimeout(() => toast("Everything is as it was on first open.", "success", "Factory reset done"), 300); } } catch {}
on("#themeBtn", () => applyTheme(document.documentElement.getAttribute("data-bs-theme") === "dark" ? "light" : "dark"));

// ---------- dropdowns and modals (Tabler markup, a few lines of script, no Bootstrap JS needed) ----------
// Menus (building, placing, closing, the page-wide click rule): src/web/ts/menu.ts.
installMenus(() => hideTip());
// Dialogs, confirm, text prompt and toasts: src/web/ts/dialogs.ts. The page's follow-up when one closes: the folder
// picker answers "none" if it was closed without a pick, and the tree gets its focus back.
installDialogs({
  afterHide: id => { if (id === "#moveModal" && moveResolve) { const r = moveResolve; moveResolve = null; r(null); } list.focus(); },
  toastSeconds: () => SET.toastSecs,
});

// Tooltips and popovers: src/web/ts/tips.ts.
installTips({enabled: () => SET.tooltips, delay: () => SET.tipDelay});

// ---------- rows ----------
function dupesTree() {
  const root = mkFolder("");
  for (const d of DATA.dupes) {
    let f = root;
    for (const part of d.o.split("/")) { let s = f.c.find(x => isFolder(x) && x.n === part); if (!s) { s = mkFolder(part); f.c.push(s); } f = s; }
    f.c.push(d.id);
  }
  return root;
}

function build(keepView = false) {
  indexParents(st().root);
  try { paintTrashBadge(); paintChipCounts(); updateAllLabel(); $("#navBmCount").textContent = liveItems().length.toLocaleString(); } catch (e) { console.error("header counts", e); }
  const root = readOnly() ? dupesTree() : grouped() ? virtualTree() : st().root;
  if (readOnly()) indexParents(root);
  const match = matcher(), auto = filtering();
  $("#clearFilters").disabled = !auto;  // nothing to clear: the button says so
  paintDates();
  const count = new Map();
  const cnt = f => { let c = 0; for (const x of f.c) c += isFolder(x) ? cnt(x) : (match(x) ? 1 : 0); count.set(f, c); return c; };
  const t = grouped() ? -1 : root.c.findIndex(x => isFolder(x) && x.n === TRASH);
  // The title counts what is outside the Trash, as All bookmarks does; the Trash row shows its own count.
  const total = cnt(root) - (t >= 0 ? count.get(root.c[t]) || 0 : 0);
  if (t >= 0 && t !== root.c.length - 1) root.c.push(...root.c.splice(t, 1));
  rows = [];
  // A filter, a search or the duplicates view opens every folder with a match; folders closed by hand while it is on
  // stay closed (by path), until the filter, search or dates change. They used to be forced open: collapse did nothing.
  const forced = auto || readOnly(), viewKey = [filter, $("#q").value.trim(), view.from, view.to].join("|");
  if (viewKey !== viewClosedKey) { viewClosed.clear(); viewClosedKey = viewKey; }
  const walk = (f, depth, path) => {
    for (const x of ordered(f)) {
      if (isFolder(x)) {
        const isTrash = depth === 0 && x.n === TRASH && !x.virtual;
        if (auto && !count.get(x) && !isTrash) continue;
        const key = (x.virtual ? "v:" : "") + path + "/" + x.n;
        const open = forced && !isTrash ? !viewClosed.has(key) : (x.virtual ? vopen.has(x.n) : isOpen(x));
        rows.push({kind: "folder", node: x, depth, open, count: count.get(x), parent: f, trash: isTrash, key});
        if (open) walk(x, depth + 1, key);
      } else if (match(x)) rows.push({kind: "link", node: x, item: ITEM.get(x), depth, parent: f});
    }
  };
  walk(root, 0, "");
  // The duplicates view is a read-only list (copies merged into the bookmarks kept): say so where the hints are, so a
  // drag that cannot happen there does not read as broken.
  const hint = $("#treeHint");
  if (hint) {
    const keys = el("button", "btn btn-ghost-secondary btn-icon btn-sm"); keys.type = "button"; keys.setAttribute("aria-label", "Keyboard shortcuts"); keys.append(icon("ti-keyboard"));
    keys.dataset.tip = (readOnly() ? "" : "Drag to move. ") + "→ open · ← close · Space select · . menu. Click for every shortcut.";
    keys.onclick = () => openShortcuts();
    // List or grid (thumbnail cards), kept in this browser.
    const lay = el("div", "btn-group btn-group-sm layout-toggle"); lay.setAttribute("role", "group"); lay.setAttribute("aria-label", "Layout");
    for (const [k, ic, tip] of [["list", "ti-list", "List"], ["grid", "ti-layout-grid", "Grid: thumbnails"]]) {
      const b = el("button", "btn btn-icon" + (LAYOUT === k ? " active" : "")); b.type = "button"; b.dataset.tip = tip; b.setAttribute("aria-label", tip);
      b.setAttribute("aria-pressed", String(LAYOUT === k)); b.append(icon(ic)); b.onclick = () => setLayout(k); lay.append(b);
    }
    const parts = [lay, keys];
    if (readOnly()) { const b = el("span", "badge bg-yellow-lt"); b.append(icon("ti-lock", "me-1"), "Read-only"); b.dataset.tip = "These copies were merged into the bookmarks you kept. Open, copy or look them up here; to move bookmarks, use All bookmarks."; parts.unshift(b); }
    hint.replaceChildren(...parts);
  }
  const tt = $("#treeTitle");
  tt.replaceChildren(readOnly() ? "Removed duplicates" : grouped() ? "Grouped by " + $("#v-group").selectedOptions[0].text.toLowerCase() : "Folders",
    el("span", "badge bg-secondary-lt ms-2", total.toLocaleString()));
  sel = Math.min(sel, Math.max(rows.length - 1, 0));
  draw(); if (!keepView) show(); FOLD?.refresh();
}

// A row's element by its index in `rows` (separators sit between rows, so position in the list is not the index).
const rowEl = k => Dom.one("row", String(k), list);
// List or grid layout of the tree (the grid shows each folder's bookmarks as thumbnail cards).
let LAYOUT = store.get("layout") === "grid" ? "grid" : "list";
function setLayout(k) { LAYOUT = k; store.set("layout", k); document.body.classList.toggle("layout-grid", k === "grid"); build(true); }
document.body.classList.toggle("layout-grid", LAYOUT === "grid");
// What the row renderer needs from the page (src/web/ts/views/tree-rows.ts).
const CARD_DEPS = {host, hue, shortUrl};  // what the Open tabs cards need (src/web/ts/views/tab-cards.ts)
const ROW_DEPS = {readOnly: () => readOnly(), titleOf, descOf, ratingOf, tagsOf, host, shortUrl, thumb, favicon, item: id => ITEM.get(id),
  isNewFolder: f => newMine.has(pathOf(f)), filtering: () => !!filtering(), label: LABEL, help: HELP, statusIcon: STATUS_ICON};
function draw() {
  hideTip();
  list.replaceChildren();
  if (!rows.length) { const e = el("div", "empty py-5"); e.append(el("p", "empty-title", "Nothing matches"), el("p", "empty-subtitle text-secondary", "Clear the filters or the search to see everything.")); list.append(e); return; }
  // Grid layout: each run of bookmarks in a folder is laid out as thumbnail cards; folders stay rows above them.
  const grid = LAYOUT === "grid"; let box = null;
  rows.forEach((r, idx) => {
    // The row's element is built by src/web/ts/views/tree-rows.ts; selection, clicks, menus and drag are wired here.
    const {row: d, twisty} = rowElement(r, idx, {selected: idx === sel, marked: marked.has(r.node), draggable: !readOnly() && !r.trash && !grouped()}, ROW_DEPS);
    if (twisty) twisty.onclick = e => { e.stopPropagation(); sel = idx; toggle(r, !r.open); };
    d.onclick = e => {
      if (e.metaKey || e.ctrlKey) toggleMark(r.node);
      else if (e.shiftKey && anchor != null) markRange(anchor, idx);
      else { marked.clear(); anchor = idx; }
      sel = idx; draw(); show(); list.focus();
    };
    d.ondblclick = () => { if (r.kind === "folder") toggle(r, !r.open); else openTab(r.item.u); };
    d.oncontextmenu = e => { e.preventDefault(); if (!marked.has(r.node)) marked.clear(); sel = idx; draw(); show(); openMenu(e.clientX, e.clientY); };
    dnd(d, r);
    // A folder holding both subfolders and bookmarks: its loose bookmarks get their own labelled group after the
    // subfolders, so they do not read as part of the last subfolder.
    const group = looseGroupAt(rows, idx);
    if (group) { box = null; list.append(looseLabel(r.depth, group.parent === st().root ? "this folder" : nice(group.parent.n), group.count)); }
    if (grid && r.kind === "link") {
      if (!box) { box = el("div", "bm-grid"); box.setAttribute("role", "group"); box.style.marginLeft = (6 + r.depth * 18) + "px"; list.append(box); }
      d.style.marginLeft = ""; box.append(d);
    } else { box = null; list.append(d); }
  });
  rowEl(sel)?.scrollIntoView({block: "nearest"});
}

// Folders closed by hand while a filter, search or the duplicates view forces matching folders open (see build).
const viewClosed = new Set(); let viewClosedKey = "";
function toggle(r, open) {
  if ((filtering() || readOnly()) && !r.trash) { open ? viewClosed.delete(r.key) : viewClosed.add(r.key); build(true); return; }
  if (readOnly()) return;
  if (r.node.virtual) { if (open) vopen.add(r.node.n); else vopen.delete(r.node.n); build(); return; }
  setOpen(r.node, open); saveOpen(); build();
}
function toggleMark(node) { if (marked.has(node)) marked.delete(node); else marked.add(node); }
function markRange(a, b) { marked.clear(); for (const n of rangeOf(rows, a, b)) marked.add(n); }
const targets = () => (marked.size ? [...marked] : rows[sel] ? [rows[sel].node] : []).flatMap(x => isFolder(x) && x.virtual ? x.c : [x]);
const targetIds = () => [...new Set(targets().flatMap(x => isFolder(x) ? descendants(x) : [x]))];
// Only web links are ever opened or linked: imported files can hold javascript:, data: or file: bookmarks.
const isWeb = u => /^https?:\/\//i.test(String(u || ""));
const isSelf = u => { try { return new URL(u).origin === location.origin; } catch { return false; } };
function openTab(u) { if (!isWeb(u)) return toast("Only web links (http and https) are opened from here.", "warning", "Not opened"); window.open(u, "_blank", "noopener,noreferrer"); }
function selectNode(x) { const k = rows.findIndex(r => r.node === x); if (k >= 0) { sel = k; draw(); show(); } }

// ---------- opening many links ----------
// In this browser: one tab per link (allow pop-ups for this page). In Chrome: tabs, a new window or an incognito window,
// through the preview server. Tab groups need the browser extension.
// Bookmarks to tabs, for good: open them (where `how` says), then move the bookmarks to the Trash, but only when
// every one opened. `how`: "window", "tabs" (the current window), "grouped" (one window per folder), "incognito".
// moveToTabs(true/false) is the older form (incognito or not) and still works.
async function moveToTabs(how = "window") {
  if (how === true) how = "incognito"; else if (how === false) how = "window";
  const ids = targetIds().filter(id => isWeb(ITEM.get(id).u)); if (!ids.length) return toast("No web links here");
  const where = OPEN_WHERE()[how];
  if (!(await askConfirm({title: `Move ${ids.length} bookmark${ids.length === 1 ? "" : "s"} to tabs?`, ok: "Open, then move to Trash", danger: false,
    body: `They open in ${where}, then the bookmarks go to the Trash. Put back or ⌘Z brings them back; emptying the Trash is what removes them.`}))) return;
  const ok = await openLinks(how, ids, true);
  if (ok) { trashNodes(); }
}
const OPEN_WHERE = () => ({browser: "new tabs in this browser", tabs: `the current ${BROWSER_NAME} window`, window: `a new ${BROWSER_NAME} window`,
  grouped: `one new ${BROWSER_NAME} window per folder`, incognito: `a new ${BROWSER_NAME} incognito window`});
async function openLinks(how, ids = targetIds(), confirmed = false, browser = null) {
  // Returns true when everything opened, so "move to tabs" only trashes bookmarks whose tabs really opened.
  const urls = ids.map(id => ITEM.get(id).u).filter(isWeb);
  if (!urls.length) { toast("No web links here"); return false; }
  const where = OPEN_WHERE()[how];
  if (!confirmed && urls.length > SET.openWarn && !(await askConfirm({title: `Open ${urls.length} links?`, ok: `Open ${urls.length}`, danger: false,
    body: `They open in ${where}. That is a lot of tabs at once.`}))) return false;
  if (how === "browser") {
    let blocked = 0;
    for (const u of urls) if (!window.open(u, "_blank", "noopener,noreferrer")) blocked++;
    toast(blocked ? `${blocked} blocked: allow pop-ups for this page, or open them in ${BROWSER_NAME}` : `Opened ${urls.length} in new tabs`, blocked ? "warning" : "success");
    return !blocked;
  }
  try {
    // One window per folder when grouped; otherwise one batch. Each batch goes in pieces of 200 (the server's limit).
    const groups = new Map();
    if (how === "grouped") for (const id of ids) { const u = ITEM.get(id).u; if (!isWeb(u)) continue; const f = PARENT.get(id); const g = groups.get(f) || []; g.push(u); groups.set(f, g); }
    else groups.set(null, urls);
    let opened = 0;
    // Grouped: the server opens each folder as a tab group named after it where the browser's extension is connected,
    // else as a window of its own.
    for (const [f, list] of groups) for (let k = 0; k < list.length; k += 200)
      opened += (await tokenPost("api/open", {how: how === "grouped" ? "grouped" : how, urls: list.slice(k, k + 200), ...(f ? {title: nice(f.n)} : {}), ...(browser ? {browser} : {})})).opened || 0;
    toast(`Opened ${opened} in ${browser ? `new tabs in ${browser}` : how === "grouped" ? `${groups.size} new ${BROWSER_NAME} window${groups.size === 1 ? "" : "s"}, one per folder` : where}`, "success");
    return opened === urls.length;
  } catch (e) { toast(String(e.message || e), "danger", `${BROWSER_NAME} could not open them`); return false; }
}
function copyLinks(ids = targetIds()) {
  const urls = ids.map(id => ITEM.get(id).u);
  navigator.clipboard?.writeText(urls.join("\n")).then(() => toast(`Copied ${urls.length} link${urls.length === 1 ? "" : "s"}`));
}

// ---------- edits ----------
const contains = (folder, x) => containsNode(folder, x, PARENT);
const detach = x => detachNode(x, PARENT);
function forgetFrom(x) { if (isFolder(x)) delete x.from; else delete st().trashFrom[x]; }
function moveNodes(nodes, target, before = null, quiet = false) {
  if (readOnly()) return;
  const tf = trashFolder();
  nodes = nodes.filter(x => x !== tf && !(isFolder(x) && contains(x, target)) && x !== before);
  if (!nodes.length) return toast("A folder can't go inside itself");
  if (!quiet) snapshot();
  for (const x of nodes) { if (target !== tf && !contains(tf, target)) forgetFrom(x); detach(x); }
  const at = before != null && target.c.includes(before) ? target.c.indexOf(before) : target.c.length;
  target.c.splice(at, 0, ...nodes);
  indexParents();
  if (target !== tf) setOpen(target, true);
  marked.clear(); save(); build(); selectNode(nodes[0]);
  if (!quiet) toast(`Moved ${nodes.length} to ${nice(pathOf(target)) || "top level"}`);
}
function reorder(delta) {
  if (readOnly()) return;
  if (grouped()) return toast("Reordering works when grouped by folders");
  const x = rows[sel]?.node, p = x && PARENT.get(x); if (!p || x === trashFolder()) return;
  snapshot(); materialize();
  const k = p.c.indexOf(x), j = k + delta; if (j < 0 || j >= p.c.length) { undoStack[mode].pop(); return; }
  [p.c[k], p.c[j]] = [p.c[j], p.c[k]]; save(); build(); selectNode(x);
}
async function newFolder(groupNodes) {
  if (readOnly()) return;
  if (grouped() && !groupNodes) return toast("New folders are made when grouped by folders");
  const r = rows[sel];
  const parent = r ? (r.kind === "folder" && r.open && !groupNodes && !r.trash ? r.node : r.parent) : st().root;
  const name = await askText({title: groupNodes ? "Group into a new folder" : "New folder",
    label: groupNodes ? `Name for the folder holding ${groupNodes.length} item(s)` : `Inside “${nice(pathOf(parent)) || "top level"}”`, value: "New folder", ok: "Create"});
  if (!name || !name.trim()) return;
  snapshot();
  const first = groupNodes ? groupNodes[0] : null, f = mkFolder(name.trim());
  const holder = first ? PARENT.get(first) : (parent.virtual ? st().root : parent);
  const at = first ? holder.c.indexOf(first) : holder.c.length;
  if (groupNodes) for (const x of groupNodes) { detach(x); f.c.push(x); }
  holder.c.splice(Math.min(at, holder.c.length), 0, f);
  indexParents(); setOpen(f, true); marked.clear(); save(); build(); selectNode(f);
  toast(groupNodes ? `Grouped ${groupNodes.length} into “${f.n}”` : `Created “${f.n}”`);
}
async function rename() {
  if (readOnly()) return;
  const r = rows[sel]; if (!r || r.trash) return;
  if (r.node.virtual) return toast("That is a group, not a folder");
  const cur = r.kind === "folder" ? nice(r.node.n) : titleOf(r.node);
  const name = await askText({title: r.kind === "folder" ? "Rename folder" : "Rename bookmark", label: "Name", value: cur});
  if (name == null || !name.trim() || name.trim() === cur) return;
  const wasOpen = r.kind === "folder" && isOpen(r.node);
  snapshot();
  if (r.kind === "folder") { r.node.n = name.trim().replaceAll("/", "∕"); if (wasOpen) setOpen(r.node, true); }
  else st().titles[r.node] = name.trim();
  save(); build();
}
// Descriptions: a note of your own on any folder or bookmark. A bookmark without one shows its page's own description.
async function editDescription() {
  if (readOnly()) return;
  const r = rows[sel]; if (!r || r.node.virtual) return;
  const isF = r.kind === "folder";
  const cur = isF ? (r.node.d || "") : (noteOf(r.node) ?? "");
  const v = await askText({title: isF ? "Folder description" : "Bookmark description", multiline: true, value: cur,
    label: isF ? nice(r.node.n) : titleOf(r.node),
    hint: isF ? "What this folder is for. Saved with your edits and written into the import file." :
      (r.item.ds ? "Leave empty to show the page's own description: " + r.item.ds.slice(0, 120) : "A note of your own; written into the import file."),
    ok: "Save  (⌘↵)"});
  if (v == null) return;
  snapshot();
  if (isF) { if (v.trim()) r.node.d = v.trim(); else delete r.node.d; }
  else if (v.trim()) st().notes[r.node] = v.trim(); else delete st().notes[r.node];
  save(); build();
}
function sortFolder() {
  const r = rows[sel]; const f = r.kind === "folder" ? r.node : r.parent;
  snapshot();
  const links = f.c.filter(x => !isFolder(x)).sort((a, b) => titleOf(a).localeCompare(titleOf(b)));
  const subs = f.c.filter(isFolder).sort((a, b) => byName(a.n, b.n));
  f.c = [...subs, ...links]; save(); build(); toast(`Sorted “${nice(pathOf(f)) || "top level"}”`);
}

// ---------- trash ----------
function trashNodes() {
  if (readOnly()) return;
  const xs = targets().filter(x => x !== trashFolder()); if (!xs.length) return;
  if (xs.every(inTrash)) return deleteForever(xs);
  snapshot();
  let bin = trashFolder();
  if (!bin) { bin = mkFolder(TRASH); st().root.c.push(bin); indexParents(); }
  const next = rows.slice(sel + 1).find(r => !xs.includes(r.node) && !xs.some(x => isFolder(x) && contains(x, r.node)))?.node;
  for (const x of xs.filter(x => !inTrash(x))) {
    const p = PARENT.get(x), from = {path: pathOf(p), index: p.c.indexOf(x)};
    if (isFolder(x)) x.from = from; else st().trashFrom[x] = from;
  }
  moveNodes(xs.filter(x => !inTrash(x)), bin, null, true);
  if (next) selectNode(next);
  toast(`Put back (P) or ⌘Z restores. The Trash is left out of the download.`, "success", `Moved ${xs.length} to Trash`);
}
// Where Put back sends an item: the folder it came from; if that path is gone (the folder was moved or renamed since),
// the one folder outside the Trash with the same name; only when there is none (or several) is the old path recreated.
function restoreTarget(path) {
  if (!path) return st().root;
  let f = st().root;
  for (const part of path.split("/")) { f = f.c.find(x => isFolder(x) && x.n === part && x !== trashFolder()); if (!f) break; }
  if (f) return f;
  const name = path.split("/").pop(), same = [];
  walkFolders(st().root, x => { if (x.n === name && !inTrash(x) && x !== trashFolder()) same.push(x); });
  return same.length === 1 ? same[0] : folderAtPath(path);
}
function folderAtPath(path) {
  let f = st().root;
  if (!path) return f;
  for (const part of path.split("/")) {
    let s = f.c.find(x => isFolder(x) && x.n === part && x !== trashFolder());
    if (!s) { s = mkFolder(part); f.c.push(s); }
    f = s;
  }
  return f;
}
function putBack() {
  if (readOnly()) return;
  const xs = targets().filter(x => inTrash(x) && PARENT.get(x) === trashFolder());
  if (!xs.length) return toast("Select something directly inside the Trash to put back");
  putBackNodes(xs);
}
// Put back explicit items (the Trash window passes the ones ticked there); putBack() above uses the selection.
function putBackNodes(xs, {select = true} = {}) {
  if (readOnly()) return;
  xs = xs.filter(x => PARENT.get(x) === trashFolder()); if (!xs.length) return;
  snapshot();
  for (const x of xs) {
    const from = isFolder(x) ? x.from : st().trashFrom[x];
    const dest = restoreTarget(from ? from.path : (isFolder(x) ? "" : ITEM.get(x)[mode]));
    detach(x); forgetFrom(x);
    dest.c.splice(Math.min(from ? from.index : dest.c.length, dest.c.length), 0, x);
    indexParents(); setOpen(dest, true);
  }
  marked.clear(); save(); build(); if (select) selectNode(xs[0]);
  toast(`Put back ${xs.length} item${xs.length > 1 ? "s" : ""}`);
}
async function deleteForever(xs) {
  const ids = xs.flatMap(x => isFolder(x) ? descendants(x) : [x]);
  if (!(await askConfirm({title: `Delete ${ids.length} bookmark${ids.length === 1 ? "" : "s"} permanently?`, ok: "Delete permanently",
    body: "They are left out of the import file for good. ⌘Z can still bring them back until you reload this page; after that, only Reset edits can."}))) return;
  snapshot();
  for (const x of xs) { detach(x); forgetFrom(x); }
  for (const id of ids) { st().gone.add(id); delete st().trashFrom[id]; }
  indexParents(); marked.clear(); save(); build();
  toast(`${ids.length} bookmark${ids.length === 1 ? "" : "s"} deleted from the import`);
}
function emptyTrash() {
  const bin = trashFolder(); if (!bin || !bin.c.length) return toast("The trash is already empty");
  deleteForever([...bin.c]);
}
// The Trash window (views/trash.ts): every item directly inside the Trash, where it came from, and how many
// bookmarks it holds. Opened from the Trash button; the badge shows how many bookmarks are in it.
// Why each item is in the Trash (views/trash.ts reasonOf): a duplicate of a bookmark still kept (the kept copy is shown
// as its parent), the _Review group it was sorted into, its link check, or put there by you.
const sameAddress = u => { try { const x = new URL(u); return x.host.toLowerCase().replace(/^www\./, "") + x.pathname.replace(/\/+$/, "") + x.search; } catch { return u; } };
function trashEntries() {
  const bin = trashFolder(); if (!bin) return [];
  const kept = new Map();  // address → bookmarks outside the Trash
  const walk = (f, path) => { for (const x of f.c) { if (x === bin) continue; if (isFolder(x)) walk(x, [...path, x.n]); else { const k = sameAddress(ITEM.get(x).u); (kept.get(k) || kept.set(k, []).get(k)).push({id: x, path}); } } };
  walk(st().root, []);
  const reviewOf = path => { const p = (path || "").split("/"), k = p.indexOf("_Review"); return k >= 0 ? p[k + 1] || "" : ""; };
  const facts = (id, review) => { const k = sameAddress(ITEM.get(id).u), ks = kept.get(k) || []; return {k, ks, f: {copies: ks.length, review, status: ITEM.get(id).s}}; };
  return bin.c.map(x => {
    if (isFolder(x)) {
      const reasons = {};
      const inner = (f, path) => { for (const y of f.c) { if (isFolder(y)) inner(y, [...path, y.n]); else { const r = reasonOf(facts(y, reviewOf([x.from?.path, x.n, ...path].join("/"))).f); reasons[r.key] = (reasons[r.key] || 0) + 1; } } };
      inner(x, []);
      return {node: x, folder: true, title: x.n, from: nice(x.from?.path || ""), count: descendants(x).length, reasons};
    }
    const from = st().trashFrom[x]?.path || "", {k, ks, f} = facts(x, reviewOf(from)), r = reasonOf(f);
    const e = {node: x, folder: false, title: titleOf(x), url: ITEM.get(x).u, from: nice(from), count: 1, reasons: {[r.key]: 1}, copies: ks.length};
    if (r.key === "duplicate") { const first = ks[0]; e.dupKey = k; e.kept = {title: titleOf(first.id), path: nice(first.path.join("/")), url: ITEM.get(first.id).u}; }
    return e;
  });
}
const TRASH_WIN = new TrashDialog({
  list: trashEntries,
  restore: xs => putBackNodes(xs, {select: false}),
  purge: xs => deleteForever(xs),
  open: u => openTab(u),
  pageSize: () => listSize("trash"), setPageSize: n => setListSize("trash", n),
});
function openTrash() { if (readOnly()) return toast("Switch to the Bookmarks tree to see the Trash"); TRASH_WIN.open(); }
on("#trashBtn", openTrash);
function paintTrashBadge() {
  const bin = trashFolder(), n = bin ? descendants(bin).length : 0, b = $("#trashBadge");
  if (b) b.textContent = n ? n.toLocaleString() : "";
}

// ---------- refresh: re-check links and re-take snapshots through the local server ----------
let jobRunning = false;
async function refresh(all = false) {
  const xs = all ? [st().root] : targets(); if (!xs.length) return;
  if (jobRunning) return toast("A refresh is already running");
  const ids = [...new Set(xs.flatMap(x => isFolder(x) ? descendants(x) : [x]))].filter(id => /^https?:/i.test(ITEM.get(id).u));
  if (!ids.length) return toast("Nothing to refresh here");
  if (all && !(await askConfirm({title: `Refresh all ${ids.length.toLocaleString()} links?`, ok: "Start refreshing", danger: false,
    body: "Every link is checked again and every snapshot retaken. Sites are visited politely, a few at a time, so this takes a while. You can pause or cancel it, and keep working meanwhile."}))) return;
  let job, total;
  if (WS?.refresh) {
    try { ({job, total} = await WS.refresh(ids)); } catch (e) { return toast(String(e?.message || e), "danger", "The link check could not start"); }
  } else {
    let res;
    try {
      res = await fetch("api/refresh", {method: "POST", headers: {"Content-Type": "application/json"}, body: JSON.stringify({items: ids.map(id => ({id, u: ITEM.get(id).u}))})});
      if (!res.ok) throw new Error(res.status);
    } catch { return toast("Start it with browser-foreman serve, then try again.", "danger", "The server isn't answering"); }
    ({job, total} = await res.json());
  }
  jobRunning = true;
  trackJob(job, total);
  toast(`Progress shows bottom right; you can keep working, pause or stop it.`, "info", `Refreshing ${total.toLocaleString()} link${total === 1 ? "" : "s"}`);
}

// Activity panel: what is running, how far along, how long is left, and the controls to pause, resume or cancel it.
const fmtDur = s => s < 60 ? `${Math.max(1, Math.round(s))}s` : s < 3600 ? `${Math.round(s / 60)} min` : `${(s / 3600).toFixed(1)} h`;
function trackJob(job, total) {
  const panel = $("#jobPanel"); panel.hidden = false;
  const ctl = async verb => { try { if (WS?.ctl) await WS.ctl(job, verb); else await fetch(`api/job/${job}/${verb}`, {method: "POST", headers: {"Content-Type": "application/json"}, body: "{}"}); } catch {} tick(); };
  $("#jobPause").onclick = () => ctl($("#jobPause").dataset.state === "paused" ? "resume" : "pause");
  $("#jobPause").disabled = $("#jobCancel").disabled = false;
  $("#jobCancel").onclick = async () => {
    if (!(await askConfirm({title: "Stop this refresh?", ok: "Stop refresh", no: "Keep going",
      body: "It stops within a second or two. Everything checked so far is kept and applied."}))) return;
    $("#jobPause").disabled = $("#jobCancel").disabled = true; $("#jobTitle").textContent = "Stopping…";
    ctl("cancel");
  };
  let misses = 0;
  const tick = async () => {
    let j; try { if (WS?.job) j = await WS.job(job); else { const r = await fetch("api/job/" + job); if (r.status === 404) j = {done: true, state: "lost", results: {}, rendered: 0}; else j = await r.json(); } }
    catch { if (++misses > 15) { finish({done: true, state: "lost", results: {}, rendered: 0}); return; } setTimeout(tick, 2000); return; }
    misses = 0;
    // Checking is about 60% of the work, snapshots the rest.
    const p = j.phase === "checking" ? 0.6 * j.checked / Math.max(1, total)
      : 0.6 + 0.4 * (j.render_total ? j.rendered / j.render_total : 1);
    const pct = Math.min(100, Math.round((j.done ? 1 : p) * 100));
    const elapsed = Date.now() / 1000 - j.started, eta = p > 0.02 && !j.done ? elapsed / p * (1 - p) : null;
    $("#jobBar").style.width = pct + "%"; $("#jobPct").textContent = pct + "%";
    $("#jobBar").classList.toggle("bg-warning", j.state === "paused");
    $("#jobSpin").hidden = j.state === "paused" || j.done;
    if (j.done) return finish(j);
    $("#jobTitle").textContent = j.state === "paused" ? "Paused" : j.state === "cancelled" ? "Stopping…" :
      j.phase === "checking" ? `Checking links` : "Taking snapshots";
    $("#jobDetail").textContent = (j.phase === "checking" ? `${j.checked} of ${total} checked` : `${j.rendered} of ${j.render_total} snapshots`) +
      (eta != null && j.state === "running" ? ` · about ${fmtDur(eta)} left` : "") + ` · ${fmtDur(elapsed)} so far`;
    $("#jobPause").dataset.state = j.state;
    $("#jobPause").replaceChildren(icon(j.state === "paused" ? "ti-player-play" : "ti-player-pause", "me-1"), j.state === "paused" ? "Resume" : "Pause");
    $("#job").hidden = false; $("#job").replaceChildren(el("span", "spinner-border spinner-border-sm me-1"), `${pct}%`);
    setTimeout(tick, 1200);
  };
  const finish = j => {
    let changed = 0; const now = Date.now(), kept = store.get("refreshed") || {};
    let kept0 = 0;
    for (const [id, r] of Object.entries(j.results)) {
      const i = ITEM.get(+id); if (!i) continue;
      // A timeout or a blocked check says nothing new: keep what was known rather than downgrade a link to "could not verify".
      if (r.s === "unverified" && i.s !== "unverified" && i.s !== "unchecked") { kept0++; if (r.h) { i.h = r.h; thumbVer.set(r.h, now); } continue; }
      if (i.s !== r.s) changed++;
      i.s = r.s; i.n = r.n; if (r.h) { i.h = r.h; thumbVer.set(r.h, now); }
      if ("dk" in r) i.dk = r.dk; else delete i.dk;
      if ("nu" in r) i.nu = r.nu; else delete i.nu;
      kept[id] = {s: i.s, n: i.n, h: i.h, dk: i.dk, nu: i.nu};
    }
    store.set("refreshed", kept);
    jobRunning = false; panel.hidden = true; $("#job").hidden = true; summary(); build();
    const got = j.checked ?? Object.keys(j.results).length, what = `${got.toLocaleString()} of ${total.toLocaleString()} checked · ${changed} changed status · ${j.rendered} new snapshot${j.rendered === 1 ? "" : "s"}` +
      (kept0 ? ` · ${kept0} could not be reached this time, so their earlier status is kept` : "");
    if (j.state === "cancelled") toast(what + ". Results so far are applied.", "warning", "Refresh stopped");
    else if (j.state === "failed") toast((j.error || "The refresh failed") + ". Results so far are applied.", "danger", "Refresh failed");
    else if (j.state === "lost") toast("The server stopped answering, or was restarted. Start the refresh again.", "danger", "Refresh interrupted");
    else toast(what, "success", "Refresh finished");
  };
  tick();
}

// ---------- moved links: update to the new address, one, some or all ----------
function applyUpdates(ids, quiet) {
  let n = 0;
  for (const id of ids) {
    const i = ITEM.get(id); if (!i || !pendingUpdate(i)) continue;
    i.prev = i.u; i.u = i.nu; i.applied = true; URLS[id] = i.u; n++;
  }
  store.set("urls", URLS); updateAllLabel(); build();
  if (!quiet) n ? toast(`Updated ${n} link${n === 1 ? "" : "s"} to the new address`, "success") : toast("Nothing to update here");
  return n;
}
function revertUpdate(id) {
  const i = ITEM.get(id); if (!i || !i.applied) return;
  i.u = i.prev; delete i.prev; i.applied = false; delete URLS[id]; store.set("urls", URLS); updateAllLabel(); build(); toast("Back to the old address");
}
// The bookmarks in the tree now, outside the Trash: what counts and bulk actions use (not the original set, which
// still holds deleted and trashed ones).
function liveItems() {
  const bin = trashFolder(), out = [];
  const walk = f => { for (const x of f.c) { if (isFolder(x)) { if (x !== bin) walk(x); } else { const i = ITEM.get(x); if (i) out.push(i); } } };
  walk(st().root); return out;
}
const updateIds = () => targetIds().filter(id => pendingUpdate(ITEM.get(id)));
function updateAllLabel() {
  const same = liveItems().filter(i => pendingUpdate(i) && i.nk === "same").length;
  const b = $("#updateAll"); $("#updateCount").textContent = same.toLocaleString(); $("#updateCount").hidden = !same; b.disabled = !same;
}
async function updateAllMoved() {
  const live = liveItems();
  const ids = live.filter(i => pendingUpdate(i) && i.nk === "same").map(i => i.id);
  const other = live.filter(i => pendingUpdate(i) && i.nk === "other").length;
  if (!ids.length) return toast("No moved links left to update");
  if (!(await askConfirm({title: `Update ${ids.length} moved links?`, ok: "Update links", danger: false,
    body: `Each one switches to the address it now redirects to on the same site. You can revert any of them.` +
      (other ? ` The ${other} links that now go to a different site are left for you to check one by one.` : "")}))) return;
  applyUpdates(ids);
}
on("#updateAll", updateAllMoved);

// ---------- drag and drop ----------
let dragging = null;
function dnd(d, r) {
  // Where a drop lands and what it does: dropZone(), dropAction() and afterTarget() in src/web/ts/tree-rules.ts.
  // Before, into or after any row; at the row's left edge after a folder's last row, one level out.
  const OUT = 28;  // px from the row's left edge that mean "one level out"
  // Measured from the row itself: e.offsetX/Y are relative to whatever child is under the pointer (the title, an icon),
  // which put drops near a row's bottom in its top half.
  const at = e => { const b = d.getBoundingClientRect(); return {x: e.clientX - b.left, y: e.clientY - b.top, h: b.height}; };
  const zoneOf = e => { const p = at(e); return dropZone(r, p.y, p.h); };
  d.addEventListener("dragstart", e => { dragging = dragged(r, marked); e.dataTransfer.effectAllowed = "move"; e.dataTransfer.setData("text/plain", "bookmarks"); });
  d.addEventListener("dragend", () => { dragging = null; clearDrop(); });
  d.addEventListener("dragover", e => {
    if (!dragging) return; e.preventDefault(); clearDrop();
    const z = zoneOf(e), out = z === "after" && at(e).x < OUT && dropAction(r, z, {outdent: true, rows})?.parent !== r.parent;
    d.classList.add(z === "into" ? "drop-into" : z === "before" ? "drop-before" : out ? "drop-after-out" : "drop-after");
  });
  d.addEventListener("dragleave", () => d.classList.remove("drop-into", "drop-before", "drop-after", "drop-after-out"));
  d.addEventListener("drop", e => {
    e.preventDefault(); if (!dragging) return;
    const c = d.classList, zone = c.contains("drop-into") ? "into" : c.contains("drop-before") ? "before" : "after";
    const act = dropAction(r, zone, {outdent: c.contains("drop-after-out"), rows}), xs = dragging; clearDrop(); dragging = null;
    if (!act) return;
    if (act.kind === "trash") { marked.clear(); xs.forEach(x => marked.add(x)); trashNodes(); }
    else if (act.kind === "into") moveNodes(xs, act.folder);
    else if (act.kind === "before") { materialize(); moveNodes(xs, act.parent, act.before); }
    else { materialize(); moveNodes(xs, act.parent, afterTarget(act.parent, act.after, xs)); }
  });
}
function clearDrop() { for (const x of list.querySelectorAll(".drop-into,.drop-before,.drop-after,.drop-after-out")) x.classList.remove("drop-into", "drop-before", "drop-after", "drop-after-out"); }

// ---------- commands: one list behind the context menu, the palette and the shortcuts sheet ----------
// [id, label, icon, shortcut, enabled(), run(), group, danger?]
function commands() {
  const r = rows[sel], ro = readOnly(), many = marked.size > 1, link = r && r.kind === "link";
  const ids = r ? targetIds() : [], nIds = ids.length, bin = trashFolder();
  const inBin = r && !r.trash && inTrash(r.node), what = many ? `${marked.size} items` : r && r.kind === "folder" ? "this folder" : "this link";
  const n = nIds > 1 ? ` ${nIds.toLocaleString()} links` : "";
  return [
    ["open", "Open" + (link && !many ? "" : n) + " in new tabs (this browser)", "ti-external-link", "O", () => nIds > 0, () => openLinks("browser"), "Open"],
    ["open-tabs", "Open" + n + ` as tabs in ${BROWSER_NAME}`, "ti-browser", "", () => nIds > 0, () => openLinks("tabs"), "Open"],
    ["open-window", "Open" + n + ` in a new ${BROWSER_NAME} window`, "ti-app-window", "⇧O", () => nIds > 0, () => openLinks("window"), "Open"],
    ["open-incognito", "Open" + n + ` in a new ${BROWSER_NAME} incognito window`, "ti-spy", "I", () => nIds > 0, () => openLinks("incognito"), "Open"],
    // One per browser on this Mac (the system default first), this time only: new tabs in that browser.
    ...OPEN_CHOICES.map(c => ["open-in:" + c.name, "Open" + n + " in " + c.label, c.ic, "", () => nIds > 0, () => openLinks("tabs", ids, false, c.name), "Open"]),
    ["move-to-tabs", "Move" + n + " to tabs (open in a new window, then to Trash)", "ti-arrow-right-bar", "", () => nIds > 0 && !readOnly(), () => moveToTabs(false), "Open"],
    ["move-to-tabs-incognito", "Move" + n + " to incognito tabs (open, then to Trash)", "ti-spy", "", () => nIds > 0 && !readOnly(), () => moveToTabs(true), "Open"],
    ["move-to-tabs-current", "Move" + n + " to tabs in the current window (then to Trash)", "ti-browser-plus", "", () => nIds > 0 && !readOnly(), () => moveToTabs("tabs"), "Open"],
    ["move-to-tabs-grouped", "Move" + n + " to tabs, one window per folder (then to Trash)", "ti-layout-columns", "", () => nIds > 0 && !readOnly(), () => moveToTabs("grouped"), "Open"],
    ["open-grouped", "Open" + n + " in one window per folder", "ti-layout-columns", "", () => nIds > 0, () => openLinks("grouped"), "Open"],
    ["live", "Load the live page here", "ti-world-www", "L", () => !!link, live, "Open"],
    ["copy", "Copy" + (n || " link"), "ti-copy", "C", () => nIds > 0, () => copyLinks(), "Open"],
    ["refresh", `Refresh ${what} (re-check, new snapshot)`, "ti-refresh", "R", () => !!r && !ro, () => refresh(), "Links"],
    ["refresh-all", "Refresh all bookmarks…", "ti-refresh-dot", "", () => !ro, () => refresh(true), "Links"],
    ["update", updateIds().length > 1 ? `Update ${updateIds().length} moved links to their new address` : "Update to the new address", "ti-arrow-forward-up", "U", () => !!r && !ro && updateIds().length > 0, () => applyUpdates(updateIds()), "Links"],
    ["update-all", "Update all same-site moved links…", "ti-arrow-forward-up", "", () => !ro, updateAllMoved, "Links"],
    ["move", many ? `Move ${marked.size} items to…` : "Move to…", "ti-folder-symlink", "M", () => !!r && !ro && !r.trash, moveDialog, "Organise"],
    ["group", many ? `Group ${marked.size} items into a new folder…` : "Group into a new folder…", "ti-stack-2", "G", () => !!r && !ro && !r.trash, () => newFolder(targets()), "Organise"],
    ["new-folder", "New folder here…", "ti-folder-plus", "N", () => !ro, () => newFolder(null), "Organise"],
    ["rename", "Rename…", "ti-pencil", "F2", () => !!r && !many && !ro && !r.trash, rename, "Organise"],
    ["describe", "Edit description…", "ti-notes", "D", () => !!r && !many && !ro && !r.node.virtual, editDescription, "Organise"],
    ["rate", many ? `Rate ${marked.size} items…` : "Rate (1–5, 0 clears)", "ti-star", "1–5", () => !!r && !ro && !r.trash, () => many ? askRateMany() : $("#info .star-rating .btn")?.focus(), "Organise"],
    ["tag-add", many ? `Tag ${marked.size} items…` : "Edit tags", "ti-tag", "T", () => !!r && !ro && !r.trash, () => many ? tagMany(true) : KEY_ACTIONS.tag(), "Organise"],
    ["up", "Move up", "ti-arrow-up", "⌥↑", () => !!r && !many && !ro, () => reorder(-1), "Organise"],
    ["down", "Move down", "ti-arrow-down", "⌥↓", () => !!r && !many && !ro, () => reorder(1), "Organise"],
    ["sort", "Sort this folder A–Z", "ti-sort-ascending-letters", "", () => !!r && !ro, sortFolder, "Organise"],
    ["trash", inBin ? "Delete permanently…" : many ? `Move ${marked.size} items to Trash` : "Move to Trash", inBin ? "ti-trash-x" : "ti-trash", "⌫", () => !!r && !ro && !r.trash, trashNodes, "Organise", true],
    ["putback", "Put back from Trash", "ti-arrow-back-up", "P", () => inBin && PARENT.get(r.node) === bin, putBack, "Organise"],
    ["openTrash", "Open the Trash", "ti-trash", "", () => !ro, openTrash, "Organise"],
    ["empty", "Empty trash…", "ti-trash-x", "", () => !!bin && bin.c.length > 0 && !ro, emptyTrash, "Organise", true],
    ["expand", r && r.kind === "folder" && r.open ? "Collapse folder" : "Expand folder", "ti-folder-open", "→ ←", () => r && r.kind === "folder", () => toggle(r, !r.open), "View"],
    ["expand-in", "Expand everything inside", "ti-chevrons-down", "*", () => r && r.kind === "folder", () => expandInside(r), "View"],
    ["expand-all", "Expand all folders", "ti-chevrons-down", "", () => true, () => { expandAllFolders(); list.focus(); }, "View"],
    ["collapse-all", "Collapse all folders", "ti-chevrons-up", "-", () => true, () => { collapseAllFolders(); list.focus(); }, "View"],
    ["search", "Search", "ti-search", "/", () => true, () => SEARCH.focus(), "View"],
    ["clear", "Clear all filters", "ti-filter-off", "", () => true, clearFilters, "View"],
    ...FILTERS.map(([k, l, ic]) => ["filter-" + k, "Show: " + l, ic, "", () => true, () => setFilter(k), "Filter"]),
    ...VIEW_OPTS[0][2].map(([v, t]) => ["group-" + v, "Group by: " + t, "ti-category", "", () => true, () => { setView("group", v); build(); }, "View"]),
    ...VIEW_OPTS[1][2].map(([v, t]) => ["sort-" + v, "Sort: " + t, "ti-arrows-sort", "", () => true, () => { setView("sort", v); build(); }, "View"]),
    ...VIEW_OPTS[2][2].map(([v, t]) => ["folders-" + v, t, "ti-folders", "", () => true, () => { setView("folders", v); build(); }, "View"]),
    ...DATE_PRESETS.map(([l, fn]) => ["date-" + l, "Added: " + l, "ti-calendar-time", "", () => true, () => setDates(...fn(), l), "Filter"]),
    ["select-all", "Select everything shown", "ti-checks", "⌘A", () => rows.length > 0, () => { rows.forEach(x => marked.add(x.node)); draw(); show(); }, "View"],
    ["undo", "Undo" + (UNDO_LABEL("undo") ? `: ${UNDO_LABEL("undo")}` : ""), "ti-arrow-back-up", "⌘Z", () => !!UNDO_LABEL("undo"), () => UNDO.undo(), "General"],
    ["redo", "Redo" + (UNDO_LABEL("redo") ? `: ${UNDO_LABEL("redo")}` : ""), "ti-arrow-forward-up", "⇧⌘Z", () => !!UNDO_LABEL("redo"), () => UNDO.redo(), "General"],
    ["download", "Download the import file", "ti-download", "", () => true, downloadFile, "General"],
    ["theme", "Switch dark / light", "ti-moon", "", () => true, () => $("#themeBtn").click(), "General"],
    ["settings", "Settings…", "ti-settings", ",", () => true, openSettings, "General"],
    ["view-tabs", "Open tabs: windows, snapshots, reopen after a crash", "ti-browser", "", () => true, () => setAppView("tabs"), "Open tabs"],
    ["view-timeline", "Timeline: what opened, closed, changed and was added", "ti-timeline", "", () => true, () => setAppView("timeline"), "Open tabs"],
    ["tabs-save", "Save open tabs now", "ti-camera", "", () => true, () => { setAppView("tabs"); TABS.snapNow(); }, "Open tabs"],
    ["view-bookmarks", "Back to bookmarks", "ti-bookmarks", "", () => view_ !== "bookmarks", () => setAppView("bookmarks"), "Open tabs"],
    ["import", "Import bookmarks…", "ti-file-import", "", () => true, () => IO.importer(), "Import & export"],
    ["browser-history", "Browser history…", "ti-history-toggle", "", () => !!wsUI()?.browserHistory, () => wsUI().browserHistory(), "Import & export"],
    ["add-files", "Add bookmarks to this workspace…", "ti-file-plus", "", () => !!wsUI()?.addFiles, () => wsUI().addFiles(), "Import & export"],
    ["export-html", "Export: import file for any browser (HTML)", "ti-download", "", () => true, () => IO.exportHtml(), "Import & export"],
    ["export-chrome", "Export: Chrome bookmarks JSON", "ti-braces", "", () => true, () => IO.exportChrome(), "Import & export"],
    ["export-dupes", "Export: merged duplicates (CSV)", "ti-file-spreadsheet", "", () => true, () => IO.duplicates(), "Import & export"],
    ["history", "History and progress…", "ti-history", "", () => true, () => IO.history(), "Workspace"],
    ["workspaces", "Workspaces… (switch, delete, restore)", "ti-stack-2", "", () => true, () => IO.workspaces(), "Workspace"],
    ["ws-lock", "Lock or unlock this workspace (read-only)", "ti-lock", "", () => !!wsUI()?.toggleLock, () => wsUI().toggleLock(!window.BM_WS.locked), "Workspace"],
    ["ws-delete", "Delete this workspace… (to the workspace trash)", "ti-trash", "", () => !!wsUI()?.deleteWs && window.BM_WS.id !== "main", () => wsUI().deleteWs(), "Workspace", true],
    ["mcp-access", "MCP access (Claude): connect, disconnect, allow changes…", "ti-plug-connected", "", () => !!wsUI()?.mcpAccess, () => wsUI().mcpAccess(), "Workspace"],
    ["ws-update", "Update this workspace from its source…", "ti-refresh-dot", "", () => !!wsUI()?.updateWs, () => wsUI().updateWs(), "Workspace"],
    ["ws-reset", "Reset this workspace to its start…", "ti-restore", "", () => !!wsUI()?.resetWs, () => wsUI().resetWs(), "Workspace", true],
    ["reset-edits", "Reset edits…", "ti-restore", "", () => !readOnly(), resetEdits, "General", true],
    ["factory-reset", "Factory reset…", "ti-alert-triangle", "", () => true, factoryReset, "General", true],
    ["shortcuts", "Keyboard shortcuts", "ti-keyboard", "?", () => true, openShortcuts, "General"],
    ["help", "Help", "ti-help-circle", "F1", () => true, () => openAbout("help"), "General"],
    ["tour", "Take the guided tour", "ti-route", "", () => true, () => tour(0), "General"],
    ["about", "About this tool (version, author, licence)", "ti-info-circle", "", () => true, () => openAbout("about"), "General"],
    ["privacy", "Privacy and safety", "ti-shield-check", "", () => true, () => openAbout("privacy"), "General"],
    ["palette", "Command palette", "ti-command", "⌘K", () => true, openPalette, "General"],
  ];
}

// Context menu: a header saying what it acts on, then labelled sections of the commands that apply.
const ctx = $("#ctx");
// [section, [[command id, short label (null = the command's own)]]]
// The browser links open in (Browsers button); menus name it instead of assuming Chrome.
// ---------- which browsers' tabs are shown: one, some, or all (kept in this browser) ----------
// Empty set = all. Filters Open tabs, its summary and every action there (open, close, clean up, restore).
// "Brave Browser and Safari", "Chrome, Brave Browser and Safari".
const listNames = ns => ns.length < 2 ? (ns[0] || "") : ns.slice(0, -1).join(", ") + " and " + ns[ns.length - 1];
// A browser's state in a live read: "3 windows", "not running", or why it could not be read.
function browserState(d, name) {
  const b = (d?.browsers || []).find(x => x.browser === name);
  if (!b) return "not tracked (Browsers and access…)";
  if (b.error) return b.error === "needs-permission" ? "needs permission to read its tabs" : b.error;
  if (b.running === false) return "not running";
  const n = (b.windows || []).length;
  return n ? `${n} window${n === 1 ? "" : "s"}` : "running, no windows";
}
// What a view shows when no browser is chosen: say so, and offer the way back.
function noneChosen(where) {
  const n = el("div", "empty py-5");
  n.append(el("div", "empty-icon"), el("p", "empty-title", "No browsers chosen"),
    el("p", "empty-subtitle text-secondary", where === "tabs" ? "Tabs and snapshots are shown for the browsers ticked in the Browsers menu." : "Tab snapshots in the timeline follow the Browsers menu. Edits and bookmarks added are still listed when their kinds are on."));
  n.firstChild.append(icon("ti-world-off"));
  const b = el("button", "btn btn-primary"); b.append(icon("ti-world", "me-1"), "Show all browsers"); b.onclick = () => SCOPE.setAll(true);
  const a = el("div", "empty-action"); a.append(b); n.append(a);
  return n;
}
// Which browsers to show (all, some or none): src/web/ts/scope.ts. A change re-reads every view that depends on it.
const SCOPE = new BrowserScope(STORE + ":scope").onChange(() => {
  paintScope();
  window.dispatchEvent(new CustomEvent("bf:scope"));
});
const inScope = name => SCOPE.has(name);
let LIVE_STATE = null;  // the last live read, so the Browsers menu can say which browsers are running
window.BF_IN_SCOPE = inScope;  // the clean-up panel (components.ts) asks the same question
function paintScope() {
  let wrap = Dom.one("scope");
  if (!wrap) { wrap = Dom.write(el("div", "dropdown"), "scope", ""); $("#bkBrowsers")?.before(wrap); }
  // One Browsers menu: which browsers to show, then the dialog for tracking, opening links and access.
  const names = SCOPE.all, b = el("button", "btn nav-browsers" + (SCOPE.isNone ? " text-warning" : "")); b.setAttribute("data-dd", "");
  b.append(icon(SCOPE.isNone ? "ti-world-off" : "ti-world", "me-1"), SCOPE.label(), icon("ti-chevron-down", "ms-1"));
  b.dataset.tip = "Which browsers to show (all, some or none); tracking, opening links and access";
  const m = el("div", "dropdown-menu dropdown-menu-end");
  const row = (label, checked, fn, mixed = false, note = "", ic = null) => { const l = el("label", "dropdown-item d-flex align-items-center gap-2"); const c = el("input", "form-check-input m-0"); c.type = "checkbox"; c.checked = checked; c.indeterminate = mixed;
    c.onchange = () => fn(c.checked); l.append(c, ...(ic ? [icon(ic)] : []), el("span", "flex-fill", label)); if (note) l.append(el("span", "dd-note text-secondary small", note)); l.onclick = e => e.stopPropagation(); m.append(l); };
  if (names.length) {
    m.append(el("h6", "dropdown-header", "Show tabs and history from"));
    row("All browsers", SCOPE.isAll, on => SCOPE.setAll(on), !SCOPE.isAll && !SCOPE.isNone);
    for (const n of names) row(n, SCOPE.has(n), on => SCOPE.toggle(n, on), false, LIVE_STATE ? browserState(LIVE_STATE, n) : "", "browser:" + n);
    m.append(el("div", "dropdown-divider"));
  }
  const manage = el("a", "dropdown-item"); manage.href = "#"; manage.append(icon("ti-settings", "me-2"), "Browsers and access…");
  manage.onclick = e => { e.preventDefault(); closeDropdowns(); window.BF?.browsers(); };
  m.append(manage);
  // Everything about the browser extension lives here too: install it per browser, connect, disconnect.
  if (wsUI()?.extensions) {
    const ext = el("a", "dropdown-item"); ext.href = "#"; ext.append(icon("ti-puzzle", "me-2"), "Browser extensions…");
    ext.onclick = e => { e.preventDefault(); closeDropdowns(); void wsUI().extensions(); };
    m.append(ext);
  }
  wrap.replaceChildren(b, m);
}
// One chosen browser is named ("New Brave Browser window"); none yet, or several at once, reads "browser"
// ("New browser window"), since the links then open in each chosen browser.
let BROWSER_NAME = "browser";
// Every browser links can open in, for the "Open in…" choice: the system default first, then each installed one.
let OPEN_CHOICES = [];
addEventListener("bf:browsers", e => applyBrowsers(e.detail));
// The browser list can arrive before this script runs (the Browsers component loads first): read the latest one too.
queueMicrotask(() => { if (window.BF_BROWSERS) applyBrowsers(window.BF_BROWSERS); });
function applyBrowsers(detail) {
  const e = {detail};
  const opens = e.detail.opens || (e.detail.openWith ? [e.detail.openWith] : []);
  const sys = e.detail.systemDefault;
  BROWSER_NAME = opens.length === 1 ? (opens[0] === "System default" ? (sys || "default browser") : opens[0]) : "browser";
  OPEN_CHOICES = [{name: "System default", label: sys ? `System default (${sys})` : "System default", ic: sys ? "browser:" + sys : "ti-device-desktop"},
    ...(e.detail.installed || []).map(n => ({name: n, label: n, ic: "browser:" + n}))];
  if (e.detail.installed) { SCOPE.known(e.detail.installed); paintScope(); }
  try { paintIoMenu(); } catch {}
  try { if (view_ === "bookmarks" && !modalOpen()) show(); } catch {}
}
const CTX_SECTIONS = [
  // Short labels here: the palette's full ones explain more than a menu row has room for.
  ["Open", [["open", "In new tabs (this page's browser)"], ["open-tabs", "As tabs in the current window"], ["open-window", "In a new window"],
    ["open-grouped", "One window per folder"], ["open-incognito", "In a new incognito window"], ["live", "Load the live page here"], ["copy", null]]],
  ["Move to tabs, then Trash", [["move-to-tabs", "Into a new window"], ["move-to-tabs-current", "Into the current window"],
    ["move-to-tabs-grouped", "One window per folder"], ["move-to-tabs-incognito", "Into an incognito window"]]],
  ["Check", [["refresh", "Re-check and take a new snapshot"], ["update", null]]],
  ["Organise", [["move", null], ["group", null], ["new-folder", null], ["rename", null], ["describe", null], ["up", null], ["down", null], ["sort", null]]],
  ["View", [["expand", null], ["expand-in", null]]],
  ["Trash", [["putback", null], ["trash", null], ["empty", null]]],
  ["View", [["expand-all", "Expand all folders"], ["collapse-all", "Collapse all folders"]]],
  ["Trash", [["putback", "Put back"], ["empty", "Empty trash…"]]],
  ["", [["undo", "Undo"], ["redo", "Redo"]]],
];
// What the right-click menu offers depends on what was clicked. Each plan lists command ids by section; the short
// labels come from CTX_SECTIONS, and a command whose own rule says it does not apply still drops out.
const SHORT = Object.fromEntries(CTX_SECTIONS.flatMap(([, es]) => es.filter(([, l]) => l)));
// What the right-click menu offers, by what was clicked: a plan for layoutMenu() (src/web/ts/ctxmenu.ts). Groups become
// submenus only when they hold more than one thing that applies; a command whose own rule says no drops out.
const G_OPEN_IN = (grouped) => ({group: "Open in…", ic: "ti-app-window", items: ["open-tabs", "open-window", ...(grouped ? ["open-grouped"] : []), "open-incognito",
  "-", {group: "Another browser", ic: "ti-world", items: ["open-in"]}]});
const G_TO_TABS = (grouped) => ({group: "Move to tabs, then Trash", ic: "ti-arrow-bar-to-up", items: ["move-to-tabs", "move-to-tabs-current", ...(grouped ? ["move-to-tabs-grouped"] : []), "move-to-tabs-incognito"]});
const G_CHECK = {group: "Check", ic: "ti-refresh", items: ["refresh", "update"]};
const G_VIEW = (...ids) => ({group: "View", ic: "ti-eye", items: ids});
const G_ORG = (...ids) => ({group: "Organise", ic: "ti-folders", items: ids});
const UNDO_ROW = ["-", "undo", "redo"];
function ctxPlan() {
  const r = rows[sel], bin = trashFolder();
  if (!r) return ["new-folder", G_VIEW("expand-all", "collapse-all"), ...UNDO_ROW];
  const inBin = !r.trash && inTrash(r.node);
  if (marked.size > 1) return ["open", G_OPEN_IN(true), "copy", "-", G_TO_TABS(true), G_CHECK, "-", "move", "group",
    "-", ...(inBin ? ["putback"] : []), "trash", ...UNDO_ROW];
  if (r.trash) return ["empty", G_VIEW("expand"), ...UNDO_ROW];  // the Trash folder itself
  if (inBin) return ["putback", "trash", "-", ...(r.kind === "link" ? ["open", "copy"] : []), ...UNDO_ROW];
  if (r.node.virtual) return ["open", G_OPEN_IN(true), "-", G_TO_TABS(true), G_VIEW("expand", "expand-in"), ...UNDO_ROW];
  if (r.kind === "link") return ["open", G_OPEN_IN(false), "live", "copy", "-", G_TO_TABS(false), G_CHECK,
    "-", "move", "rename", G_ORG("describe", "up", "down"), "-", "trash", ...UNDO_ROW];
  const top = r.depth === 0 && PARENT.get(r.node) === st().root && r.node !== bin;  // a browser root, like Bookmarks Bar
  if (top) return ["open", G_OPEN_IN(true), "-", G_CHECK, "-", G_ORG("new-folder", "sort", "describe"), G_VIEW("expand", "expand-in"), ...UNDO_ROW];
  return ["open", G_OPEN_IN(true), "-", G_TO_TABS(true), G_CHECK, "-", "move", "rename",
    G_ORG("new-folder", "group", "describe", "sort", "up", "down"), G_VIEW("expand", "expand-in"), "-", "trash", ...UNDO_ROW];
}
function menuHeader() {
  const r = rows[sel];
  if (marked.size > 1) return [`${marked.size} items selected`, `${targetIds().length.toLocaleString()} links in all`, "ti-checks"];
  if (!r) return ["Bookmarks", "", "ti-bookmarks"];
  if (r.kind === "folder") {
    const n = descendants(r.node).length;
    return [nice(r.node.n), (r.trash ? "Trash" : r.node.virtual ? "Group" : "Folder") + ` · ${n.toLocaleString()} bookmark${n === 1 ? "" : "s"}` + (r.node.virtual ? "" : ` · ${nice(pathOf(PARENT.get(r.node))) || "top level"}`), r.trash ? "ti-trash" : "ti-folder"];
  }
  const i = r.item;
  return [titleOf(r.node) || i.u, `Bookmark · ${host(i.u)} · ${i.s === "ok" ? "working" : (LABEL[i.s] || i.s).split(" —")[0]}`, "ti-bookmark"];
}
function openMenu(x, y) {
  const all = Object.fromEntries(commands().map(c => [c[0], c])), r = rows[sel];
  const nIds = r ? targetIds().length : 0;
  ctx.replaceChildren();
  const [title, desc, ic] = menuHeader();
  const head = el("div", "px-3 pt-2 pb-2 mb-1 border-bottom d-flex gap-2 align-items-start");
  const ht = el("div", "min-w-0"); ht.append(el("div", "fw-semibold text-break", title)); if (desc) ht.append(el("div", "text-secondary small text-break", desc));
  head.append(icon(ic, "text-primary mt-1"), ht); ctx.append(head);
  // Each id resolves to its command if it applies; "open-in" stands for one entry per browser (the system default first).
  const single = r && r.kind === "link" && marked.size <= 1;
  const TOP = {open: single ? "Open in a new tab" : nIds > 1 ? `Open ${nIds.toLocaleString()} in new tabs` : "Open in new tabs"};
  const entry = id => {
    const c = all[id]; if (!c || !c[4]()) return null;
    return {label: TOP[id] || SHORT[id] || c[1], short: SHORT[id] || (id.startsWith("open-in:") ? c[1].replace(/^Open( [\d,]+ links)? in /, "") : undefined),
      ic: c[2], key: c[3], danger: !!c[7], run: () => { closeCtx(); c[5](); }};
  };
  const expand = n => n === "open-in" ? OPEN_CHOICES.map(c => "open-in:" + c.name) : typeof n === "object" && n !== null ? {...n, items: n.items.flatMap(expand)} : [n];
  const items = layoutMenu(ctxPlan().flatMap(expand), entry);
  const built = buildMenu(items);
  ctx.append(...[...built.children]);
  for (const a of ctx.querySelectorAll(".dropdown-item")) a.tabIndex = -1;
  if (!ctx.querySelector(".dropdown-item")) ctx.append(el("div", "text-secondary small px-3 py-2", "Nothing to do here"));
  ctx.classList.add("show");
  // Open to the right of and below the pointer; flip left or up when that side has no room, like a native menu.
  ctx.style.maxHeight = ""; let w = ctx.offsetWidth, h = ctx.offsetHeight;
  if (h > innerHeight - 16) { ctx.style.maxHeight = (innerHeight - 16) + "px"; ctx.style.overflowY = "auto"; h = innerHeight - 16; }
  let left = x + w + 8 > innerWidth && x - w > 8 ? x - w : x;
  let top = y + h + 8 > innerHeight && y - h > 8 ? y - h : y;
  ctx.style.left = Math.max(8, Math.min(left, innerWidth - w - 8)) + "px"; ctx.style.top = Math.max(8, Math.min(top, innerHeight - h - 8)) + "px";
  ctx.focus(); ctxItemsIn(ctx)[0]?.focus();
}
// Keyboard: ↑ ↓ move within the menu level that has the focus, → opens a submenu (menu.ts), ← or Esc leaves one, Esc
// on the top level closes the menu, Enter or Space runs the item.
const ctxItemsIn = m => [...m.querySelectorAll(":scope > .dropdown-item, :scope > .dd-subwrap > .dropdown-item")];
function closeCtx() { ctx.classList.remove("show"); for (const s of ctx.querySelectorAll(".dd-sub.show")) s.classList.remove("show"); list.focus(); }
ctx.addEventListener("keydown", e => {
  e.stopPropagation();
  const cur = document.activeElement, level = cur?.closest?.(".dropdown-menu") || ctx, items = ctxItemsIn(level);
  const k = items.indexOf(cur);
  if (e.key === "ArrowDown" || e.key === "ArrowUp") {
    e.preventDefault(); if (!items.length) return;
    const next = items[(k + (e.key === "ArrowDown" ? 1 : -1) + items.length) % items.length]; next.focus(); next.scrollIntoView({block: "nearest"});
  } else if (e.key === "Enter" || e.key === " ") {
    e.preventDefault(); cur?.classList.contains("dd-has-sub") ? cur.dispatchEvent(new KeyboardEvent("keydown", {key: "ArrowRight", bubbles: true})) : cur?.click();
  } else if (e.key === "Escape" || e.key === "ArrowLeft") {
    e.preventDefault();
    if (level !== ctx) { level.classList.remove("show"); level.parentElement?.querySelector(":scope > .dropdown-item")?.focus(); }
    else closeCtx();
  }
});
document.addEventListener("mousedown", e => { if (ctx.classList.contains("show") && !ctx.contains(e.target)) ctx.classList.remove("show"); });
// Right-click on a tile: select that bookmark or folder in the tree, then show the same menu as the tree.
function tileMenu(e, parent, node) {
  e.preventDefault(); e.stopPropagation(); hideTip();
  // Select it in the tree (so every command acts on it) without replacing the folder view on the right.
  if (!readOnly() && !parent.open) { setOpen(parent, true); saveOpen(); build(true); }
  const k = rows.findIndex(r => r.node === node);
  if (k < 0) return;
  marked.clear(); sel = k; draw();
  for (const t of grid.querySelectorAll(".tile, .folder-card")) t.classList.toggle("menu-target", t === e.currentTarget);
  openMenu(e.clientX, e.clientY);
}
// The snapshot and the detail pane use the menu for what is shown.
for (const id of ["#shot", "#info", "#infoHead"]) $(id).addEventListener("contextmenu", e => { if (e.target.closest("a")) return; e.preventDefault(); if (rows[sel]) openMenu(e.clientX, e.clientY); });
function menuAtSelection() { const d = rowEl(sel); const b = d ? d.getBoundingClientRect() : {left: 40, bottom: 120}; openMenu(b.left + 40, b.bottom); }

// Command palette: every command, searchable, with its shortcut.
// The command palette and the shortcuts sheet are typed (views/palette.ts); these keep the old names.
const PALETTE = new CommandPalette(() => commands().map(toCommand));
function openPalette() { PALETTE.open(); }
const NAV_KEYS = [["↑ ↓", "Move through the list"], ["⇧↑ ⇧↓", "Extend the selection"], ["→ ←", "Open / close a folder, or go up"],
  ["Space", "Select or unselect"], ["⌘-click / ⇧-click", "Select several"], ["Enter", "Open a link / a folder"], ["Home End", "First / last row"],
  [". or right-click", "Context menu"], ["Esc", "Clear the selection, close a dialog"],
  ["1 … 5", "Rate the bookmark (stars)"], ["0", "Clear its rating"], ["T", "Edit its tags"]];
function openShortcuts() {
  PALETTE.shortcuts(NAV_KEYS);
  showModal("#keysModal");
}

// ---------- "Move to…" and every other "which folder?" question ----------
// chooseFolder: a searchable list of folders (keyboard: ↑ ↓ Enter Esc). Resolves with the folder node, an `extra`
// entry's value, or null when closed. moveDialog and "save tabs to bookmarks" both use it.
let moveSel = 0, moveList = [], moveResolve = null;
function chooseFolder({title, exclude = () => false, extra = []}) {
  moveResolve?.(null);
  return new Promise(resolve => {
    moveResolve = resolve;
    const all = [], bin = trashFolder();
    const walk = f => { for (const x of f.c) if (isFolder(x) && x !== bin) { all.push(x); walk(x); } };
    walk(st().root);
    $("#moveTitle").replaceChildren(icon("ti-folder-symlink", "me-2"), title);
    $("#moveIn").value = "";
    const pick = k => { const it = moveList[k]; if (!it) return; const r = moveResolve; moveResolve = null; hideModal("#moveModal"); r?.(it[0]); };
    const paint = () => {
      const q = $("#moveIn").value.trim().toLowerCase();
      const fixed = extra.filter(([, label]) => !q || label.toLowerCase().includes(q)).map(([value, label, ic]) => [value, label, ic]);
      moveList = [...fixed, ...all.filter(f => !exclude(f)).map(f => [f, nice(pathOf(f)), "ti-folder"]).filter(([, p]) => !q || p.toLowerCase().includes(q)).slice(0, 300)];
      moveSel = Math.min(moveSel, Math.max(0, moveList.length - 1));
      $("#moveList").replaceChildren(...moveList.map(([, p, ic], k) => { const o = el("a", "list-group-item list-group-item-action" + (k === moveSel ? " active" : "")); o.append(icon(ic || "ti-folder"), el("span", null, p)); o.onclick = () => pick(k); return o; }));
      $("#moveList").children[moveSel]?.scrollIntoView({block: "nearest"});
    };
    $("#moveIn").oninput = () => { moveSel = 0; paint(); };
    $("#moveIn").onkeydown = e => {
      e.stopPropagation();
      if (e.key === "ArrowDown") { e.preventDefault(); moveSel = Math.min(moveSel + 1, moveList.length - 1); paint(); }
      else if (e.key === "ArrowUp") { e.preventDefault(); moveSel = Math.max(moveSel - 1, 0); paint(); }
      else if (e.key === "Enter") { e.preventDefault(); pick(moveSel); }
      else if (e.key === "Escape") hideModal("#moveModal");
    };
    moveSel = 0; paint(); showModal("#moveModal"); setTimeout(() => $("#moveIn").focus(), 30);
  });
}
async function moveDialog() {
  const xs = targets(); if (!xs.length) return;
  const f = await chooseFolder({title: `Move ${xs.length === 1 ? "“" + (isFolder(xs[0]) ? nice(xs[0].n) : titleOf(xs[0])) + "”" : xs.length + " items"} to…`,
    exclude: f => xs.some(x => isFolder(x) && contains(x, f))});
  if (f) moveNodes(xs, f);
}

// ---------- detail pane ----------
function reset() {
  hideTip();
  info.replaceChildren(); $("#infoHead").replaceChildren(); $("#infoActs").replaceChildren();
  frame.hidden = true; frame.removeAttribute("src");
  shot.hidden = true; shot.removeAttribute("src"); grid.hidden = true; grid.replaceChildren(); $("#msg").hidden = true;
}
function message(t) { $("#msg").hidden = false; $("#msgText").textContent = t; }
function head(ic, title, sub) {
  const h = el("div", "d-flex gap-2 align-items-start");
  h.append(ic, el("div", "min-w-0"));
  h.lastChild.append(el("h3", "card-title mb-0 text-break", title));
  if (sub) h.lastChild.append(el("div", "text-secondary small text-break", sub));
  $("#infoHead").append(h);
}
function act(ic, label, fn, cls = "") { const b = el("button", "btn btn-sm " + cls); b.append(icon(ic, "me-1"), label); b.onclick = () => { fn(); list.focus(); }; $("#infoActs").append(b); return b; }
// A header button that opens a menu. Items: {h: "Section"}, "-", or {l, ic, fn, k, danger, sub: [items]}.
// A `sub` makes a submenu, opened by hover, click or →; it opens to the right, or to the left when there is no room.
function menuButton(label, ic, items, cls = "") {
  const wrap = el("div", "dropdown"), b = el("button", "btn btn-sm " + cls); b.type = "button"; b.setAttribute("data-dd", "");
  if (ic) b.append(icon(ic, label ? "me-1" : "")); if (label) b.append(label); b.append(icon("ti-chevron-down", "ms-1 dd-chev"));
  if (!label) b.setAttribute("aria-label", "More actions");
  wrap.append(b, buildMenu(items)); $("#infoActs").append(wrap); return wrap;
}
function menuButtonEl(label, ic, items, cls = "") {
  const wrap = el("div", "dropdown"), b = el("button", "btn btn-sm " + cls); b.type = "button"; b.setAttribute("data-dd", "");
  if (ic) b.append(icon(ic, label ? "me-1" : "")); if (label) b.append(label); b.append(icon("ti-chevron-down", "ms-1 dd-chev"));
  wrap.append(b, buildMenu(items)); return wrap;
}
// "Move to" as nested menus: each folder is a submenu of its subfolders (two levels), "Choose any folder…" for the rest.
function moveToItems() {
  const xs = targets(), bin = trashFolder();
  const ok = f => f !== bin && !xs.some(x => isFolder(x) && contains(x, f));
  const level = (f, depth) => {
    const subs = ordered(f).filter(x => isFolder(x) && ok(x)).slice(0, 40);
    return subs.map(x => {
      const kids = depth < 2 ? ordered(x).filter(y => isFolder(y) && ok(y)) : [];
      const here = {l: nice(x.n), ic: "ti-folder", fn: () => moveNodes(xs, x)};
      return kids.length ? {l: nice(x.n), ic: "ti-folder", sub: [{l: `Into “${nice(x.n)}”`, ic: "ti-corner-down-right", fn: () => moveNodes(xs, x)}, "-", ...level(x, depth + 1)]} : here;
    });
  };
  // One top-level folder (Bookmarks Bar) is a level that tells nothing: start inside it.
  const tops = st().root.c.filter(x => isFolder(x) && x !== bin);
  const first = tops.length === 1 && ok(tops[0]) ? [{l: `Into “${nice(tops[0].n)}”`, ic: "ti-corner-down-right", fn: () => moveNodes(xs, tops[0])}, "-", ...level(tops[0], 1)] : level(st().root, 0);
  return [...first, "-", {l: "Choose any folder…", ic: "ti-search", k: "M", fn: moveDialog}];
}
// Every way to open or move the selected bookmarks; the right-click menu, the details Open menu and the folder Open
// menu all use this one list.
function openItems(single) {
  const ids = targetIds(), n = ids.length, folders = new Set(ids.map(id => PARENT.get(id))).size;
  return [{h: single ? "Open this link" : `Open ${n.toLocaleString()} links`},
    {l: "New tabs (this browser)", ic: "ti-external-link", k: "O", fn: () => openLinks("browser")},
    {l: `Tabs in the current ${BROWSER_NAME} window`, ic: "ti-browser", fn: () => openLinks("tabs")},
    {l: `New ${BROWSER_NAME} window`, ic: "ti-app-window", k: "⇧O", fn: () => openLinks("window")},
    folders > 1 ? {l: `One ${BROWSER_NAME} window per folder (${folders})`, ic: "ti-layout-columns", fn: () => openLinks("grouped")} : null,
    {l: "New incognito window", ic: "ti-spy", k: "I", fn: () => openLinks("incognito")},
    // Any one browser on this Mac, this time only (Browsers › Opens links sets the usual choice).
    OPEN_CHOICES.length > 1 ? {l: "Open in…", ic: "ti-world", sub: OPEN_CHOICES.map(c => ({l: c.label, ic: c.ic, fn: () => openLinks("tabs", ids, false, c.name)}))} : null,
    ...(readOnly() ? [] : ["-", {h: single ? "Move to tabs (then Trash)" : `Move ${n.toLocaleString()} to tabs (then Trash)`},
      {l: "Into a new window", ic: "ti-arrow-bar-to-up", fn: () => moveToTabs("window")},
      {l: "Into the current window", ic: "ti-browser-plus", fn: () => moveToTabs("tabs")},
      folders > 1 ? {l: "One window per folder", ic: "ti-layout-columns", fn: () => moveToTabs("grouped")} : null,
      {l: "Into an incognito window", ic: "ti-spy", fn: () => moveToTabs("incognito")}]),
    "-", {l: single ? "Copy link" : "Copy links", ic: "ti-copy", k: "C", fn: copyLinks}];
}
// Section cards, alerts, fact grids, the folder summary and the "All details" panel: src/web/ts/views/details.ts.
const alertBox = (type, ic, title, text, actions = []) => alertBoxOf(type, ic, title, text, actions, () => list.focus());
// The status of one link, said plainly, with what to do about it.
// The rules are typed (views/link-panel.ts); the page wires each action and draws the alert.
function statusAlert(i, id) {
  const spec = linkAlert(i, {readOnly: readOnly(), inTrash: inTrash(id), help: HELP, statusName: STATUS_NAME, statusIcon: STATUS_ICON});
  if (!spec) return null;
  const run = {recheck: refresh, retry: refresh, putBack, trash: trashNodes, update: () => applyUpdates([id]), openNew: () => openTab(i.nu),
    revert: () => revertUpdate(id), open: () => openTab(i.u)};
  return alertBox(spec.tone, spec.icon, spec.title, spec.body, spec.actions.map(([l, ic, k, cls]) => cls === undefined ? [l, ic, run[k]] : [l, ic, run[k], cls]));
}
const DETAILS = new DetailsPanel({
  title: f => f.virtual ? nice(f.n) : nice(pathOf(f)) || nice(f.n),
  items: f => descendants(f).map(id => ITEM.get(id)),
  host, canFilter: k => FILTERS.some(x => x[0] === k), filter: k => setFilter(k), help: HELP,
  hideTip, closed: () => list.focus(),
});
const openDetails = (f, focus) => DETAILS.open(f, focus);
const closeDetails = () => DETAILS.close();
const folderSummary = (f, its) => folderSummaryOf(f, its, {canFilter: k => FILTERS.some(x => x[0] === k), filter: k => setFilter(k), openDetails, help: HELP});
const descendFolders = countFolders;
function show() {
  reset();
  const r = rows[sel];
  if (r && !restoringPlace && view_ === "bookmarks") PLACE.update({node: nodeKey(r)});
  if (!r) return message("Nothing selected.");
  if (marked.size > 1) {
    head(el("span", "avatar avatar-sm bg-primary-lt", String(marked.size)), `${marked.size} items selected`, `${targetIds().length} links in all`);
    menuButton("Open", "ti-external-link", openItems(false));
    menuButton("Actions", "ti-dots", [{h: "Organise"}, {l: "Move to", ic: "ti-folder-symlink", sub: moveToItems()},
      {l: "Group into a new folder…", ic: "ti-stack-2", k: "G", fn: () => newFolder(targets())},
      {l: "Refresh", ic: "ti-refresh", k: "R", fn: refresh},
      ...(readOnly() ? [] : [{h: "Label them"}, {l: "Rate", ic: "ti-star", sub: [1, 2, 3, 4, 5].map(v => ({l: "★".repeat(v), fn: () => rateMany(v)})).concat([{l: "Clear the rating", ic: "ti-star-off", fn: () => rateMany(0)}])},
        {l: "Add a tag…", ic: "ti-tag", fn: () => tagMany(true)}, {l: "Remove a tag…", ic: "ti-tag-off", fn: () => tagMany(false)}]),
      "-", {l: "Move to Trash", ic: "ti-trash", k: "⌫", danger: true, fn: trashNodes}]);
    info.append(el("p", "text-secondary mb-0", "Open them, move them (M), group them (G), refresh them (R), rate and tag them (Actions), send them to the Trash (⌫), or drag them onto a folder."));
    return;
  }
  if (r.kind === "folder") showFolder(r); else showLink(r.item, r.node);
}

function showFolder(r) {
  const f = r.node, ids = descendants(f), its = ids.map(id => ITEM.get(id));
  head(el("span", "avatar avatar-sm " + (r.trash ? "bg-red-lt" : "bg-primary-lt"), "").appendChild(icon(r.trash ? "ti-trash" : f.virtual ? "ti-category" : "ti-folder-open")).parentNode,
    readOnly() || f.virtual ? nice(f.n) : nice(pathOf(f)), `${ids.length.toLocaleString()} bookmarks`);
  if (!readOnly()) {
    if (r.trash) act("ti-trash-x", "Empty trash…", emptyTrash, "btn-danger");
    else {
      // A top-level folder (Bookmarks Bar) is the root of the import: it can't be moved or trashed.
      const top = !f.virtual && st().root.c.includes(f);
      if (ids.length) menuButton("Open", "ti-external-link", openItems(false));
      menuButton("Actions", "ti-dots", [
        ids.length ? {l: "All details", ic: "ti-layout-sidebar-right-expand", fn: () => openDetails(f)} : null,
        {h: "Check"}, {l: `Refresh ${ids.length.toLocaleString()} links`, ic: "ti-refresh", k: "R", fn: refresh},
        {h: "Edit"}, f.virtual ? null : {l: "Rename…", ic: "ti-pencil", k: "F2", fn: rename},
        f.virtual ? null : {l: f.d ? "Edit description…" : "Add description…", ic: "ti-notes", k: "D", fn: editDescription},
        {h: "Organise"}, top ? null : {l: f.virtual ? "Move all to" : "Move to", ic: "ti-folder-symlink", sub: moveToItems()},
        f.virtual ? null : {l: "New folder inside…", ic: "ti-folder-plus", k: "N", fn: () => newFolder()},
        f.virtual || top ? null : "-", f.virtual || top ? null : {l: "Move folder to Trash", ic: "ti-trash", k: "⌫", danger: true, fn: trashNodes}]);
    }
  }
  if (f.d) info.append(el("p", "lead-text mb-3", f.d));
  if (r.trash) { info.append(alertBox("secondary", "ti-trash", "Left out of the download", "Everything in the Trash is left out of the import file. Put back (P) restores an item where it was; Empty trash removes them for good.")); }
  else if (ids.length) info.append(folderSummary(f, its));
  grid.hidden = false;
  const subs = ordered(f).filter(isFolder), links = ordered(f).filter(x => !isFolder(x));
  if (subs.length) {
    grid.append(el("div", "subheader", `Folders · ${subs.length}`));
    const fg = el("div", "folder-grid"); grid.append(fg);
    for (const s of subs) {
      const sub = descendants(s), t = el("div", "card folder-card"), b = el("div", "card-body");
      const txt = el("div", "min-w-0 flex-fill"); txt.append(el("div", "name", nice(s.n)), el("div", "text-secondary small", `${sub.length.toLocaleString()} bookmark${sub.length === 1 ? "" : "s"}`));
      b.append(el("span", "avatar avatar-sm bg-primary-lt flex-none"), txt); b.firstChild.append(icon(s.n === TRASH ? "ti-trash" : "ti-folder"));
      if (s.d) b.dataset.tip = s.d;
      t.append(b);
      t.onclick = () => { if (!readOnly()) { setOpen(f, true); saveOpen(); build(); } selectNode(s); list.focus(); };
      t.oncontextmenu = e => tileMenu(e, f, s);
      fg.append(t);
    }
  }
  if (links.length) grid.append(el("div", "subheader", `Bookmarks · ${links.length.toLocaleString()}`));
  const lg = el("div", "tile-grid"); grid.append(lg);
  // One page of cards; the size is the folder overview's own (its footer), else Settings › Cards per page.
  const fkey = pathOf(f), pager = new Pager({size: PAGE_SIZES.cards ?? SET.tileLimit, sizes: [60, 120, 240, 500, 0], noun: "bookmarks", onChange: p => {
    if (p.pageSize !== (PAGE_SIZES.cards ?? SET.tileLimit)) setListSize("cards", p.pageSize);
    tilePage.set(fkey, p.page); show(); $("#grid").scrollIntoView({block: "start"}); }}).total(links.length).goto(tilePage.get(fkey) || 1);
  for (const id of pager.slice(links)) {
    const i = ITEM.get(id), t = el("div", "card tile");
    t.append(thumb(i, "img"));
    const b = el("div", "card-body"), tt = el("div", "tile-title");
    tt.append(favicon(i)); if (STATUS_ICON[i.s] && i.s !== "ok") tt.append(icon(STATUS_ICON[i.s], "st-" + i.s)); tt.append(el("span", null, titleOf(id) || i.u));
    const tu = el("div", "tile-url", shortUrl(i.u, 48)); tu.dataset.tip = i.u;
    b.append(tt, tu); t.append(b);
    t.onclick = () => { if (!readOnly()) { setOpen(f, true); saveOpen(); build(); } selectNode(id); list.focus(); };
    t.oncontextmenu = e => tileMenu(e, f, id);
    lg.append(t);
  }
  if (pager.pages > 1) { const pf = pager.render(); pf.classList.add("mt-3", "rounded", "border"); grid.append(pf); }
  if (!subs.length && !links.length) { grid.hidden = true; message(r.trash ? "The trash is empty." : "Empty folder."); }
}

function showLink(i, id) {
  const fv = favicon(i); fv.style.width = fv.style.height = "20px";
  head(fv, titleOf(id) || "(no title)", shortUrl(i.u));
  if (!readOnly()) {
    // What needs a decision stays a visible button; everything else lives in two menus.
    if (pendingUpdate(i)) act("ti-arrow-forward-up", "Update link", () => applyUpdates([id]), "btn-warning");
    if (inTrash(id)) act("ti-arrow-back-up", "Put back", putBack, "btn-primary");
    menuButton("Open", "ti-external-link", [...openItems(true).slice(0, -2), "-", {l: "Load the live page here", ic: "ti-world-www", k: "L", fn: live}, ...openItems(true).slice(-1)]);
    menuButton("Actions", "ti-dots", [
      {h: "Check"}, {l: "Refresh (re-check, new snapshot)", ic: "ti-refresh", k: "R", fn: refresh},
      i.applied ? {l: "Revert to the old link", ic: "ti-arrow-back", fn: () => revertUpdate(id)} : null,
      {h: "Edit"}, {l: "Rename…", ic: "ti-pencil", k: "F2", fn: rename},
      {l: noteOf(id) ? "Edit description…" : "Add description…", ic: "ti-notes", k: "D", fn: editDescription},
      {h: "Organise"}, {l: "Move to", ic: "ti-folder-symlink", sub: moveToItems()},
      inTrash(id) ? null : "-", inTrash(id) ? null : {l: "Move to Trash", ic: "ti-trash", k: "⌫", danger: true, fn: trashNodes}]);
  }
  const ds = descOf(id);
  if (ds) info.append(el("p", "lead-text mb-3", ds));
  // Your own labels (a 1–5 star rating and tags) are rows of Details below, saved with your edits.
  const labels = readOnly() ? null : {
    stars: starRating(ratingOf(id), v => { snapshot(); if (v) st().ratings[id] = v; else delete st().ratings[id]; save(); build(true); }),
    tags: tagEditor(tagsOf(id), t => { snapshot(); if (t.length) st().tags[id] = t; else delete st().tags[id]; save(); build(true); }, allTags()),
  };
  if (labels) labels.tags.dataset.wide = "1";  // the tag field takes the whole row
  const al = statusAlert(i, id); if (al) info.append(al);
  const tags = el("div", "d-flex flex-wrap gap-2 mb-3");
  for (const [text, cls, ic, tip] of linkBadges(i, !!noteOf(id))) { const b = badge(text, cls, ic); if (tip) b.dataset.tip = HELP[tip]; tags.append(b); }
  if (tags.childElementCount) { tags.className = "head-tags"; const sub = $("#infoHead").querySelector(".min-w-0 > .small"); (sub || info).append(tags); }
  // The link as people read it, on one line; the full address is in the tooltip and on Copy.
  const a = el("div", "d-flex align-items-center gap-2 min-w-0"); a.dataset.wide = "1";
  const la = el("a", "text-truncate min-w-0", shortUrl(i.u, 110)); if (isWeb(i.u)) la.href = i.u; la.target = "_blank"; la.rel = "noopener noreferrer"; la.dataset.tip = i.u;
  const cp = el("button", "btn btn-sm btn-ghost-secondary btn-icon flex-none"); cp.type = "button"; cp.dataset.tip = "Copy the full link"; cp.setAttribute("aria-label", "Copy link");
  cp.append(icon("ti-copy")); cp.onclick = () => navigator.clipboard?.writeText(i.u).then(() => toast("Link copied", "success"));
  a.append(la, cp);
  info.append(sectionCard("Details", (() => { const g = datagrid([
    ["Link", a], ["Updated from", i.applied ? i.prev : ""],
    ["Your rating", labels ? labels.stars : ""], ["Tags", labels ? labels.tags : ""],
    ["Added", fmtDate(i.a)],
    ["Last opened", (o => o ? `${fmtDate(o.t)} · ${o.n.toLocaleString()} visit${o.n === 1 ? "" : "s"}` : OPENED_READ ? "Not in your browsers' history"
      : OPENED_BLOCKED ? "Unknown: macOS blocks reading browser history (Browsers › Allow reading)" : "")(openedOf(i.u))],
    ["Was in", nice(i.o)], ["Why it is here", i.p ? "Kept exactly where you had it" : i.w],
    ["Original title", st().titles[id] != null ? i.t : ""],
    ["Page's own description", noteOf(id) && i.ds ? i.ds : ""],
    ["In the Trash", !readOnly() && inTrash(id) ? "from " + (nice((st().trashFrom[id] || {}).path || "") || "top level") : ""],
    ["Duplicate of", i.k ? "the copy kept in " + nice(i.kg || i.k) : ""],
  ]); g.classList.remove("mt-3"); return g; })()));
  if (i.h) {
    shot.hidden = false; shot.src = `thumbs/${i.h}.jpg` + (thumbVer.has(i.h) ? `?v=${thumbVer.get(i.h)}` : "");
    shot.onerror = () => { shot.hidden = true; message(NO_SNAPSHOT); };
  } else message(snapshotMessage(i));
}
function live() {
  const r = rows[sel]; if (!r || r.kind !== "link") return;
  // Never this tool's own address or a local one (views/link-panel.ts liveLoad).
  const may = liveLoad(r.item.u, isSelf); if (may === "web") return;
  if (may === "local") return toast("Local addresses are not loaded here. Open them in a tab instead.", "warning", "Not loaded");
  shot.hidden = true; $("#msg").hidden = true; frame.hidden = false; frame.src = r.item.u;
}

// ---------- download: a Chrome import file built from the edited tree, Trash left out, descriptions included ----------
function downloadFile() {
  // The file itself: netscapeFile() in src/web/ts/exportfile.ts (typed, tested).
  const bin = trashFolder();
  const {text, links} = netscapeFile(st().root, {heading: `Bookmarks (cleaned ${new Date().toISOString().slice(0, 10)}, unified)`, skip: bin,
    order: ordered, name: f => nice(f.n), url: id => ITEM.get(id).u, title: titleOf, added: id => ITEM.get(id).a, note: noteOf});
  const a = el("a"); a.href = URL.createObjectURL(new Blob([text], {type: "text/html"}));
  a.download = "bookmarks-clean-edited.html"; document.body.append(a); a.click(); a.remove();
  toast(`${links.toLocaleString()} bookmarks saved to ${a.download}` + (bin && bin.c.length ? ` (${descendants(bin).length} in Trash left out)` : ""), "success", "Import file downloaded");
}

// ---------- keyboard ----------
function move(to, extend) {
  const from = sel; sel = Math.max(0, Math.min(rows.length - 1, to));
  if (extend) { if (anchor == null) anchor = from; markRange(anchor, sel); } else { anchor = sel; marked.clear(); }
  draw(); show();
}
// Which action a key asks for: keyAction() in src/web/ts/keys.ts (typed, tested); what each action does is here.
const KEY_ACTIONS = {
  palette: () => { if (visibleModals().includes($("#palModal"))) hideModal("#palModal"); else { for (const m of visibleModals()) if (m.id) hideModal("#" + m.id); openPalette(); } },
  undo: () => UNDO.undo(), redo: () => UNDO.redo(),
  selectAll: () => { rows.forEach(r => marked.add(r.node)); draw(); show(); },
  toList: () => list.focus(),
  escape: () => { closeDropdowns(); if (marked.size) { marked.clear(); draw(); show(); } list.focus(); },
  search: () => SEARCH.focus(), shortcuts: () => openShortcuts(), help: () => openAbout("help"), settings: () => openSettings(),
  moveUp: () => reorder(-1), moveDown: () => reorder(1),
  mark: () => { toggleMark(rows[sel].node); anchor = sel; draw(); show(); },
  enter: () => { const r = rows[sel]; if (r.kind === "folder") toggle(r, !r.open); else openTab(r.item.u); },
  expandAll: () => { const r = rows[sel]; if (r.kind === "folder") expandInside(r); },
  collapseAll: () => collapseAllFolders(),
  openHere: () => openLinks("browser"), openWindow: () => openLinks("window"), openIncognito: () => openLinks("incognito"),
  copy: () => copyLinks(), describe: () => editDescription(), live: () => live(),
  refresh: () => { if (!readOnly()) refresh(); }, putBack: () => putBack(),
  update: () => { if (!readOnly()) applyUpdates(updateIds()); }, move: () => { if (!readOnly()) moveDialog(); },
  group: () => { if (!readOnly()) newFolder(targets()); }, newFolder: () => newFolder(null),
  rename: () => rename(), trash: () => trashNodes(), menu: () => menuAtSelection(),
  rate0: () => rateSelected(0), rate1: () => rateSelected(1), rate2: () => rateSelected(2), rate3: () => rateSelected(3),
  rate4: () => rateSelected(4), rate5: () => rateSelected(5),
  tag: () => { const r = rows[sel]; if (r?.kind === "link" && !readOnly()) $("#info .tag-input")?.focus(); },
};
// Rate or tag every bookmark in the selection (folders count their bookmarks), as one undoable edit.
function rateMany(v) {
  const ids = targetIds(); if (!ids.length || readOnly()) return;
  snapshot(); for (const id of ids) { if (v) st().ratings[id] = v; else delete st().ratings[id]; }
  save(); build(true); show();
  toast(v ? `${ids.length.toLocaleString()} bookmarks rated ${"★".repeat(v)}` : `Rating cleared on ${ids.length.toLocaleString()} bookmarks`, "success");
}
async function askRateMany() {
  const v = await askText({title: `Rate ${targetIds().length.toLocaleString()} bookmarks`, label: "Stars, 1 to 5 (0 clears)", value: "5", hint: "Or use Actions › Label them › Rate."});
  const n = Number(String(v ?? "").trim()); if (v == null || !Number.isInteger(n) || n < 0 || n > 5) return v == null ? undefined : toast("Give a number from 0 to 5.", "warning", "Not rated");
  rateMany(n);
}
async function tagMany(add) {
  const ids = targetIds(); if (!ids.length || readOnly()) return;
  const inUse = [...new Set(ids.flatMap(id => tagsOf(id)))];
  const raw = await askText({title: add ? `Add a tag to ${ids.length.toLocaleString()} bookmarks` : `Remove a tag from ${ids.length.toLocaleString()} bookmarks`,
    label: "Tag", value: "", hint: add ? (allTags().slice(0, 8).join(", ") || "work, read later…") : (inUse.join(", ") || "None of them has a tag")});
  const tag = String(raw ?? "").trim(); if (!tag) return;
  const r = cleanTags([tag]); if (r.error) return toast(r.error, "warning", "Not tagged");
  const t = r.tags[0], same = x => x.toLowerCase() === t.toLowerCase();
  let n = 0;
  snapshot();
  for (const id of ids) {
    const cur = tagsOf(id);
    if (add && !cur.some(same)) {
      const next = cleanTags([...cur, t]); if (next.error) continue;
      st().tags[id] = next.tags; n++;
    } else if (!add && cur.some(same)) {
      const next = cur.filter(x => !same(x)); if (next.length) st().tags[id] = next; else delete st().tags[id]; n++;
    }
  }
  save(); build(true); show();
  toast(add ? `Tagged ${n.toLocaleString()} bookmarks “${t}”` : `“${t}” removed from ${n.toLocaleString()} bookmarks`, "success");
}
// Rate the selected bookmark (0 clears), as the stars in its details do; folders and read-only views are left alone.
function rateSelected(v) {
  const r = rows[sel]; if (!r || r.kind !== "link" || readOnly()) return;
  if ((ratingOf(r.node) || 0) === v) return;
  snapshot(); if (v) st().ratings[r.node] = v; else delete st().ratings[r.node];
  save(); build(true); show();
  toast(v ? `Rated ${"★".repeat(v)}` : "Rating cleared", "success");
}
document.addEventListener("keydown", e => {
  const typing = /^(INPUT|TEXTAREA|SELECT)$/.test(document.activeElement?.tagName);
  const hit = keyAction({key: e.key, shift: e.shiftKey, alt: e.altKey, meta: e.metaKey, ctrl: e.ctrlKey},
    {dialogOpen: modalOpen(), menuOpen: ctx.classList.contains("show"), typing, inSearch: document.activeElement === $("#hq"), hasRow: !!rows[sel]});
  if (!hit) return;
  if (hit.prevent) e.preventDefault();
  if (hit.action === "nav") {
    // Arrows, Home and End: the rule is navigate() in src/web/ts/tree-rules.ts; shift extends the selection.
    const r = rows[sel], step = navigate(rows, sel, e.key, false);  // folders open and close in every view now
    if (step?.kind === "toggle") toggle(r, step.open);
    else if (step) move(step.to, e.shiftKey && (e.key === "ArrowDown" || e.key === "ArrowUp"));
    return;
  }
  KEY_ACTIONS[hit.action]();
});

setupFold();
buildToolbar();
setTimeout(() => void loadOpened(), 0);
summary();
tipsFromTitles();
updateAllLabel();
setMode("g"); // one structure: your tree with the new structure merged in

// Back where you were: the address (refresh, back, forward, a shared link) or, opened bare, the last place here.
function restorePlace(p) {
  restoringPlace = true;
  try {
    if (FILTERS.some(f => f[0] === (p.filter || "all"))) { filter = p.filter || "all"; paintChips(); }
    $("#q").value = p.q || "";
    sel = 0; build();
    if (p.node) {
      const want = p.node.slice(2), folder = p.node.startsWith("f:");
      const id = folder ? null : Number(want);
      if (folder) { let acc = ""; for (const part of want.split("/").slice(0, -1)) { acc = acc ? acc + "/" + part : part; st().open.add(acc); } }
      else for (let f = PARENT.get(id); f && f !== st().root; f = PARENT.get(f)) setOpen(f, true);
      build();
      const k = rows.findIndex(r => folder ? r.kind === "folder" && pathOf(r.node) === want : r.kind === "link" && r.node === id);
      if (k >= 0) { sel = k; draw(); show(); list.querySelector(".active, [aria-selected=true]")?.scrollIntoView({block: "center"}); }
    }
    if (p.snapshot) TABS.prefer(p.snapshot);
    setAppView(p.view, {restoring: true});
  } finally { restoringPlace = false; }
  PLACE.sync();
}
paintScope();
restorePlace(PLACE.read());
PLACE.onNavigate(restorePlace);

// ---------- names other scripts call directly (src/web/ts/ws_client.js, src/web/ts/legacy.ts) ----------
// A classic script shared these by being global; a module must say so. `mode` changes, so it is a live getter.
Object.assign(window, {S, askConfirm, askText, closeDropdowns, contains, hideModal, loadState, setMode, showModal, toast,
  trashFolder, undoStack, tokenPost, host, shortUrl});
Object.defineProperty(window, "mode", {get: () => mode, configurable: true});

// ---------- the test handle: what the end-to-end checks drive (src/tests and the browser checks) ----------
// Explicit, so a test can never depend on something being global by accident. Mutable state is read through getters.
window.__bf = {
  FILTERS, VIEW_OPTS, setFilter, setView, setDates, build, draw, show, st, indexParents, walkFolders, nice, titleOf, descOf,
  setOpen, saveOpen, selectNode, rename, undo, moveNodes, trashNodes, inTrash, trashFolder, putBack, newFolder,
  editDescription, isFolder, downloadFile, openPalette, openMenu, closeCtx, openAbout, openSettings, openShortcuts,
  openDetails, closeDetails, setAppView, tour, endTour, applyTheme, moveToTabs, openLinks, paintIoMenu, TABS, TL,
  get rows() { return rows; }, get sel() { return sel; }, set sel(v) { sel = v; }, get marked() { return marked; }, refresh, get palList() { return PALETTE.list; },
  get list() { return list; }, get ctx() { return ctx; }, get view() { return view_; },
};
