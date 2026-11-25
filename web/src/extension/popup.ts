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
  $("unpaired").hidden = s.paired; $("paired").hidden = !s.paired; $("write").hidden = !s.paired || !s.bookmarks; $("foot").hidden = !s.paired;
  $("read").hidden = !s.bookmarks; // Safari: no bookmarks API for extensions
  ($("server") as HTMLInputElement).value = s.offer && !s.paired ? s.offer : s.server;
  if (s.offer && !s.paired) say("askMsg", `A Browser Foreman page at ${s.offer} asked to connect. Press Connect if it is yours.`);
  $("who").textContent = s.browser; $("where").textContent = s.server;
  $("last").textContent = s.lastError ? `Last report failed: ${s.lastError}` : s.lastReport ? `Tabs last sent ${new Date(s.lastReport).toLocaleTimeString()}` : "";
  if (s.paired) {
    const { workspaces } = await ask({ kind: "workspaces" }).catch(() => ({ workspaces: [] }));
    const sel = $("ws") as HTMLSelectElement;
    sel.replaceChildren(...workspaces.map((w: { id: string; name: string; bookmarks: number }) => new Option(`${w.name} (${w.bookmarks})`, w.id)));
  }
}

// One-click pairing: ask, show the match code, and wait (up to five minutes) for the owner's answer.
let waitTimer: ReturnType<typeof setInterval> | undefined;
async function waitForAnswer(): Promise<void> {
  clearInterval(waitTimer);
  const tick = async () => {
    try {
      const a = await ask({ kind: "answer" });
      if (a.state === "pending") { $("waiting").hidden = false; $("match").textContent = a.match; ($("ask") as HTMLButtonElement).disabled = true; return; }
      clearInterval(waitTimer); $("waiting").hidden = true; ($("ask") as HTMLButtonElement).disabled = false;
      if (a.state === "approved") { say("askMsg", "Connected."); await show(); }
      else if (a.state === "denied") say("askMsg", "Browser Foreman said no.", true);
      else if (a.state === "expired") say("askMsg", "No answer in five minutes; connect again.", true);
    } catch (e) { clearInterval(waitTimer); ($("ask") as HTMLButtonElement).disabled = false; say("askMsg", (e as Error).message, true); }
  };
  await tick(); waitTimer = setInterval(() => void tick(), 2000);
}
$("ask").onclick = async () => {
  try {
    ($("ask") as HTMLButtonElement).disabled = true; say("askMsg", "Asking…");
    const server = checkServer(($("server") as HTMLInputElement).value);
    await ask({ kind: "ask", server });
    say("askMsg", "");
    await waitForAnswer();
  } catch (e) { ($("ask") as HTMLButtonElement).disabled = false; say("askMsg", (e as Error).message, true); }
};
$("pair").onclick = async () => {
  try {
    const server = checkServer(($("server") as HTMLInputElement).value);
    await ask({ kind: "pair", server, code: ($("code") as HTMLInputElement).value });
    await show();
  } catch (e) { say("pairMsg", (e as Error).message, true); }
};
// The popup shows what went wrong rather than going quiet: a failed call says so in the section it came from.
const shown = () => show().catch(e => say("askMsg", (e as Error).message, true));
$("send").onclick = async () => { await ask({ kind: "report" }).catch(() => {}); await shown(); };
$("read").onclick = async () => {
  try { say("readMsg", "Reading…"); say("readMsg", (await ask({ kind: "read" })).summary); await show(); }
  catch (e) { say("readMsg", (e as Error).message, true); }
};
// What Write applies: the workspace previewed, onto the bookmarks as they were when previewed. Another choice of
// workspace needs a new preview.
let previewed: { workspace: string; expect: string } | null = null;
($("ws") as HTMLSelectElement).onchange = () => { previewed = null; $("apply").hidden = true; say("planMsg", ""); };
$("preview").onclick = async () => {
  $("apply").hidden = true; previewed = null;
  try {
    const workspace = ($("ws") as HTMLSelectElement).value;
    const p = await ask({ kind: "plan", workspace });
    previewed = { workspace, expect: p.expect };
    if (!p.steps) return say("planMsg", "This browser already matches that workspace.");
    say("planMsg", `${p.created} added, ${p.updated} renamed or readdressed, ${p.moved} moved, ` +
      `${p.removedBookmarks} bookmark${p.removedBookmarks === 1 ? "" : "s"} and ${p.removedFolders} folder${p.removedFolders === 1 ? "" : "s"} removed.`, p.removedBookmarks > 0);
    $("apply").hidden = false;
  } catch (e) { say("planMsg", (e as Error).message, true); }
};
$("apply").onclick = async () => {
  if (!confirm("Write this workspace into this browser's bookmarks? The bookmarks as they are now are saved to Browser Foreman first.")) return;
  $("apply").hidden = true;
  if (!previewed) return;
  try { say("planMsg", "Writing…"); const r = await ask({ kind: "apply", ...previewed }); previewed = null; say("planMsg", `Done. Saved first: ${r.backup}`); }
  catch (e) { say("planMsg", (e as Error).message, true); }
};
$("unpair").onclick = async () => {
  try { await ask({ kind: "unpair" }); } catch (e) { say("readMsg", (e as Error).message, true); }
  await shown();
};
void shown().then(waitForAnswer);  // reopening the popup while a request waits shows its code again
