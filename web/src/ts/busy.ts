// Feedback while the page works, so nothing ever looks stuck and nothing can be pressed twice:
//
//   await busy("Expanding every folder…", () => expandAll());   // blocks the page: a veil, a centred spinner, buttons off
//   box.replaceChildren(skeleton("Loading history…", 6));       // grey placeholder rows with the message
//   watchRequests();                                            // once, at boot: saves show a spinner in their button
//
// busy() paints its indicator before the work starts (the work may block the page for a moment, so it waits for the
// indicator to be drawn first). The veil and the spinner appear only when the work takes longer than a blink, so quick
// work never flickers, but buttons are disabled at once, so a second click cannot start the work again. Calls may
// overlap; the indicator stays while any is running and shows the latest message.
//
// watchRequests() follows every request that changes something (and every download): the button that started it
// shows a spinner and is disabled until the answer arrives, and a slow one also shows the bar and a message.

import { el } from "./ui";

let running = 0, bar: HTMLElement | null = null, veil: HTMLElement | null = null, text: HTMLElement | null = null, note: HTMLElement | null = null;
const locked: HTMLButtonElement[] = [];

function ensure(): void {
  if (bar && veil && text && note) return;
  bar = el("div", "busy-bar"); bar.setAttribute("aria-hidden", "true"); bar.append(el("div", "busy-bar-fill"));
  veil = el("div", "busy-veil");
  const card = el("div", "busy-card"); card.setAttribute("role", "status"); card.setAttribute("aria-live", "polite");
  text = el("div", "busy-msg");
  card.append(el("span", "spinner-border busy-spin"), text);
  veil.append(card);
  // A slow save does not block the page: its note sits at the top, under the bar.
  note = el("div", "busy-note"); note.setAttribute("role", "status"); note.setAttribute("aria-live", "polite");
  document.body.append(bar, veil, note);
}

/** Lets the browser draw the indicator before blocking work starts (a frame, or a short timer when the page is hidden). */
const painted = (): Promise<void> => new Promise(resolve => {
  let done = false; const go = () => { if (!done) { done = true; resolve(); } };
  requestAnimationFrame(() => setTimeout(go, 0)); setTimeout(go, 50);
});

/** Every enabled button outside the indicator goes off until the work ends (and comes back as it was). */
function lock(): void {
  for (const b of document.querySelectorAll<HTMLButtonElement>("button:not(:disabled)")) {
    if (veil?.contains(b)) continue;
    b.disabled = true; b.dataset.busyLocked = "1"; locked.push(b);
  }
  document.body.setAttribute("aria-busy", "true");
}
function unlock(): void {
  for (const b of locked.splice(0)) { if (b.dataset.busyLocked) { b.disabled = false; delete b.dataset.busyLocked; } }
  document.body.removeAttribute("aria-busy");
}

/** Run `work` with the page blocked and `message` showing; resolves with what the work returns. */
export async function busy<T>(message: string, work: () => T | Promise<T>): Promise<T> {
  ensure();
  if (running++ === 0) lock();
  text!.textContent = message;
  document.body.classList.add("is-busy");
  await painted();
  try { return await work(); }
  finally { if (--running === 0) { document.body.classList.remove("is-busy"); unlock(); } }
}

/** Placeholder rows with a spinner and a message, for anything still loading (lists, tables, panels). */
export function skeleton(message: string, rows = 5): HTMLElement {
  const box = el("div", "skeleton placeholder-glow"); box.setAttribute("role", "status"); box.setAttribute("aria-live", "polite");
  const head = el("div", "skeleton-msg"); head.append(el("span", "spinner-border spinner-border-sm"), el("span", null, message));
  box.append(head);
  for (let k = 0; k < rows; k++) {
    const r = el("div", "skeleton-row");
    r.append(el("span", "placeholder skeleton-dot"), el("span", `placeholder col-${[7, 5, 8, 6, 9][k % 5]}`));
    box.append(r);
  }
  return box;
}

// ---------- requests: the button that started a save shows a spinner and stays off until the answer ----------

let lastPress: { button: HTMLButtonElement; at: number } | null = null, pending = 0, slowTimer = 0;

/** A request that changes something, or a download; plain reads (lists, counts, polling) are not shown. */
export function isWork(method: string, url: string): boolean {
  return method.toUpperCase() !== "GET" || /\/(export|backup)\b|\.csv(\?|$)/.test(url);
}

/** The button pressed just before a request, if it is still on the page and not a link inside a menu. */
function presser(): HTMLButtonElement | null {
  const p = lastPress; lastPress = null;
  return p && Date.now() - p.at < 1000 && p.button.isConnected ? p.button : null;
}

/** Follow every request the page makes (call once, at boot). */
export function watchRequests(): void {
  ensure();
  document.addEventListener("click", e => {
    const b = (e.target as Element | null)?.closest?.("button");
    if (b instanceof HTMLButtonElement && !b.disabled) lastPress = { button: b, at: Date.now() };
  }, true);
  const send = window.fetch.bind(window);
  window.fetch = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const url = typeof input === "string" ? input : input instanceof URL ? input.href : input.url;
    const method = init?.method ?? (input instanceof Request ? input.method : "GET");
    if (init?.keepalive || !isWork(method, url)) return send(input, init);
    const b = presser();
    if (b) { b.disabled = true; b.classList.add("btn-loading"); b.setAttribute("aria-busy", "true"); }
    if (pending++ === 0) slowTimer = window.setTimeout(() => {
      note!.replaceChildren(el("span", "spinner-border spinner-border-sm"), el("span", null, b?.textContent?.trim() ? `${b.textContent.trim().replace(/…$/, "")}…` : "Saving…"));
      document.body.classList.add("is-saving");
    }, 400);
    try { return await send(input, init); }
    finally {
      if (b) { b.disabled = false; b.classList.remove("btn-loading"); b.removeAttribute("aria-busy"); }
      if (--pending === 0) { clearTimeout(slowTimer); document.body.classList.remove("is-saving"); }
    }
  };
}
