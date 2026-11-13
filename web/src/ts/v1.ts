// The bridge between the server's bookmark tree (/api/v1, the source of truth) and the page's own model: a flat list
// of bookmarks with numeric ids plus a saved folder tree (see tree.ts). toPage builds what the page reads at start;
// fromPage turns what the page saves back into a server tree. Folders keep their server id in `k`; bookmarks map
// through `ids` (ids[n - 1] is bookmark n's server id). Pure: no DOM, no network (the client is in v1-boot.ts).

import { TRASH, type SavedFolder, type BookmarkId } from "./tree";

export interface V1Node { id: string; kind: "folder" | "bookmark"; title: string; url?: string; note?: string; added?: number; children?: V1Node[] }
export interface V1Trashed { node: V1Node; from: string; index: number; at: number; reason?: string }
export interface V1Tree { root: V1Node; trash?: V1Trashed[] }
/** The last link check of an address (store.LinkResult). */
export interface V1Link { url: string; status: string; code?: number; final?: string; note?: string; kind?: string; checked: string }

/** s: the link check's status ("unchecked" until one runs), n its note, dk "page" or "host" for a dead link, nu the
 *  address a moved link now answers at; g: the folder it is in; o: the folder it came from. */
export interface PageItem { id: BookmarkId; u: string; t: string; a?: number; s: string; g: string; o: string; n?: string; dk?: string; nu?: string;
  /** For a merged copy (data.dupes): the folder of the bookmark kept for it. */
  k?: string; kg?: string }

/** The page's fields for one check result. */
export function linkFields(l: V1Link): Pick<PageItem, "s" | "n" | "dk" | "nu"> {
  const f: Pick<PageItem, "s" | "n" | "dk" | "nu"> = { s: l.status };
  if (l.note) f.n = l.note;
  if (l.kind) f.dk = l.kind;
  if (l.status === "moved" && l.final) f.nu = l.final;
  return f;
}
export interface From { path: string; index: number }
export interface PageState {
  root: SavedFolder;
  titles: Record<string, string>;
  notes: Record<string, string>;
  gone: BookmarkId[];
  trashFrom: Record<string, From>;
}
export interface Page {
  /** dupes: the copies merged away (Trash entries "duplicate of <id>"), as a read-only list with their own ids after
   *  keep's; they are also in the Trash, where they can be put back. */
  data: { keep: PageItem[]; dupes: PageItem[]; newMine: never[]; total: number; built: string; folders: string[] };
  state: PageState;
  ids: string[];
  /** When each trashed node (by server id) went to the Trash, and why: the page does not keep these. */
  trashed: Record<string, { at: number; reason?: string }>;
}

/** The Trash reason of a copy merged into another bookmark; the kept one's id follows (merge.DuplicateOf). */
export const DUPLICATE_OF = "duplicate of ";

/** A "/" inside a folder name is stored as "∕" in page paths (tree.ts `nice` shows it back). */
const seg = (title: string): string => title.replaceAll("/", "∕");
const unseg = (name: string): string => name.replaceAll("∕", "/");

export function toPage(t: V1Tree, built = "", links: Record<string, V1Link> = {}): Page {
  const keep: PageItem[] = [], ids: string[] = [], folders: string[] = [], pathOf = new Map<string, string>(), placed = new Map<string, string>();
  const trashed: Page["trashed"] = {};
  const state: PageState = { root: { n: "", c: [], k: t.root.id }, titles: {}, notes: {}, gone: [], trashFrom: {} };
  const bookmark = (b: V1Node, path: string): BookmarkId => {
    const id = keep.length + 1;
    const item: PageItem = { id, u: b.url ?? "", t: b.title, s: "unchecked", g: path, o: path };
    const l = links[item.u];
    if (l) Object.assign(item, linkFields(l));
    if (b.added) item.a = b.added;
    keep.push(item); ids.push(b.id); placed.set(b.id, path);
    if (b.note) state.notes[id] = b.note;
    return id;
  };
  const folder = (f: V1Node, path: string): SavedFolder => {
    const out: SavedFolder = { n: seg(f.title), c: [], k: f.id };
    if (f.note) out.d = f.note;
    fill(out, f, path);
    return out;
  };
  const fill = (into: SavedFolder, f: V1Node, path: string): void => {
    for (const c of f.children ?? []) {
      if (c.kind === "bookmark") { into.c.push(bookmark(c, path)); continue; }
      const p = path ? `${path}/${seg(c.title)}` : seg(c.title);
      folders.push(p); pathOf.set(c.id, p);
      into.c.push(folder(c, p));
    }
  };
  fill(state.root, t.root, "");
  const total = keep.length;
  if (t.trash?.length) {
    const bin: SavedFolder = { n: TRASH, c: [] };
    for (const x of t.trash) {
      trashed[x.node.id] = x.reason ? { at: x.at, reason: x.reason } : { at: x.at };
      const from: From = { path: pathOf.get(x.from) ?? "", index: x.index };
      if (x.node.kind === "bookmark") {
        const id = bookmark(x.node, from.path);
        state.trashFrom[id] = from;
        bin.c.push(id);
      } else {
        const f = folder(x.node, from.path);
        (f as SavedFolder & { from?: unknown }).from = from as unknown as string;  // the page keeps {path, index} here
        bin.c.push(f);
      }
    }
    state.root.c.push(bin);
  }
  const dupes: PageItem[] = [];
  for (const x of t.trash ?? []) {
    const kept = x.reason?.startsWith(DUPLICATE_OF) ? x.reason.slice(DUPLICATE_OF.length).split(" ")[0] : undefined;
    if (!kept || x.node.kind !== "bookmark") continue;
    const was = pathOf.get(x.from) ?? "", item: PageItem = { id: keep.length + dupes.length + 1, u: x.node.url ?? "", t: x.node.title, s: "unchecked", g: was, o: was };
    if (x.node.added) item.a = x.node.added;
    const l = links[item.u];
    if (l) Object.assign(item, linkFields(l));
    const at = placed.get(kept);
    if (at !== undefined) { item.k = at; item.kg = at; }
    dupes.push(item);
  }
  return { data: { keep, dupes, newMine: [], total, built, folders }, state, ids, trashed };
}

/** The server tree for what the page saved. `urls` are the page's applied address updates; `gone` ids (deleted for
 *  good) are left out; anything in the top Trash folder becomes the tree's Trash, with the folder it came from. */
export function fromPage(state: PageState, urls: Record<string, string>, page: Page, newId: () => string, now = Math.floor(Date.now() / 1000)): V1Tree {
  const items = new Map(page.data.keep.map(i => [i.id, i]));
  const gone = new Set(state.gone);
  const node = (x: SavedFolder | BookmarkId): V1Node | null => {
    if (typeof x === "number") {
      const i = items.get(x), id = page.ids[x - 1];
      if (!i || !id || gone.has(x)) return null;
      const b: V1Node = { id, kind: "bookmark", title: state.titles[x] ?? i.t, url: urls[x] ?? i.u };
      const note = state.notes[x];
      if (note) b.note = note;
      if (i.a) b.added = i.a;
      return b;
    }
    const f: V1Node = { id: x.k || newId(), kind: "folder", title: unseg(x.n), children: [] };
    if (x.d) f.note = x.d;
    for (const c of x.c) { const n = node(c); if (n) f.children!.push(n); }
    return f;
  };
  const root: V1Node = { id: state.root.k || "root", kind: "folder", title: "", children: [] };
  let bin: SavedFolder | undefined;
  for (const c of state.root.c) {
    if (typeof c === "object" && c.n === TRASH && !bin) { bin = c; continue; }
    const n = node(c); if (n) root.children!.push(n);
  }
  const byPath = new Map<string, string>([["", root.id]]);
  const index = (f: V1Node, path: string): void => {
    for (const c of f.children ?? []) if (c.kind === "folder") {
      const p = path ? `${path}/${seg(c.title)}` : seg(c.title);
      if (!byPath.has(p)) byPath.set(p, c.id);
      index(c, p);
    }
  };
  index(root, "");
  const trash: V1Trashed[] = [];
  for (const c of bin?.c ?? []) {
    const n = node(c); if (!n) continue;
    const from = (typeof c === "number" ? state.trashFrom[c] : (c as unknown as { from?: From }).from) ?? { path: "", index: 0 };
    const meta = page.trashed[n.id];
    const entry: V1Trashed = { node: n, from: byPath.get(from.path) ?? root.id, index: from.index ?? 0, at: meta?.at ?? now };
    if (meta?.reason) entry.reason = meta.reason;
    trash.push(entry);
  }
  const out: V1Tree = { root };
  if (trash.length) out.trash = trash;
  return out;
}
