// Which browsers the page shows: all of them, some, or none. Kept in this browser and announced on change, so every
// view re-reads with it (Open tabs, its snapshot list and counts, the Timeline and its chart, Clean up).
//
//   const scope = new BrowserScope("bm-preview-v2:scope").known(["Google Chrome", "Safari"]).onChange(() => repaint());
//   scope.toggle("Safari", false);   scope.setAll(true);   scope.setAll(false);   // none
//   scope.has("Google Chrome");      scope.query();         // "browsers=Google%20Chrome" ("" for all)

export class BrowserScope {
  private picked: Set<string> | null = null;  // null: all browsers, including ones installed later
  private names: string[] = [];
  private readonly listeners: (() => void)[] = [];

  constructor(private readonly key: string) {
    try {
      const v = JSON.parse(localStorage.getItem(key) ?? "null");
      // Saved before none was possible, an empty list meant "all".
      this.picked = v && typeof v === "object" && !Array.isArray(v) && Array.isArray(v.only) ? new Set<string>(v.only)
        : Array.isArray(v) && v.length ? new Set<string>(v) : null;
    } catch { this.picked = null; }
  }

  /** The installed browsers. Names no longer installed drop out of a saved choice. */
  known(names: string[]): this {
    this.names = [...names];
    if (this.picked) this.picked = new Set([...this.picked].filter(n => this.names.includes(n)));
    return this;
  }
  get all(): string[] { return [...this.names]; }
  get isAll(): boolean { return this.picked === null || (this.names.length > 0 && this.names.every(n => this.picked!.has(n))); }
  get isNone(): boolean { return this.picked !== null && this.picked.size === 0; }
  get shown(): string[] { return this.names.filter(n => this.has(n)); }

  has(name: string): boolean { return this.picked === null || this.picked.has(name); }

  toggle(name: string, on: boolean): this {
    const next = new Set(this.picked ?? this.names);
    if (on) next.add(name); else next.delete(name);
    return this.save(this.names.length && this.names.every(n => next.has(n)) ? null : next);
  }
  setAll(on: boolean): this { return this.save(on ? null : new Set()); }

  /** For API reads: "" for all, "browsers=" for none, else the names. */
  query(): string {
    return this.picked === null || this.isAll ? "" : "browsers=" + encodeURIComponent(this.shown.join(","));
  }

  label(): string {
    if (!this.names.length) return "Browsers";
    if (this.isAll) return this.names.length === 1 ? this.names[0]! : "All browsers";
    const s = this.shown;
    return !s.length ? "No browsers" : s.length === 1 ? s[0]! : `${s.length} of ${this.names.length} browsers`;
  }

  onChange(fn: () => void): this { this.listeners.push(fn); return this; }

  private save(next: Set<string> | null): this {
    this.picked = next;
    try { localStorage.setItem(this.key, JSON.stringify(next === null ? null : { only: [...next] })); } catch { /* blocked storage: this visit only */ }
    for (const fn of this.listeners) fn();
    return this;
  }
}
