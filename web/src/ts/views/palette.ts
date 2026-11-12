// The command palette (⌘K): every command, searchable, with its shortcut; and the keyboard shortcuts sheet, generated
// from the same list so it can never drift from what the keys do. Moved from the page script on 2026-11-12; same
// markup and keys.
//
//   const palette = new CommandPalette(() => commands().map(toCommand));   palette.open();   palette.shortcuts(NAV_KEYS);

import { type Command, matchCommands, shortcutColumns } from "../commands";
import { hideModal, showModal } from "../dialogs";
import { $, el, icon, kbd, plural } from "../ui";

export class CommandPalette {
  private sel = 0;
  private shown: Command[] = [];

  constructor(private readonly source: () => Command[]) {
    const input = $<HTMLInputElement>("#palIn");
    if (!input) return;
    input.oninput = () => { this.sel = 0; this.paint(); };
    input.onkeydown = e => {
      e.stopPropagation();
      if (e.key === "ArrowDown") { e.preventDefault(); this.sel = Math.min(this.sel + 1, this.shown.length - 1); this.paint(); }
      else if (e.key === "ArrowUp") { e.preventDefault(); this.sel = Math.max(this.sel - 1, 0); this.paint(); }
      else if (e.key === "Enter") { e.preventDefault(); this.run(this.sel); }
      else if (e.key === "Escape") hideModal("#palModal");
    };
  }

  /** What is listed now (the browser checks read it). */
  get list(): Command[] { return this.shown; }

  open(): void {
    const input = $<HTMLInputElement>("#palIn"); if (!input) return;
    input.value = ""; this.sel = 0; this.paint(); showModal("#palModal"); setTimeout(() => input.focus(), 30);
  }

  private paint(): void {
    const box = $("#palList"), input = $<HTMLInputElement>("#palIn"); if (!box || !input) return;
    this.shown = matchCommands(this.source(), input.value);
    this.sel = Math.min(this.sel, Math.max(0, this.shown.length - 1));
    box.replaceChildren(...this.shown.map((c, k) => {
      const a = el("button", "list-group-item list-group-item-action pal-item" + (k === this.sel ? " active" : "") + (c.danger ? " text-danger" : ""));
      a.type = "button"; a.setAttribute("role", "option");
      a.onmousemove = () => { if (this.sel !== k) { this.sel = k; this.paint(); } };
      a.append(icon(c.icon), el("span", "flex-fill", c.label), el("span", "text-secondary small", c.group));
      if (c.key) a.append(kbd(c.key));
      a.onclick = () => this.run(k);
      return a;
    }));
    if (!this.shown.length) box.replaceChildren(el("div", "text-secondary text-center py-4", "No command matches. Try other words."));
    const count = $("#palCount"); if (count) count.textContent = plural(this.shown.length, "command");
    (box.children[this.sel] as HTMLElement | undefined)?.scrollIntoView({ block: "nearest" });
  }

  private run(k: number): void { const c = this.shown[k]; if (!c) return; hideModal("#palModal"); c.run(); }

  /** The keyboard shortcuts sheet. */
  shortcuts(navigate: [string, string][]): void {
    const body = $("#keysBody"); if (!body) return;
    body.replaceChildren(...shortcutColumns(this.source(), navigate).map(([g, ks]) => {
      const col = el("div", "col-md-6 mb-3"); col.append(el("h4", "subheader mb-2", g));
      const t = el("table", "table table-sm table-borderless mb-0");
      for (const [k, l] of ks) { const tr = el("tr"), a = el("td", "text-nowrap"); a.style.width = "9rem"; a.append(kbd(k)); tr.append(a, el("td", null, l)); t.append(tr); }
      col.append(t); return col;
    }));
  }
}
