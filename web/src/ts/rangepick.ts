// A period picker used wherever the tool asks "when": one compact button reading "Label  Value ▾" that opens a menu
// of quick ranges and, optionally, a custom date-and-time range (From / To, either may stay empty). The Timeline,
// Browser history and the Added filter all use it, so every period looks and works the same.
//
//   const p = new RangePicker({label: "Range", options: [{key: "7", label: "7 days"}, …], value: "7", defaultKey: "7",
//                              onPick: key => …, onRange: (from, to) => …});
//   host.append(p.el);   p.set("30");   p.setRange("2026-11-01T09:00", "");   // datetime-local strings, "" = open
//
// It works with the page's dropdown rules (menu.ts): the button carries data-dd and the menu sits beside it.

import { closeDropdowns } from "./menu";
import { el, icon } from "./ui";

export interface RangeOption { key: string; label: string; }

export interface RangePickerOptions {
  label: string;                 // what is chosen ("Range", "Added")
  options: RangeOption[];
  value: string;                 // the chosen option's key
  defaultKey?: string;           // the button is outlined while the value differs from this
  icon?: string;                 // Tabler icon name; default a calendar
  heading?: string;              // the quick ranges' header
  columns?: 1 | 2;               // quick ranges per row; default 2 when there are more than five
  end?: boolean;                 // open the menu right-aligned
  onPick: (key: string) => void;
  /** Turns on the custom range: From / To as datetime-local strings ("" = unbounded). */
  onRange?: (from: string, to: string) => void;
}

/** "13 Nov 2026, 09:30" for a datetime-local string; the year only when it is not this one. */
export function fmtLocal(v: string): string {
  const d = new Date(v); if (isNaN(d.getTime())) return v;
  const same = d.getFullYear() === new Date().getFullYear();
  const day = d.toLocaleDateString(undefined, same ? { day: "numeric", month: "short" } : { day: "numeric", month: "short", year: "numeric" });
  const time = d.getHours() || d.getMinutes() ? ", " + d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" }) : "";
  return day + time;
}

/** The label for a typed range. */
export function rangeLabel(from: string, to: string): string {
  return from && to ? `${fmtLocal(from)} – ${fmtLocal(to)}` : from ? `Since ${fmtLocal(from)}` : to ? `Until ${fmtLocal(to)}` : "";
}

export class RangePicker {
  readonly el: HTMLElement;
  private readonly btn: HTMLButtonElement;
  private readonly val: HTMLElement;
  private readonly list: HTMLElement;
  private readonly from?: HTMLInputElement;
  private readonly to?: HTMLInputElement;
  private value: string;

  constructor(private readonly o: RangePickerOptions) {
    this.value = o.value;
    this.el = el("div", "dropdown range-pick");
    this.btn = el("button", "btn btn-sm tb-pick"); this.btn.type = "button";
    this.btn.setAttribute("data-dd", ""); this.btn.setAttribute("aria-expanded", "false"); this.btn.setAttribute("aria-haspopup", "menu");
    this.val = el("b");
    this.btn.append(icon(o.icon || "ti-calendar-time", "text-secondary"), el("span", "tb-pick-label", o.label), this.val, icon("ti-chevron-down", "text-secondary"));
    const menu = el("div", "dropdown-menu range-menu" + (o.end ? " dropdown-menu-end" : "") + (o.onRange ? " has-custom" : ""));
    const quick = el("div", "range-quick");
    if (o.heading) quick.append(el("h6", "dropdown-header", o.heading));
    const cols = o.columns ?? (o.options.length > 5 ? 2 : 1);
    this.list = el("div", "range-options" + (cols === 2 ? " cols-2" : ""));
    for (const opt of o.options) {
      const a = el("a", "dropdown-item", opt.label); a.href = "#"; a.dataset.key = opt.key; a.setAttribute("role", "menuitemradio");
      a.onclick = e => { e.preventDefault(); closeDropdowns(); this.set(opt.key); o.onPick(opt.key); };
      this.list.append(a);
    }
    quick.append(this.list);
    menu.append(quick);
    if (o.onRange) {
      // The custom range: two date-and-time fields, a check that To is not before From, Apply and Clear.
      const box = el("form", "range-custom"); box.noValidate = true;
      const field = (name: string) => {
        const id = `rp-${name}-${Math.random().toString(36).slice(2, 8)}`;
        const lab = el("label", "form-label", name); lab.htmlFor = id;
        const inp = el("input", "form-control form-control-sm"); inp.type = "datetime-local"; inp.id = id;
        box.append(lab, inp); return inp;
      };
      box.append(el("h6", "dropdown-header px-0", "Custom range"));
      this.from = field("From"); this.to = field("To");
      const err = el("div", "invalid-feedback", "To is before From.");
      const acts = el("div", "range-acts");
      const clear = el("button", "btn btn-sm btn-ghost-secondary", "Clear"); clear.type = "button";
      const apply = el("button", "btn btn-sm btn-primary", "Apply"); apply.type = "submit";
      acts.append(clear, apply); box.append(err, acts);
      const check = () => { const bad = !!(this.from!.value && this.to!.value && this.to!.value < this.from!.value); this.to!.classList.toggle("is-invalid", bad); apply.disabled = bad || (!this.from!.value && !this.to!.value); return !bad; };
      this.from.oninput = this.to.oninput = () => void check();
      box.onsubmit = e => { e.preventDefault(); if (!check()) return; closeDropdowns(); this.setRange(this.from!.value, this.to!.value); o.onRange!(this.from!.value, this.to!.value); };
      clear.onclick = () => { this.from!.value = this.to!.value = ""; check(); };
      check();
      menu.append(box);
    }
    this.el.append(this.btn, menu);
    this.set(o.value);
  }

  /** Show quick range `key` as chosen. */
  set(key: string): void {
    this.value = key;
    if (this.from && this.to) { this.from.value = ""; this.to.value = ""; this.from.dispatchEvent(new Event("input")); }
    this.paint(this.o.options.find(x => x.key === key)?.label ?? "", key);
  }

  /** Show a typed range (datetime-local strings, "" = unbounded); both empty falls back to the default key. */
  setRange(from: string, to: string): void {
    if (!from && !to) { this.set(this.o.defaultKey ?? this.o.options[0]?.key ?? ""); return; }
    this.value = "";
    if (this.from && this.to) { this.from.value = from; this.to.value = to; this.from.dispatchEvent(new Event("input")); }
    this.paint(rangeLabel(from, to), "");
  }

  private paint(text: string, key: string): void {
    this.val.textContent = text;
    this.btn.classList.toggle("is-set", key === "" || (this.o.defaultKey !== undefined && key !== this.o.defaultKey));
    this.btn.setAttribute("aria-label", `${this.o.label}: ${text}`);
    for (const a of Array.from(this.list.children) as HTMLElement[]) {
      const on = key !== "" && a.dataset.key === key; a.classList.toggle("active", on); a.setAttribute("aria-checked", String(on));
    }
  }

  /** The chosen quick range's key; "" while a typed range is shown. */
  get key(): string { return this.value; }
  /** The typed range, when one is shown. */
  get range(): { from: string; to: string } | null { return this.value === "" && this.from && this.to ? { from: this.from.value, to: this.to.value } : null; }
}
