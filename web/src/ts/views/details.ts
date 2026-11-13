// The details pane's building blocks: status names and colours, section cards, alerts, fact grids, the folder summary
// (health bar, status grid, facts, where the bookmarks came from) and the slide-in "All details" panel. The page
// decides what to show and which actions an alert offers; this builds it. Moved from the page script on 2026-11-12;
// same markup.

import { $, el, fmtDay, icon } from "../ui";
import type { BookmarkId, Folder } from "../tree";
import { isFolder, nice } from "../tree";

/** A bookmark as the details pane reads it. */
export interface DetailItem { id: BookmarkId; u: string; s: string; o: string; a?: number }

export const STATUS_ORDER = ["ok", "moved", "moved-elsewhere", "needs-login", "unverified", "dead", "local", "skipped", "unchecked", "n/a"] as const;
export const STATUS_BG: Record<string, string> = { ok: "bg-success", moved: "bg-warning", "moved-elsewhere": "bg-purple", "needs-login": "bg-info",
  unverified: "bg-secondary", dead: "bg-danger", local: "bg-dark-lt", skipped: "bg-dark-lt", unchecked: "bg-secondary-lt", "n/a": "bg-dark-lt" };
/** Short labels where space is tight (the folder summary); the full name stays in the tooltip. */
export const STATUS_SHORT: Record<string, string> = { ok: "Working", moved: "Moved", "moved-elsewhere": "Other site", "needs-login": "Needs login",
  unverified: "Unverified", dead: "Dead", local: "Local", skipped: "Skipped", unchecked: "Not checked yet", "n/a": "Not a web link" };
export const STATUS_NAME: Record<string, string> = { ok: "Working", moved: "Moved, update ready", "moved-elsewhere": "Redirects to another site", "needs-login": "Requires login",
  unverified: "Could not be verified", dead: "Dead", local: "Local or staging", skipped: "Not checked (action link)", unchecked: "Not checked yet", "n/a": "Not a web link" };

const countBy = <T>(xs: T[], key: (x: T) => string): Record<string, number> => { const c: Record<string, number> = {}; for (const x of xs) { const k = key(x); c[k] = (c[k] ?? 0) + 1; } return c; };
const statusCounts = (its: DetailItem[]) => countBy(its, i => i.s);
const statusesIn = (c: Record<string, number>) => STATUS_ORDER.filter(k => c[k]);
const pctOf = (n: number, of: number) => Math.round(100 * n / Math.max(1, of)) + "%";
/** How many folders sit inside `f`, at any depth. */
export const countFolders = (f: Folder): number => f.c.reduce((n: number, x) => isFolder(x) ? n + 1 + countFolders(x) : n, 0);

export function sectionCard(title: string, body: Node): HTMLElement {
  const c = el("div", "card card-sm mb-3"), h = el("div", "card-header"), b = el("div", "card-body");
  h.append(el("h3", "card-title", title)); b.append(body); c.append(h, b); return c;
}

export type AlertAction = [label: string, icon: string, run: () => unknown, cls?: string];
/** A Tabler alert with a title, a line of text and buttons; `after` runs after any button (the page refocuses the tree). */
export function alertBox(type: string, ic: string, title: string, text: string, actions: AlertAction[] = [], after: () => void = () => {}): HTMLElement {
  const a = el("div", `alert alert-${type} mb-3`); a.setAttribute("role", "alert");
  const d = el("div", "d-flex gap-2"), t = el("div", "min-w-0");
  d.append(icon(ic + " alert-icon")); t.append(el("h4", "alert-title", title));
  if (text) t.append(el("div", "text-secondary", text));
  if (actions.length) {
    const bl = el("div", "btn-list mt-2");
    for (const [l, aic, fn, cls] of actions) { const b = el("button", "btn btn-sm " + (cls || "")); b.append(icon(aic, "me-1"), l); b.onclick = () => { fn(); after(); }; bl.append(b); }
    t.append(bl);
  }
  d.append(t); a.append(d); return a;
}

/** Label/value pairs as a Tabler datagrid; empty values are left out, an element marked data-wide spans the row. */
export function datagrid(pairs: [string, string | Node | null | undefined][]): HTMLElement {
  const g = el("div", "datagrid mt-3");
  for (const [t, v] of pairs) {
    if (v == null || v === "") continue;
    const it = el("div", "datagrid-item"); it.append(el("div", "datagrid-title", t));
    if (v instanceof HTMLElement && v.dataset.wide) it.style.gridColumn = "1 / -1";
    const c = el("div", "datagrid-content"); if (v instanceof Node) c.append(v); else c.textContent = v;
    it.append(c); g.append(it);
  }
  return g;
}

export interface SummaryDeps {
  canFilter: (status: string) => boolean;   // is there a "Show" chip for this status
  filter: (status: string) => void;
  openDetails: (f: Folder, focus?: "from") => void;
  help: Record<string, string>;
}

/** The folder overview: one flat panel in two balanced columns. Left: the health bar and an even grid of statuses
 *  (click one to filter). Right: the facts and the folders the bookmarks came from. Stacks on narrow screens. */
export function folderSummary(f: Folder, its: DetailItem[], d: SummaryDeps): HTMLElement {
  const c = statusCounts(its), order = statusesIn(c), n = its.length;
  const box = el("div", "fsum"), left = el("div", "fsum-main"), right = el("div", "fsum-side");
  const bar = el("div", "progress progress-separated fsum-bar");
  for (const k of order) {
    const seg = el("div", "progress-bar " + STATUS_BG[k]); seg.style.width = (100 * c[k]! / n) + "%";
    seg.dataset.tip = `${STATUS_NAME[k]}: ${c[k]!.toLocaleString()} (${Math.round(100 * c[k]! / n)}%)`; bar.append(seg);
  }
  const grid = el("div", "fsum-grid");
  for (const k of order) {
    const can = d.canFilter(k);
    const p = el(can ? "button" : "div", "fsum-stat" + (can ? " clickable" : ""));
    if (can && p instanceof HTMLButtonElement) { p.type = "button"; p.onclick = () => d.filter(k); }
    p.append(el("span", "legend-dot " + STATUS_BG[k]), el("span", "fsum-name", STATUS_SHORT[k] || STATUS_NAME[k]), el("span", "fsum-num", c[k]!.toLocaleString()),
      el("span", "fsum-pct", Math.round(100 * c[k]! / n) + "%"));
    if (d.help[k]) p.dataset.tip = d.help[k] + (can ? " Click to show only these." : "");
    grid.append(p);
  }
  const lh = el("div", "fsum-head d-flex align-items-center"); lh.append(el("span", "flex-fill", "Link health"));
  const all = el("button", "btn btn-link btn-sm p-0 fsum-all"); all.type = "button"; all.append("All details", icon("ti-layout-sidebar-right-expand", "ms-1")); all.onclick = () => d.openDetails(f); lh.append(all);
  left.append(lh, bar, grid);
  const dates = its.map(i => i.a).filter((x): x is number => !!x), lo = Math.min(...dates), hi = Math.max(...dates);
  const facts = el("div", "fsum-facts");
  const fact = (ic: string, label: string, value: string) => { const x = el("div", "fsum-fact"); x.append(icon(ic), el("span", "text-secondary", label), el("b", null, value)); facts.append(x); };
  fact("ti-bookmarks", "Bookmarks", n.toLocaleString());
  const nf = countFolders(f); if (nf) fact("ti-folders", "Folders inside", nf.toLocaleString());
  if (dates.length) fact("ti-calendar", "Added", lo === hi ? fmtDay(lo) : `${fmtDay(lo)} – ${fmtDay(hi)}`);
  right.append(el("div", "fsum-head", "About"), facts);
  const entries = Object.entries(countBy(its, i => nice(i.o))).sort((a, b) => b[1] - a[1]);
  if (entries.length) {
    right.append(el("div", "fsum-head mt-3", entries.length === 1 ? "Came from" : `Came from ${entries.length} of your folders`));
    const ul = el("div", "fsum-from");
    // A folder name used in several places shows its parent too, so two "Tools & resources" can be told apart.
    const leafOf = (k: string) => (k.split("/").pop() || k).trim() || k;
    const seen = new Map<string, number>();
    for (const [k] of entries) seen.set(leafOf(k), (seen.get(leafOf(k)) || 0) + 1);
    for (const [k, v] of entries.slice(0, 4)) {
      const row = el("div", "fsum-fromrow"), parts = k.split("/"), leaf = (seen.get(leafOf(k)) || 0) > 1 && parts.length > 1 ? parts.slice(-2).join(" › ") : leafOf(k);
      const nm = el("span", "text-truncate"); nm.append(icon("ti-folder", "text-secondary me-1"), leaf); nm.dataset.tip = k;
      row.append(nm, el("span", "text-secondary", v.toLocaleString())); ul.append(row);
    }
    if (entries.length > 4) { const mo = el("button", "fsum-more btn btn-link p-0", `+ ${entries.length - 4} more`); mo.type = "button"; mo.onclick = () => d.openDetails(f, "from"); ul.append(mo); }
    right.append(ul);
  }
  box.append(left, right);
  return box;
}

export interface PanelDeps {
  title: (f: Folder) => string;
  items: (f: Folder) => DetailItem[];
  host: (url: string) => string;
  canFilter: (status: string) => boolean;
  filter: (status: string) => void;
  help: Record<string, string>;
  hideTip: () => void;
  closed: () => void;                       // after the panel closes (the page refocuses the tree)
}

type TableRow = [lead: string | Node | null, name: string, n: number, extra: string, click: (() => void) | null, tip?: string];

/** The slide-in details for a folder: every status with a filter link, every source folder (searchable), the top
 *  sites, and bookmarks added by year. Escape, the close button or the backdrop closes it. */
export class DetailsPanel {
  private backdrop: HTMLElement | null = null;

  constructor(private readonly d: PanelDeps) {
    $("#detailClose")?.addEventListener("click", e => { e.preventDefault(); this.close(); });
    document.addEventListener("keydown", e => {
      if (e.key === "Escape" && this.isOpen) { e.stopPropagation(); e.preventDefault(); this.close(); }
    }, true);
  }

  get isOpen(): boolean { return !!$("#detailPanel")?.classList.contains("show"); }

  open(f: Folder, focus?: "from"): void {
    const its = this.d.items(f), body = $("#detailBody"), panel = $("#detailPanel");
    if (!body || !panel) return;
    $("#detailTitle")!.textContent = this.d.title(f);
    $("#detailSub")!.textContent = `${its.length.toLocaleString()} bookmarks`;
    body.replaceChildren();
    const section = (title: string, node: Node) => { const s = el("section", "bm-panel-sec"); s.append(el("div", "fsum-head", title), node); body.append(s); return s; };
    const pct = (n: number) => pctOf(n, its.length);
    const c = statusCounts(its);
    section("Link health", this.table(statusesIn(c).map(k => [el("span", "legend-dot me-2 " + STATUS_BG[k]), STATUS_NAME[k]!, c[k]!, pct(c[k]!),
      this.d.canFilter(k) ? () => { this.close(); this.d.filter(k); } : null, this.d.help[k]] as TableRow)));
    const entries = Object.entries(countBy(its, i => nice(i.o))).sort((a, b) => b[1] - a[1]);
    const fwrap = el("div"), q = el("input", "form-control form-control-sm mb-2"); q.type = "search"; q.placeholder = `Search ${entries.length} folders…`;
    const holder = el("div");
    const paint = () => { const w = q.value.trim().toLowerCase(); holder.replaceChildren(this.table(entries.filter(([k]) => !w || k.toLowerCase().includes(w)).map(([k, v]) => ["ti-folder", k, v, pct(v), null] as TableRow))); };
    q.oninput = paint; paint(); fwrap.append(q, holder);
    const fs = section(`Came from ${entries.length} of your folders`, fwrap);
    const sites = countBy(its, i => this.d.host(i.u) || "(not a web link)");
    const top = Object.entries(sites).sort((a, b) => b[1] - a[1]).slice(0, 15);
    section(`Top sites · ${Object.keys(sites).length.toLocaleString()} in all`, this.table(top.map(([h, v]) => ["ti-world", h, v, pct(v), null] as TableRow)));
    const years = countBy(its, i => i.a ? String(new Date(i.a * 1000).getFullYear()) : "unknown");
    const yrows = Object.entries(years).sort((a, b) => b[0].localeCompare(a[0]));
    const max = Math.max(...yrows.map(r => r[1]));
    const yl = el("div", "bm-years");
    for (const [y, v] of yrows) {
      const r = el("div", "bm-year"), barw = el("div", "bm-year-bar"), track = el("div", "bm-year-track"); barw.style.width = (100 * v / max) + "%";
      track.append(barw); r.append(el("span", "num", y), track, el("span", "num text-end", v.toLocaleString())); yl.append(r);
    }
    section("Added by year", yl);
    panel.classList.add("show"); panel.style.visibility = "visible";
    if (!this.backdrop) { this.backdrop = el("div", "offcanvas-backdrop fade show"); this.backdrop.onclick = () => this.close(); document.body.append(this.backdrop); }
    this.d.hideTip();
    setTimeout(() => { if (focus === "from") { fs.scrollIntoView({ block: "start" }); q.focus(); } else $("#detailClose")?.focus(); }, 60);
  }

  close(): void {
    const p = $("#detailPanel"); if (!p) return;
    p.classList.remove("show");
    setTimeout(() => { if (!p.classList.contains("show")) p.style.visibility = "hidden"; }, 300);
    this.backdrop?.remove(); this.backdrop = null;
    this.d.closed();
  }

  private table(rows: TableRow[]): HTMLElement {
    const t = el("table", "table table-sm table-vcenter mb-0 bm-panel-table"), tb = el("tbody");
    for (const [lead, name, n, extra, click, tip] of rows) {
      const tr = el("tr"); if (click) { tr.classList.add("clickable"); tr.onclick = click; }
      const td = el("td", "text-truncate");
      if (lead instanceof Node) td.append(lead); else if (lead) td.append(icon(lead, "text-secondary me-2"));
      td.append(name); if (tip) td.dataset.tip = tip;
      tr.append(td, el("td", "text-end fw-medium num", n.toLocaleString()), el("td", "text-end text-secondary small num", extra || "")); tb.append(tr);
    }
    t.append(tb); return t;
  }
}
