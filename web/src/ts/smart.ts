// Smart folders: saved searches. Each is a name over the page's own view state (the Show filter and the search text),
// kept in the server's preferences as smart_folders, so it follows you to every browser
// and an assistant can open it over MCP. Opening one restores that view; nothing is copied or moved.
//
//   const mine = smartFor(prefs.smart_folders ?? [], workspaceId);
//   const next = addSmart(all, { name: "To read", q: "#later", filter: "all" }, workspaceId);

export interface SmartFolder {
  id: string;
  name: string;
  workspace?: string; // only in this workspace; every workspace when left out
  q?: string;         // the search text ("#tag" matches a tag exactly)
  filter?: string;    // the Show filter: dead, moved, unverified…; "all" when left out
}

export const MAX_SMART = 50;

/** The smart folders that apply in a workspace: its own and the ones for every workspace, in saved order. */
export function smartFor(all: SmartFolder[], workspace: string | null): SmartFolder[] {
  return all.filter(s => !s.workspace || s.workspace === workspace);
}

/** The one saved for exactly this view, if any. */
export function smartMatching(all: SmartFolder[], filter: string, q: string): SmartFolder | undefined {
  const f = filter || "all", t = q.trim();
  return all.find(s => (s.filter || "all") === f && (s.q || "").trim() === t);
}

const idOf = (): string => "sf_" + Math.random().toString(36).slice(2, 10);

/** Adds a smart folder, refusing an empty name, a view with nothing to narrow, and a fiftieth-plus one. */
export function addSmart(all: SmartFolder[], want: { name: string; q?: string; filter?: string }, workspace: string | null): SmartFolder[] {
  const name = want.name.trim();
  if (!name) throw new Error("Give it a name.");
  if (name.length > 60) throw new Error("A name is at most 60 characters.");
  const q = (want.q || "").trim(), filter = want.filter && want.filter !== "all" ? want.filter : "";
  if (!q && !filter) throw new Error("Search for something or pick a Show filter first: a smart folder of everything is just All bookmarks.");
  if (all.length >= MAX_SMART) throw new Error(`At most ${MAX_SMART} smart folders.`);
  const s: SmartFolder = { id: idOf(), name };
  if (workspace) s.workspace = workspace;
  if (q) s.q = q;
  if (filter) s.filter = filter;
  return [...all, s];
}

/** What a smart folder shows, in words, for its tooltip. */
export function describeSmart(s: SmartFolder, filterName: (k: string) => string): string {
  const parts: string[] = [];
  if (s.filter) parts.push(filterName(s.filter));
  else parts.push("Bookmarks");
  if (s.q) parts.push(s.q.startsWith("#") && !s.q.includes(" ") ? `tagged ${s.q.slice(1)}` : `matching “${s.q}”`);
  return parts.join(" ") + (s.workspace ? "" : " (every workspace)");
}
