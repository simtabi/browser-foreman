// The bookmark tree's interaction rules, as pure functions over the rows the tree shows: keyboard navigation,
// range selection, drag-and-drop targets, and where the "Bookmarks in <folder>" label goes. The page keeps the state
// (rows, selection, marks) and applies what these return. Moved from the page script on 2026-11-12; behaviour is
// unchanged.

import type { BookmarkId, Folder, TreeNode } from "./tree";
import { isFolder } from "./tree";

/** One visible row of the tree. */
export interface TreeRow {
  kind: "folder" | "link";
  node: TreeNode;
  depth: number;
  parent?: Folder;
  open?: boolean;
  trash?: boolean;
  count?: number;
}

export type NavKey = "ArrowDown" | "ArrowUp" | "ArrowRight" | "ArrowLeft" | "Home" | "End";
export type NavStep = { kind: "select"; to: number } | { kind: "toggle"; open: boolean } | null;

const clamp = (to: number, n: number) => Math.max(0, Math.min(n - 1, to));
export const parentRow = (rows: TreeRow[], r: TreeRow): number => rows.findIndex(x => x.node === r.parent);

/** What a navigation key does at row `sel`: move the selection, open or close the folder, or nothing.
 *  → opens a closed folder, then steps into it; ← closes an open folder (not in a read-only view), else goes to the
 *  parent folder. */
export function navigate(rows: TreeRow[], sel: number, key: NavKey, readOnly: boolean): NavStep {
  const r = rows[sel];
  if (!r) return null;
  switch (key) {
    case "ArrowDown": return { kind: "select", to: clamp(sel + 1, rows.length) };
    case "ArrowUp": return { kind: "select", to: clamp(sel - 1, rows.length) };
    case "Home": return { kind: "select", to: 0 };
    case "End": return { kind: "select", to: clamp(rows.length - 1, rows.length) };
    case "ArrowRight":
      if (r.kind !== "folder") return null;
      return r.open ? { kind: "select", to: clamp(sel + 1, rows.length) } : { kind: "toggle", open: true };
    case "ArrowLeft": {
      if (r.kind === "folder" && r.open && !readOnly) return { kind: "toggle", open: false };
      const k = parentRow(rows, r);
      return k >= 0 ? { kind: "select", to: k } : null;
    }
  }
}

/** The nodes between two rows, both included (shift-click, shift-arrow). */
export const rangeOf = (rows: TreeRow[], a: number, b: number): TreeNode[] =>
  rows.slice(Math.min(a, b), Math.max(a, b) + 1).map(r => r.node);

// ---- drag and drop ----
/** into: inside a folder; before / after: beside a row, in the same folder. */
export type DropZone = "into" | "before" | "after";
/** Where over a row the pointer is. A bookmark: top half before it, bottom half after it. A folder: its top quarter
 *  before it, the middle into it, its bottom quarter after it (a closed folder only: below an open one, the next rows
 *  are its own contents, so a drop there goes into it). The Trash takes drops anywhere. Changed 2026-11-12 (owner:
 *  "allow user to move anywhere inside/outside the folders"): it used to be into (lower 70%) or before, so nothing
 *  could land after the last row of a folder or below a closed folder. */
export function dropZone(row: TreeRow, offsetY: number, height: number): DropZone {
  if (row.kind === "folder") {
    if (row.trash) return "into";
    if (offsetY < height * 0.25) return "before";
    if (offsetY > height * 0.75 && !row.open) return "after";
    return "into";
  }
  return offsetY < height / 2 ? "before" : "after";
}
export type DropAction =
  | { kind: "trash" }
  | { kind: "into"; folder: Folder }
  | { kind: "before"; parent: Folder; before: TreeNode }
  | { kind: "after"; parent: Folder; after: TreeNode };
/** What a drop does. `outdent`: after the last row of a folder, the pointer at the row's left edge places the items
 *  after that folder instead, one level out (repeat on the folder's own row to go further). `rows` finds the parent
 *  folder's own place. */
export function dropAction(row: TreeRow, zone: DropZone, opts: { outdent?: boolean; rows?: TreeRow[] } = {}): DropAction | null {
  if (zone === "into") {
    if (row.trash) return { kind: "trash" };
    return isFolder(row.node) ? { kind: "into", folder: row.node } : null;
  }
  if (!row.parent) return null;
  if (zone === "before") return { kind: "before", parent: row.parent, before: row.node };
  if (opts.outdent && row.parent.c[row.parent.c.length - 1] === row.node) {
    const up = opts.rows?.find(r => r.node === row.parent);
    // Only to a folder that is itself listed: the invisible top level (beside Bookmarks Bar) is the browser's, not ours.
    if (up?.parent && !up.trash && opts.rows?.some(r => r.node === up.parent)) return { kind: "after", parent: up.parent, after: up.node };
  }
  return { kind: "after", parent: row.parent, after: row.node };
}
/** Where an "after" drop goes in its folder: before the next item that is not itself being moved (null: at the end). */
export function afterTarget(parent: Folder, after: TreeNode, moving: TreeNode[]): TreeNode | null {
  const k = parent.c.indexOf(after);
  return parent.c.slice(k + 1).find(x => !moving.includes(x)) ?? null;
}
/** What is dragged: every marked item when the dragged row is one of them, otherwise just that row. */
export const dragged = (row: TreeRow, marked: Set<TreeNode>): TreeNode[] => marked.has(row.node) ? [...marked] : [row.node];

// ---- the "Bookmarks in <folder>" label ----
/** Does row `idx` start the loose bookmarks of a folder that also has subfolders? If so, how many are shown. */
export function looseGroupAt(rows: TreeRow[], idx: number): { parent: Folder; count: number } | null {
  const r = rows[idx];
  if (!r || r.kind !== "link" || !r.parent || !r.parent.c.some(isFolder)) return null;
  const prev = rows[idx - 1];
  if (idx > 0 && prev && prev.parent === r.parent && prev.kind === "link") return null;
  return { parent: r.parent, count: rows.filter(x => x.kind === "link" && x.parent === r.parent).length };
}

export type { BookmarkId };
