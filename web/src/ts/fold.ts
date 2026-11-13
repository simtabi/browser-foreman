// One Expand all / Collapse all button for the whole page, in the header beside Undo and Redo. Each view registers
// what folding means there (Bookmarks: folders; Open tabs: windows); the button acts on the view you are in and
// hides where nothing folds (the Timeline). Built like UndoCenter (undo.ts): views register, the page says which
// one is showing, and every change calls refresh().
//
//   const FOLD = new FoldCenter(button).register("tabs", {anyOpen: () => …, expand: () => …, collapse: () => …});
//   FOLD.show("tabs");   …after the view repaints:   FOLD.refresh();

import { icon } from "./ui";

export interface FoldScope {
  anyOpen(): boolean | null;   // null: nothing to fold right now
  expand(): void;
  collapse(): void;
}

export class FoldCenter {
  private readonly scopes = new Map<string, FoldScope>();
  private current = "";

  constructor(private readonly btn: HTMLButtonElement | null) {
    if (btn) btn.onclick = e => { e.preventDefault(); this.toggle(); };
  }

  register(name: string, scope: FoldScope): this { this.scopes.set(name, scope); return this.refresh(); }
  show(name: string): this { this.current = name; return this.refresh(); }

  /** Collapse while anything is open, otherwise expand. */
  toggle(): void {
    const s = this.scopes.get(this.current), open = s?.anyOpen();
    if (!s || open == null) return;
    if (open) s.collapse(); else s.expand();
    this.refresh();
  }

  refresh(): this {
    const b = this.btn; if (!b) return this;
    const open = this.scopes.get(this.current)?.anyOpen() ?? null;
    b.hidden = open == null;
    const what = this.current === "tabs" ? "windows" : "folders";
    const label = open ? `Collapse all ${what}` : `Expand all ${what}`;
    b.setAttribute("aria-label", label); b.dataset.tip = label + (open ? " (-)" : "");
    b.setAttribute("aria-pressed", String(!open));
    b.replaceChildren(icon(open ? "ti-fold" : "ti-arrows-vertical"));
    return this;
  }
}
