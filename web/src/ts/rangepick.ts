// A period picker used wherever the tool asks "how far back": one compact button reading "Label  Value ▾" that opens
// a menu of choices, with an optional custom part below them (the Added filter's from/to fields). It replaces the
// rows of range buttons (Timeline) and the plain selects (Browser history) so every period looks and works the same.
//
//   const p = new RangePicker({label: "Range", options: [{key: "7", label: "7 days"}, …], value: "7",
//                              defaultKey: "7", onPick: key => …});
//   host.append(p.el);   p.set("30");   p.set("", "Since 2 Mar 2026");   // a value no option names
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
  heading?: string;              // the menu's first header
  custom?: { heading: string; body: HTMLElement };  // extra controls under the choices
  columns?: 1 | 2;               // choices per menu row; default 2 when there are more than five
  end?: boolean;                 // open the menu right-aligned
  onPick: (key: string) => void;
}

export class RangePicker {
  readonly el: HTMLElement;
  private readonly btn: HTMLButtonElement;
  private readonly val: HTMLElement;
  private readonly list: HTMLElement;
  private value: string;

  constructor(private readonly o: RangePickerOptions) {
    this.value = o.value;
    this.el = el("div", "dropdown range-pick");
    this.btn = el("button", "btn btn-sm tb-pick"); this.btn.type = "button";
    this.btn.setAttribute("data-dd", ""); this.btn.setAttribute("aria-expanded", "false"); this.btn.setAttribute("aria-haspopup", "menu");
    this.val = el("b");
    this.btn.append(icon(o.icon || "ti-calendar-time", "text-secondary"), el("span", "tb-pick-label", o.label), this.val, icon("ti-chevron-down", "text-secondary"));
    const menu = el("div", "dropdown-menu range-menu" + (o.end ? " dropdown-menu-end" : ""));
    if (o.heading) menu.append(el("h6", "dropdown-header", o.heading));
    const cols = o.columns ?? (o.options.length > 5 ? 2 : 1);
    this.list = el("div", "range-options" + (cols === 2 ? " cols-2" : ""));
    for (const opt of o.options) {
      const a = el("a", "dropdown-item", opt.label); a.href = "#"; a.dataset.key = opt.key; a.setAttribute("role", "menuitemradio");
      a.onclick = e => { e.preventDefault(); closeDropdowns(); this.set(opt.key); o.onPick(opt.key); };
      this.list.append(a);
    }
    menu.append(this.list);
    if (o.custom) menu.append(el("div", "dropdown-divider"), el("h6", "dropdown-header", o.custom.heading), o.custom.body);
    this.el.append(this.btn, menu);
    this.set(o.value);
  }

  /** Show `key` as chosen. `label` names a value no option has (a typed range); the key may then be "". */
  set(key: string, label?: string): void {
    this.value = key;
    const opt = this.o.options.find(x => x.key === key);
    this.val.textContent = label ?? opt?.label ?? "";
    this.btn.classList.toggle("is-set", this.o.defaultKey !== undefined && key !== this.o.defaultKey);
    this.btn.setAttribute("aria-label", `${this.o.label}: ${this.val.textContent}`);
    for (const a of Array.from(this.list.children) as HTMLElement[]) {
      const on = !label && a.dataset.key === key; a.classList.toggle("active", on); a.setAttribute("aria-checked", String(on));
    }
  }

  get key(): string { return this.value; }
}
