// Pagination for any long list: one page of items, and the controls to move between pages.
// Configurable (page size, size choices, wording) and chainable; it holds no DOM of its own until render().
//
//   const pager = new Pager({ size: 25, onChange: p => redraw(p) }).total(list.length).goto(saved);
//   pager.slice(list).forEach(drawRow);
//   if (pager.needed) card.append(pager.render());
//
// Size 0 means "all on one page". Pages are 1-based; goto() clamps, so a page remembered for a list that has
// since shrunk lands on its last page instead of an empty one.

import { Dom } from "./dom";

export interface PagerOptions {
  size?: number;                       // items per page; 0 = all
  sizes?: readonly number[];           // the choices offered, 0 shown as "All"
  noun?: string;                       // "tabs" → "26–50 of 70 tabs"
  window?: number;                     // page buttons either side of the current one
  onChange?: (p: Pager) => void;       // a page or size was chosen
}

const DEFAULTS = { size: 25, sizes: [10, 25, 50, 100, 0] as readonly number[], noun: "items", window: 1 };

export class Pager {
  private readonly opts: Required<Omit<PagerOptions, "onChange">> & Pick<PagerOptions, "onChange">;
  private count = 0;
  private current = 1;
  private perPage: number;

  constructor(opts: PagerOptions = {}) {
    this.opts = { ...DEFAULTS, ...opts };
    this.perPage = Pager.validSize(this.opts.size, this.opts.sizes);
  }

  total(n: number): this { this.count = Math.max(0, Math.floor(n)); return this.goto(this.current); }
  size(n: number): this { this.perPage = Pager.validSize(n, this.opts.sizes); return this.goto(this.current); }
  goto(page: number): this { this.current = Math.min(Math.max(1, Math.floor(page) || 1), this.pages); return this; }
  next(): this { return this.goto(this.current + 1); }
  prev(): this { return this.goto(this.current - 1); }

  get page(): number { return this.current; }
  get pageSize(): number { return this.perPage; }
  get pages(): number { return this.perPage ? Math.max(1, Math.ceil(this.count / this.perPage)) : 1; }
  get start(): number { return this.perPage ? (this.current - 1) * this.perPage : 0; }
  get end(): number { return this.perPage ? Math.min(this.count, this.start + this.perPage) : this.count; }
  /** More items than one page of the smallest size: only then are controls worth showing. */
  get needed(): boolean { return this.count > Math.min(...this.opts.sizes.filter(s => s > 0)); }

  slice<T>(items: readonly T[]): T[] { return items.slice(this.start, this.end); }

  /** Page numbers to show, with null for a gap: 1 … 4 5 6 … 12. */
  numbers(): (number | null)[] {
    const out: (number | null)[] = [], w = this.opts.window, last = this.pages;
    for (let p = 1; p <= last; p++) {
      if (p === 1 || p === last || Math.abs(p - this.current) <= w) out.push(p);
      else if (out[out.length - 1] !== null) out.push(null);
    }
    return out;
  }

  /** Tabler's card footer: "Showing 1 to 25 of 70 tabs", a per-page select, and its .pagination list. */
  render(): HTMLElement {
    const foot = Dom.write(document.createElement("div"), "pager", this.opts.noun);
    foot.className = "card-footer d-flex align-items-center flex-wrap gap-2";
    const info = document.createElement("p");
    info.className = "m-0 text-secondary small text-nowrap";
    info.textContent = this.count
      ? `${(this.start + 1).toLocaleString()}–${this.end.toLocaleString()} of ${this.count.toLocaleString()} ${this.opts.noun}`
      : `No ${this.opts.noun}`;
    const size = Dom.write(document.createElement("select"), "action", "page-size");
    size.className = "form-select form-select-sm w-auto";
    size.setAttribute("aria-label", `${this.opts.noun} per page`);
    for (const n of this.opts.sizes) {
      const o = document.createElement("option");
      o.value = String(n); o.textContent = n ? `${n} per page` : "All";
      size.append(o);
    }
    size.value = String(this.perPage);
    size.onchange = () => { this.size(Number(size.value)).goto(1); this.opts.onChange?.(this); };
    foot.append(info, size);
    if (this.pages > 1) {
      const ul = document.createElement("ul");
      ul.className = "pagination pagination-sm m-0 ms-auto";
      ul.setAttribute("aria-label", `Pages of ${this.opts.noun}`);
      ul.append(this.item("prev", "ti-chevron-left", "Previous page", this.current > 1, false, () => this.prev()));
      for (const p of this.numbers()) {
        if (p === null) { const li = document.createElement("li"); li.className = "page-item disabled"; li.innerHTML = '<span class="page-link">…</span>'; ul.append(li); continue; }
        ul.append(this.item(String(p), null, `Page ${p}`, true, p === this.current, () => this.goto(p)));
      }
      ul.append(this.item("next", "ti-chevron-right", "Next page", this.current < this.pages, false, () => this.next()));
      foot.append(ul);
    }
    return foot;
  }

  private item(text: string, ic: string | null, label: string, enabled: boolean, active: boolean, go: () => void): HTMLLIElement {
    const li = document.createElement("li");
    li.className = "page-item" + (enabled ? "" : " disabled") + (active ? " active" : "");
    const a = Dom.write(document.createElement("a"), "action", "page");
    a.className = "page-link"; a.href = "#"; a.setAttribute("aria-label", label);
    if (active) a.setAttribute("aria-current", "page");
    if (!enabled) { a.setAttribute("aria-disabled", "true"); a.tabIndex = -1; }
    const icon = ic ? Object.assign(document.createElement("i"), { className: `ti ${ic}` }) : null;
    if (icon) a.append(icon); else a.textContent = text;  // prev and next are their chevrons, named by aria-label
    a.onclick = e => { e.preventDefault(); e.stopPropagation(); if (!enabled || active) return; go(); this.opts.onChange?.(this); };
    li.append(a);
    return li;
  }

  private static validSize(n: number, sizes: readonly number[]): number {
    return sizes.includes(n) ? n : (sizes.find(s => s > 0) ?? 0);
  }
}
