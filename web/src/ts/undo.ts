// Undo and redo, per place. Each view registers what it can undo (bookmark edits, closed tabs…); the buttons in that
// view's bar and ⌘Z / ⇧⌘Z act on the view you are in and nowhere else. A button is enabled only when its place has
// something to undo or redo, and its tooltip names it ("Undo: Renamed “Py”").
//
//   const undo = new UndoCenter()
//     .register("bookmarks", { undoLabel: () => …, redoLabel: () => …, undo: async () => …, redo: async () => … })
//     .show("bookmarks");
//   undo.undo();                       // the current place's last change
//   undo.refresh();                    // after anything that may change what can be undone

import { Dom } from "./dom";

export interface UndoScope {
  undoLabel(): string | null;  // what Undo would do, or null when there is nothing
  redoLabel(): string | null;
  undo(): Promise<unknown> | unknown;
  redo(): Promise<unknown> | unknown;
}

export class UndoCenter {
  private readonly scopes = new Map<string, UndoScope>();
  private current = "";
  private busy = false;

  register(name: string, scope: UndoScope): this { this.scopes.set(name, scope); return this.refresh(); }
  show(name: string): this { this.current = name; return this.refresh(); }

  async undo(): Promise<void> { await this.run(s => s.undoLabel() ? s.undo() : undefined); }
  async redo(): Promise<void> { await this.run(s => s.redoLabel() ? s.redo() : undefined); }

  /** What Undo or Redo would do in the current place, or null. */
  label(kind: "undo" | "redo"): string | null {
    const s = this.scopes.get(this.current);
    return s ? (kind === "undo" ? s.undoLabel() : s.redoLabel()) : null;
  }

  /** Repaint every Undo/Redo control: a container with data-brofo-undo="<place>" (or "current", the view you are
   *  in) holding [data-brofo-action=undo|redo] buttons or menu items. */
  refresh(): this {
    for (const group of Dom.all("undo")) {
      const name = Dom.read(group, "undo") ?? "";
      const s = this.scopes.get(name === "current" ? this.current : name);
      for (const [act, label] of [["undo", s?.undoLabel() ?? null], ["redo", s?.redoLabel() ?? null]] as const) {
        const b = Dom.one<HTMLElement>("action", act, group);
        if (!b) continue;
        const off = !label || this.busy;
        if (b instanceof HTMLButtonElement) b.disabled = off;
        b.classList.toggle("disabled", off); b.setAttribute("aria-disabled", String(off));
        b.dataset.tip = label ? `${act === "undo" ? "Undo" : "Redo"}: ${label}` : act === "undo" ? "Nothing to undo here" : "Nothing to redo here";
        b.setAttribute("aria-label", b.dataset.tip);
      }
    }
    return this;
  }

  private async run(fn: (s: UndoScope) => unknown): Promise<void> {
    const s = this.scopes.get(this.current);
    if (!s || this.busy) return;
    this.busy = true; this.refresh();
    try { await fn(s); } finally { this.busy = false; this.refresh(); }
  }
}
