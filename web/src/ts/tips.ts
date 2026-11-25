// Tooltips (hover or keyboard focus) and popovers (click), drawn with Tabler's own markup, caret included. Moved from
// the page script into a typed module on 2026-11-12; behaviour is unchanged.
//
// Any element with data-tip="…" gets a tooltip; showPop(button, title, body) opens a popover. Placement: an element's
// data-placement wins; otherwise the side that has room, in an order that suits where the element sits (tree rows
// open beside the row, header items below, everything else above). The box stays inside the window and the caret
// slides along its edge to point at the element's centre.
//
//   installTips({ enabled: () => settings.tooltips, delay: () => settings.tipDelay });

import { el } from "./ui";

type Kind = "tooltip" | "popover";
type Side = "top" | "bottom" | "left" | "right";
export interface TipOptions { enabled?: () => boolean; delay?: () => number }

const GAP = 4;
let opts: TipOptions = {};
let tipEl: HTMLElement | null = null, tipTimer = 0, tipFor: HTMLElement | null = null, usingKeyboard = false;
let popEl: HTMLElement | null = null;

function placementsFor(target: HTMLElement, kind: Kind): Side[] {
  if (target.dataset.placement) return [target.dataset.placement as Side, "top", "bottom", "right", "left"];
  if (target.closest("#list")) return ["right", "top", "bottom", "left"];
  if (target.closest("header, #stats, .toolbar")) return ["bottom", "top", "right", "left"];
  return kind === "popover" ? ["bottom", "top", "right", "left"] : ["top", "bottom", "right", "left"];
}

function place(box: HTMLElement, target: HTMLElement, kind: Kind): void {
  const r = target.getBoundingClientRect(), arrow = box.querySelector<HTMLElement>(`.${kind}-arrow`);
  if (!arrow) return;
  const fits = (side: Side) => {
    box.className = box.className.replace(/\bbs-(tooltip|popover)-\w+/g, "").trim() + ` bs-${kind}-${side}`;
    const w = box.offsetWidth, h = box.offsetHeight, a = arrow.offsetHeight || 6;
    const room = { top: r.top - h - a - GAP, bottom: innerHeight - r.bottom - h - a - GAP, left: r.left - w - a - GAP, right: innerWidth - r.right - w - a - GAP }[side];
    return { side, w, h, a, ok: room >= 8 };
  };
  const tries = placementsFor(target, kind).map(fits);
  const pick = tries.find(t => t.ok) ?? tries[0]!;
  fits(pick.side);
  const { w, h, a, side } = pick, cx = r.left + r.width / 2, cy = r.top + r.height / 2;
  let left: number, top: number;
  if (side === "top" || side === "bottom") {
    left = Math.max(8, Math.min(cx - w / 2, innerWidth - w - 8));
    top = side === "top" ? r.top - h - a - GAP : r.bottom + a + GAP;
    arrow.style.left = Math.max(10, Math.min(cx - left - arrow.offsetWidth / 2, w - arrow.offsetWidth - 10)) + "px"; arrow.style.top = "";
  } else {
    top = Math.max(8, Math.min(cy - h / 2, innerHeight - h - 8));
    left = side === "left" ? r.left - w - a - GAP : r.right + a + GAP;
    arrow.style.top = Math.max(8, Math.min(cy - top - arrow.offsetHeight / 2, h - arrow.offsetHeight - 8)) + "px"; arrow.style.left = "";
  }
  box.style.left = left + "px"; box.style.top = top + "px";
}

export function hideTip(): void { clearTimeout(tipTimer); tipEl?.remove(); tipEl = null; tipFor = null; }

export function showTip(t: HTMLElement): void {
  if (opts.enabled && !opts.enabled()) return;
  if (t === tipFor) return;
  hideTip(); tipFor = t;
  tipTimer = window.setTimeout(() => {
    if (!t.isConnected || !t.dataset.tip || tipFor !== t) return;
    tipEl = el("div", "tooltip show tip"); tipEl.setAttribute("role", "tooltip");
    const arrow = el("div", "tooltip-arrow"); arrow.style.position = "absolute";
    tipEl.append(arrow, el("div", "tooltip-inner text-start", t.dataset.tip)); document.body.append(tipEl);
    place(tipEl, t, "tooltip");
  }, opts.delay?.() ?? 400);
}

/** A popover beside its button; a click elsewhere, Escape or a resize closes it. */
export function showPop(target: HTMLElement, title: string, body: string): void {
  popEl?.remove(); hideTip();
  popEl = el("div", "popover show pop"); popEl.setAttribute("role", "dialog");
  const arrow = el("div", "popover-arrow"); arrow.style.position = "absolute";
  popEl.append(arrow, el("h3", "popover-header", title), el("div", "popover-body", body)); document.body.append(popEl);
  place(popEl, target, "popover");
}

/** Gives every button with a tooltip but no text of its own (an icon) that tooltip as its accessible name, unless it
 *  has a name already. */
export function nameIconButtons(root: ParentNode): void {
  const named = (b: HTMLElement) => b.hasAttribute("aria-label") && b.dataset.tipName !== "1";
  const scope = root instanceof Element && root.matches("button[data-tip]") ? [root as HTMLElement] : [];
  for (const b of [...scope, ...Array.from(root.querySelectorAll<HTMLElement>("button[data-tip]"))]) {
    if (named(b) || (b.textContent ?? "").trim()) continue;
    b.setAttribute("aria-label", b.dataset.tip ?? ""); b.dataset.tipName = "1"; // kept in step when the tip changes
  }
}

/** Plain title attributes on buttons become proper tooltips. */
export function tipsFromTitles(root: ParentNode = document): void {
  for (const b of Array.from(root.querySelectorAll<HTMLElement>(".btn[title], a[title].dropdown-item"))) { b.dataset.tip = b.title; b.removeAttribute("title"); }
}

/** The page-wide rules, once. A tooltip lives exactly as long as the pointer is over its element (or keyboard focus is
 *  on it); any press, scroll, key or blur hides it. */
export function installTips(o: TipOptions = {}): void {
  opts = o;
  // An icon-only button says nothing to a screen reader; its tooltip is what it does, so that becomes its name too.
  nameIconButtons(document);
  new MutationObserver(ms => {
    for (const m of ms) { // only what changed: the nodes added, or the button whose tip changed
      if (m.type === "attributes" && m.target instanceof Element) nameIconButtons(m.target);
      else for (const n of Array.from(m.addedNodes)) if (n instanceof Element) nameIconButtons(n);
    }
  })
    .observe(document.body, { subtree: true, childList: true, attributes: true, attributeFilter: ["data-tip"] });
  document.addEventListener("pointermove", e => {
    usingKeyboard = false;
    const t = (e.target as Element | null)?.closest?.<HTMLElement>("[data-tip]");
    if (t) showTip(t); else if (tipFor) hideTip();
  }, { passive: true });
  document.addEventListener("pointerleave", hideTip);
  document.addEventListener("keydown", () => { usingKeyboard = true; hideTip(); }, true);
  document.addEventListener("focusin", e => {
    const t = (e.target as Element).closest<HTMLElement>("[data-tip]");
    if (t && usingKeyboard && t.matches(":focus-visible")) showTip(t);
  });
  document.addEventListener("focusout", hideTip);
  document.addEventListener("pointerdown", hideTip, true);
  addEventListener("scroll", hideTip, true);
  addEventListener("blur", hideTip);
  document.addEventListener("mousedown", e => { const t = e.target as Element; if (popEl && !popEl.contains(t) && !t.closest("[data-pop]")) { popEl.remove(); popEl = null; } });
  document.addEventListener("keydown", e => { if (e.key === "Escape" && popEl) { popEl.remove(); popEl = null; } }, true);
  addEventListener("resize", () => { hideTip(); popEl?.remove(); popEl = null; });
}
