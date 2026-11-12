// Open tabs: the menu items for a tab, a window, a section and the View menu, in the shared MenuItem format. The page
// keeps the state and the actions and passes them in; these decide what each menu offers. Moved from the page script
// on 2026-11-12; same items, same order.

import type { MenuItem } from "../menu";
import type { OpenHow, TabItem } from "../tabs";

/** What the menus can do; the page implements each. */
export interface TabActions {
  open: (items: TabItem[], how: OpenHow, grouped: boolean, opts?: { crossMode?: boolean }) => unknown;
  openOne: (url: string) => unknown;                       // in the browser that opens links
  moveMode: (items: TabItem[], to: "normal" | "incognito") => unknown;
  copy: (items: TabItem[]) => unknown;
  saveAsFolder: (items: TabItem[], suggested?: string) => unknown;
  moveToBookmarks: (items: TabItem[], suggested?: string) => unknown;
  close: (items: TabItem[]) => unknown;
  select: (items: TabItem[], on: boolean) => unknown;
}

const n = (a: unknown[]): string => a.length === 1 ? "" : ` ${a.length}`;

/** The four ways to open a set, under a header. */
export const openMenuItems = (items: TabItem[], label: string, a: TabActions): MenuItem[] => [
  { h: label },
  { l: "In a new window, grouped as they were", ic: "ti-layout-columns", fn: () => a.open(items, "window", true) },
  { l: "All in one new window", ic: "ti-app-window", fn: () => a.open(items, "window", false) },
  { l: "In a new incognito window", ic: "ti-spy", fn: () => a.open(items, "incognito", false) },
  { l: "As tabs in the current window", ic: "ti-browser", fn: () => a.open(items, "tabs", false) },
];

/** Incognito ⇄ normal: live tabs move (open in the other mode, then close here); saved ones can only be opened. */
export function modeMenuItems(items: TabItem[], live: boolean, a: TabActions): MenuItem[] {
  const inc = items.filter(i => i.x.inc), norm = items.filter(i => !i.x.inc);
  return [
    inc.length && live ? { l: `Move${n(inc)} to a normal window`, ic: "ti-arrow-big-right-lines", fn: () => a.moveMode(inc, "normal") } : null,
    norm.length && live ? { l: `Move${n(norm)} to an incognito window`, ic: "ti-spy", fn: () => a.moveMode(norm, "incognito") } : null,
    inc.length ? { l: `Open${n(inc)} in a normal window`, ic: "ti-app-window", fn: () => a.open(inc, "window", true, { crossMode: true }) } : null,
  ];
}

/** A tab's right-click menu. `sel` is what it acts on: the picked tabs when this one is among several, else just it. */
export function tabMenuItems(i: TabItem, sel: TabItem[], o: { live: boolean; picked: boolean; title: string; window: TabItem[]; note?: () => unknown }, a: TabActions): MenuItem[] {
  const { t, x } = i, many = sel.length > 1;
  return [
    ...(many ? openMenuItems(sel, `${sel.length} selected tabs`, a) : [
      { h: o.title.slice(0, 48) },
      x.inc ? null : { l: "Open in this browser", ic: "ti-external-link", fn: () => a.openOne(t.url) },
      { l: "Open in a new window", ic: "ti-app-window", fn: () => a.open([i], x.inc ? "incognito" : "window", false) },
      { l: "Open in an incognito window", ic: "ti-spy", fn: () => a.open([i], "incognito", false) },
      x.inc ? null : { l: "Open as a tab in the current window", ic: "ti-browser", fn: () => a.open([i], "tabs", false) },
    ] as MenuItem[]), "-",
    ...modeMenuItems(sel, o.live, a), "-",
    { l: o.picked ? "Unselect" : "Select", ic: "ti-checkbox", fn: () => a.select([i], !o.picked) },
    { l: "Select this window's tabs", ic: "ti-checks", fn: () => a.select(o.window, true) },
    { l: many ? `Copy ${sel.length} links` : "Copy link", ic: "ti-copy", fn: () => a.copy(sel) },
    !many && o.note ? { l: "Note and tags…", ic: "ti-tag", fn: o.note } : null,
    { l: many ? `Save ${sel.length} as a bookmark folder…` : "Save as a bookmark…", ic: "ti-folder-plus", fn: () => a.saveAsFolder(sel, o.title) },
    o.live ? { l: many ? `Move ${sel.length} to bookmarks (save, then close)…` : "Move to bookmarks (save, then close)…", ic: "ti-arrow-bar-to-right", fn: () => a.moveToBookmarks(sel) } : null,
    o.live ? "-" : null,
    o.live ? { l: many ? `Close ${sel.length} tabs` : "Close this tab", ic: "ti-square-x", danger: true, fn: () => a.close(sel) } : null,
  ];
}

/** A window's ⋯ and right-click menu. */
/** `title` heads the menu ("Brave · window 2"); `name` is the suggested folder name ("Brave window 2"). */
export function windowMenuItems(all: TabItem[], o: { live: boolean; title: string; name: string; folded: boolean; fold: () => unknown;
  saveFile: () => unknown; note?: () => unknown }, a: TabActions): MenuItem[] {
  return [
    ...openMenuItems(all, o.title, a), "-",
    ...modeMenuItems(all, o.live, a), "-",
    { l: "Select all in this window", ic: "ti-checks", fn: () => a.select(all, true) },
    { l: o.folded ? "Unfold" : "Fold", ic: o.folded ? "ti-fold-down" : "ti-fold", fn: o.fold },
    { l: "Copy links", ic: "ti-copy", fn: () => a.copy(all) },
    { l: "Save as a bookmarks file", ic: "ti-bookmark-plus", fn: o.saveFile },
    { l: "Save as a bookmark folder…", ic: "ti-folder-plus", fn: () => a.saveAsFolder(all, o.name) },
    o.live ? { l: "Move to bookmarks (save, then close)…", ic: "ti-arrow-bar-to-right", fn: () => a.moveToBookmarks(all, o.name) } : null,
    o.note ? { l: "Note and tags…", ic: "ti-tag", fn: o.note } : null,
    o.live ? "-" : null,
    o.live ? { l: `Close this window's ${all.length} tabs`, ic: "ti-square-x", danger: true, fn: () => a.close(all) } : null,
  ];
}

/** A section's (Windows, Incognito) menu. */
export const sectionMenuItems = (all: TabItem[], title: string, fold: (on: boolean) => unknown, a: TabActions): MenuItem[] => [
  ...openMenuItems(all, `${title}: ${all.length} tabs`, a), "-",
  { l: "Select all here", ic: "ti-checks", fn: () => a.select(all, true) },
  { l: "Fold every window", ic: "ti-fold", fn: () => fold(true) },
  { l: "Unfold every window", ic: "ti-fold-down", fn: () => fold(false) },
];

/** The view options the Open tabs View menu sets. */
export interface TabViewPrefs { incFirst: boolean; winSort: string; tabSort: string; compact: boolean }

/** The View menu: a tick marks the current choice. */
export function viewMenuItems(v: TabViewPrefs, set: <K extends keyof TabViewPrefs>(k: K, val: TabViewPrefs[K]) => unknown, fold: (on: boolean) => unknown): MenuItem[] {
  const pick = <K extends keyof TabViewPrefs>(k: K, val: TabViewPrefs[K], label: string): MenuItem =>
    ({ l: label, ic: v[k] === val ? "ti-check" : "ti-point", fn: () => set(k, val) });
  return [
    { h: "Show first" }, pick("incFirst", false, "Normal windows first"), pick("incFirst", true, "Incognito first"),
    { h: "Order windows" }, pick("winSort", "original", "As in the browser"), pick("winSort", "most", "Most tabs first"), pick("winSort", "fewest", "Fewest tabs first"), pick("winSort", "browser", "By browser"),
    { h: "Order tabs" }, pick("tabSort", "original", "As in the window"), pick("tabSort", "title", "By title"), pick("tabSort", "site", "By site"),
    { h: "Density" }, { l: v.compact ? "Comfortable rows" : "Compact rows", ic: v.compact ? "ti-layout-list" : "ti-list", fn: () => set("compact", !v.compact) },
    { l: "Fold every window", ic: "ti-fold", fn: () => fold(true) },
    { l: "Unfold every window", ic: "ti-fold-down", fn: () => fold(false) },
  ];
}
