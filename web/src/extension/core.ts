// What the extension decides without touching the browser: which browser this is, what the tab report looks like,
// and how to talk to the server. Tested in Node (src/tests/extension.test.mjs); background.ts and popup.ts do the rest.

import type { ExtGroup, ExtWindow } from "./ext";

/** The browser's name as Browser Foreman calls it, so an extension's report lines up with the same browser read
 *  through automation. brave: whether navigator.brave exists (Brave reports Chrome's brands). */
export function browserName(ua: string, brands: string[], brave: boolean): string {
  if (brave) return "Brave Browser";
  if (/Firefox\//.test(ua)) return "Firefox";
  if (brands.includes("Microsoft Edge") || /Edg\//.test(ua)) return "Microsoft Edge";
  if (brands.includes("Opera") || /OPR\//.test(ua)) return "Opera";
  if (/Vivaldi\//.test(ua)) return "Vivaldi";
  if (brands.includes("Google Chrome") || /Chrome\//.test(ua)) return "Google Chrome";
  if (/Safari\//.test(ua)) return "Safari";
  return "Chromium";
}

export interface TabReport {
  windows: { id: number; incognito: boolean; tabs: { id: number; url: string; title: string; active: boolean; pinned: boolean; groupId: number }[] }[];
  groups: { id: number; windowId: number; title: string; color: string; collapsed: boolean }[];
}

/** The tab report the server takes: normal browser windows only (no popups or devtools), every tab with its group. */
export function tabReport(windows: ExtWindow[], groups: ExtGroup[]): TabReport {
  return {
    windows: windows.filter(w => (w.type ?? "normal") === "normal" && w.id !== undefined).map(w => ({
      id: w.id!, incognito: w.incognito,
      tabs: (w.tabs ?? []).map(t => ({ id: t.id ?? -1, url: t.url ?? "", title: t.title ?? "", active: t.active, pinned: t.pinned, groupId: t.groupId ?? -1 })),
    })),
    groups: groups.map(g => ({ id: g.id, windowId: g.windowId, title: g.title ?? "", color: g.color, collapsed: g.collapsed })),
  };
}

/** Only the local server: anything else would send your tabs and bookmarks off this computer. */
export function checkServer(raw: string): string {
  let u: URL;
  try { u = new URL(raw.trim()); } catch { throw new Error("That is not an address: use http://127.0.0.1:8765"); }
  if (u.protocol !== "http:" && u.protocol !== "https:") throw new Error("Use an http:// address");
  if (!["127.0.0.1", "localhost", "[::1]"].includes(u.hostname)) throw new Error("Only a server on this computer (127.0.0.1 or localhost)");
  return u.origin;
}

export type Fetch = (url: string, init?: { method?: string; headers?: Record<string, string>; body?: string }) => Promise<{ ok: boolean; status: number; json(): Promise<any> }>;

/** The server's extension routes, with this extension's token. */
export class Client {
  constructor(private readonly server: string, private readonly token: string, private readonly fetch: Fetch) {}

  static async pair(server: string, code: string, browser: string, fetch: Fetch): Promise<string> {
    const r = await fetch(`${checkServer(server)}/api/v1/extension/pair`, { method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ code: code.trim(), browser }) });
    const j = await r.json().catch(() => ({}));
    if (!r.ok) throw new Error(j.error ?? `the server answered ${r.status}`);
    return j.token as string;
  }

  /** One-click pairing, step one: ask to connect. The owner approves in Browser Foreman, comparing `match`. */
  static async request(server: string, browser: string, version: string, fetch: Fetch): Promise<PairRequest> {
    const r = await fetch(`${checkServer(server)}/api/v1/extension/requests`, { method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ browser, version }) });
    const j = await r.json().catch(() => ({}));
    if (!r.ok) throw new Error(j.error ?? `the server answered ${r.status}`);
    return { id: j.id, poll: j.poll, match: j.match, expires: j.expires };
  }

  /** Step two, repeated: has the owner answered? Approved, the answer carries the token (once). */
  static async answer(server: string, req: PairRequest, fetch: Fetch): Promise<{ state: string; token?: string }> {
    const r = await fetch(`${checkServer(server)}/api/v1/extension/requests/${encodeURIComponent(req.id)}/poll`, { method: "POST",
      headers: { "Content-Type": "application/json" }, body: JSON.stringify({ poll: req.poll }) });
    const j = await r.json().catch(() => ({}));
    if (r.status === 404) return { state: "expired" };
    if (!r.ok) throw new Error(j.error ?? `the server answered ${r.status}`);
    return j;
  }

  private async call(method: string, path: string, body?: unknown): Promise<any> {
    const r = await this.fetch(`${this.server}/api/v1/${path}`, { method, headers: { "Content-Type": "application/json", Authorization: `Bearer ${this.token}` },
      ...(body === undefined ? {} : { body: JSON.stringify(body) }) });
    if (r.status === 204) return {};
    const j = await r.json().catch(() => ({}));
    if (!r.ok) throw Object.assign(new Error(j.error ?? `the server answered ${r.status}`), { status: r.status });
    return j;
  }

  sendTabs(report: TabReport & { version?: string }): Promise<unknown> { return this.call("POST", "extension/tabs", report); }
  sendBookmarks(nodes: unknown[], workspace?: string): Promise<{ entry: { seq: number; summary: string } }> {
    return this.call("POST", "extension/bookmarks", workspace ? { nodes, workspace } : { nodes });
  }
  commands(wait: number): Promise<{ commands: Command[] }> { return this.call("GET", `extension/commands?wait=${wait}`); }
  done(id: string, ok: boolean, error = "", result?: unknown): Promise<unknown> { return this.call("POST", `extension/commands/${encodeURIComponent(id)}/done`, { ok, error, ...(result === undefined ? {} : { result }) }); }
  workspaces(): Promise<{ workspaces: { id: string; name: string; bookmarks: number }[] }> { return this.call("GET", "workspaces"); }
  workspace(id: string): Promise<{ tree: any; workspace: { head: number } }> { return this.call("GET", `workspaces/${encodeURIComponent(id)}`); }
}

export interface PairRequest { id: string; poll: string; match: string; expires: string }

export interface Command { id: string; kind: "open" | "open-group" | "reload" | "read-bookmarks" | "write-bookmarks"; urls: string[]; title?: string; color?: string; incognito?: boolean; workspace?: string; expect?: string }

/** Tab group colours the browsers accept; anything else becomes grey. */
export const GROUP_COLORS = ["grey", "blue", "red", "yellow", "green", "pink", "purple", "cyan", "orange"];
export const groupColor = (c?: string): string => GROUP_COLORS.includes(c ?? "") ? c! : "grey";

/** Only web links are opened from a command. */
export const webOnly = (urls: string[]): string[] => urls.filter(u => /^https?:\/\//i.test(u)).slice(0, 200);

/** What the extension does about connecting, each minute and when it starts, while it is not connected:
 *  - "wait": a request is waiting for the owner's answer: ask the server for it;
 *  - "ask": nothing is waiting: ask the server to connect (the owner approves, comparing the letters on the icon);
 *  - "rest": the owner said no, or three requests went unanswered: ask again only when Connect is pressed. */
export function connectStep(s: { paired: boolean; asking?: { expires: string }; declined?: boolean; unanswered?: number }, now: number): "none" | "wait" | "ask" | "rest" {
  if (s.paired) return "none";
  if (s.asking && Date.parse(s.asking.expires) > now) return "wait";
  if (s.declined || (s.unanswered ?? 0) >= 3) return "rest";
  return "ask";
}

/** The browser's bookmarks as a fingerprint, computed as the server does (writeback.ExtFingerprint): every node below
 *  the browser's own root, depth first, "id TAB title TAB url NEWLINE" with the title's spaces collapsed and the
 *  address trimmed (separators left out), SHA-256 in hex. A write compares it with the plan's and refuses a change. */
export async function extFingerprint(roots: { id: string; title?: string; url?: string; type?: string; children?: any[] }[]): Promise<string> {
  let text = "";
  const walk = (n: { children?: any[] }) => {
    for (const c of n.children ?? []) {
      if (c.type === "separator") continue;
      text += `${c.id}\t${(c.title ?? "").split(/\s+/).filter(Boolean).join(" ")}\t${(c.url ?? "").trim()}\n`;
      walk(c);
    }
  };
  const only = roots.length === 1 ? roots[0] : undefined;
  const tops = only && !only.url ? only : { children: roots }; // the browser's unnamed root, when it sent one
  walk(tops);
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(text));
  return [...new Uint8Array(digest)].map(b => b.toString(16).padStart(2, "0")).join("");
}
