// The extension's background: reports this browser's tabs to the local server when they change (and each minute),
// waits for the server's commands (open links, open a tab group, read the bookmarks), and carries out what the popup asks.

import { ext, type ExtBookmark } from "./ext";
import { browserName, Client, connectStep, groupColor, tabReport, webOnly, type Command } from "./core";
import { plan, run, type WsNode } from "./apply";

interface Settings { server?: string; token?: string; browser?: string }
const VERSION = (): string => ext.runtime.getManifest().version;

const settings = async (): Promise<Settings> => ext.storage.local.get(["server", "token", "browser"]) as Promise<Settings>;
const thisBrowser = (): string => {
  const nav = navigator as Navigator & { userAgentData?: { brands: { brand: string }[] }; brave?: unknown };
  return browserName(navigator.userAgent, (nav.userAgentData?.brands ?? []).map(b => b.brand), !!nav.brave);
};
async function client(): Promise<Client | null> {
  const s = await settings();
  return s.server && s.token ? new Client(s.server, s.token, (u, i) => fetch(u, i)) : null;
}
// A token the server no longer accepts (revoked) unpairs the extension, so it stops calling.
async function unpairedIf(e: unknown): Promise<void> {
  if ((e as { status?: number }).status === 401) { await ext.storage.local.remove(["token"]); await ext.storage.local.set({ declined: true }); } // disconnected on purpose: no asking again by itself
}

// ---- tabs ----
let timer: ReturnType<typeof setTimeout> | undefined;
async function report(): Promise<void> {
  const c = await client(); if (!c) return;
  const windows = await ext.windows.getAll({ populate: true, windowTypes: ["normal"] });
  const groups = ext.tabGroups ? await ext.tabGroups.query({}) : [];
  try { await c.sendTabs({ ...tabReport(windows, groups), version: VERSION() }); await ext.storage.local.set({ lastReport: Date.now(), lastError: "" }); }
  catch (e) { await ext.storage.local.set({ lastError: String((e as Error).message) }); await unpairedIf(e); }
}
const soon = (): void => { clearTimeout(timer); timer = setTimeout(() => void report(), 3000); };
for (const h of [ext.tabs.onCreated, ext.tabs.onRemoved, ext.tabs.onMoved, ext.tabs.onAttached, ext.windows.onCreated, ext.windows.onRemoved]) h.addListener(soon);
ext.tabs.onUpdated.addListener((_id, change) => { if (change.url || change.title || change.status === "complete") soon(); });
ext.tabGroups?.onUpdated.addListener(soon);

// ---- commands ----
let polling = false;
async function openLinks(c: Command): Promise<void> {
  const urls = webOnly(c.urls);
  if (!urls.length) throw new Error("no web links");
  if (c.kind === "open-group" && ext.tabs.group && ext.tabGroups) {
    const win = await ext.windows.create({ url: urls, ...(c.incognito ? { incognito: true } : {}), focused: true });
    const ids = (win.tabs ?? []).map(t => t.id!).filter(x => x !== undefined);
    const gid = await ext.tabs.group({ tabIds: ids, createProperties: { windowId: win.id! } });
    await ext.tabGroups.update(gid, { title: c.title ?? "", color: groupColor(c.color) });
    return;
  }
  await ext.windows.create({ url: urls, ...(c.incognito ? { incognito: true } : {}), focused: true }); // no tab groups here (Firefox, Safari): a window
}
async function poll(): Promise<void> {
  if (polling) return;
  polling = true;
  try {
    for (let i = 0; i < 2; i++) { // about a minute; the alarm starts the next round, as a service worker may be stopped between
      const c = await client(); if (!c) return;
      let got;
      try { got = await c.commands(25); } catch (e) { await unpairedIf(e); return; }
      for (const cmd of got.commands) {
        if (cmd.kind === "reload") { await c.done(cmd.id, true).catch(() => {}); ext.runtime.reload(); return; } // read the folder again
        if (cmd.kind === "read-bookmarks") { // a write-back's dry run wants the bookmarks as they are now, in <browser>-live
          try { await readInto(c); await c.done(cmd.id, true); }
          catch (e) { await c.done(cmd.id, false, String((e as Error).message)).catch(() => {}); }
          continue;
        }
        try { await openLinks(cmd); await c.done(cmd.id, true); }
        catch (e) { await c.done(cmd.id, false, String((e as Error).message)).catch(() => {}); }
      }
    }
  } finally { polling = false; }
}
// ---- connecting by itself ----
// Installed and not connected, the extension asks Browser Foreman to connect on its own, so the owner is prompted
// without opening the popup. Its four letters show on the toolbar icon, to compare with the prompt (in the page, in
// the terminal running serve, or `browser-foreman pair --approve`). Nothing connects unless the owner approves.
const DEFAULT_SERVER = "http://127.0.0.1:8765";
const DEFAULT_TITLE = "Browser Foreman";
async function badge(match: string): Promise<void> {
  await ext.action?.setBadgeText({ text: match }).catch(() => {});
  await ext.action?.setBadgeBackgroundColor({ color: "#066fd1" }).catch(() => {});
  await ext.action?.setTitle({ title: match ? `Browser Foreman: approve ${match} to connect` : DEFAULT_TITLE }).catch(() => {});
}
/** The answer to a waiting request, carried out: approved connects; anything but pending ends the request. */
async function settle(asking: any): Promise<string> {
  const a = await Client.answer(asking.server, asking, (u, i) => fetch(u, i));
  if (a.state === "approved" && a.token) {
    await ext.storage.local.set({ server: asking.server, token: a.token, browser: thisBrowser(), unanswered: 0 });
    await ext.storage.local.remove(["asking", "declined"]);
    await badge("");
    void report(); void poll();
  } else if (a.state !== "pending") {
    const st = await ext.storage.local.get(["unanswered"]);
    await ext.storage.local.set(a.state === "denied" ? { declined: true } : { unanswered: (st.unanswered ?? 0) + 1 });
    await ext.storage.local.remove(["asking"]);
    await badge("");
  }
  return a.state;
}
let connecting = false;
async function autoConnect(): Promise<void> {
  if (connecting) return;
  connecting = true;
  try {
    const s = await settings();
    const st = await ext.storage.local.get(["asking", "declined", "unanswered"]);
    switch (connectStep({ paired: !!s.token, ...st }, Date.now())) {
      case "wait": await settle(st.asking).catch(() => {}); break;
      case "ask": {
        const server = s.server ?? DEFAULT_SERVER;
        const req = await Client.request(server, thisBrowser(), VERSION(), (u, i) => fetch(u, i)).catch(() => null); // not running: next minute
        if (req) { await ext.storage.local.set({ asking: { server, ...req } }); await badge(req.match); }
        break;
      }
      default: if (!st.asking) await badge("");
    }
  } finally { connecting = false; }
}

ext.alarms.create("tick", { periodInMinutes: 1 });
ext.alarms.onAlarm.addListener(a => { if (a.name === "tick") { void report(); void poll(); void autoConnect(); } });
ext.runtime.onStartup.addListener(() => { void report(); void poll(); void autoConnect(); });
ext.runtime.onInstalled.addListener(() => { void report(); void autoConnect(); });

// ---- the popup's requests ----
const slug = (s: string) => s.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "");
async function readInto(c: Client, workspace?: string): Promise<string> {
  const nodes = await ext.bookmarks.getTree();
  return (await c.sendBookmarks(nodes, workspace)).entry.summary;
}
async function handle(msg: any): Promise<unknown> {
  switch (msg.kind) {
    case "status": {
      const s = await settings();
      const st = await ext.storage.local.get(["lastReport", "lastError"]);
      return { paired: !!s.token, server: s.server ?? "http://127.0.0.1:8765", browser: s.browser ?? thisBrowser(), ...st };
    }
    case "pair": {
      const browser = thisBrowser();
      const token = await Client.pair(msg.server, msg.code, browser, (u, i) => fetch(u, i));
      await ext.storage.local.set({ server: new URL(msg.server).origin, token, browser });
      void report(); void poll();
      return { paired: true, browser };
    }
    case "ask": { // Connect pressed: ask now (again, even after a no), then wait for the owner's answer
      const server = new URL(msg.server).origin, browser = thisBrowser();
      const req = await Client.request(server, browser, VERSION(), (u, i) => fetch(u, i));
      await ext.storage.local.set({ asking: { server, ...req }, unanswered: 0 });
      await ext.storage.local.remove(["declined"]);
      await badge(req.match);
      return { match: req.match };
    }
    case "answer": {
      const { asking } = await ext.storage.local.get(["asking"]);
      if (!asking) return { state: "none" };
      return { state: await settle(asking), match: asking.match };
    }
    case "unpair": await ext.storage.local.remove(["token"]); await ext.storage.local.set({ declined: true }); return { paired: false };
    case "report": await report(); return { ok: true };
    case "read": { const c = await client(); if (!c) throw new Error("not paired"); return { summary: await readInto(c, msg.workspace || undefined) }; }
    case "workspaces": { const c = await client(); if (!c) throw new Error("not paired"); return c.workspaces(); }
    case "plan": case "apply": {
      const c = await client(); if (!c) throw new Error("not paired");
      const ws = (await c.workspace(msg.workspace)).tree.root as WsNode;
      const p = plan(await ext.bookmarks.getTree() as ExtBookmark[], ws);
      const counts = { created: p.created, updated: p.updated, moved: p.moved, removedBookmarks: p.removedBookmarks, removedFolders: p.removedFolders, steps: p.steps.length };
      if (msg.kind === "plan") return counts;
      // The browser's bookmarks as they are now go to the server first, so this write can be undone from there.
      const backup = await readInto(c, `${slug(thisBrowser())}-before-write`);
      const made = await run(p, ext.bookmarks);
      // Browsers differ on how a move's index counts within one folder: re-plan and fix any order left over.
      for (let i = 0; i < 3; i++) {
        const fix = plan(await ext.bookmarks.getTree() as ExtBookmark[], ws, made);
        if (!fix.steps.length) break;
        if (fix.steps.some(s => s.op === "create" || s.op === "remove")) throw new Error("the browser's bookmarks changed while writing; read them again and retry");
        await run(fix, ext.bookmarks);
      }
      // Read the result back, so the workspace's ids are the browser's and the next write matches nothing twice.
      const reread = await readInto(c, msg.workspace);
      return { ...counts, backup, reread };
    }
  }
  throw new Error("unknown request");
}
ext.runtime.onMessage.addListener((msg, _sender, reply) => {
  handle(msg).then(r => reply({ ok: true, result: r }), e => reply({ ok: false, error: String((e as Error).message ?? e) }));
  return true; // the reply comes later
});
