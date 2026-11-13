// Feedback while the page works, so nothing ever looks stuck:
//
//   await busy("Expanding every folder…", () => expandAll());   // a spinner with the message and a progress bar
//   box.replaceChildren(skeleton("Loading history…", 6));       // grey placeholder rows with the message
//
// busy() paints its indicator before the work starts (the work may block the page for a moment, so it waits for the
// indicator to be drawn first), and shows it only when the work takes longer than a blink, so quick work never
// flickers. Calls may overlap; the indicator stays while any is running and shows the latest message.

import { el } from "./ui";

let running = 0, bar: HTMLElement | null = null, chip: HTMLElement | null = null, text: HTMLElement | null = null;

function ensure(): void {
  if (bar && chip && text) return;
  bar = el("div", "busy-bar"); bar.setAttribute("aria-hidden", "true"); bar.append(el("div", "busy-bar-fill"));
  chip = el("div", "busy-chip"); chip.setAttribute("role", "status"); chip.setAttribute("aria-live", "polite");
  text = el("span");
  chip.append(el("span", "spinner-border spinner-border-sm"), text);
  document.body.append(bar, chip);
}

/** Lets the browser draw the indicator before blocking work starts (a frame, or a short timer when the page is hidden). */
const painted = (): Promise<void> => new Promise(resolve => {
  let done = false; const go = () => { if (!done) { done = true; resolve(); } };
  requestAnimationFrame(() => setTimeout(go, 0)); setTimeout(go, 50);
});

/** Run `work` with the busy indicator showing `message`; resolves with what the work returns. */
export async function busy<T>(message: string, work: () => T | Promise<T>): Promise<T> {
  ensure(); running++;
  text!.textContent = message;
  document.body.classList.add("is-busy");
  await painted();
  try { return await work(); }
  finally { if (--running === 0) document.body.classList.remove("is-busy"); }
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
