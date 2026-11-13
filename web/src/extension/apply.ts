// Writing a workspace into the browser's own bookmarks. plan() works out every step against a model of the browser's
// tree, so the steps, run in order, leave the browser holding exactly the workspace; run() carries them out through
// the bookmarks API. Nothing here runs without the popup showing the plan's counts and the owner confirming, and the
// popup saves the browser's current bookmarks to the server first.

import type { ExtBookmark } from "./ext";

export interface WsNode { id: string; kind: "folder" | "bookmark"; title: string; url?: string; children?: WsNode[] }
const EXT = "ext-"; // a workspace node read from this browser: "ext-" + the browser's id

/** A step. Ids starting with "new:" name a node created earlier in the plan. */
export type Step =
  | { op: "create"; ref: string; ws: string; parent: string; index: number; title: string; url?: string }
  | { op: "update"; id: string; title?: string; url?: string }
  | { op: "move"; id: string; parent: string; index: number }
  | { op: "remove"; id: string; folder: boolean; bookmarks: number };

export interface Plan { steps: Step[]; created: number; updated: number; moved: number; removedBookmarks: number; removedFolders: number }

interface M { id: string; parent: string; title: string; url?: string; children: string[] }

/** The plan for making the browser's tree (browserRoots: what getTree() returns) hold the workspace's tree. made maps
 *  workspace ids to browser ids for nodes an earlier run created (so re-planning never creates them twice). */
export function plan(browserRoots: ExtBookmark[], ws: WsNode, made: Map<string, string> = new Map()): Plan {
  const root = browserRoots[0];
  if (!root) throw new Error("the browser sent no bookmarks");
  const tops = root.children ?? [];
  const model = new Map<string, M>();
  const walk = (n: ExtBookmark, parent: string) => {
    model.set(n.id, { id: n.id, parent, title: n.title, ...(n.url !== undefined ? { url: n.url } : {}), children: (n.children ?? []).map(c => c.id) });
    for (const c of n.children ?? []) walk(c, n.id);
  };
  walk(root, "");
  const fromBrowser = (id: string): string | null => {
    const m = made.get(id);
    if (m && model.has(m)) return m;
    return id.startsWith(EXT) && model.has(id.slice(EXT.length)) ? id.slice(EXT.length) : null;
  };
  // A workspace that holds nothing from this browser was not read from it: applying it would delete everything.
  let known = 0;
  const countKnown = (n: WsNode) => { if (fromBrowser(n.id)) known++; for (const c of n.children ?? []) countKnown(c); };
  countKnown(ws);
  if (known === 0 && model.size > tops.length + 1) throw new Error("This workspace was not read from this browser. Read this browser's bookmarks into Browser Foreman first, edit that workspace, then write it back.");

  const steps: Step[] = [];
  const out: Plan = { steps, created: 0, updated: 0, moved: 0, removedBookmarks: 0, removedFolders: 0 };
  const topIds = new Set(tops.map(t => t.id));
  const fallbackTop = tops[1]?.id ?? tops[0]?.id ?? root.id; // Chrome cannot hold anything at its root: new top folders go in "Other bookmarks"
  const keep = new Set<string>([root.id, ...topIds]);
  let refN = 0;
  const detach = (id: string) => { const m = model.get(id)!; const p = model.get(m.parent); if (p) p.children.splice(p.children.indexOf(id), 1); };
  const attach = (id: string, parent: string, index: number) => { const p = model.get(parent)!; p.children.splice(Math.min(index, p.children.length), 0, id); model.get(id)!.parent = parent; };

  const place = (n: WsNode, parent: string, index: number): void => {
    let id = fromBrowser(n.id);
    if (id && topIds.has(id) && parent !== root.id) id = null; // a browser root moved inside a folder is written as a copy
    if (!id) {
      id = `new:${++refN}`;
      model.set(id, { id, parent, title: n.title, ...(n.kind === "bookmark" ? { url: n.url ?? "" } : {}), children: [] });
      attach(id, parent, index);
      steps.push({ op: "create", ref: id, ws: n.id, parent, index, title: n.title, ...(n.kind === "bookmark" ? { url: n.url ?? "" } : {}) });
      out.created++;
    } else {
      keep.add(id);
      const m = model.get(id)!;
      if (!topIds.has(id)) {
        const upd: Step = { op: "update", id };
        if (m.title !== n.title) upd.title = n.title;
        if (n.kind === "bookmark" && (m.url ?? "") !== (n.url ?? "")) upd.url = n.url ?? "";
        if (upd.title !== undefined || upd.url !== undefined) { steps.push(upd); out.updated++; m.title = n.title; if (upd.url !== undefined) m.url = upd.url; }
        const p = model.get(m.parent)!;
        if (m.parent !== parent || p.children.indexOf(id) !== index) {
          detach(id); attach(id, parent, index);
          steps.push({ op: "move", id, parent, index });
          out.moved++;
        }
      }
    }
    keep.add(id);
    if (n.kind === "folder") (n.children ?? []).forEach((c, i) => place(c, id!, i));
  };
  // The workspace's top folders line up with the browser's roots by id; a new top folder goes in the fallback root.
  for (const top of ws.children ?? []) {
    const id = fromBrowser(top.id);
    if (id && topIds.has(id)) { keep.add(id); (top.children ?? []).forEach((c, i) => place(c, id, i)); }
    else place(top, fallbackTop, model.get(fallbackTop)!.children.length);
  }
  // Removals last, after every move, so a node moved out of a folder is never removed with it.
  const removeFrom = (id: string) => {
    for (const c of [...(model.get(id)?.children ?? [])]) {
      if (keep.has(c)) { removeFrom(c); continue; }
      const folder = model.get(c)?.url === undefined;
      // Counted on the model, after the moves: a bookmark moved out of this folder is not lost with it.
      const count = (id: string): number => { const m = model.get(id)!; return m.url !== undefined ? 1 : m.children.reduce((a, k) => a + count(k), 0); };
      const bm = count(c);
      steps.push({ op: "remove", id: c, folder, bookmarks: bm });
      out.removedBookmarks += bm;
      if (folder) out.removedFolders++;
    }
  };
  removeFrom(root.id);
  return out;
}

export interface BookmarksApi {
  create(o: { parentId: string; index?: number; title: string; url?: string }): Promise<{ id: string }>;
  update(id: string, o: { title?: string; url?: string }): Promise<unknown>;
  move(id: string, o: { parentId: string; index?: number }): Promise<unknown>;
  remove(id: string): Promise<void>;
  removeTree(id: string): Promise<void>;
}

/** Runs a plan's steps in order and returns, for each node it created, workspace id → browser id. */
export async function run(p: Plan, api: BookmarksApi, onStep: (done: number, total: number) => void = () => {}): Promise<Map<string, string>> {
  const madeIds = new Map<string, string>();
  const real = new Map<string, string>();
  const id = (x: string) => x.startsWith("new:") ? real.get(x)! : x;
  let done = 0;
  for (const s of p.steps) {
    if (s.op === "create") {
      const made = await api.create({ parentId: id(s.parent), index: s.index, title: s.title, ...(s.url !== undefined ? { url: s.url } : {}) });
      real.set(s.ref, made.id);
      madeIds.set(s.ws, made.id);
    } else if (s.op === "update") {
      await api.update(s.id, { ...(s.title !== undefined ? { title: s.title } : {}), ...(s.url !== undefined ? { url: s.url } : {}) });
    } else if (s.op === "move") {
      await api.move(id(s.id), { parentId: id(s.parent), index: s.index });
    } else if (s.folder) {
      await api.removeTree(s.id);
    } else {
      await api.remove(s.id);
    }
    onStep(++done, p.steps.length);
  }
  return madeIds;
}
