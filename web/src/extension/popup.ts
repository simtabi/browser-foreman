// The extension's popup: connect with a pairing code, send tabs, read bookmarks, and write a workspace back after
// showing exactly what would change.

import { ext } from "./ext";
import { checkServer } from "./core";

const $ = <T extends HTMLElement = HTMLElement>(id: string) => document.getElementById(id) as T;
const ask = async (msg: object): Promise<any> => {
  const r = await ext.runtime.sendMessage(msg);
  if (!r?.ok) throw new Error(r?.error ?? "no answer from the extension");
  return r.result;
};
const say = (id: string, text: string, bad = false) => { const el = $(id); el.textContent = text; el.classList.toggle("bad", bad); };

async function show(): Promise<void> {
  const s = await ask({ kind: "status" });
  $("unpaired").hidden = s.paired; $("paired").hidden = !s.paired; $("write").hidden = !s.paired; $("foot").hidden = !s.paired;
  ($("server") as HTMLInputElement).value = s.server;
  $("who").textContent = s.browser; $("where").textContent = s.server;
  $("last").textContent = s.lastError ? `Last report failed: ${s.lastError}` : s.lastReport ? `Tabs last sent ${new Date(s.lastReport).toLocaleTimeString()}` : "";
  if (s.paired) {
    const { workspaces } = await ask({ kind: "workspaces" }).catch(() => ({ workspaces: [] }));
    const sel = $("ws") as HTMLSelectElement;
    sel.replaceChildren(...workspaces.map((w: { id: string; name: string; bookmarks: number }) => new Option(`${w.name} (${w.bookmarks})`, w.id)));
  }
}

$("pair").onclick = async () => {
  try {
    const server = checkServer(($("server") as HTMLInputElement).value);
    await ask({ kind: "pair", server, code: ($("code") as HTMLInputElement).value });
    await show();
  } catch (e) { say("pairMsg", (e as Error).message, true); }
};
$("send").onclick = async () => { await ask({ kind: "report" }).catch(() => {}); await show(); };
$("read").onclick = async () => {
  try { say("readMsg", "Reading…"); say("readMsg", (await ask({ kind: "read" })).summary); await show(); }
  catch (e) { say("readMsg", (e as Error).message, true); }
};
$("preview").onclick = async () => {
  $("apply").hidden = true;
  try {
    const p = await ask({ kind: "plan", workspace: ($("ws") as HTMLSelectElement).value });
    if (!p.steps) return say("planMsg", "This browser already matches that workspace.");
    say("planMsg", `${p.created} added, ${p.updated} renamed or readdressed, ${p.moved} moved, ` +
      `${p.removedBookmarks} bookmark${p.removedBookmarks === 1 ? "" : "s"} and ${p.removedFolders} folder${p.removedFolders === 1 ? "" : "s"} removed.`, p.removedBookmarks > 0);
    $("apply").hidden = false;
  } catch (e) { say("planMsg", (e as Error).message, true); }
};
$("apply").onclick = async () => {
  if (!confirm("Write this workspace into this browser's bookmarks? The bookmarks as they are now are saved to Browser Foreman first.")) return;
  $("apply").hidden = true;
  try { say("planMsg", "Writing…"); const r = await ask({ kind: "apply", workspace: ($("ws") as HTMLSelectElement).value }); say("planMsg", `Done. Saved first: ${r.backup}`); }
  catch (e) { say("planMsg", (e as Error).message, true); }
};
$("unpair").onclick = async () => { await ask({ kind: "unpair" }); await show(); };
void show();
