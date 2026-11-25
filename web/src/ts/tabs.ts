// Open tabs: the shapes the server sends, and the pure rules for finding tabs worth closing.
import { api } from "./api";

export type Mode = "normal" | "incognito";
export interface Tab { url: string; title: string; id?: number | null }
export interface Win { index: number; id?: number | null; mode: Mode; active?: number | null; tabs?: Tab[]; tab_count?: number; redacted?: boolean }
export interface Browser { browser: string; running?: boolean; error?: string; windows?: Win[] }
export interface Live { at: string; browsers: Browser[] }

/** One open tab with everything needed to close it safely (ids) and to reopen it (mode). */
export interface OpenTab { browser: string; windowId: number | null; windowN: number; tabId: number | null; pos: number; url: string; title: string; mode: Mode }

export const flatten = (live: Live): OpenTab[] =>
  live.browsers.flatMap(b => (b.windows ?? []).flatMap((w, wi) => (w.tabs ?? []).map((t, pos) => ({
    browser: b.browser, windowId: w.id ?? null, windowN: wi + 1, tabId: t.id ?? null, pos, url: t.url, title: t.title, mode: w.mode,
  }))));

/** Addresses compared the way people mean "the same page": no fragment, no trailing slash, no www. */
export const sameKey = (u: string): string => {
  try {
    const x = new URL(u);
    x.hash = "";
    return (x.protocol + "//" + x.hostname.replace(/^www\./, "") + x.pathname.replace(/\/$/, "") + x.search).toLowerCase();
  } catch { return u.toLowerCase(); }
};

export const isWeb = (u: string): boolean => /^https?:\/\//i.test(u);

export class TabCleaner {
  constructor(private readonly tabs: OpenTab[]) {}

  /** Every copy after the first of a page open more than once, within the same mode (incognito never pairs with normal). */
  duplicates(): OpenTab[] {
    const seen = new Set<string>();
    return this.tabs.filter(t => { if (!isWeb(t.url)) return false; const k = t.mode + " " + sameKey(t.url); if (seen.has(k)) return true; seen.add(k); return false; });
  }

  /** Tabs first seen more than `days` ago, oldest first. firstSeen comes from the snapshots (POST tabs/ages). */
  stale(firstSeen: Record<string, string>, days: number, now = Date.now()): (OpenTab & { since: string })[] {
    const cutoff = now - days * 86400000;
    return this.tabs.filter(t => t.mode === "normal" && firstSeen[t.url] && Date.parse(firstSeen[t.url]!) < cutoff)
      .map(t => ({ ...t, since: firstSeen[t.url]! })).sort((a, b) => Date.parse(a.since) - Date.parse(b.since));
  }

  /** Tabs whose page is already one of your bookmarks. */
  bookmarked(bookmarkUrls: Iterable<string>): OpenTab[] {
    const keys = new Set([...bookmarkUrls].filter(isWeb).map(sameKey));
    return this.tabs.filter(t => isWeb(t.url) && keys.has(sameKey(t.url)));
  }

  incognito(): OpenTab[] { return this.tabs.filter(t => t.mode === "incognito"); }
}

export interface CloseResult { closed: number; skipped: number; details: { url?: string; why: string }[]; undo_snapshot?: string; after?: string }

/** Close tabs through the server. It stores a snapshot first; the result names it, so the close can be undone. */
export const closeTabs = (tabs: OpenTab[]): Promise<CloseResult> =>
  api.path("tabs", "close").post<CloseResult>({
    items: tabs.filter(t => isWeb(t.url)).map(t => ({ browser: t.browser, window_id: t.windowId, tab_id: t.tabId, pos: t.pos, url: t.url })),
  });

/** A tab's key for a selection: its browser's own ids when it has them. Without ids (a browser read by position) the
 *  address is part of the key, so when a re-read moves tabs along, a selected tab drops out instead of the tab that took
 *  its place being selected, and then closed. */
export const keyOf = (t: OpenTab): string =>
  t.tabId != null ? `${t.browser}|${t.windowId ?? t.windowN}|${t.tabId}` : `${t.browser}|${t.windowN}|${t.pos}|${t.url}`;

// ---- windows as the Open tabs view lists them: flat records, grouped and sorted any way (moved from page.ts) ----
/** One window in a read or snapshot: its browser, its number in that browser (1-based), and whether it is incognito.
 *  `missing` (filled in by the view for saved snapshots) holds the tabs that are no longer open. */
export interface WinRecord { browser: string; n: number; w: Win; inc: boolean; missing?: Tab[] }
/** One tab with its position in its window and the window it belongs to. */
export interface TabItem { t: Tab; pos: number; x: WinRecord }
export type WinSort = "original" | "most" | "fewest" | "browser";
export type TabSort = "original" | "title" | "site";

const hostOf = (u: string): string => { try { return new URL(u).hostname.replace(/^www\./, ""); } catch { return ""; } };

/** Every window of a read or snapshot, skipping browsers that could not be read. */
export const windowsOf = (d: { browsers?: Browser[] }): WinRecord[] =>
  (d.browsers ?? []).filter(b => !b.error).flatMap(b => (b.windows ?? []).map((w, wi) => ({ browser: b.browser, n: wi + 1, w, inc: w.mode === "incognito" })));

export const tabsIn = (x: WinRecord): TabItem[] => (x.w.tabs ?? []).map((t, pos) => ({ t, pos, x }));

/** Tabs in a window: the listed ones, or the kept count when incognito pages were not saved. */
export const tabCount = (x: WinRecord): number => x.w.tabs ? x.w.tabs.length : x.w.tab_count ?? 0;

export function sortTabs(list: TabItem[], by: TabSort): TabItem[] {
  if (by === "title") return [...list].sort((a, b) => (a.t.title || a.t.url).localeCompare(b.t.title || b.t.url));
  if (by === "site") return [...list].sort((a, b) => hostOf(a.t.url).localeCompare(hostOf(b.t.url)) || a.pos - b.pos);
  return list;
}

export function sortWindows(xs: WinRecord[], by: WinSort): WinRecord[] {
  if (by === "most") return [...xs].sort((a, b) => tabCount(b) - tabCount(a));
  if (by === "fewest") return [...xs].sort((a, b) => tabCount(a) - tabCount(b));
  if (by === "browser") return [...xs].sort((a, b) => a.browser.localeCompare(b.browser) || a.n - b.n);
  return xs;
}

// ---- opening a set of tabs: which windows, which mode (moved from page.ts on 2026-11-12) ----
/** "window": new windows; "incognito": new incognito windows; "tabs": tabs in the current window. */
export type OpenHow = "window" | "incognito" | "tabs";
export interface OpenBatch { urls: string[]; how: OpenHow }
export interface OpenPlan { batches: OpenBatch[]; total: number; skipped: number }

/** How a set of tabs opens. Only web links open. Grouped: one window per original window; otherwise one window per
 *  mode. Incognito tabs never land in a normal window unless `crossMode` (the caller asked for exactly that), and
 *  never as tabs in the current window (counted in `skipped`). Empty batches are dropped. */
export function planOpen(items: TabItem[], how: OpenHow, grouped: boolean, crossMode = false): OpenPlan {
  const web = items.filter(i => isWeb(i.t.url));
  const toInc = (i: TabItem) => how === "incognito" || (i.x.inc && !crossMode);
  const batches: OpenBatch[] = [];
  if (how === "tabs") batches.push({ urls: web.filter(i => !i.x.inc).map(i => i.t.url), how: "tabs" });
  else if (grouped) {
    for (const x of [...new Set(web.map(i => i.x))])
      batches.push({ urls: web.filter(i => i.x === x).map(i => i.t.url), how: how === "incognito" || (x.inc && !crossMode) ? "incognito" : "window" });
  } else {
    const inc = web.filter(toInc), norm = web.filter(i => !toInc(i));
    if (norm.length) batches.push({ urls: norm.map(i => i.t.url), how: "window" });
    if (inc.length) batches.push({ urls: inc.map(i => i.t.url), how: "incognito" });
  }
  const real = batches.filter(b => b.urls.length);
  return { batches: real, total: real.reduce((n, b) => n + b.urls.length, 0), skipped: how === "tabs" ? web.filter(i => i.x.inc).length : 0 };
}

// ---- the Open tabs view's state rules: counts, times, list rows (moved from page.ts on 2026-11-12) ----
export interface TabSummary { windows: number; tabs: number; incognito_windows: number; incognito_tabs: number }

/** Window and tab counts for the browsers shown only, normal and incognito apart (a read covers every tracked one). */
export function scopedSummary(l: { browsers?: Browser[] } | null | undefined, inScope: (browser: string) => boolean): TabSummary {
  const s: TabSummary = { windows: 0, tabs: 0, incognito_windows: 0, incognito_tabs: 0 };
  for (const b of l?.browsers ?? []) if (inScope(b.browser)) for (const w of b.windows ?? []) {
    const n = w.tabs ? w.tabs.length : w.tab_count ?? 0;
    if (w.mode === "incognito") { s.incognito_windows++; s.incognito_tabs += n; } else { s.windows++; s.tabs += n; }
  }
  return s;
}

/** "Tue 10:14 AM": a snapshot's time in the reader's own format; "" for a bad date. */
export const when = (iso: string): string => { const d = new Date(iso); return isNaN(+d) ? "" : d.toLocaleString(undefined, { weekday: "short", hour: "numeric", minute: "2-digit" }); };

/** "just now", "5 min ago", "3 h ago", "2 d ago", then the date. */
export function ago(iso: string, now = Date.now()): string {
  const s = (now - +new Date(iso)) / 1000;
  return s < 60 ? "just now" : s < 3600 ? `${Math.round(s / 60)} min ago` : s < 86400 ? `${Math.round(s / 3600)} h ago`
    : s < 30 * 86400 ? `${Math.round(s / 86400)} d ago` : new Date(iso).toLocaleDateString();
}

const wins = (n: number) => `${n} window${n === 1 ? "" : "s"}`;

/** The "Open now" row's second line. */
export const liveRowSub = (s: TabSummary | null): string =>
  s ? `${wins(s.windows)} · ${s.tabs} tabs` + (s.incognito_windows ? ` · ${s.incognito_tabs} incognito` : "") : "reading the browsers…";

export interface SnapshotInfo { id: string; at: string; reason?: string; browsers?: string[]; incognito_pages_kept?: boolean;
  summary?: { windows?: number; tabs?: number; incognito_tabs?: number } }
export interface TimelineRow { id: string; day: string; title: string; sub: string; icon: string }

/** A saved snapshot as a row of the Open tabs list: its day (for the day headings), time, counts and browsers. */
export function snapshotRow(s: SnapshotInfo): TimelineRow {
  const d = new Date(s.at), m = s.summary ?? {}, manual = s.reason === "manual";
  return {
    id: s.id, icon: manual ? "ti-camera" : "ti-clock",
    day: d.toLocaleDateString(undefined, { weekday: "long", month: "short", day: "numeric" }),
    title: d.toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" }) + (manual ? " · saved by you" : ""),
    sub: `${wins(m.windows || 0)} · ${m.tabs || 0} tabs` + (m.incognito_tabs ? ` · ${m.incognito_tabs} incognito${s.incognito_pages_kept ? "" : " (count only)"}` : "") + ` · ${(s.browsers ?? []).join(", ")}`,
  };
}

/** The footer's autosave line. */
export const autosaveText = (cfg: { autosave?: boolean; save_mode?: string; interval?: number }): string =>
  !cfg.autosave ? "Tab autosave off" : cfg.save_mode === "interval" ? `Tabs saved every ${Math.round((cfg.interval ?? 0) / 60)} min` : "Tabs saved as they change";

/** One read at a time: a second caller while a read runs waits for that same read; a result younger than `maxAge`
 *  ms is reused. The page's live read of the browsers goes through this. */
export class SingleRead<T extends object> {
  private value: (T & { _got?: number }) | null = null;
  private running: Promise<T & { _got?: number }> | null = null;
  constructor(private readonly read: () => Promise<T>, private readonly onError: (e: unknown) => T, private readonly clock: () => number = Date.now) {}
  get current(): (T & { _got?: number }) | null { return this.value; }
  /** Is there a result, and is it younger than `maxAge` ms? */
  fresh(maxAge: number): boolean { return !!this.value?._got && this.clock() - this.value._got < maxAge; }
  async get(maxAge = 0): Promise<T & { _got?: number }> {
    if (this.value && this.fresh(maxAge)) return this.value;
    if (this.running) return this.running;
    this.running = (async () => {
      let v: T & { _got?: number };
      try { v = await this.read(); } catch (e) { v = this.onError(e); }
      v._got = this.clock(); this.value = v; return v;
    })();
    try { return await this.running; } finally { this.running = null; }
  }
}
