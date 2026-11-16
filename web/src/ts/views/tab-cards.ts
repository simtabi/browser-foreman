// Open tabs: a tab's row and a window's card, as elements. The page keeps the state (picked tabs, folded windows,
// pages) and the menus and actions; these build the markup and call back for clicks. Moved from the page script on
// 2026-11-12; same markup.

import type { Tab, TabItem, WinRecord } from "../tabs";
import { badge, el, icon } from "../ui";

export interface TabCardDeps { host: (u: string) => string; hue: (s: string) => number; shortUrl: (u: string, max?: number) => string }
export interface TabNote { note: string; tags: string[] }

/** A letter tile in the site's colour, for tabs (their favicons are not kept). */
export function siteLetter(site: string, d: TabCardDeps): HTMLElement {
  const fv = el("span", "fvd", (site[0] || "?").toUpperCase()); fv.style.background = `hsl(${d.hue(site)} 45% 45%)`; return fv;
}

/** One tab: checkbox, site letter, title and address, note and tags, "active", an extra label, and an open button. */
export function tabRowElement(i: TabItem, o: { live: boolean; picked: boolean; checkbox: HTMLElement; note?: TabNote; extra?: string },
  open: () => void, d: TabCardDeps): HTMLElement {
  const { t, x, pos } = i;
  const r = el("div", "list-group-item tab-row" + (!o.live && (x.missing ?? []).includes(t) ? " missing" : "") + (o.picked ? " picked" : "") + (x.inc ? " inc" : ""));
  const hst = d.host(t.url) || t.url.split(":")[0] || "";
  const tx = el("div", "min-w-0 flex-fill"), url = el("div", "tab-url", d.shortUrl(t.url, 80)); url.dataset.tip = t.url;
  const title = el("div", "tab-title", t.title || t.url); title.title = t.title || t.url;  // the whole title on hover
  tx.append(title, url);
  r.append(o.checkbox, siteLetter(hst, d), tx);
  if (o.note) {
    const tg = el("span", "tab-tags");
    for (const g of o.note.tags) tg.append(el("span", "badge bg-azure-lt", g));
    if (o.note.note) { const ni = icon("ti-notes", "text-secondary"); ni.dataset.tip = o.note.note; tg.append(ni); }
    r.append(tg);
  }
  if (x.w.active === pos + 1) r.append(badge("active", "bg-primary-lt")); // the current item: primary, as "open" in Workspaces
  if (o.extra) r.append(el("span", "badge bg-secondary-lt", o.extra));
  if (/^https?:/i.test(t.url)) {
    const b = el("button", "btn btn-sm btn-ghost-secondary btn-icon"); b.append(icon("ti-external-link"));
    b.dataset.tip = x.inc ? "Open in an incognito window" : "Open";
    b.onclick = e => { e.stopPropagation(); open(); };
    r.append(b);
  }
  return r;
}

export interface WindowCardCallbacks {
  fold: () => void;
  reopenMissing: () => void;
  openAll: (button: HTMLElement) => void;
  menu: (e: MouseEvent, more: HTMLElement) => void;   // the ⋯ button and a right-click on the header
}

/** A window's card with its header (fold, checkbox, icon, "Browser · window N", tab count, incognito and "not open
 *  now" marks; Reopen missing, Open all, ⋯). The page appends the body: the tab list and its pager, or a note. */
export function windowCard(x: WinRecord, o: { folded: boolean; checkbox: HTMLElement; count: number; missing: Tab[] }, cb: WindowCardCallbacks): HTMLElement {
  const card = el("div", "card tab-win" + (x.inc ? " incognito" : "") + (o.folded ? " folded" : "")), h = el("div", "card-header tab-win-head");
  const fold = el("button", "btn btn-sm btn-ghost-secondary btn-icon tab-fold"); fold.append(icon(o.folded ? "ti-chevron-right" : "ti-chevron-down"));
  fold.setAttribute("aria-expanded", String(!o.folded)); fold.dataset.tip = o.folded ? "Unfold" : "Fold"; fold.onclick = cb.fold;
  const ttl = el("div", "min-w-0 flex-fill"); ttl.append(el("div", "tab-win-title", `${x.browser} · window ${x.n}`));
  const meta = el("div", "tab-win-meta"); meta.append(`${o.count} tabs`);
  if (x.inc) { meta.append(" · "); const p = el("span", "tabs-inc-pill"); p.append(icon("ti-spy"), "Incognito"); meta.append(p); }
  if (o.missing.length) meta.append(" · ", badge(`${o.missing.length} not open now`, "bg-yellow-lt", "ti-alert-triangle"));
  ttl.append(meta);
  // The browser's own icon; an incognito window keeps the spy mark, with the browser's icon as a small badge.
  const ico = el("span", "tab-win-icon" + (x.inc ? " has-badge" : "")); ico.append(icon(x.inc ? "ti-spy" : "browser:" + x.browser));
  if (x.inc) ico.append(icon("browser:" + x.browser, "tab-win-badge"));
  h.append(fold, o.checkbox, ico, ttl);
  const wa = el("div", "btn-list flex-nowrap tab-win-acts");
  if (o.missing.length) { const b = el("button", "btn btn-sm btn-warning"); b.append(icon("ti-restore", "me-1"), `Reopen ${o.missing.length} missing`); b.onclick = cb.reopenMissing; wa.append(b); }
  const more = el("button", "btn btn-sm btn-ghost-secondary btn-icon"); more.append(icon("ti-dots")); more.dataset.tip = "More"; more.onclick = e => cb.menu(e, more);
  if (!x.w.redacted) { const ob = el("button", "btn btn-sm"); ob.append(icon("ti-external-link", "me-1"), "Open all", icon("ti-chevron-down", "ms-1")); ob.onclick = () => cb.openAll(ob); wa.append(ob); }
  wa.append(more); h.append(wa); h.oncontextmenu = e => cb.menu(e, more);
  card.append(h);
  return card;
}

/** The body of a window whose incognito pages were not kept. */
export function redactedNote(count: number): HTMLElement {
  const n = el("div", "card-body text-secondary small"); n.append(icon("ti-eye-off", "me-1"), `${count} incognito tabs; only the count is kept.`); return n;
}
