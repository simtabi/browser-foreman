// The bookmark view's keyboard: which action a key press asks for, given where the focus is. Pure, so the whole table
// can be tested; the page runs the action. Moved from the page script's keydown handler on 2026-11-12; same keys,
// same order of checks.

export type KeyAction =
  | "palette" | "undo" | "redo" | "selectAll" | "toList" | "escape" | "search" | "shortcuts" | "help" | "settings"
  | "moveUp" | "moveDown" | "nav" | "mark" | "enter" | "expandAll" | "collapseAll" | "openHere" | "openWindow"
  | "openIncognito" | "copy" | "describe" | "live" | "refresh" | "putBack" | "update" | "move" | "group"
  | "newFolder" | "rename" | "trash" | "menu";

export interface KeyPress { key: string; shift?: boolean; alt?: boolean; meta?: boolean; ctrl?: boolean }
export interface KeyContext {
  dialogOpen: boolean;   // a dialog is showing (it handles its own keys)
  menuOpen: boolean;     // the right-click menu is showing
  typing: boolean;       // focus is in a text field, text area or select
  inSearch: boolean;     // focus is in the bookmark search box
  hasRow: boolean;       // a row is selected
}
/** `prevent`: the browser's own handling of the key is stopped. */
export interface KeyResult { action: KeyAction; prevent: boolean }

const LETTERS: Record<string, KeyAction> = {
  " ": "mark", Enter: "enter", "*": "expandAll", "-": "collapseAll", o: "openHere", O: "openWindow",
  i: "openIncognito", I: "openIncognito", c: "copy", C: "copy", d: "describe", D: "describe", l: "live", L: "live",
  r: "refresh", R: "refresh", p: "putBack", P: "putBack", u: "update", U: "update", m: "move", M: "move",
  g: "group", G: "group", n: "newFolder", N: "newFolder", F2: "rename", Delete: "trash", Backspace: "trash",
  ".": "menu", ContextMenu: "menu",
};
const NAV = new Set(["ArrowDown", "ArrowUp", "ArrowRight", "ArrowLeft", "Home", "End"]);

export function keyAction(k: KeyPress, c: KeyContext): KeyResult | null {
  const cmd = !!(k.meta || k.ctrl), low = k.key.toLowerCase(), yes = (action: KeyAction): KeyResult => ({ action, prevent: true });
  if (cmd && low === "k") return yes("palette");  // the palette opens over anything, even a dialog
  if (c.dialogOpen || c.menuOpen) return null;
  if (cmd && !c.typing && low === "z") return yes(k.shift ? "redo" : "undo");
  if (cmd && !c.typing && low === "y") return yes("redo");
  if (cmd && !c.typing && low === "a") return yes("selectAll");
  if (cmd) return null;
  if (c.inSearch && (k.key === "ArrowDown" || k.key === "Enter")) return yes("toList");
  if (k.key === "Escape") return { action: "escape", prevent: false };
  if (c.typing) return null;
  if (k.key === "/") return yes("search");
  if (k.key === "?") return yes("shortcuts");
  if (k.key === "F1") return yes("help");
  if (k.key === ",") return yes("settings");
  if (!c.hasRow) return null;
  if (k.alt && (k.key === "ArrowUp" || k.key === "ArrowDown")) return yes(k.key === "ArrowUp" ? "moveUp" : "moveDown");
  if (NAV.has(k.key)) return yes("nav");
  if (k.key === "F10") return k.shift ? yes("menu") : null;
  const a = LETTERS[k.key];
  return a ? yes(a) : null;
}
