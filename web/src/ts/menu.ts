// Menus: one item format for every menu in the app, built into Tabler dropdowns, placed where there is room with a
// caret pointing at the button, cascading submenus, and one rule for closing. Moved from the page script into a typed
// module on 2026-11-12; behaviour is unchanged.
//
//   const items: MenuItem[] = [{ h: "Open" }, { l: "In a new window", ic: "ti-app-window", k: "⇧O", fn: open }, "-", null];
//   wrap.append(button, buildMenu(items));        // button carries data-dd: a click opens and places it
//   placeDropdown(menu, button);                  // or place one yourself

import { Dom } from "./dom";
import { el, icon, kbd } from "./ui";

export type MenuItem =
  | { h: string }                                                                              // a section header
  | { l: string; ic?: string; k?: string; danger?: boolean; fn: () => unknown }                // an action
  | { l: string; ic?: string; sub: MenuItem[] }                                                // a submenu
  | "-" | null | undefined | false;                                                            // divider; skipped

const isHeader = (it: MenuItem): it is { h: string } => !!it && it !== "-" && "h" in it;

/** Build a menu. Optional entries may be null; a divider only sits between two real entries, never doubled or at an
 *  end, and never straight after a header. */
export function buildMenu(items: MenuItem[], sub = false): HTMLElement {
  const m = el("div", "dropdown-menu" + (sub ? " dd-sub" : " dropdown-menu-end")); m.setAttribute("role", "menu");
  const list = items.filter(Boolean).filter((it, k, a) => it !== "-" || (k > 0 && k < a.length - 1 && a[k - 1] !== "-" && !isHeader(a[k - 1]!)));
  for (const it of list) {
    if (!it) continue;
    if (it === "-") { m.append(el("div", "dropdown-divider")); continue; }
    if (isHeader(it)) { m.append(el("h6", "dropdown-header", it.h)); continue; }
    const hasSub = "sub" in it;
    const a = el("a", "dropdown-item" + (!hasSub && it.danger ? " text-danger" : "") + (hasSub ? " dd-has-sub" : "")); a.href = "#"; a.setAttribute("role", "menuitem");
    a.append(icon(it.ic || "ti-point", "me-2"), el("span", "flex-fill", it.l));
    if (!hasSub && it.k) a.append(kbd(it.k));
    if (hasSub) {
      a.append(icon("ti-chevron-right", "ms-2 text-secondary"));
      const wrap = el("div", "dd-subwrap"), sm = buildMenu(it.sub, true);
      wrap.append(a, sm);
      const open = () => {
        for (const o of Array.from(wrap.parentElement?.querySelectorAll(":scope > .dd-subwrap > .dd-sub.show") ?? [])) if (o !== sm) o.classList.remove("show");
        sm.classList.add("show"); placeSub(sm, a);
      };
      a.onmouseenter = open; a.onclick = e => { e.preventDefault(); e.stopPropagation(); open(); };
      a.onkeydown = e => { if (e.key === "ArrowRight") { e.preventDefault(); open(); sm.querySelector<HTMLElement>(".dropdown-item")?.focus(); } };
      sm.onkeydown = e => { if (e.key === "ArrowLeft") { e.preventDefault(); e.stopPropagation(); sm.classList.remove("show"); a.focus(); } };
      m.append(wrap); continue;
    }
    a.onmouseenter = () => { for (const o of Array.from(m.querySelectorAll(":scope > .dd-subwrap > .dd-sub.show"))) o.classList.remove("show"); };
    a.onclick = e => { e.preventDefault(); closeDropdowns(); it.fn(); };
    m.append(a);
  }
  return m;
}

/** A submenu beside its item: keep cascading the way the parent went, so levels never fold back over each other;
 *  turn only at the screen edge. */
export function placeSub(sm: HTMLElement, a: HTMLElement): void {
  sm.style.position = "fixed"; sm.style.maxHeight = "";
  const r = a.getBoundingClientRect(), w = sm.offsetWidth;
  const h = Math.min(sm.offsetHeight, innerHeight - 16); if (sm.offsetHeight > h) { sm.style.maxHeight = h + "px"; sm.style.overflowY = "auto"; }
  const parent = a.closest<HTMLElement>(".dropdown-menu"); if (!parent) return;
  const pr = parent.getBoundingClientRect();
  const wantLeft = parent.dataset.dir === "left" || (!parent.classList.contains("dd-sub") && pr.right + w + 4 > innerWidth - 8);
  const fitsLeft = pr.left - w - 2 >= 8, fitsRight = pr.right + w + 2 <= innerWidth - 8;
  const left = wantLeft ? fitsLeft || !fitsRight : !fitsRight && fitsLeft;
  sm.dataset.dir = left ? "left" : "right";
  const x = left ? Math.max(8, pr.left - w - 2) : Math.min(pr.right + 2, innerWidth - w - 8);
  const y = Math.max(8, Math.min(r.top - 6, innerHeight - h - 8));
  sm.style.left = x + "px"; sm.style.top = y + "px";
}

/** Drop-down menus open where there is room: below the button unless the space is above, aligned to whichever edge
 *  of the button keeps the menu on screen, with a caret pointing at the button. Fixed positioning, so no card or
 *  scroll pane clips it. */
export function placeDropdown(m: HTMLElement, btn: Element): void {
  m.classList.add("dd-placed"); m.style.position = "fixed"; m.style.inset = "auto"; m.style.transform = "none"; m.style.margin = "0";
  m.style.maxHeight = ""; m.style.left = "0px"; m.style.top = "0px";
  const b = btn.getBoundingClientRect(), gap = 8, pad = 8;
  const w = m.offsetWidth, below = innerHeight - b.bottom - gap - pad, above = b.top - gap - pad;
  const up = m.offsetHeight > below && above > below;
  const room = up ? above : below;
  if (m.offsetHeight > room) m.style.maxHeight = Math.max(160, room) + "px";
  const h = Math.min(m.offsetHeight, Math.max(160, room));
  let x = b.left; if (x + w > innerWidth - pad) x = b.right - w; x = Math.max(pad, Math.min(x, innerWidth - w - pad));
  const y = up ? b.top - gap - h : b.bottom + gap;
  m.style.left = x + "px"; m.style.top = y + "px";
  m.dataset.side = up ? "top" : "bottom";
  m.style.setProperty("--caret-x", Math.max(14, Math.min(w - 14, b.left + b.width / 2 - x)) + "px");
}

/** Close every open menu. Page menus lose "show"; menus built on demand (.pop-menu) are removed. A menu opened by the
 *  click now being handled (data-brofo-fresh) stays: closing it there is what made every pop-up menu look dead. The
 *  right-click menu (#ctx) closes itself. */
export function closeDropdowns(): void {
  openAt = null;
  for (const b of Array.from(document.querySelectorAll("[data-dd][aria-expanded=true]"))) b.setAttribute("aria-expanded", "false");
  for (const m of Array.from(document.querySelectorAll<HTMLElement>(".dropdown-menu.show"))) {
    if (m.id === "ctx" || Dom.read(m, "fresh")) continue;
    if (m.classList.contains("pop-menu")) m.remove(); else m.classList.remove("show");
  }
}

/** Where an open menu's button is, as a path a rebuilt copy of it shares: the nearest ancestor with an id or a
 *  data-brofo-* name, then which [data-dd] button inside it. A view that repaints (a poll, a count, a list arriving)
 *  replaces the button and its menu; the path finds the new ones, so the menu stays open instead of vanishing. */
function anchorOf(btn: Element): { host: string; index: number } | null {
  for (let h: Element | null = btn.parentElement; h && h !== document.body; h = h.parentElement) {
    const key = h.id ? `#${CSS.escape(h.id)}` : Array.from(h.attributes).find(a => a.name.startsWith("data-brofo-"))?.name;
    if (key) {
      const sel = h.id ? key : `[${key}]`;
      return { host: sel, index: Array.from(h.querySelectorAll("[data-dd]")).indexOf(btn) };
    }
  }
  return null;
}
let openAt: { btn: Element; menu: HTMLElement; anchor: { host: string; index: number } | null } | null = null;

function openMenu(t: Element, m: HTMLElement): void {
  m.classList.add("show"); placeDropdown(m, t); t.setAttribute("aria-expanded", "true");
  openAt = { btn: t, menu: m, anchor: anchorOf(t) };
}

/** After a repaint: reopen the rebuilt menu where the open one was replaced, or forget it if its button is gone. */
function followRepaint(): void {
  if (!openAt || openAt.menu.isConnected) return;
  const a = openAt.anchor; openAt = null;
  const host = a && document.querySelector(a.host);
  const t = host && a!.index >= 0 ? host.querySelectorAll("[data-dd]")[a!.index] : null;
  const m = t?.parentElement?.querySelector<HTMLElement>(".dropdown-menu");
  if (t && m && !m.classList.contains("show")) openMenu(t, m);
}

/** Keep the open menu beside its button as the page scrolls or resizes; close it once the button is off screen. */
function follow(): void {
  if (!openAt) return;
  const { btn, menu } = openAt;
  if (!menu.isConnected || !menu.classList.contains("show")) { openAt = null; return; }
  const r = btn.getBoundingClientRect();
  if (!btn.isConnected || r.bottom < 0 || r.top > innerHeight || r.width === 0) { closeDropdowns(); return; }
  placeDropdown(menu, btn);
}

/** The page-wide rules, once: a [data-dd] button toggles the menu beside it; a click elsewhere or Escape closes open
 *  menus. Scrolling and resizing move an open menu with its button (closing it only once the button is off screen),
 *  and a repaint that rebuilds the button keeps its menu open. `beforeOpen` runs first (the page hides its tooltip). */
export function installMenus(beforeOpen: () => void = () => {}): void {
  addEventListener("resize", () => requestAnimationFrame(follow));
  addEventListener("scroll", e => { if (!(e.target as Element | null)?.closest?.(".dropdown-menu")) requestAnimationFrame(follow); }, true);
  new MutationObserver(followRepaint).observe(document.body, { childList: true, subtree: true });
  document.addEventListener("keydown", e => { if (e.key === "Escape" && openAt) { const b = openAt.btn; closeDropdowns(); (b as HTMLElement).focus?.(); } });
  document.addEventListener("click", e => {
    const target = e.target as Element;
    const t = target.closest("[data-dd]");
    if (t) {
      e.preventDefault(); beforeOpen();
      const m = t.parentElement?.querySelector<HTMLElement>(".dropdown-menu"); if (!m) return;
      const was = m.classList.contains("show"); closeDropdowns();
      if (!was) openMenu(t, m);
      return;
    }
    if (!target.closest(".dropdown-menu")) closeDropdowns();
  });
}

// ---- menus built on demand: beside a button, or at the pointer (moved from the Open tabs view on 2026-11-12) ----
let popEl: HTMLElement | null = null;
const fresh = (m: HTMLElement) => { Dom.write(m, "fresh", "1"); setTimeout(() => Dom.write(m, "fresh", null), 0); };

/** Open a menu beside a button (placed like a dropdown, the button's chevron flips), or at the pointer (x, y). One at
 *  a time. `beforeOpen` runs first (the page hides its tooltip). */
export function popMenu(items: MenuItem[], at: Element | number, y?: number, beforeOpen: () => void = () => {}): HTMLElement {
  popEl?.remove(); beforeOpen(); closeDropdowns();
  const m = buildMenu(items); m.classList.add("show", "pop-menu"); m.style.position = "fixed"; document.body.append(m); popEl = m;
  // The click that opened it is still bubbling to the page's close-every-dropdown handler: let that one pass.
  fresh(m);
  if (at instanceof Element) {
    placeDropdown(m, at); at.setAttribute("aria-expanded", "true");
    new MutationObserver((_, mo) => { if (!m.isConnected) { at.setAttribute("aria-expanded", "false"); mo.disconnect(); } }).observe(document.body, { childList: true });
    return m;
  }
  const x = at, py = y ?? 0;
  const w = m.offsetWidth, h = Math.min(m.offsetHeight, innerHeight - 16); if (m.offsetHeight > h) { m.style.maxHeight = h + "px"; m.style.overflowY = "auto"; }
  m.style.left = Math.max(8, x + w + 8 > innerWidth ? x - w : x) + "px"; m.style.top = Math.max(8, py + h + 8 > innerHeight ? py - h : py) + "px";
  return m;
}
export function closePopMenu(): void { popEl?.remove(); popEl = null; }

/** Once: a press outside, Escape or a scroll outside closes the open pop menu. */
export function installPopMenus(): void {
  document.addEventListener("mousedown", e => { if (popEl && !popEl.contains(e.target as Node)) closePopMenu(); }, true);
  document.addEventListener("keydown", e => { if (popEl && e.key === "Escape") { e.stopPropagation(); closePopMenu(); } }, true);
  addEventListener("scroll", e => { if (popEl && !popEl.contains(e.target as Node)) closePopMenu(); }, true);
}
