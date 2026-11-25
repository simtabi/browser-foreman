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

  static async pair(server: string, code: string, browser: string, fetch: Fetch, instance = "", version = ""): Promise<string> {
    const r = await fetch(`${checkServer(server)}/api/v1/extension/pair`, { method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ code: code.trim(), browser, instance, version }) });
    const j = await r.json().catch(() => ({}));
    if (!r.ok) throw new Error(j.error ?? `the server answered ${r.status}`);
    return j.token as string;
  }

  /** One-click pairing, step one: ask to connect. The owner approves in Browser Foreman, comparing `match`. */
  static async request(server: string, browser: string, version: string, fetch: Fetch, instance = "", replace = false): Promise<PairRequest> {
    const r = await fetch(`${checkServer(server)}/api/v1/extension/requests`, { method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ browser, version, instance, replace }) });
    const j = await r.json().catch(() => ({}));
    if (!r.ok) throw Object.assign(new Error(j.error ?? `the server answered ${r.status}`), { status: r.status });
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

/** Whether an error only says Chrome stopped or replaced the extension's service worker while it ran (a reload or an
 *  update): "No SW", or "Extension context invalidated". Nothing to report; the next worker carries on. */
export function workerGone(message: string): boolean {
  return /\bNo SW\b|Extension context invalidated/i.test(message);
}

/** Whether origin is a page on this computer (http on 127.0.0.1, localhost or ::1): the only pages the bridge serves. */
export function isLoopback(origin: string): boolean {
  try {
    const u = new URL(origin);
    return u.protocol === "http:" && ["127.0.0.1", "localhost", "[::1]"].includes(u.hostname);
  } catch { return false; }
}

/** Why a page at origin may not ask this extension to connect to it, or "" when it may. A connected extension
 *  connects again only to the server it uses; an unconnected one goes to the asking page's server, whose owner must
 *  still approve it there. So a page on another port can never take a connected extension away. */
export function connectRefusal(origin: string, server: string, paired: boolean): string {
  if (!isLoopback(origin)) return "only a Browser Foreman page on this computer can ask the extension to connect";
  if (paired && new URL(origin).origin !== new URL(server).origin) {
    return `the extension is connected to ${new URL(server).origin}: disconnect it there (or in its popup) first`;
  }
  return "";
}

/** Whether a page's request to connect waits for the person instead of going ahead: an unconnected extension connects
 *  by itself only to its usual server (the one it last used, or the default address). Any other page on this computer,
 *  which could be some other program's server, gets the address put in the popup, where Connect must be pressed. */
export function needsConfirmation(origin: string, server: string, paired: boolean): boolean {
  if (paired) return false; // a connected extension is held to its own server by connectRefusal
  try { return new URL(origin).origin !== new URL(server).origin; } catch { return true; }
}

/** Which messages the background takes from whom: a page (through the bridge) only "page-hello" and "page-connect";
 *  the popup only its own, never those two. So no web page can ask for a pairing code, a write or an unpair. */
export function messageAllowed(fromPage: boolean, kind: unknown): boolean {
  const bridge = kind === "page-hello" || kind === "page-connect";
  return fromPage === bridge;
}

/** A new instance id: random, made once by each installed copy and kept in its storage, so two copies that call
 *  themselves the same browser ("Google Chrome" twice on one computer) are told apart. */
export function newInstance(rand: (n: number) => Uint8Array = n => crypto.getRandomValues(new Uint8Array(n))): string {
  return Array.from(rand(16), b => b.toString(16).padStart(2, "0")).join("");
}
