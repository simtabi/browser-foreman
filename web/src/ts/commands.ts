// Commands: one record per thing the app can do, behind the command palette, the right-click menu and the shortcuts
// sheet. The page builds the list (each command calls page actions); these are the typed shape and the pure rules.

/** One command. `enabled` is asked each time the list is shown; `danger` paints it red. */
export interface Command {
  id: string; label: string; icon: string; key: string; group: string; danger: boolean;
  enabled: () => boolean; run: () => unknown;
}
/** The page's compact form: [id, label, icon, key, enabled, run, group, danger?]. */
export type CommandTuple = [string, string, string, string, () => boolean, () => unknown, string, boolean?];
export const toCommand = ([id, label, icon, key, enabled, run, group, danger]: CommandTuple): Command =>
  ({ id, label, icon, key, enabled, run, group, danger: !!danger });

/** The commands that are available and match every word typed (in the name or the group), in list order. */
export function matchCommands(list: Command[], query: string): Command[] {
  const words = query.trim().toLowerCase().split(/\s+/).filter(Boolean);
  return list.filter(c => c.enabled() && words.every(w => `${c.label} ${c.group}`.toLowerCase().includes(w)));
}

/** The shortcuts sheet's columns: the fixed navigation keys first, then each group's commands that have a key. */
export function shortcutColumns(list: Command[], navigate: [string, string][], skipGroups: string[] = ["Filter"]): [string, [string, string][]][] {
  const cols = new Map<string, [string, string][]>([["Navigate", navigate]]);
  for (const c of list) {
    if (!c.key || skipGroups.includes(c.group)) continue;
    const g = cols.get(c.group) ?? []; g.push([c.key, c.label]); cols.set(c.group, g);
  }
  return [...cols.entries()];
}
