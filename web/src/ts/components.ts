// Alpine components for the new tools: tab clean-up, clearing history, notes and tags, and link re-checks.
// Each is a plain object factory: state + methods, no DOM queries; the markup binds to it with x-data.
import { api } from "./api";
import { confirm, legacy, shortUrl, toast } from "./legacy";
import { closeTabs, CloseResult, flatten, isWeb, keyOf, Live, OpenTab, TabCleaner } from "./tabs";
import { v } from "./validate";

type Group = "duplicates" | "stale" | "bookmarked" | "incognito";
const GROUPS: { id: Group; label: string; help: string }[] = [
  { id: "duplicates", label: "Open twice", help: "Every copy after the first of a page open more than once (incognito and normal are never paired)." },
  { id: "stale", label: "Untouched for a while", help: "Open since before the chosen age, according to your saved snapshots." },
  { id: "bookmarked", label: "Already bookmarked", help: "The page is already one of your bookmarks, so the tab can go." },
  { id: "incognito", label: "Incognito", help: "Every tab in an incognito window." },
];

// ---- Open tabs: undo and redo for what this page did to your tabs (close, move between incognito and normal) ----
// Undo reopens what was closed (and closes the moved copies); redo does it again. Tabs are matched by address in the
// same browser and mode, since a reopened tab has a new id. Kept for this page visit only.
interface TabStep { label: string; closed: OpenTab[]; movedTo?: "normal" | "incognito" }
const emitTabs = () => window.dispatchEvent(new CustomEvent("bf:undo", { detail: { scope: "tabs" } }));
async function liveMatching(want: { url: string; browser: string; mode: string }[]): Promise<OpenTab[]> {
  const live = flatten(await api.path("tabs").get<Live>()), used = new Set<string>(), out: OpenTab[] = [];
  for (const w of want) {
    const t = live.find(x => x.url === w.url && x.browser === w.browser && x.mode === w.mode && !used.has(keyOf(x)));
    if (t) { used.add(keyOf(t)); out.push(t); }
  }
  return out;
}
const reopen = (tabs: OpenTab[], mode?: "normal" | "incognito") => {
  const byWin = new Map<string, { urls: string[]; incognito: boolean; browser: string }>();
  for (const t of tabs) { const m = mode ?? t.mode, k = `${t.browser}|${t.windowN}|${m}`; const b = byWin.get(k) ?? { urls: [], incognito: m === "incognito", browser: t.browser }; b.urls.push(t.url); byWin.set(k, b); }
  return legacy().openWindows([...byWin.values()]);
};
export const tabHistory = {
  done: [] as TabStep[], undone: [] as TabStep[],
  record(step: TabStep) { this.done.push(step); this.undone = []; emitTabs(); },
  get undoLabel() { const s = this.done.at(-1); return s ? s.label : null; },
  get redoLabel() { const s = this.undone.at(-1); return s ? s.label : null; },
  async undo() {
    const s = this.done.pop(); if (!s) return;
    try {
      if (s.movedTo) { const copies = await liveMatching(s.closed.map(t => ({ ...t, mode: s.movedTo === "normal" ? "normal" : "incognito" }))); if (copies.length) await closeTabs(copies); }
      await reopen(s.closed);
      this.undone.push(s); toast(`Undone: ${s.label}`, "success", "Undone");
    } catch (e) { this.done.push(s); toast(String((e as Error).message ?? e), "danger", "Could not undo"); }
    emitTabs(); await legacy().refreshTabs();
  },
  async redo() {
    const s = this.undone.pop(); if (!s) return;
    try {
      if (s.movedTo) await reopen(s.closed, s.movedTo);
      const again = await liveMatching(s.closed);
      if (again.length) await closeTabs(again);
      this.done.push(s); toast(`Redone: ${s.label}` + (again.length < s.closed.length ? ` (${s.closed.length - again.length} were no longer open)` : ""), "success", "Redone");
    } catch (e) { this.undone.push(s); toast(String((e as Error).message ?? e), "danger", "Could not redo"); }
    emitTabs(); await legacy().refreshTabs();
  },
};

/** Close tabs after one confirmation; the server snapshots first. One implementation for every place that closes. */
export async function closeWithConfirm(list: OpenTab[]): Promise<CloseResult | null> {
  if (!list.length) { toast("Tick the tabs to close first."); return null; }
  const inc = list.filter(t => t.mode === "incognito").length;
  if (!(await confirm({ title: `Close ${list.length} tab${list.length === 1 ? "" : "s"}?`, ok: `Close ${list.length}`, no: "Keep them",
    body: "A snapshot of every window is saved first, so you can reopen them from the timeline or with Undo." + (inc ? ` ${inc} of them are incognito.` : "") +
      " A tab that changed page since it was read is left open." }))) return null;
  try {
    const r = await closeTabs(list);
    if (r.closed) tabHistory.record({ label: `Close ${r.closed} tab${r.closed === 1 ? "" : "s"}`, closed: list });
    toast(`${r.closed} closed` + (r.skipped ? `, ${r.skipped} left open (changed or already gone)` : "") + ". Reopen them from the timeline if needed.", r.skipped ? "warning" : "success", "Tabs closed");
    await legacy().refreshTabs();
    return r;
  } catch (e) { toast(String((e as Error).message ?? e), "danger", "Could not close them"); return null; }
}

/** Move tabs between incognito and normal windows: open them in the other mode, in the browser they are in (one
 *  new window per original window), then close the originals. One confirmation; the server snapshots before any
 *  close, so the timeline or Undo brings them back. A page cannot change a tab's mode in place, so this is the move. */
export async function moveToMode(list: OpenTab[], to: "normal" | "incognito"): Promise<boolean> {
  const tabs = list.filter(t => isWeb(t.url) && t.mode !== to);
  if (!tabs.length) { toast(to === "normal" ? "Those tabs are already in normal windows." : "Those tabs are already incognito."); return false; }
  const where = to === "normal" ? "a normal window" : "an incognito window";
  if (!(await confirm({ title: `Move ${tabs.length} tab${tabs.length === 1 ? "" : "s"} to ${where}?`, ok: "Move", no: "Keep them", danger: false,
    body: `They open in ${where} of the same browser, then close where they are now. A snapshot is saved first, so the timeline can bring them back.` +
      (to === "normal" ? " Once in a normal window they are kept in that browser's history like any other page." : "") }))) return false;
  const byWin = new Map<string, { urls: string[]; incognito: boolean; browser: string }>();
  for (const t of tabs) { const k = `${t.browser}|${t.windowN}`; const b = byWin.get(k) ?? { urls: [], incognito: to === "incognito", browser: t.browser }; b.urls.push(t.url); byWin.set(k, b); }
  try { await legacy().openWindows([...byWin.values()]); }
  catch (e) { toast(String((e as Error).message ?? e), "danger", "Nothing was moved"); return false; }  // nothing closes if the open failed
  try {
    const r = await closeTabs(tabs);
    tabHistory.record({ label: `Move ${tabs.length} tab${tabs.length === 1 ? "" : "s"} to ${where}`, closed: tabs, movedTo: to });
    toast(`${tabs.length} moved to ${where}` + (r.skipped ? `; ${r.skipped} left open where they were (changed since read)` : "") + ".", r.skipped ? "warning" : "success", "Tabs moved");
  } catch (e) { toast(String((e as Error).message ?? e) + " The new window is open; the originals were left as they were.", "warning", "Opened, not closed"); }
  await legacy().refreshTabs();
  return true;
}

export const tabCleanup = () => ({
  open: false, loading: false, error: "", group: "duplicates" as Group, staleDays: 7,
  groups: GROUPS, lists: { duplicates: [], stale: [], bookmarked: [], incognito: [] } as Record<Group, (OpenTab & { since?: string })[]>,
  picked: new Set<string>(), lastClosed: [] as OpenTab[], undoId: "",

  async show(group: Group = "duplicates") { this.open = true; this.group = group; await this.load(); },
  hide() { this.open = false; },
  async load() {
    this.loading = true; this.error = "";
    try {
      const live = await api.path("tabs").get<Live>();
      // Only the browsers Show tabs from includes (the nav filter); all of them when it is not set.
      const inScope = (window as unknown as { BF_IN_SCOPE?: (b: string) => boolean }).BF_IN_SCOPE ?? (() => true);
      const all = flatten(live).filter(t => inScope(t.browser)), c = new TabCleaner(all);
      const { first_seen } = await api.path("tabs", "ages").post<{ first_seen: Record<string, string> }>({ urls: [...new Set(all.map(t => t.url))].slice(0, 5000) });
      this.lists = { duplicates: c.duplicates(), stale: c.stale(first_seen, this.staleDays), bookmarked: c.bookmarked(legacy().data.keep.map(b => b.u)), incognito: c.incognito() };
      this.picked = new Set(this.lists[this.group].map(keyOf));  // a group starts fully selected; untick what to keep
    } catch (e) { this.error = String((e as Error).message ?? e); } finally { this.loading = false; }
  },
  choose(group: Group) { this.group = group; this.picked = new Set(this.lists[group].map(keyOf)); },
  count(group: Group) { return this.lists[group].length; },
  get rows() { return this.lists[this.group]; },
  key: keyOf, short: (u: string) => shortUrl(u, 70),
  age(since?: string) { if (!since) return ""; const d = Math.floor((Date.now() - Date.parse(since)) / 86400000); return d < 1 ? "today" : `${d} day${d === 1 ? "" : "s"}`; },
  toggle(t: OpenTab) { const k = keyOf(t); this.picked.has(k) ? this.picked.delete(k) : this.picked.add(k); this.picked = new Set(this.picked); },
  isPicked(t: OpenTab) { return this.picked.has(keyOf(t)); },
  allPicked() { return this.rows.length > 0 && this.rows.every(t => this.picked.has(keyOf(t))); },
  toggleAll() { this.picked = this.allPicked() ? new Set() : new Set(this.rows.map(keyOf)); },

  async closePicked() {
    const sel = this.rows.filter(t => this.picked.has(keyOf(t)));
    const r = await closeWithConfirm(sel);
    if (!r) return;
    this.lastClosed = sel; this.undoId = r.undo_snapshot ?? "";
    await this.load();
  },
  async undo() {
    if (!this.lastClosed.length) return;
    const byWin = new Map<string, { urls: string[]; incognito: boolean }>();
    for (const t of this.lastClosed) { const k = `${t.browser}|${t.windowN}|${t.mode}`; const b = byWin.get(k) ?? { urls: [], incognito: t.mode === "incognito" }; b.urls.push(t.url); byWin.set(k, b); }
    await legacy().openWindows([...byWin.values()]);
    this.lastClosed = [];
  },
});

interface SnapshotRow { id: string; at: string; summary: { tabs: number; windows: number } }
export const historyTools = () => ({
  open: false, how: "one" as "one" | "before" | "all", snapshots: [] as SnapshotRow[],
  chosen: "", before: "", typed: "", error: "", busy: false,

  async show() { this.open = true; this.error = ""; this.typed = ""; const r = await api.path("tabs", "snapshots").get<{ snapshots: SnapshotRow[] }>(); this.snapshots = r.snapshots; this.chosen = this.snapshots[0]?.id ?? ""; },
  hide() { this.open = false; },
  label(s: SnapshotRow) { return `${new Date(s.at).toLocaleString()} · ${s.summary.windows} windows · ${s.summary.tabs} tabs`; },
  get affected() {
    if (this.how === "one") return this.chosen ? 1 : 0;
    if (this.how === "all") return this.snapshots.length;
    return this.snapshots.filter(s => this.before && s.id.slice(0, 10) < this.before).length;
  },
  get canDelete() { return this.how === "all" ? this.typed === "DELETE ALL" : this.affected > 0; },
  async run() {
    this.error = "";
    let body: Record<string, unknown>;
    if (this.how === "one") { const r = v.snapshotId().check(this.chosen); if (!r.ok) { this.error = r.error; return; } body = { ids: [r.value] }; }
    else if (this.how === "before") { const r = v.day().check(this.before); if (!r.ok) { this.error = r.error; return; } body = { before: r.value }; }
    else body = { everything: true, confirm: this.typed };
    if (!(await confirm({ title: `Delete ${this.affected} snapshot${this.affected === 1 ? "" : "s"} for good?`, ok: "Delete for good", no: "Keep them",
      body: "This cannot be undone: the snapshots are overwritten in the database, not hidden. Your bookmarks, notes and settings are not touched." }))) return;
    this.busy = true;
    try { const r = await api.path("tabs", "delete").post<{ deleted: number; files: number }>(body); toast(`${r.deleted} snapshot${r.deleted === 1 ? "" : "s"} deleted for good.`, "success", "History cleared"); this.hide(); await legacy().refreshTabs(); }
    catch (e) { this.error = String((e as Error).message ?? e); } finally { this.busy = false; }
  },
});

export const noteEditor = () => ({
  open: false, kind: "tab" as "tab" | "window" | "snapshot", key: "", label: "", note: "", tags: "", error: "", onSaved: null as null | (() => void),
  show(kind: "tab" | "window" | "snapshot", key: string, label: string, note = "", tags: string[] = [], onSaved?: () => void) {
    Object.assign(this, { open: true, kind, key, label, note, tags: tags.join(", "), error: "", onSaved: onSaved ?? null });
  },
  hide() { this.open = false; },
  async save() {
    const t = v.tags().check(this.tags), n = v.text().max(2000).check(this.note);
    if (!t.ok) { this.error = "Tags: " + t.error; return; }
    if (!n.ok) { this.error = "Note: " + n.error; return; }
    try { await api.path("tabs", "note").post({ kind: this.kind, key: this.key, note: n.value, tags: t.value }); toast("Saved", "success"); this.hide(); this.onSaved?.(); }
    catch (e) { this.error = String((e as Error).message ?? e); }
  },
});

interface Run { run_id: string; started: string; finished?: string; total: number; checked: number; newly_broken: number; fixed: number; state: string }
export const rechecks = () => ({
  status: null as null | { running: string | null; runs: Run[]; recheck_days: number }, report: null as null | { newly_broken: { url: string; before: string; note: string }[]; fixed: { url: string }[] },
  timer: 0,
  async init() { await this.load(); },
  async load() {
    try { this.status = await api.path("rechecks").get(); } catch { this.status = null; return; }
    const last = this.status?.runs.find(r => r.state !== "running");
    if (last && last.newly_broken + last.fixed) this.report = await api.path("rechecks", last.run_id).get();
    clearTimeout(this.timer);
    if (this.status?.running) this.timer = window.setTimeout(() => this.load(), 5000);
  },
  get current() { return this.status?.runs.find(r => r.state === "running") ?? null; },
  get last() { return this.status?.runs.find(r => r.state !== "running") ?? null; },
  pct(r: Run) { return r.total ? Math.round(100 * r.checked / r.total) : 0; },
  when(iso?: string) { return iso ? new Date(iso).toLocaleString() : ""; },
  short: (u: string) => shortUrl(u, 70),
  async runNow() {
    if (!(await confirm({ title: "Re-check every link now?", ok: "Start", danger: false, body: "Every bookmarked link is visited again, a few at a time and politely. It takes a while; the page stays usable and you can stop it." }))) return;
    try { await api.path("rechecks", "run").post(); toast("Link re-check started.", "info"); await this.load(); } catch (e) { toast(String((e as Error).message ?? e), "danger"); }
  },
  async stop() { await api.path("rechecks", "cancel").post(); await this.load(); },
  async setDays(days: number) { await api.path("tabs", "settings").post({ recheck_days: Number(days) }); await this.load(); toast(Number(days) ? `Links re-checked every ${days} days` : "Scheduled re-checks off", "success"); },
});

// ---- browsers: which are installed, which to track, which opens links ----
interface BrowserInfo { name: string; engine: string; scriptable: boolean; session_file?: boolean; private_known: boolean; private_flag: string | null }
/** Its tabs can be read: by automation, or from the session file it keeps. */
const readable = (b: BrowserInfo): boolean => b.scriptable || !!b.session_file;
interface BrowsersState { installed: BrowserInfo[]; tracked: string[]; open_with: string[] | string | null; onboarded: boolean; system_default?: string | null }
/** The open-links choice meaning "whichever browser macOS opens links in"; the server resolves it at each open. */
export const SYSTEM_DEFAULT = "System default";
// What each feature can reach per browser. Profile names (bookmark files) are short ("Chrome"); app names are full.
const PROFILE_NAME: Record<string, string> = { "Google Chrome": "Chrome", "Brave Browser": "Brave", "Microsoft Edge": "Edge", Safari: "Safari", Arc: "Arc", Vivaldi: "Vivaldi", Firefox: "Firefox" };
interface Profile { browser: string; profile?: string; path: string; readable: boolean; why?: string }
type Reach = "readable" | "blocked" | "missing";

export const browserPicker = () => ({
  open: false, onboarding: false, loading: false, error: "",
  profiles: [] as Profile[], historyOf: {} as Record<string, Reach>,
  installed: [] as BrowserInfo[], tracked: new Set<string>(), opens: new Set<string>(),

  async init() {
    await this.load();
    if (!this.error && !(this as unknown as { state: BrowsersState }).state?.onboarded && this.installed.length) {
      // first run: ask once which browsers to track (after the welcome, if it is showing)
      // Only when no dialog is visible: Alpine dialogs keep the "show" class while hidden, so look at display.
      const anyOpen = () => [...document.querySelectorAll(".modal.show")].some(m => getComputedStyle(m).display !== "none");
      setTimeout(() => { if (!anyOpen()) this.show(true); }, 2500);
    }
  },
  state: null as BrowsersState | null,
  async load() {
    this.loading = true; this.error = "";
    try {
      const s = await api.path("tabs", "browsers").get<BrowsersState>();
      this.state = s; this.installed = s.installed; this.tracked = new Set(s.tracked);
      const ow = s.open_with;  // the server sends the effective list; an older server sent one name
      this.opens = new Set(Array.isArray(ow) ? ow : ow ? [ow] : []);
      this.systemDefault = s.system_default ?? null;
      // Kept on window too: this can arrive before the page script listens, and the page reads the latest on start.
      const detail = { openWith: this.openWith, opens: [...this.opens], tracked: [...this.tracked], installed: s.installed.map(b => b.name), systemDefault: this.systemDefault };
      (window as unknown as { BF_BROWSERS?: typeof detail }).BF_BROWSERS = detail;
      window.dispatchEvent(new CustomEvent("bf:browsers", { detail }));
      void this.loadReach();
    } catch (e) { this.error = String((e as Error).message ?? e); } finally { this.loading = false; }
  },
  show(onboarding = false) { this.onboarding = onboarding; this.open = true; },
  // Cancel throws the unsaved choices away (re-read from the server); "Later" on first run saves what is ticked.
  hide() { this.open = false; if (this.onboarding) void this.persist(false); else void this.load(); },
  can(b: BrowserInfo) {
    return [b.scriptable ? "reads tabs" : b.session_file ? "reads tabs from its session file (not private windows)" : "can't read tabs",
      b.private_known ? "tells incognito apart" : b.scriptable ? "can't tell private windows apart" : "",
      b.private_flag ? "opens incognito/private" : "no private window"].filter(Boolean).join(" · ");
  },
  isTracked(name: string) { return this.tracked.has(name); },
  // Bookmarks and history per browser: whether this Mac lets the server read them (counts nothing, reads nothing).
  async loadReach() {
    const [p, h] = await Promise.allSettled([api.path("profiles").get<{ profiles: Profile[] }>(), api.path("history", "access").get<{ browsers: { browser: string; state: Reach }[] }>()]);
    this.profiles = p.status === "fulfilled" ? p.value.profiles : [];
    this.historyOf = h.status === "fulfilled" ? Object.fromEntries(h.value.browsers.map(b => [b.browser, b.state])) : {};
  },
  bookmarks(name: string): { label: string; cls: string; tip: string } {
    const mine = this.profiles.filter(p => p.browser === (PROFILE_NAME[name] ?? name));
    if (!mine.length) return { label: "no file", cls: "text-secondary", tip: "No bookmark file found for this browser" };
    const ok = mine.filter(p => p.readable).length;
    return ok ? { label: `${ok} profile${ok === 1 ? "" : "s"}`, cls: "text-success", tip: "Its bookmarks can be imported into a workspace" }
      : { label: "blocked", cls: "text-warning", tip: (mine[0]?.why || "macOS does not allow reading it here") + ". Export from the browser and import that file instead." };
  },
  history(name: string): { label: string; cls: string } {
    const st = this.historyOf[name];
    return st === "readable" ? { label: "readable", cls: "text-success" } : st === "blocked" ? { label: "blocked", cls: "text-warning" } : { label: "—", cls: "text-secondary" };
  },
  importBookmarks() { this.open = false; (window as unknown as { BM_WS?: { ui?: { importer?: () => void } } }).BM_WS?.ui?.importer?.(); },
  fixHistory() { this.open = false; (window as unknown as { __bf?: { openSettings?: () => void } }).__bf?.openSettings?.(); setTimeout(() => document.querySelector('[data-brofo-panel="history-access"]')?.scrollIntoView({ block: "center" }), 300); },
  // Links open in the system's default browser, or in any number of chosen browsers at once; one choice always stays.
  systemDefault: null as string | null,   // the browser macOS opens links in, when it is one this tool knows
  get useSystem(): boolean { return this.opens.has(SYSTEM_DEFAULT); },
  setUseSystem(on: boolean) {
    this.opens = on ? new Set([SYSTEM_DEFAULT]) : new Set([this.systemDefault ?? this.installed[0]?.name ?? ""].filter(Boolean));
  },
  get systemLabel(): string { return this.systemDefault ? `${SYSTEM_DEFAULT} (${this.systemDefault})` : SYSTEM_DEFAULT; },
  get openWith(): string {
    if (this.useSystem) return this.systemLabel;
    const o = [...this.opens]; return o.length > 2 ? `${o.length} browsers` : o.join(" and ");
  },
  opensIn(name: string) { return !this.useSystem && this.opens.has(name); },
  toggleOpen(name: string) {
    const next = new Set(this.useSystem ? [] : this.opens);   // ticking a browser leaves the system default
    if (next.has(name)) { if (next.size === 1) return toast("Links need at least one browser to open in.", "warning"); next.delete(name); }
    else next.add(name);
    this.opens = next;
  },
  get allOpen() { return !this.useSystem && this.installed.length > 0 && this.installed.every(b => this.opens.has(b.name)); },
  openInAll(on: boolean) { this.opens = on ? new Set(this.installed.map(b => b.name)) : new Set([SYSTEM_DEFAULT]); },
  readable,
  toggle(b: BrowserInfo) { if (!readable(b)) return; this.tracked.has(b.name) ? this.tracked.delete(b.name) : this.tracked.add(b.name); this.tracked = new Set(this.tracked); },
  async persist(announce = true) {
    try {
      await api.path("tabs", "settings").post({ tracked_browsers: [...this.tracked], open_with: [...this.opens], onboarded_browsers: true });
      if (announce) toast(`Tracking ${this.tracked.size ? [...this.tracked].join(", ") : "no browser"}; links open in ${this.openWith || "the default"}.`, "success", "Browsers saved");
      await this.load(); await legacy().refreshTabs();
    } catch (e) { this.error = String((e as Error).message ?? e); }
  },
  async save() { await this.persist(true); this.open = false; this.onboarding = false; },
});
