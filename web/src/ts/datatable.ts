// A Tabler data table for any list in a dialog or a card: a search field and optional tabs in the card header, a
// `table card-table table-vcenter` with sortable columns, an optional row checkbox, per-row actions in a dropdown
// (with at most one primary action as a button), an empty state, and the Pager footer ("Showing 1 to 10 of 42").
//
//   const t = new DataTable<Row>({ noun: "workspaces", columns: [{ label: "Name", cell: r => r.name, sort: by(r => r.name) }],
//     search: (r, q) => r.name.toLowerCase().includes(q), actions: r => [{ l: "Open", ic: "ti-external-link", fn: () => open(r) }] });
//   container.append(t.element); t.rows(list);

import { buildMenu, type MenuItem } from "./menu";
import { Pager } from "./pager";
import { el, icon } from "./ui";

export interface Column<T> {
  label: string;
  cell: (r: T) => Node | string;
  /** Compare two rows; a column with it sorts when its header is clicked. */
  sort?: (a: T, b: T) => number;
  cls?: string;   // classes for the cells, e.g. "text-end" for numbers, "w-1" for a narrow one
}

export interface Tab { id: string; label: string; icon?: string; count?: number }

export interface DataTableOptions<T> {
  columns: Column<T>[];
  noun: string;
  size?: number;
  sizes?: number[];
  search?: (r: T, q: string) => boolean;   // lower-cased query; no search field without it
  placeholder?: string;
  primary?: (r: T) => { label: string; icon?: string; fn: () => unknown; cls?: string } | null;
  actions?: (r: T) => MenuItem[];
  /** Ticks: a checkbox per row (key identifies a row across pages); onPick hears every change. */
  pick?: { key: (r: T) => string; disabled?: (r: T) => boolean; onPick: (keys: Set<string>) => void };
  empty?: { title: string; text?: string; icon?: string };
  tabs?: Tab[];
  tab?: string;
  onTab?: (id: string) => void;
  /** A header toolbar after the search (filters, switches). */
  tools?: HTMLElement[];
}

/** A compare function for a column: by(r => r.name). */
export const by = <T>(f: (r: T) => string | number): ((a: T, b: T) => number) => (a, b) => {
  const x = f(a), y = f(b);
  return typeof x === "number" && typeof y === "number" ? x - y : String(x).localeCompare(String(y), undefined, { numeric: true, sensitivity: "base" });
};

export class DataTable<T> {
  readonly element: HTMLElement;
  readonly picked = new Set<string>();
  private all: T[] = [];
  private q = "";
  private sortCol = -1;
  private sortDir = 1;
  private readonly pager: Pager;
  private readonly head: HTMLElement;
  private readonly body: HTMLElement;
  /** The pager's footer; a dialog docks it outside its scrolling body (see v1-ui modal().dock) so it never scrolls. */
  readonly foot: HTMLElement;
  private readonly tabsEl: HTMLElement;
  private scroll: HTMLElement = el("div");
  private fullHeight = 0;     // the height of a full page, kept so a shorter page does not pull the footer up
  private fullFor = "";       // the page size and tab that height was measured for

  constructor(private readonly o: DataTableOptions<T>) {
    this.element = el("div", "card");
    this.tabsEl = el("div", "card-header");
    const bar = el("div", "card-body border-bottom py-3 d-flex flex-wrap gap-2 align-items-center");
    if (o.search) {
      const wrap = el("div", "input-icon flex-fill"), q = el("input", "form-control"); q.type = "search";
      q.placeholder = o.placeholder ?? `Search ${o.noun}…`; q.setAttribute("aria-label", q.placeholder);
      const ic = el("span", "input-icon-addon"); ic.append(icon("ti-search"));
      q.oninput = () => { this.q = q.value.trim().toLowerCase(); this.pager.goto(1); this.paint(); };
      wrap.append(ic, q); bar.append(wrap);
    }
    for (const t of o.tools ?? []) bar.append(t);
    const scroll = el("div", "table-responsive"), table = el("table", "table card-table table-vcenter datatable");
    this.head = el("thead"); this.body = el("tbody");
    table.append(this.head, this.body); scroll.append(table);
    this.foot = el("div", "datatable-foot");
    this.pager = new Pager({ size: o.size ?? 10, sizes: o.sizes ?? [10, 25, 50, 100], noun: o.noun, onChange: () => this.paint() });
    const head = el("div", "datatable-head");
    if (o.tabs) head.append(this.tabsEl);
    if (o.search || o.tools?.length) head.append(bar);
    if (head.childElementCount) this.element.append(head);
    this.element.append(scroll, this.foot);
    this.scroll = scroll;
  }

  /** Replace the rows (keeps the page, search and sort; ticks of rows no longer there are dropped). */
  rows(list: T[]): this {
    this.all = list;
    if (this.o.pick) {
      const keys = new Set(list.map(this.o.pick.key));
      for (const k of [...this.picked]) if (!keys.has(k)) this.picked.delete(k);
    }
    this.paint();
    return this;
  }

  setTabs(tabs: Tab[], active: string): this { this.o.tabs = tabs; this.o.tab = active; this.paint(); return this; }

  private shown(): T[] {
    let l = this.o.search && this.q ? this.all.filter(r => this.o.search!(r, this.q)) : [...this.all];
    const c = this.o.columns[this.sortCol];
    if (c?.sort) l = l.sort((a, b) => this.sortDir * c.sort!(a, b));
    return l;
  }

  private paint(): void {
    if (this.o.tabs) {
      const ul = el("ul", "nav nav-tabs card-header-tabs"); ul.setAttribute("role", "tablist");
      for (const t of this.o.tabs) {
        const li = el("li", "nav-item"), a = el("a", "nav-link" + (t.id === this.o.tab ? " active" : "")); a.href = "#"; a.setAttribute("role", "tab");
        if (t.icon) a.append(icon(t.icon, "me-2"));
        a.append(t.label);
        if (t.count != null) a.append(el("span", "badge bg-secondary-lt ms-2", t.count.toLocaleString()));
        a.onclick = e => { e.preventDefault(); if (t.id !== this.o.tab) { this.o.tab = t.id; this.pager.goto(1); this.o.onTab?.(t.id); } };
        li.append(a); ul.append(li);
      }
      this.tabsEl.replaceChildren(ul);
    }
    const list = this.shown();
    this.pager.total(list.length);
    const page = this.pager.slice(list);
    // Header: a tick-all box for the page, the columns (sortable ones are buttons), and an empty actions column.
    const tr = el("tr");
    if (this.o.pick) {
      const th = el("th", "w-1"), box = el("input", "form-check-input m-0 align-middle"); box.type = "checkbox"; box.setAttribute("aria-label", "Select every row on this page");
      const can = page.filter(r => !this.o.pick!.disabled?.(r));
      box.checked = can.length > 0 && can.every(r => this.picked.has(this.o.pick!.key(r)));
      box.onchange = () => { for (const r of can) { const k = this.o.pick!.key(r); if (box.checked) this.picked.add(k); else this.picked.delete(k); } this.o.pick!.onPick(this.picked); this.paint(); };
      th.append(box); tr.append(th);
    }
    this.o.columns.forEach((c, k) => {
      const th = el("th", c.cls ?? null);
      if (c.sort) {
        const b = el("button", "table-sort" + (this.sortCol === k ? (this.sortDir > 0 ? " asc" : " desc") : "")); b.type = "button"; b.textContent = c.label;
        b.onclick = () => { this.sortDir = this.sortCol === k ? -this.sortDir : 1; this.sortCol = k; this.paint(); };
        th.append(b);
      } else th.textContent = c.label;
      tr.append(th);
    });
    if (this.o.primary || this.o.actions) tr.append(el("th", "w-1"));
    this.head.replaceChildren(tr);
    this.body.replaceChildren();
    if (!page.length) {
      const td = el("td"); td.colSpan = this.o.columns.length + (this.o.pick ? 1 : 0) + (this.o.primary || this.o.actions ? 1 : 0);
      const e = el("div", "empty py-4"), ic = el("div", "empty-icon"); ic.append(icon(this.o.empty?.icon ?? "ti-mood-empty"));
      e.append(ic, el("p", "empty-title", this.q ? `No ${this.o.noun} match “${this.q}”` : this.o.empty?.title ?? `No ${this.o.noun}`));
      if (!this.q && this.o.empty?.text) e.append(el("p", "empty-subtitle text-secondary", this.o.empty.text));
      td.append(e);
      const row = el("tr"); row.append(td); this.body.append(row);
    }
    for (const r of page) {
      const row = el("tr");
      if (this.o.pick) {
        const td = el("td"), box = el("input", "form-check-input m-0 align-middle"); box.type = "checkbox"; box.setAttribute("aria-label", "Select");
        const k = this.o.pick.key(r); box.checked = this.picked.has(k); box.disabled = !!this.o.pick.disabled?.(r);
        box.onchange = () => { if (box.checked) this.picked.add(k); else this.picked.delete(k); this.o.pick!.onPick(this.picked); this.paint(); };
        td.append(box); row.append(td);
      }
      for (const c of this.o.columns) { const td = el("td", c.cls ?? null); td.append(c.cell(r)); row.append(td); }
      if (this.o.primary || this.o.actions) {
        const td = el("td"), wrap = el("div", "btn-list flex-nowrap justify-content-end");
        const p = this.o.primary?.(r);
        if (p) { // on a phone, just its icon (named by aria-label and a tooltip)
          const b = el("button", `btn btn-sm ${p.cls ?? ""}`.trim()); b.type = "button"; b.setAttribute("aria-label", p.label); b.dataset.tip = p.label;
          if (p.icon) b.append(icon(p.icon, "me-sm-1"));
          b.append(el("span", p.icon ? "d-none d-sm-inline" : null, p.label)); b.onclick = () => void p.fn(); wrap.append(b);
        }
        const items = this.o.actions?.(r) ?? [];
        if (items.some(Boolean)) {
          const dd = el("div", "dropdown"), b = el("button", "btn btn-sm btn-icon btn-ghost-secondary"); b.type = "button"; b.dataset.dd = "";
          b.setAttribute("aria-label", "More actions"); b.append(icon("ti-dots-vertical"));
          dd.append(b, buildMenu(items)); wrap.append(dd);
        }
        td.append(wrap); row.append(td);
      }
      this.body.append(row);
    }
    // Nothing to page through or label: no footer and no column headers, just the empty state.
    this.foot.replaceChildren(...(list.length ? [this.pager.render()] : []));
    this.head.hidden = !list.length && !this.q;
    // Hold the height of a full page: a last page with fewer rows leaves room rather than shrinking the dialog.
    const key = `${this.pager.pageSize}|${this.o.tab ?? ""}|${this.q}`;
    if (key !== this.fullFor) { this.fullFor = key; this.fullHeight = 0; this.scroll.style.minHeight = ""; }
    if (this.pager.pages > 1 && page.length === this.pager.pageSize) {
      requestAnimationFrame(() => { const h = this.scroll.offsetHeight; if (h > this.fullHeight) { this.fullHeight = h; this.scroll.style.minHeight = h + "px"; } });
    }
  }
}

/** A Tabler avatar with a name's initials, coloured from the name. */
export function avatar(name: string, cls = "avatar-sm"): HTMLElement {
  const colours = ["blue", "azure", "indigo", "purple", "pink", "red", "orange", "yellow", "lime", "green", "teal", "cyan"];
  let h = 0; for (const ch of name) h = (h * 31 + ch.charCodeAt(0)) >>> 0;
  const words = name.trim().split(/\s+/).filter(Boolean);
  const ini = ((words[0]?.[0] ?? "?") + (words[1]?.[0] ?? words[0]?.[1] ?? "")).toUpperCase();
  return el("span", `avatar ${cls} bg-${colours[h % colours.length]}-lt`, ini);
}

/** Name and a muted line under it: the usual first cell of a Tabler table. */
export function nameCell(title: string | Node, sub: string, lead?: HTMLElement): HTMLElement {
  const d = el("div", "d-flex py-1 align-items-center dt-name"), t = el("div", "flex-fill min-w-0");
  const top = el("div", "font-weight-medium text-truncate"); top.append(title);
  t.append(top, el("div", "text-secondary small text-truncate", sub));
  if (lead) { lead.classList.add("me-2"); d.append(lead); }
  d.append(t); return d;
}
