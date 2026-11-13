// The Timeline view: tab snapshots, bookmark edits and bookmarks added, by day, with two charts. Moved from the page
// script into a typed module on 2026-11-12; behaviour is unchanged.
//
// Everything it needs from the rest of the page arrives through TimelineDeps, so the module reads nothing global
// but the DOM hooks it owns (data-brofo-timeline="range|kinds|feed|tabs-chart|tabs-note|bm-chart|bm-note|query").
//
//   const timeline = new TimelineView(deps);   timeline.open();

import { Dom } from "../dom";
import { Pager } from "../pager";
import { RangePicker } from "../rangepick";
import type { BrowserScope } from "../scope";
import { el, icon, plural, svg } from "../ui";

type Kind = "tabs" | "edits" | "added";
interface Summary { windows: number; tabs: number; incognito_windows: number; incognito_tabs: number }
interface Change { mode: string; url: string; title: string }
interface TabSnap { id: string; at: string; reason: string; summary: Summary; opened_n?: number; closed_n?: number; opened?: Change[]; closed?: Change[] }
interface HistoryEntry { seq: number; t?: number; at: string; op: string; summary?: string; by?: string }
export interface AddedBookmark { id: number; u: string; a?: number }
interface Note { kind: string; key: string; note: string; tags: string[] }
type Ev = { kind: "tabs"; t: number; s: TabSnap } | { kind: "edits"; t: number; e: HistoryEntry } | { kind: "added"; t: number; items: AddedBookmark[] };

export interface TimelineDeps {
  scope: () => BrowserScope;                 // read when used: the page creates its scope after this view
  bookmarks: () => AddedBookmark[];          // the kept bookmarks (DATA.keep)
  titleOf: (id: number) => string;
  host: (url: string) => string;
  shortUrl: (url: string, max?: number) => string;
  workspaceId: () => string | null;          // null when the page has no workspace (opened as a file)
  pageSize: () => number;
  setPageSize: (n: number) => void;
  showSnapshot: (id: string) => void;        // go to Open tabs at that snapshot
  showAddedBetween: (from: Date, to: Date) => void;  // go to Bookmarks, filtered to those dates
  showHistory: () => void;
  noneChosen: () => HTMLElement;             // the "No browsers chosen" panel
  isOpen: () => boolean;                     // is the Timeline the current view
  storeKey: string;                          // where the chosen range is kept
}

const KINDS: [Kind, string, string][] = [["tabs", "Tab snapshots", "ti-browser"], ["edits", "Edits", "ti-pencil"], ["added", "Bookmarks added", "ti-bookmark-plus"]];
const EDIT_ICON: Record<string, string> = { migrate: "ti-arrows-exchange", start: "ti-flag", reset: "ti-restore", rollback: "ti-arrow-back-up", import: "ti-file-import", update: "ti-refresh-dot", lock: "ti-lock" };
const dayKey = (d: Date) => d.toLocaleDateString(undefined, { weekday: "long", year: "numeric", month: "long", day: "numeric" });
const timeOf = (d: Date) => d.toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" });

/** The Timeline's periods; 0 is everything kept. */
const RANGES = [{ key: "1", label: "24 hours" }, { key: "7", label: "7 days" }, { key: "30", label: "30 days" },
  { key: "365", label: "A year" }, { key: "0", label: "All" }];

export class TimelineView {
  private range: RangePicker | null = null;
  private span: { from: string; to: string } | null = null;  // a typed range, this visit only
  private days: number;
  private readonly on = new Set<Kind>(KINDS.map(k => k[0]));
  private tabs: { snapshots: TabSnap[] } | null = null;
  private hist: HistoryEntry[] = [];
  private notes = new Map<string, Note>();
  private page = 1;

  constructor(private readonly d: TimelineDeps) {
    this.days = (() => { try { return JSON.parse(localStorage.getItem(d.storeKey) ?? "null") ?? 7; } catch { return 7; } })();
    addEventListener("bf:scope", () => { if (d.isOpen()) void this.open(); });
    const q = this.part<HTMLInputElement>("query");
    if (q) q.oninput = () => this.paint();
  }

  async open(): Promise<void> {
    this.page = 1;
    this.paintControls();
    this.part("feed")?.replaceChildren(el("div", "text-secondary", "Loading…"));
    await this.load();
    this.paint();
  }

  private part<T extends HTMLElement = HTMLElement>(name: string): T | null { return Dom.one<T>("timeline", name); }
  /** The range shown, in ms (either bound may be open): the typed one, else the last `days`. */
  private bounds(): [number, number] {
    if (this.span) return [this.span.from ? new Date(this.span.from).getTime() : -Infinity, this.span.to ? new Date(this.span.to).getTime() : Infinity];
    return [this.days ? Date.now() - this.days * 86_400_000 : -Infinity, Infinity];
  }
  private inRange = (t: number) => { const [a, b] = this.bounds(); return t >= a && t <= b; };

  private async load(): Promise<void> {
    const q = this.d.scope().query();
    const json = async <T>(url: string, fallback: T): Promise<T> => { try { const r = await fetch(url); return r.ok ? await r.json() as T : fallback; } catch { return fallback; } };
    const when = this.span ? new URLSearchParams(Object.entries(this.span).filter(([, v]) => v)).toString() : `days=${this.days}`;
    this.tabs = await json(`api/tabs/timeline?${when}${q ? "&" + q : ""}`, { snapshots: [] });
    this.notes = new Map((await json<{ notes: Note[] }>("api/tabs/notes", { notes: [] })).notes.map(n => [`${n.kind}|${n.key}`, n]));
    const ws = this.d.workspaceId();
    this.hist = ws ? (await json<{ entries: HistoryEntry[] }>(`api/ws/${encodeURIComponent(ws)}/history?limit=2000`, { entries: [] })).entries : [];
  }

  private paintControls(): void {
    const host = this.part("range");
    if (host && !this.range) {
      this.range = new RangePicker({ label: "Range", heading: "Show the last…", value: String(this.days), defaultKey: "7", options: RANGES,
        onPick: async key => { this.days = Number(key); this.span = null; try { localStorage.setItem(this.d.storeKey, key); } catch { /* this visit only */ } await this.open(); },
        onRange: async (from, to) => { this.span = from || to ? { from, to } : null; await this.open(); } });
      host.replaceChildren(this.range.el);
    }
    if (this.span) this.range?.setRange(this.span.from, this.span.to); else this.range?.set(String(this.days));
    const k = this.part("kinds"); if (!k) return;
    k.replaceChildren();
    for (const [id, label, ic] of KINDS) {
      const b = el("button", "btn btn-sm tl-kind" + (this.on.has(id) ? " active" : "")); b.append(icon(ic, "me-1"), label);
      b.onclick = () => { if (this.on.has(id)) this.on.delete(id); else this.on.add(id); this.page = 1; this.paintControls(); this.paint(); };
      k.append(b);
    }
  }

  private events(): Ev[] {
    const ev: Ev[] = [];
    if (this.on.has("tabs")) for (const s of this.tabs?.snapshots ?? []) ev.push({ kind: "tabs", t: new Date(s.at).getTime(), s });
    if (this.on.has("edits")) for (const e of this.hist) { const t = (e.t ?? 0) * 1000 || Date.parse(e.at); if (this.inRange(t)) ev.push({ kind: "edits", t, e }); }
    if (this.on.has("added")) {
      const byDay = new Map<string, { t: number; items: AddedBookmark[] }>();
      for (const i of this.d.bookmarks()) {
        if (!i.a) continue;
        const t = i.a * 1000; if (!this.inRange(t)) continue;
        const day = new Date(t), key = day.toDateString();
        let g = byDay.get(key);
        if (!g) { g = { t: new Date(day.getFullYear(), day.getMonth(), day.getDate(), 23, 59).getTime(), items: [] }; byDay.set(key, g); }
        g.items.push(i);
      }
      for (const g of byDay.values()) ev.push({ kind: "added", t: g.t, items: g.items });
    }
    const q = (this.part<HTMLInputElement>("query")?.value ?? "").trim().toLowerCase();
    const noteText = (u: string) => { const n = this.notes.get("tab|" + u); return n ? ` ${n.note} ${n.tags.join(" ")}` : ""; };
    const text = (x: Ev) => x.kind === "tabs" ? [...(x.s.opened ?? []), ...(x.s.closed ?? [])].map(o => `${o.title} ${o.url}${noteText(o.url)}`).join(" ")
      : x.kind === "edits" ? `${x.e.summary ?? ""} ${x.e.op ?? ""}` : x.items.map(i => `${this.d.titleOf(i.id) || ""} ${i.u}`).join(" ");
    return ev.filter(x => !q || text(x).toLowerCase().includes(q)).sort((a, b) => b.t - a.t);
  }

  private paint(): void {
    this.charts();
    const feed = this.part("feed"); if (!feed) return;
    feed.replaceChildren();
    if (this.d.scope().isNone && this.on.has("tabs") && this.on.size === 1) { feed.append(this.d.noneChosen()); return; }
    const ev = this.events();
    if (!ev.length) { feed.append(el("div", "pane-note", "Nothing in this range. Choose a longer range, or turn on more kinds of events.")); return; }
    const pager = new Pager({ size: this.d.pageSize(), noun: "events", onChange: p => {
      if (p.pageSize !== this.d.pageSize()) this.d.setPageSize(p.pageSize);
      this.page = p.page; this.paint(); feed.scrollIntoView({ block: "start" });
    } }).total(ev.length).goto(this.page);
    let day = "", list: HTMLElement | null = null;
    for (const x of pager.slice(ev)) {
      const when = new Date(x.t), k = dayKey(when);
      if (k !== day || !list) { day = k; feed.append(el("div", "tl-feed-day", k)); list = el("div", "tl-feed-list"); feed.append(list); }
      list.append(this.item(x, when));
    }
    if (pager.needed) feed.append(pager.render());
  }

  private go(label: string, fn: () => void): HTMLButtonElement {
    const b = el("button", "btn btn-sm btn-ghost-primary tl-ev-go"); b.append(label, icon("ti-arrow-right", "ms-1")); b.onclick = fn; return b;
  }

  private item(x: Ev, when: Date): HTMLElement {
    const r = el("div", "tl-ev tl-" + x.kind), dot = el("span", "tl-ev-dot"), body = el("div", "tl-ev-body min-w-0");
    const head = el("div", "tl-ev-head");
    if (x.kind === "tabs") {
      const s = x.s, m = s.summary;
      dot.append(icon(s.reason === "manual" ? "ti-camera" : "ti-browser"));
      head.append(el("b", null, `${plural(m.windows, "window")} · ${m.tabs} tabs`), m.incognito_tabs ? el("span", "tabs-inc-pill", `${m.incognito_tabs} incognito`) : "",
        el("span", "text-secondary", `${s.reason === "manual" ? "saved by you" : "saved automatically"} · ${timeOf(when)}`));
      body.append(head);
      if (s.opened_n != null) {
        const diff = el("div", "tl-ev-diff");
        if (s.opened_n) diff.append(el("span", "tl-plus", `+${s.opened_n} opened`));
        if (s.closed_n) diff.append(el("span", "tl-minus", `−${s.closed_n} closed`));
        if (!s.opened_n && !s.closed_n) diff.append(el("span", "text-secondary", "no change"));
        body.append(diff);
        this.details(body, "Opened", s.opened ?? [], s.opened_n);
        this.details(body, "Closed", s.closed ?? [], s.closed_n ?? 0);
      }
      r.append(dot, body, this.go("Open in Open tabs", () => this.d.showSnapshot(s.id)));
    } else if (x.kind === "edits") {
      const e = x.e;
      dot.append(icon(EDIT_ICON[e.op] ?? "ti-pencil"));
      head.append(el("b", null, e.summary || e.op),
        el("span", "text-secondary", `${e.by === "mcp" ? "by an assistant (MCP)" : e.by === "page" ? "in this page" : e.by ?? ""} · ${timeOf(when)}`));
      body.append(head);
      r.append(dot, body, this.go("History", () => this.d.showHistory()));
    } else {
      dot.append(icon("ti-bookmark-plus"));
      const n = x.items.length;
      head.append(el("b", null, `${plural(n, "bookmark")} added`), el("span", "text-secondary", "that day"));
      const names = el("div", "tl-ev-names text-secondary small");
      names.textContent = x.items.slice(0, 4).map(i => this.d.titleOf(i.id) || this.d.host(i.u)).join(" · ") + (n > 4 ? ` · +${n - 4} more` : "");
      body.append(head, names);
      r.append(dot, body, this.go("Show them", () => {
        const d0 = new Date(x.t), from = new Date(d0.getFullYear(), d0.getMonth(), d0.getDate());
        this.d.showAddedBetween(from, new Date(from.getTime() + 86_399_000));
      }));
    }
    return r;
  }

  private details(body: HTMLElement, label: string, arr: Change[], n: number): void {
    if (!n) return;
    const dd = el("details", "tl-ev-det"); dd.append(el("summary", null, `${label} (${n})`));
    const ul = el("div", "tl-ev-list");
    for (const o of arr) {
      const li = el("div", "tl-ev-li" + (o.mode === "incognito" ? " inc" : ""));
      li.append(el("span", "text-truncate", o.title || o.url), el("span", "text-secondary text-truncate small", this.d.shortUrl(o.url, 60)));
      li.dataset.tip = o.url; ul.append(li);
    }
    if (n > arr.length) ul.append(el("div", "text-secondary small", `and ${n - arr.length} more`));
    dd.append(ul); body.append(dd);
  }

  // Two small SVG charts drawn with the theme's own colours; every point or bar leads to that moment.
  private charts(): void {
    const snaps = this.tabs?.snapshots ?? [];
    const tn = this.part("tabs-note"); if (tn) tn.textContent = snaps.length ? plural(snaps.length, "snapshot") : "";
    const tc = this.part("tabs-chart");
    if (tc) {
      tc.replaceChildren();
      if (!snaps.length) tc.append(el("div", "text-secondary small p-3", "No tab snapshots in this range yet."));
      else tc.append(this.lineChart(snaps.map(s => ({ t: new Date(s.at).getTime(), v: s.summary.tabs, v2: s.summary.incognito_tabs, id: s.id }))));
    }
    // Bookmarks added: by day for two months or less, by month for a year or less, by year beyond that.
    const its = this.d.bookmarks().filter(i => i.a && this.inRange(i.a * 1000));
    const [lo, hi] = this.bounds(), first = Math.min(...its.map(i => (i.a ?? 0) * 1000));
    const spanDays = ((Number.isFinite(hi) ? hi : Date.now()) - (Number.isFinite(lo) ? lo : first)) / 86_400_000;
    const by = spanDays > 400 ? "year" : spanDays > 62 ? "month" : "day", buckets = new Map<string, number>();
    const pad = (n: number) => String(n).padStart(2, "0");
    for (const i of its) {
      const d = new Date((i.a ?? 0) * 1000);
      const k = by === "year" ? `${d.getFullYear()}` : by === "month" ? `${d.getFullYear()}-${pad(d.getMonth() + 1)}` : `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
      buckets.set(k, (buckets.get(k) ?? 0) + 1);
    }
    const bn = this.part("bm-note"); if (bn) bn.textContent = `${its.length.toLocaleString()} in range · by ${by}`;
    const bc = this.part("bm-chart");
    if (bc) {
      bc.replaceChildren();
      const keys = [...buckets.keys()].sort();
      if (!keys.length) bc.append(el("div", "text-secondary small p-3", "No bookmarks were added in this range."));
      else bc.append(this.barChart(keys.map(k => ({ k, v: buckets.get(k) ?? 0 })), by));
    }
  }

  private lineChart(pts: { t: number; v: number; v2: number; id: string }[]): SVGSVGElement {
    const W = 560, H = 150, P = { l: 34, r: 10, t: 10, b: 22 };
    const root = svg("svg", { viewBox: `0 0 ${W} ${H}`, class: "tl-chart", role: "img", "aria-label": "Open tabs over time" });
    const t0 = pts[0]!.t, t1 = pts[pts.length - 1]!.t || t0 + 1, vmax = Math.max(1, ...pts.map(p => p.v + (p.v2 || 0)));
    const x = (t: number) => P.l + (t1 === t0 ? (W - P.l - P.r) / 2 : (t - t0) / (t1 - t0) * (W - P.l - P.r));
    const y = (v: number) => H - P.b - v / vmax * (H - P.t - P.b);
    for (const g of [0, .5, 1]) { const yy = y(vmax * g); root.append(svg("line", { x1: P.l, x2: W - P.r, y1: yy, y2: yy, class: "tl-grid" }), svg("text", { x: P.l - 6, y: yy + 3, class: "tl-axis", "text-anchor": "end" }, Math.round(vmax * g))); }
    const path = (key: "v" | "v2", cls: string) => root.append(svg("path", { d: pts.map((p, k) => `${k ? "L" : "M"}${x(p.t).toFixed(1)},${y(p[key] || 0).toFixed(1)}`).join(" "), class: cls }));
    path("v", "tl-line"); if (pts.some(p => p.v2)) path("v2", "tl-line inc");
    for (const p of pts) {
      const c = svg("circle", { cx: x(p.t), cy: y(p.v), r: 4, class: "tl-pt", tabindex: 0 });
      c.append(svg("title", {}, `${new Date(p.t).toLocaleString()}: ${p.v} tabs${p.v2 ? `, ${p.v2} incognito` : ""}`));
      c.addEventListener("click", () => this.d.showSnapshot(p.id));
      c.addEventListener("keydown", e => { if (e.key === "Enter") this.d.showSnapshot(p.id); });
      root.append(c);
    }
    const lab = (t: number) => new Date(t).toLocaleString(undefined, { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" });
    root.append(svg("text", { x: P.l, y: H - 6, class: "tl-axis" }, lab(t0)), svg("text", { x: W - P.r, y: H - 6, class: "tl-axis", "text-anchor": "end" }, lab(t1)));
    return root;
  }

  private barChart(rows: { k: string; v: number }[], by: "year" | "month" | "day"): SVGSVGElement {
    const W = 560, H = 150, P = { l: 34, r: 10, t: 10, b: 22 }, n = rows.length, bw = Math.min(48, (W - P.l - P.r) / n), vmax = Math.max(1, ...rows.map(r => r.v));
    P.l += ((W - P.l - P.r) - bw * n) / 2;  // a few bars sit centred instead of stretching across the chart
    const root = svg("svg", { viewBox: `0 0 ${W} ${H}`, class: "tl-chart", role: "img", "aria-label": "Bookmarks added" });
    for (const g of [0, .5, 1]) { const yy = H - P.b - g * (H - P.t - P.b); root.append(svg("line", { x1: 34, x2: W - 10, y1: yy, y2: yy, class: "tl-grid" }), svg("text", { x: 28, y: yy + 3, class: "tl-axis", "text-anchor": "end" }, Math.round(vmax * g))); }
    rows.forEach((r, k) => {
      const h = r.v / vmax * (H - P.t - P.b);
      const b = svg("rect", { x: P.l + k * bw + Math.min(2, bw * .15), y: H - P.b - h, width: Math.max(1, bw - Math.min(4, bw * .3)), height: h, rx: 2, class: "tl-bar", tabindex: 0 });
      b.append(svg("title", {}, `${bucketLabel(r.k, by, true)}: ${plural(r.v, "bookmark")} added`));
      b.addEventListener("click", () => {
        const [yy, mm = 1, dd = 1] = r.k.split("-").map(Number) as [number, number?, number?];
        const from = new Date(yy, mm - 1, dd);
        const to = by === "year" ? new Date(yy + 1, 0, 1) : by === "month" ? new Date(yy, mm, 1) : new Date(yy, mm - 1, dd + 1);
        this.d.showAddedBetween(from, new Date(to.getTime() - 60_000));
      });
      root.append(b);
    });
    const step = Math.ceil(n / 8);
    rows.forEach((r, k) => { if (k % step === 0) root.append(svg("text", { x: P.l + k * bw + bw / 2, y: H - 6, class: "tl-axis", "text-anchor": "middle" }, bucketLabel(r.k, by))); });
    return root;
  }
}

/** "2026-11" as people read it: "Nov 26" on the axis, "November 2026" in full. */
/** A bar's label: "2024", "Mar 24" / "March 2024", or "12 Mar" / "12 March 2024". */
function bucketLabel(k: string, by: "year" | "month" | "day", full = false): string {
  if (by === "year") return k;
  const [y, m, d = 1] = k.split("-").map(Number) as [number, number, number?];
  const at = new Date(y, m - 1, d);
  if (by === "month") return at.toLocaleDateString(undefined, full ? { month: "long", year: "numeric" } : { month: "short", year: "2-digit" });
  return at.toLocaleDateString(undefined, full ? { day: "numeric", month: "long", year: "numeric" } : { day: "numeric", month: "short" });
}
