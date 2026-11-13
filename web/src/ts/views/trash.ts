// The Trash window: everything in the Trash in one list (searchable, a page at a time), with where each item came
// from, and Put back or Delete permanently for the ones ticked, or for all. The page owns the tree and does the
// moving; this lists, selects and asks. Added 2026-11-12.
//
//   const trash = new TrashDialog({ list, restore, purge, pageSize, setPageSize });   trash.open();

import { askConfirm, showModal } from "../dialogs";
import { Pager } from "../pager";
import { $, el, icon, plural } from "../ui";

/** Why a bookmark is in the Trash. */
export type ReasonKey = "duplicate" | "dead" | "redirect" | "unverified" | "login" | "search" | "local" | "not-web" | "session" | "yours";
export interface Reason { key: ReasonKey; label: string; cls: string }
export const REASONS: Record<ReasonKey, Reason> = {
  duplicate: { key: "duplicate", label: "Duplicate", cls: "bg-azure-lt" },
  dead: { key: "dead", label: "Dead link", cls: "bg-red-lt" },
  redirect: { key: "redirect", label: "Redirects elsewhere", cls: "bg-orange-lt" },
  unverified: { key: "unverified", label: "Could not verify", cls: "bg-secondary-lt" },
  login: { key: "login", label: "Needs login", cls: "bg-secondary-lt" },
  search: { key: "search", label: "Google search", cls: "bg-purple-lt" },
  local: { key: "local", label: "Local or staging", cls: "bg-secondary-lt" },
  "not-web": { key: "not-web", label: "Not a web link", cls: "bg-secondary-lt" },
  session: { key: "session", label: "Session page", cls: "bg-secondary-lt" },
  yours: { key: "yours", label: "Trashed by you", cls: "bg-green-lt" },
};
/** What is known about one trashed bookmark. `copies`: how many bookmarks outside the Trash have the same address.
 *  `review`: the _Review subfolder it came from, if any. `status`: its link check. */
export interface ReasonFacts { copies: number; review?: string; status?: string }

/** Why it is in the Trash, most telling first: a copy of something still kept; the _Review group it was sorted
 *  into; its link check; otherwise you put it there. */
export function reasonOf(f: ReasonFacts): Reason {
  if (f.copies > 0) return REASONS.duplicate;
  const r = (f.review ?? "").toLowerCase();
  if (r.startsWith("dead")) return REASONS.dead;
  if (r.startsWith("google")) return REASONS.search;
  if (r.startsWith("local")) return REASONS.local;
  if (r.startsWith("not web")) return REASONS["not-web"];
  if (r.startsWith("session")) return REASONS.session;
  switch (f.status) {
    case "dead": return REASONS.dead;
    case "moved-elsewhere": return REASONS.redirect;
    case "unverified": return REASONS.unverified;
    case "needs-login": return REASONS.login;
    case "local": return REASONS.local;
    case "n/a": case "skipped": return REASONS["not-web"];
  }
  return REASONS.yours;
}

/** One thing directly inside the Trash: a bookmark, or a folder with what it holds. `reasons` counts why each
 *  bookmark in it is there (one entry for a bookmark). `copies`: for a duplicate, how many are still kept. */
export interface TrashEntry<N = unknown> { node: N; folder: boolean; title: string; url?: string; from: string; count: number;
  reasons: Partial<Record<ReasonKey, number>>; copies?: number;
  /** For a duplicate: the address it shares (normalised) and the copy still kept (title, where, its address). */
  dupKey?: string; kept?: { title: string; path: string; url: string } }

/** What the list shows, one unit per line group: a single entry, or a duplicate group (the kept copy as the parent,
 *  the trashed copies of that address under it). A group is one unit, so a page never splits it. */
export type TrashUnit<N> = { kind: "one"; entry: TrashEntry<N> } | { kind: "dup"; key: string; kept: NonNullable<TrashEntry["kept"]>; entries: TrashEntry<N>[] };
export function groupTrash<N>(list: TrashEntry<N>[]): TrashUnit<N>[] {
  const out: TrashUnit<N>[] = [], groups = new Map<string, Extract<TrashUnit<N>, { kind: "dup" }>>();
  for (const e of list) {
    if (!e.dupKey || !e.kept) { out.push({ kind: "one", entry: e }); continue; }
    let g = groups.get(e.dupKey);
    if (!g) { g = { kind: "dup", key: e.dupKey, kept: e.kept, entries: [] }; groups.set(e.dupKey, g); out.push(g); }
    g.entries.push(e);
  }
  return out;
}

/** How many bookmarks there are for each reason, across entries. */
export function reasonCounts(list: TrashEntry[]): [Reason, number][] {
  const m = new Map<ReasonKey, number>();
  for (const e of list) for (const [k, n] of Object.entries(e.reasons) as [ReasonKey, number][]) m.set(k, (m.get(k) ?? 0) + n);
  return (Object.keys(REASONS) as ReasonKey[]).filter(k => m.get(k)).map(k => [REASONS[k], m.get(k)!]);
}

/** The entries whose title, address or origin holds every word typed. */
export function filterTrash<N>(list: TrashEntry<N>[], query: string, reason?: ReasonKey | null): TrashEntry<N>[] {
  const words = query.trim().toLowerCase().split(/\s+/).filter(Boolean);
  return list.filter(e => (!reason || (e.reasons[reason] ?? 0) > 0) && words.every(w => `${e.title} ${e.url ?? ""} ${e.from}`.toLowerCase().includes(w)));
}
/** Bookmarks held by a set of entries (a folder counts what it holds). */
export const bookmarksIn = (list: TrashEntry[]): number => list.reduce((n, e) => n + e.count, 0);

export interface TrashDeps<N> {
  list: () => TrashEntry<N>[];
  restore: (nodes: N[]) => unknown;        // back where each came from
  purge: (nodes: N[]) => Promise<unknown>; // asks, then deletes permanently
  open: (url: string) => unknown;          // visit a link (it stays in the Trash)
  pageSize: () => number; setPageSize: (n: number) => void;
}

export class TrashDialog<N> {
  private picked = new Set<N>();
  private reason: ReasonKey | null = null;
  private page = 1;
  constructor(private readonly d: TrashDeps<N>) {
    const q = $<HTMLInputElement>("#trashQ"); if (q) q.oninput = () => { this.page = 1; this.paint(); };
    const all = $<HTMLInputElement>("#trashAll");
    if (all) all.onchange = () => { const shown = this.shown(); for (const e of shown) all.checked ? this.picked.add(e.node) : this.picked.delete(e.node); this.paint(); };
    const on = (id: string, fn: () => unknown) => { const b = $(id); if (b) b.onclick = () => fn(); };
    on("#trashRestore", () => this.act("restore", this.chosen()));
    on("#trashPurge", () => this.act("purge", this.chosen()));
    on("#trashRestoreAll", () => this.act("restore", this.d.list()));
    on("#trashEmpty", () => this.act("purge", this.d.list()));
  }

  open(): void { this.picked.clear(); this.page = 1; this.reason = null; const q = $<HTMLInputElement>("#trashQ"); if (q) q.value = ""; this.paint(); showModal("#trashModal"); }

  private shown(): TrashEntry<N>[] { return filterTrash(this.d.list(), $<HTMLInputElement>("#trashQ")?.value ?? "", this.reason); }
  private chosen(): TrashEntry<N>[] { return this.d.list().filter(e => this.picked.has(e.node)); }

  private async act(kind: "restore" | "purge", list: TrashEntry<N>[]): Promise<void> {
    if (!list.length) return;
    const n = bookmarksIn(list);
    if (kind === "restore") {
      if (list.length > 20 && !(await askConfirm({ title: `Put back ${plural(n, "bookmark")}?`, ok: "Put back", danger: false,
        body: "Each goes back to the folder it came from. ⌘Z undoes it." }))) return;
      this.d.restore(list.map(e => e.node));
    } else await this.d.purge(list.map(e => e.node));
    for (const e of list) this.picked.delete(e.node);
    this.paint();
  }

  paint(): void {
    const all = this.d.list(), shown = this.shown(), box = $("#trashList"); if (!box) return;
    for (const p of [...this.picked]) if (!all.some(e => e.node === p)) this.picked.delete(p);
    const chips = $("#trashWhy");
    if (chips) chips.replaceChildren(...reasonCounts(all).map(([rs, n]) => {
      const c = el("button", "btn trash-chip" + (this.reason === rs.key ? " active" : "")); c.type = "button";
      c.setAttribute("aria-pressed", String(this.reason === rs.key));
      c.append(el("span", "badge " + rs.cls + " me-1", n.toLocaleString()), rs.label);
      c.onclick = () => { this.reason = this.reason === rs.key ? null : rs.key; this.page = 1; this.paint(); };
      return c;
    }));
    const count = $("#trashCount"); if (count) count.textContent = all.length ? `${plural(bookmarksIn(all), "bookmark")} in ${plural(all.length, "item")}` : "The Trash is empty";
    const units = groupTrash(shown);
    // Copies of one page are grouped into one row, so the pager counts groups whenever there are any (the header
    // counts items): "1–25 of 426 groups" beside "580 bookmarks in 539 items" no longer reads as a contradiction.
    const pager = new Pager({ size: this.d.pageSize(), noun: units.length === shown.length ? "items" : "groups", onChange: p => { if (p.pageSize !== this.d.pageSize()) this.d.setPageSize(p.pageSize); this.page = p.page; this.paint(); } })
      .total(units.length).goto(this.page);
    const rows = pager.slice(units).flatMap(u => u.kind === "one" ? [this.row(u.entry)] : this.group(u));
    box.replaceChildren(...rows);
    if (!shown.length) box.replaceChildren(el("div", "text-secondary text-center py-4", all.length ? "Nothing in the Trash matches." : "The Trash is empty."));
    const foot = $("#trashPager"); if (foot) foot.replaceChildren(...(pager.needed ? [pager.render()] : []));
    const allBox = $<HTMLInputElement>("#trashAll");
    if (allBox) { const k = shown.filter(e => this.picked.has(e.node)).length; allBox.checked = k > 0 && k === shown.length; allBox.indeterminate = k > 0 && k < shown.length; }
    this.paintBar();
  }

  private visit(url: string, title: string): HTMLElement {
    // Visit before deciding: rows are labels, so the click must not also tick the box.
    const go = el("button", "btn btn-sm btn-ghost-secondary btn-icon"); go.type = "button"; go.append(icon("ti-external-link"));
    go.dataset.tip = "Visit in the browser"; go.setAttribute("aria-label", "Visit " + title);
    go.onclick = ev => { ev.preventDefault(); ev.stopPropagation(); this.d.open(url); };
    return go;
  }

  private row(e: TrashEntry<N>, child = false): HTMLElement {
    const r = el("label", "list-group-item d-flex align-items-center gap-2 trash-row" + (child ? " trash-child" : ""));
    const c = el("input", "form-check-input m-0"); c.type = "checkbox"; c.checked = this.picked.has(e.node);
    c.onchange = () => { c.checked ? this.picked.add(e.node) : this.picked.delete(e.node); this.paint(); };
    const t = el("div", "min-w-0 flex-fill");
    t.append(el("div", "text-truncate", e.title), el("div", "text-secondary small text-truncate", (e.url ? e.url + " · " : "") + "from " + (e.from || "the top level")));
    r.append(c, icon(e.folder ? "ti-folder" : "ti-bookmark", "text-secondary"), t);
    // Why it is here: one badge for a bookmark; for a folder, the reasons of what it holds, with counts.
    const why = el("span", "d-flex flex-wrap gap-1 justify-content-end trash-why");
    for (const [rs, n] of reasonCounts([e])) {
      const b = el("span", "badge " + rs.cls, e.folder ? `${rs.label} ${n.toLocaleString()}` : rs.label);
      if (rs.key === "duplicate" && e.copies) b.dataset.tip = `${plural(e.copies, "copy", "copies")} of this address still kept`;
      why.append(b);
    }
    // A folder's reasons go under its title, with its size first, so they read as one line instead of a column on the
    // right; a bookmark's single reason stays at the right edge.
    if (e.folder) { why.className = "d-flex flex-wrap gap-1 mt-1 trash-why-inline"; why.prepend(el("span", "badge bg-secondary-lt", plural(e.count, "bookmark"))); t.append(why); }
    else if (!child) r.append(why);
    else if (e.url && /^https?:\/\//i.test(e.url)) r.append(this.visit(e.url, e.title));
    return r;
  }

  /** A duplicate group: the kept copy (not in the Trash, so no box) with the trashed copies under it. */
  private group(g: Extract<TrashUnit<N>, { kind: "dup" }>): HTMLElement[] {
    const h = el("label", "list-group-item d-flex align-items-center gap-2 trash-row trash-group");
    const c = el("input", "form-check-input m-0"); c.type = "checkbox";
    const k = g.entries.filter(e => this.picked.has(e.node)).length;
    c.checked = k > 0 && k === g.entries.length; c.indeterminate = k > 0 && k < g.entries.length;
    c.setAttribute("aria-label", "Select the trashed copies of " + g.kept.title);
    c.onchange = () => { for (const e of g.entries) c.checked ? this.picked.add(e.node) : this.picked.delete(e.node); this.paint(); };
    const t = el("div", "min-w-0 flex-fill");
    t.append(el("div", "text-truncate", g.kept.title), el("div", "text-secondary small text-truncate", "kept in " + (g.kept.path || "the top level")));
    h.append(c, icon("ti-copy", "text-azure"), t, el("span", "badge " + REASONS.duplicate.cls, `Duplicate · ${g.entries.length} in Trash`));
    if (/^https?:\/\//i.test(g.kept.url)) h.append(this.visit(g.kept.url, g.kept.title));
    return [h, ...g.entries.map(e => this.row(e, true))];
  }

  private paintBar(): void {
    const n = this.chosen().length, all = this.d.list().length;
    for (const id of ["#trashRestore", "#trashPurge"]) { const b = $<HTMLButtonElement>(id); if (b) b.disabled = !n; }
    for (const id of ["#trashRestoreAll", "#trashEmpty"]) { const b = $<HTMLButtonElement>(id); if (b) b.disabled = !all; }
    const s = $("#trashSel"); if (s) s.textContent = n ? `${n} selected` : "";
  }
}
