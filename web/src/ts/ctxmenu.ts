// The right-click menu's layout: a plan of command ids, dividers and named groups, turned into menu items. A group
// shows as a submenu only when it has more than one thing to offer; with one, that one is shown in its place (with
// its full name); with none, the group is left out. Dividers only ever sit between two shown entries (buildMenu).
// Added 2026-11-12 (owner: "nested sub menus … only to be activated if there is required/needed content").

import type { MenuItem } from "./menu";

export type CtxNode = string | "-" | CtxGroup;
export interface CtxGroup { group: string; ic: string; items: CtxNode[] }
/** What one command offers right now: null when it does not apply here. */
export interface CtxEntry { label: string; short?: string; ic: string; key?: string; danger?: boolean; run: () => unknown }

const isGroup = (n: CtxNode): n is CtxGroup => typeof n === "object";

/** Resolve a plan against the commands that apply now. Inside a group the short label is used; a group of one shows
 *  that command at the group's level under its full label. */
export function layoutMenu(plan: CtxNode[], entry: (id: string) => CtxEntry | null, inGroup = false): MenuItem[] {
  const out: MenuItem[] = [];
  for (const n of plan) {
    if (n === "-") { out.push("-"); continue; }
    if (isGroup(n)) {
      const inner = layoutMenu(n.items, entry, true).filter(x => x && x !== "-");
      if (!inner.length) continue;
      if (inner.length === 1) { out.push(...layoutMenu(firstAction(n.items, entry), entry, false)); continue; }
      out.push({ l: n.group, ic: n.ic, sub: layoutMenu(n.items, entry, true) });
      continue;
    }
    const e = entry(n);
    if (e) out.push({ l: (inGroup && e.short) || e.label, ic: e.ic, k: e.key, danger: e.danger, fn: e.run });
  }
  return out;
}

/** The one command a single-entry group resolves to, as a plan of one (searching nested groups). */
function firstAction(items: CtxNode[], entry: (id: string) => CtxEntry | null): CtxNode[] {
  for (const n of items) {
    if (n === "-") continue;
    if (isGroup(n)) { const f = firstAction(n.items, entry); if (f.length) return f; continue; }
    if (entry(n)) return [n];
  }
  return [];
}
