// Dialogs and notifications: showing and hiding the page's Tabler dialogs (one shared backdrop), a confirm, a text
// prompt, and toasts. Moved from the page script into a typed module on 2026-11-12; behaviour is unchanged.
//
//   installDialogs({ afterHide: id => …, toastSeconds: () => settings.toastSecs });
//   if (await askConfirm({ title: "Delete?", body: "…", ok: "Delete" })) …
//   const name = await askText({ title: "Rename", label: "Name", value: "Old" });   // null when cancelled
//   toast("Saved", "success");

import { $, el, icon } from "./ui";

export type ToastKind = "info" | "success" | "warning" | "danger";
export interface ConfirmOptions { title: string; body: string; ok?: string; no?: string; danger?: boolean }
/** A confirm with a third way out: later, which decides nothing (and is what Escape or closing it means). */
export interface DecideOptions extends ConfirmOptions { later: string }
export interface TextOptions { title: string; label: string; value?: string; hint?: string; multiline?: boolean; ok?: string }
export interface DialogOptions {
  afterHide?: (id: string) => void;   // the page's own follow-up (e.g. give the tree its focus back)
  toastSeconds?: () => number;        // how long a toast stays (Settings › Notifications stay for)
}

let opts: DialogOptions = {};
let backdrop: HTMLElement | null = null;
const must = <T extends Element>(sel: string): T => { const e = $<T>(sel); if (!e) throw new Error(`missing ${sel}`); return e; };

// Visible, not merely classed: the Alpine dialogs (Browsers, notes, clean-up…) keep "show" and hide with x-show, so a
// class check always found one "open" and left the backdrop over the page after every dialog closed.
export const visibleModals = (): HTMLElement[] =>
  Array.from(document.querySelectorAll<HTMLElement>(".modal.show")).filter(m => getComputedStyle(m).display !== "none");
export const modalOpen = (): boolean => visibleModals().length > 0;
function dropBackdrop(): void { if (backdrop && !visibleModals().length) { backdrop.remove(); backdrop = null; } }

// Dialogs stack: one opened over another (a confirm from inside a dialog) goes above it, and the one shared backdrop
// sits just under the topmost. Without this a confirm opened from a dialog later in the page showed behind it,
// invisible, while it waited for an answer (Disconnect… in Browser extensions, 2026-11-16).
const BASE_Z = 1055, STEP_Z = 20;
function restack(): void {
  const open = visibleModals();
  if (backdrop) backdrop.style.zIndex = String(BASE_Z - 5 + STEP_Z * Math.max(0, open.length - 1));
}

export function showModal(id: string): void {
  const m = must<HTMLElement>(id);
  const under = visibleModals().filter(x => x !== m).length;
  m.style.zIndex = String(BASE_Z + STEP_Z * under);
  m.style.display = "block"; m.classList.add("show");
  if (!backdrop) { backdrop = el("div", "modal-backdrop fade show"); document.body.append(backdrop); }
  restack();
}

export function hideModal(id: string): void {
  const m = must<HTMLElement>(id); m.classList.remove("show"); m.style.display = "none"; m.style.zIndex = "";
  opts.afterHide?.(id);
  dropBackdrop();
  restack();
}

/** Text editor dialog: rename, new folder, descriptions. Resolves with the text, or null when cancelled. */
export function askText({ title, label, value = "", hint = "", multiline = false, ok = "Save" }: TextOptions): Promise<string | null> {
  return new Promise(resolve => {
    must("#textTitle").textContent = title; must("#textLabel").textContent = label; must("#textHint").textContent = hint; must("#textOk").textContent = ok;
    const area = must<HTMLTextAreaElement>("#textArea"), line = must<HTMLInputElement>("#textIn"), okBtn = must<HTMLButtonElement>("#textOk");
    const inp = multiline ? area : line;
    area.hidden = !multiline; line.hidden = multiline; inp.value = value;
    const done = (v: string | null) => { hideModal("#textModal"); okBtn.onclick = null; inp.onkeydown = null; resolve(v); };
    okBtn.onclick = () => done(inp.value);
    inp.onkeydown = (e: KeyboardEvent) => { e.stopPropagation(); if (e.key === "Escape") done(null); if (e.key === "Enter" && (!multiline || e.metaKey || e.ctrlKey)) { e.preventDefault(); done(inp.value); } };
    for (const b of Array.from(must("#textModal").querySelectorAll<HTMLElement>("[data-close]"))) b.onclick = () => done(null);
    showModal("#textModal"); setTimeout(() => { inp.focus(); inp.select(); }, 30);
  });
}

/** What Enter means in a confirm: the button that has the focus, so Enter on a focused Cancel cancels; OK only when
 *  neither button has it. Enter used to mean OK wherever the focus was, and the dialog opens with Cancel focused. */
export function enterMeans(focused: unknown, no: unknown): boolean {
  return focused !== no;
}

// The confirm dialog is one element. A question asked while another is open (an extension asking to connect, on a
// 3 s poll, over a delete) waits its turn; it used to take the dialog over, leave the first question unanswered, and
// let the first one's Enter handler answer yes to it.
let confirmTurn: Promise<unknown> = Promise.resolve();
export function oneAtATime<T>(ask: () => Promise<T>): Promise<T> {
  const turn = confirmTurn.then(ask, ask);
  confirmTurn = turn.then(() => undefined, () => undefined);
  return turn;
}

/** Confirm dialog: danger (red) for anything destructive or irreversible, primary for big but harmless jobs. */
export function askConfirm(o: ConfirmOptions): Promise<boolean> { return oneAtATime(() => confirmNow(o)); }
function confirmNow({ title, body, ok = "Continue", no = "Cancel", danger = true }: ConfirmOptions): Promise<boolean> {
  return new Promise(resolve => {
    must("#confirmTitle").textContent = title; must("#confirmBody").textContent = body;
    must("#confirmBar").className = "modal-status " + (danger ? "bg-danger" : "bg-primary");
    must("#confirmIcon").className = "ti mb-2 " + (danger ? "ti-alert-triangle text-danger" : "ti-info-circle text-primary");
    const yes = must<HTMLButtonElement>("#confirmYes"), noBtn = must<HTMLButtonElement>("#confirmNo");
    yes.className = "btn " + (danger ? "btn-danger" : "btn-primary"); yes.textContent = ok; noBtn.textContent = no;
    const key = (e: KeyboardEvent) => { if (e.key === "Escape") { e.stopPropagation(); done(false); } if (e.key === "Enter") { e.stopPropagation(); e.preventDefault(); done(enterMeans(document.activeElement, noBtn)); } };
    const done = (v: boolean) => { hideModal("#confirmModal"); yes.onclick = noBtn.onclick = null; document.removeEventListener("keydown", key, true); resolve(v); };
    yes.onclick = () => done(true); noBtn.onclick = () => done(false);
    document.addEventListener("keydown", key, true);
    showModal("#confirmModal"); setTimeout(() => noBtn.focus({ focusVisible: false } as FocusOptions), 30);  // Esc and Enter still work; no ring until the keyboard moves
  });
}

/** Confirm with Not now: resolves "yes", "no", or "later" (also Escape), for a question that may wait, such as an
 *  extension asking to connect: later leaves it unanswered rather than refusing it. */
export function askDecide(o: DecideOptions): Promise<"yes" | "no" | "later"> { return oneAtATime(() => decideNow(o)); }
function decideNow({ title, body, ok = "Continue", no = "Cancel", later, danger = false }: DecideOptions): Promise<"yes" | "no" | "later"> {
  return new Promise(resolve => {
    must("#confirmTitle").textContent = title; must("#confirmBody").textContent = body;
    must("#confirmBar").className = "modal-status " + (danger ? "bg-danger" : "bg-primary");
    must("#confirmIcon").className = "ti mb-2 " + (danger ? "ti-alert-triangle text-danger" : "ti-info-circle text-primary");
    const yes = must<HTMLButtonElement>("#confirmYes"), noBtn = must<HTMLButtonElement>("#confirmNo"), wait = must<HTMLButtonElement>("#confirmLater");
    yes.className = "btn " + (danger ? "btn-danger" : "btn-primary"); yes.textContent = ok; noBtn.textContent = no; wait.textContent = later; wait.hidden = false;
    const key = (e: KeyboardEvent) => { if (e.key === "Escape") { e.stopPropagation(); done("later"); } };
    const done = (v: "yes" | "no" | "later") => { hideModal("#confirmModal"); yes.onclick = noBtn.onclick = wait.onclick = null; wait.hidden = true; document.removeEventListener("keydown", key, true); resolve(v); };
    yes.onclick = () => done("yes"); noBtn.onclick = () => done("no"); wait.onclick = () => done("later");
    document.addEventListener("keydown", key, true);
    showModal("#confirmModal"); setTimeout(() => wait.focus({ focusVisible: false } as FocusOptions), 30);
  });
}

// Notifications: stacked bottom-right, newest at the bottom, one style per kind; errors stay until closed.
const TOAST_KIND: Record<ToastKind, [string, string]> = { info: ["ti-info-circle", "primary"], success: ["ti-circle-check", "green"], warning: ["ti-alert-triangle", "yellow"], danger: ["ti-alert-octagon", "red"] };
export function toast(text: string, kind: ToastKind = "info", title = ""): void {
  const box = $("#toastBox"); if (!box) return;
  // The same message again (a refresh failing every few seconds) replaces its toast instead of stacking another.
  const key = `${kind}\u0000${title}\u0000${text}`;
  for (const old of [...box.children] as HTMLElement[]) if (old.dataset.key === key) old.remove();
  const [ic, col] = TOAST_KIND[kind] ?? TOAST_KIND.info;
  const t = el("div", `toast show bm-toast bm-toast-${kind}`); t.setAttribute("role", kind === "danger" ? "alert" : "status"); t.dataset.key = key;
  const body = el("div", "toast-body d-flex gap-2 align-items-start"), txt = el("div", "flex-fill min-w-0");
  if (title) txt.append(el("div", "fw-bold", title));
  txt.append(el("div", title ? "text-secondary" : null, text));
  const x = el("button", "btn-close ms-1"); x.type = "button"; x.setAttribute("aria-label", "Close");
  body.append(icon(ic, `text-${col} fs-3 flex-none`), txt, x); t.append(body);
  const close = () => { t.classList.add("hiding"); setTimeout(() => t.remove(), 180); };
  x.onclick = close;
  box.append(t);
  while (box.children.length > 4) box.firstElementChild?.remove();
  if (kind !== "danger") {
    const secs = opts.toastSeconds?.() ?? 4.5;
    let tm = setTimeout(close, secs * (kind === "warning" ? 1600 : 1000));
    t.onmouseenter = () => clearTimeout(tm); t.onmouseleave = () => { tm = setTimeout(close, 2500); };
  }
}

/** The page-wide rules, once: the page's own dialogs close from their [data-close] buttons and a click outside; Escape
 *  closes the topmost through its own Close button (so a dialog waiting for an answer still gets "no"); a click on a
 *  backdrop with no dialog left above it clears it. */
export function installDialogs(o: DialogOptions = {}): void {
  opts = o;
  document.addEventListener("mousedown", e => { if (e.target === backdrop) dropBackdrop(); }, true);
  // Bubble phase: a field that handles Escape itself stops it first.
  document.addEventListener("keydown", e => {
    if (e.key !== "Escape") return;
    const top = visibleModals().filter(m => m.id && m.querySelector("[data-close]")).pop();
    if (top) { e.preventDefault(); top.querySelector<HTMLElement>("[data-close]")?.click(); }
    setTimeout(dropBackdrop, 0);
  });
  // The page's own dialogs only: Alpine dialogs (inside x-data, no id) close themselves.
  for (const m of Array.from(document.querySelectorAll<HTMLElement>(".modal[id]"))) {
    if (m.closest("[x-data]")) continue;
    m.addEventListener("mousedown", e => { if (e.target === m) hideModal("#" + m.id); });
    for (const b of Array.from(m.querySelectorAll<HTMLElement>("[data-close]"))) b.onclick = () => hideModal("#" + m.id);
  }
}
