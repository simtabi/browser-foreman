// The bookmark tree's model: folders hold bookmark ids and subfolders. Pure rules only (no DOM, no storage): building
// the tree from the data, saving and restoring it, walking it, and finding a node's parent, path and ancestors.
// Moved from the page script into a typed module on 2026-11-12; behaviour is unchanged.
//
//   const root = defaultTree(folders, items.map(i => ({ id: i.id, path: i.g })));
//   const parents = parentsOf(root);   pathIn(folder, parents, root);   containsNode(folder, id, parents);

export const TRASH = "Trash";
export const OLD_TRASH = "_Removed (not exported)";

export type BookmarkId = number;
export interface Folder { fid?: number; n: string; c: TreeNode[]; from?: string; d?: string; virtual?: boolean }
export type TreeNode = Folder | BookmarkId;
/** A folder as it is saved: no ids of the page's own, nothing that can be rebuilt. */
export interface SavedFolder { n: string; c: (SavedFolder | BookmarkId)[]; from?: string; d?: string }
export type Parents = Map<TreeNode, Folder>;

let fidSeq = 1;
export const mkFolder = (n: string, c: TreeNode[] = [], from?: string, d?: string): Folder => {
  const f: Folder = { fid: fidSeq++, n, c };
  if (from) f.from = from;
  if (d) f.d = d;
  return f;
};
export const isFolder = (x: TreeNode | null | undefined): x is Folder => typeof x === "object" && x !== null;

/** A slash that is part of a folder name is stored as "∕"; this shows it as "/". */
export const nice = (n: string): string => String(n).replaceAll("∕", "/");

const rank = (t: string) => t === "Bookmarks Bar" ? 0 : t === TRASH ? 3 : t.startsWith("_") ? 2 : 1;
/** Bookmarks Bar first, Trash last, "_" folders before it, the rest by name. */
export const byName = (a: string, b: string): number => rank(a) - rank(b) || a.trim().localeCompare(b.trim());

/** The tree the cleanup proposed: every folder (even an empty one), each bookmark in its folder, links before
 *  subfolders, subfolders sorted by `byName`. */
export function defaultTree(folders: string[], items: { id: BookmarkId; path: string }[]): Folder {
  const root = mkFolder(""), idx = new Map<string, Folder>([["", root]]);
  const folderAt = (path: string): Folder => {
    const have = idx.get(path); if (have) return have;
    const cut = path.lastIndexOf("/"), parent = folderAt(cut < 0 ? "" : path.slice(0, cut));
    const f = mkFolder(path.slice(cut + 1)); parent.c.push(f); idx.set(path, f); return f;
  };
  for (const f of folders) folderAt(f);
  for (const i of items) folderAt(i.path).c.push(i.id);
  const tidy = (f: Folder) => {
    const links = f.c.filter(x => !isFolder(x)), subs = f.c.filter(isFolder).sort((a, b) => byName(a.n, b.n));
    f.c = [...links, ...subs]; subs.forEach(tidy);
  };
  tidy(root);
  return root;
}

export const plain = (f: Folder): SavedFolder => {
  const o: SavedFolder = { n: f.n, c: f.c.map(x => isFolder(x) ? plain(x) : x) };
  if (f.from) o.from = f.from;
  if (f.d) o.d = f.d;
  return o;
};
export const revive = (p: SavedFolder): Folder => mkFolder(p.n, p.c.map(x => typeof x === "object" ? revive(x) : x), p.from, p.d);

/** A saved tree reconciled with the current data: ids that no longer exist (or were merged away, or deleted for good)
 *  drop out, each id appears once, and bookmarks the saved tree does not have go into their own folder path. */
export function restoreTree(saved: SavedFolder, opts: { valid: (id: BookmarkId) => boolean; gone: Set<BookmarkId>; items: { id: BookmarkId; path: string }[] }): Folder {
  const root = revive(saved), seen = new Set<BookmarkId>();
  for (const f of root.c) if (isFolder(f) && f.n === OLD_TRASH) f.n = TRASH;
  const prune = (f: Folder): void => {
    f.c = f.c.filter(x => {
      if (isFolder(x)) { prune(x); return true; }
      if (!opts.valid(x) || opts.gone.has(x) || seen.has(x)) return false;
      seen.add(x); return true;
    });
  };
  prune(root);
  for (const i of opts.items) if (!seen.has(i.id) && !opts.gone.has(i.id)) {
    let f = root;
    for (const part of i.path.split("/")) {
      let s = f.c.find((x): x is Folder => isFolder(x) && x.n === part);
      if (!s) { s = mkFolder(part); f.c.push(s); }
      f = s;
    }
    f.c.push(i.id);
  }
  return root;
}

/** Every node's parent folder. */
export function parentsOf(root: Folder): Parents {
  const map: Parents = new Map();
  const walk = (f: Folder) => { for (const x of f.c) { map.set(x, f); if (isFolder(x)) walk(x); } };
  walk(root);
  return map;
}
/** "Bookmarks Bar/Development/Laravel" for a folder (names as stored). */
export function pathIn(f: Folder, parents: Parents, root: Folder): string {
  const p: string[] = [];
  for (let x: Folder | undefined = f; x && x !== root; x = parents.get(x)) p.unshift(x.n);
  return p.join("/");
}
/** Is `x` the folder itself or anywhere inside it? */
export function containsNode(folder: Folder, x: TreeNode, parents: Parents): boolean {
  for (let p: TreeNode | undefined = x; p !== undefined; p = parents.get(p)) if (p === folder) return true;
  return false;
}
/** Take a node out of its parent folder. */
export function detachNode(x: TreeNode, parents: Parents): void {
  const p = parents.get(x); if (p) p.c.splice(p.c.indexOf(x), 1);
}
/** Every folder below `f`, depth first. */
export function walkFolders(f: Folder, fn: (x: Folder) => void): void { for (const x of f.c) if (isFolder(x)) { fn(x); walkFolders(x, fn); } }
/** Every bookmark id below `f`. */
export function descendants(f: Folder, out: BookmarkId[] = []): BookmarkId[] { for (const x of f.c) if (isFolder(x)) descendants(x, out); else out.push(x); return out; }
