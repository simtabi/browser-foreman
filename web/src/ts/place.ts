// Where the user is in the app: the view, the filter, the search, the selected folder or bookmark, the open-tabs
// snapshot. Kept in the address (#/tabs?snapshot=…), so refresh, back and forward all land in the same place, and
// in this browser's storage, so opening the bare address later resumes there too. Switched off, it remembers
// nothing and forgets what it had; a factory reset clears it with every other "bm-preview-v2:" key.
//
//   const place = new PlaceMemory({ key: "bm-preview-v2:place" }).enabledWhen(() => settings.rememberPlace);
//   place.update({ view: "tabs" }, { push: true }).update({ snapshot: "2026-11-12_143000" });
//   const p = place.read();               // the address wins, then storage, then the defaults
//   place.onNavigate(p => restore(p));    // back and forward
//   place.forget();                       // "start fresh"

export type View = "bookmarks" | "tabs" | "timeline";
const VIEWS: readonly View[] = ["bookmarks", "tabs", "timeline"];

export interface Place {
  view: View;
  filter?: string;    // bookmark filter chip (all, dead, moved, …)
  q?: string;         // search text
  node?: string;      // "f:<folder path>" or "b:<bookmark id>"
  snapshot?: string;  // open-tabs snapshot id, or "live"
}

export interface PlaceOptions {
  key: string;
  storage?: Storage | null;  // null: address only (tests, private windows)
  defaults?: Place;
}

const FIELDS = ["filter", "q", "node", "snapshot"] as const;
const LIMIT = 500;  // no field is longer than this, so a crafted address cannot bloat storage

const clean = (raw: Partial<Record<keyof Place, unknown>>, base: Place): Place => {
  const out: Place = { view: VIEWS.includes(raw.view as View) ? (raw.view as View) : base.view };
  for (const f of FIELDS) {
    const v = raw[f];
    if (typeof v === "string" && v !== "" && v.length <= LIMIT) out[f] = v;
  }
  return out;
};

export class PlaceMemory {
  private readonly key: string;
  private readonly storage: Storage | null;
  private readonly defaults: Place;
  private enabled: () => boolean = () => true;
  private current: Place;

  constructor(opts: PlaceOptions) {
    this.key = opts.key;
    this.storage = opts.storage === undefined ? PlaceMemory.localStorage() : opts.storage;
    this.defaults = opts.defaults ?? { view: "bookmarks" };
    this.current = { ...this.defaults };
  }

  /** Remember only while this returns true (the "Remember where I was" setting). */
  enabledWhen(fn: () => boolean): this {
    this.enabled = fn;
    return this;
  }

  /** The address wins (a link or a refresh), then what was stored, then the defaults. */
  read(): Place {
    if (!this.enabled()) return (this.current = { ...this.defaults });
    const fromHash = PlaceMemory.parse(location.hash);
    const raw = fromHash ?? this.load() ?? this.defaults;
    return (this.current = clean(raw, this.defaults));
  }

  get place(): Place {
    return { ...this.current };
  }

  /** Merge a change in. `push` makes it a step back and forward go through (a view change); the rest replace. */
  update(patch: Partial<Place>, { push = false }: { push?: boolean } = {}): this {
    const next = clean({ ...this.current, ...patch }, this.defaults);
    for (const f of FIELDS) if (f in patch && !patch[f]) delete next[f];  // an empty value clears the field
    if (next.view !== this.current.view && !("node" in patch)) delete next.node;  // a selection belongs to its view
    const same = JSON.stringify(next) === JSON.stringify(this.current);
    this.current = next;  // tracked even while switched off, so switching back on resumes from here
    return same ? this : this.write(push);
  }

  /** Write the current place to the address and storage now (after a restore, or when switched back on). */
  sync(): this {
    return this.write(false);
  }

  /** Clear the address and storage, keeping the place in memory (the setting switched off). */
  clear(): this {
    try { this.storage?.removeItem(this.key); } catch { /* storage may be blocked */ }
    try { history.replaceState(history.state, "", location.pathname + location.search); } catch { /* as below */ }
    return this;
  }

  private write(push: boolean): this {
    if (!this.enabled()) return this;
    const url = location.pathname + location.search + PlaceMemory.format(this.current);
    try {
      if (push) history.pushState(null, "", url);
      else history.replaceState(history.state, "", url);
    } catch { /* a sandboxed frame may refuse; storage still works */ }
    this.save(this.current);
    return this;
  }

  /** Back and forward. The handler gets the place the address now names. */
  onNavigate(fn: (p: Place) => void): this {
    addEventListener("popstate", () => { if (this.enabled()) fn(this.read()); });
    return this;
  }

  /** Forget everything and clear the address; the next load opens at the defaults. */
  forget(): this {
    this.current = { ...this.defaults };
    return this.clear();
  }

  static format(p: Place): string {
    const q = new URLSearchParams();
    for (const f of FIELDS) if (p[f]) q.set(f, p[f] as string);
    const s = q.toString();
    return `#/${p.view}${s ? "?" + s : ""}`;
  }

  static parse(hash: string): Partial<Place> | null {
    const m = /^#\/([a-z]+)(?:\?(.*))?$/.exec(hash);
    if (!m) return null;
    const out: Partial<Record<keyof Place, unknown>> = { view: m[1] };
    const q = new URLSearchParams(m[2] ?? "");
    for (const f of FIELDS) { const v = q.get(f); if (v !== null) out[f] = v; }
    return out as Partial<Place>;
  }

  private load(): Partial<Place> | null {
    try {
      const v = JSON.parse(this.storage?.getItem(this.key) ?? "null");
      return v && typeof v === "object" ? (v as Partial<Place>) : null;
    } catch {
      return null;
    }
  }

  private save(p: Place): void {
    try { this.storage?.setItem(this.key, JSON.stringify(p)); } catch { /* full or blocked: the address still has it */ }
  }

  private static localStorage(): Storage | null {
    try { return window.localStorage; } catch { return null; }
  }
}
