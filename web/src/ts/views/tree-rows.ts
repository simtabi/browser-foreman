// The bookmark tree's rows as elements: a folder row (twisty, icon, name, badges, description, count) and a bookmark
// row (snapshot with the site icon in its corner; title with status and date; site and path; description), plus the
// "Bookmarks in <folder>" label. Building only: the page wires selection, clicks, menus and drag onto what this
// returns. Moved from the page script on 2026-11-12; same markup.

import type { TreeRow } from "../tree-rules";
import type { BookmarkId, Folder } from "../tree";
import { isFolder, nice } from "../tree";
import { badge, el, fmtDate, fmtDay, icon, plural } from "../ui";
import { starsText } from "../labels";

/** A bookmark as the page's data carries it (only the fields rows show). */
export interface Bookmark { id: BookmarkId; u: string; s: string; dk?: string; a?: number; m?: number; nu?: string; applied?: boolean; p?: number }
export type BookmarkRow = TreeRow & { item?: Bookmark };

export interface RowDeps {
  readOnly: () => boolean;
  titleOf: (id: BookmarkId) => string;
  descOf: (id: BookmarkId) => string;
  /** Your stars (0: none) and tags, shown small on the row. */
  ratingOf?: (id: BookmarkId) => number;
  tagsOf?: (id: BookmarkId) => string[];
  host: (url: string) => string;
  shortUrl: (url: string) => string;
  thumb: (i: Bookmark, cls: string) => HTMLElement;
  favicon: (i: Bookmark) => HTMLElement;
  item: (id: BookmarkId) => Bookmark | undefined;
  isNewFolder: (f: Folder) => boolean;      // a folder the cleanup added
  filtering: () => boolean;                 // filters or a search are on (counts then mean "that match")
  label: Record<string, string>;            // status → its words (LABEL)
  help: Record<string, string>;             // status or kind → its explanation (HELP)
  statusIcon: Record<string, string>;
}

/** A moved link whose new address has not been applied yet. */
export const pendingUpdate = (i: Bookmark): boolean => !!i.nu && !i.applied && i.nu !== i.u;

export interface RowState { selected: boolean; marked: boolean; draggable: boolean }

/** The row element and, for folders, its twisty (the page makes it open and close the folder). */
export function rowElement(r: BookmarkRow, idx: number, st: RowState, d: RowDeps): { row: HTMLElement; twisty: HTMLElement | null } {
  const row = el("div", "bm-row " + r.kind + (r.trash ? " trash" : "") + (st.selected ? " sel" : "") + (st.marked ? " marked" : ""));
  row.setAttribute("role", "treeitem"); row.setAttribute("aria-level", String(r.depth + 1));
  row.setAttribute("aria-selected", String(st.selected || st.marked));
  // Indent by margin, not padding, so hover and selection start at the row's own level instead of the pane edge.
  row.style.marginLeft = (6 + r.depth * 18) + "px";
  row.setAttribute("data-brofo-row", String(idx));
  row.draggable = st.draggable;
  const body = el("div", "bm-body");
  if (r.kind === "folder" && isFolder(r.node)) {
    const f = r.node, count = r.count ?? 0;
    row.setAttribute("aria-expanded", String(!!r.open));
    const twisty = icon(r.open ? "ti-chevron-down" : "ti-chevron-right", "tw");
    const line = el("div", "bm-line");
    line.append(el("span", "bm-title", nice(f.n)));
    if (!d.readOnly() && f.c.some(x => !isFolder(x) && d.item(x)?.p)) { const b = badge("untouched", "bg-green-lt", "ti-lock"); b.dataset.tip = d.help.prot ?? ""; line.append(b); }
    if (!d.readOnly() && !f.virtual && d.isNewFolder(f)) { const b = badge("new", "bg-blue-lt"); b.dataset.tip = "A folder the cleanup added to your tree."; line.append(b); }
    body.append(line);
    if (f.d) body.append(el("div", "bm-desc", f.d));
    const cnt = el("span", "bm-count", count.toLocaleString());
    cnt.dataset.tip = `${plural(count, "bookmark")} in this folder` + (d.filtering() ? " that match the filters" : "");
    row.append(twisty, icon(r.trash ? "ti-trash" : r.open ? "ti-folder-open" : count ? "ti-folder" : "ti-folder-off", "fi"), body, cnt);
    return { row, twisty };
  }
  const id = r.node as BookmarkId, i = r.item ?? d.item(id);
  if (!i) return { row, twisty: null };
  const t = d.titleOf(id) || i.u;
  const top = el("div", "bm-top"), title = el("div", "bm-title", t); title.dataset.tip = t; top.append(title);
  const aside = el("div", "bm-aside");
  if (i.s !== "ok" && d.label[i.s]) {
    const b = badge(i.s === "dead" ? (i.dk === "host" ? "site gone" : "page gone") : d.label[i.s]!.split(" —")[0]!, "bg-transparent st-" + i.s, d.statusIcon[i.s]);
    b.dataset.tip = d.help[i.s] ?? ""; aside.append(b);
  }
  if (pendingUpdate(i)) aside.append(badge("update ready", "bg-yellow-lt", "ti-arrow-forward-up"));
  if (i.m) aside.append(badge("matched", "bg-green-lt", "ti-arrow-merge"));
  const stars = d.ratingOf?.(id) ?? 0;
  if (stars) { const r = el("span", "stars-inline", starsText(stars)); r.dataset.tip = `Rated ${stars} of 5`; r.setAttribute("aria-label", `Rated ${stars} of 5`); aside.append(r); }
  if (i.a) { const dt = el("span", "bm-date", fmtDay(i.a)); dt.dataset.tip = "Added " + fmtDate(i.a); aside.append(dt); }
  if (aside.childNodes.length) top.append(aside);
  const h = d.host(i.u), path = d.shortUrl(i.u).replace(/^(?:www\.)?[^/]+/, "");
  const sub = el("div", "bm-sub"); sub.dataset.tip = i.u;
  sub.append(el("span", "bm-host", h || i.u), path && path !== "/" ? el("span", "bm-path", path) : "");
  body.append(top, sub);
  const ds = d.descOf(id); if (ds) body.append(el("div", "bm-desc", ds));
  const tags = d.tagsOf?.(id) ?? [];
  if (tags.length) { const tg = el("div", "bm-tags"); for (const t of tags.slice(0, 4)) tg.append(el("span", "badge bg-azure-lt", t)); if (tags.length > 4) tg.append(el("span", "text-secondary small", `+${tags.length - 4}`)); body.append(tg); }
  const vis = el("span", "bm-vis"), fav = el("span", "bm-fav"); fav.append(d.favicon(i)); vis.append(d.thumb(i, "th"), fav);
  row.append(vis, body);
  return { row, twisty: null };
}

/** The label before a folder's loose bookmarks: "Bookmarks in Development · 12". */
export function looseLabel(depth: number, parentName: string, count: number): HTMLElement {
  const sep = el("div", "bm-sep"); sep.setAttribute("role", "presentation"); sep.style.marginLeft = (6 + depth * 18) + "px";
  sep.append(icon("ti-bookmarks", "me-1"), el("span", null, `Bookmarks in ${parentName}`), el("span", "bm-sep-n", count.toLocaleString()));
  return sep;
}
