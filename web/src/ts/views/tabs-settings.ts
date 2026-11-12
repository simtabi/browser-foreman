// Open tabs › Snapshots › settings: how tabs are saved, what incognito keeps, purging, and clearing history. Moved
// from the page script into a typed module on 2026-11-12; same markup and behaviour.
//
//   const panel = new TabsSettingsPanel(box, deps);   panel.paint(settings, status);

import { el, icon, plural } from "../ui";

/** The tab-saving settings the server keeps (tabs.settings()). */
export interface TabSettings {
  autosave: boolean; save_mode?: "change" | "interval"; interval: number; watch_seconds: number; keep_days: number;
  include_incognito: boolean; incognito_purge_hours: number; [key: string]: unknown;
}
export interface TabStatus { incognito_pages_stored?: number; snapshots?: number }

export interface TabsSettingsDeps {
  post: <T>(path: string, body: unknown) => Promise<T>;   // POST to api/tabs<path> with the run's token
  confirm: (o: { title: string; body: string; ok?: string; no?: string; danger?: boolean }) => Promise<boolean>;
  toast: (text: string, kind?: "info" | "success" | "warning" | "danger", title?: string) => void;
  saved: (cfg: TabSettings) => void;                      // after a setting is stored (the footer status repaints)
  purged: () => Promise<void>;                            // after a purge (the lists re-read)
  clearHistory: () => void;                               // opens the Clear tab history dialog
}

type Opt = [string | number, string];

export class TabsSettingsPanel {
  private cfg: TabSettings | null = null;
  private stat: TabStatus = {};

  constructor(private readonly box: HTMLElement, private readonly d: TabsSettingsDeps) {}

  paint(cfg: TabSettings | null, stat: TabStatus): this {
    this.cfg = cfg; this.stat = stat;
    const box = this.box; box.replaceChildren();
    if (!cfg) { box.append(el("div", "text-secondary small", "Tab tracking needs the local server (src/server/serve.py).")); return this; }
    box.append(el("div", "tabs-set-title", "Saving"),
      this.row(this.toggle("autosave", "Save automatically", "Snapshots open tabs in the background; an unchanged set is never stored again."), this.modeSelect()),
      cfg.save_mode === "interval"
        ? this.row(this.label("How often"), this.select("interval", [2, 5, 10, 15, 30, 60].map(m => [m * 60, `every ${m} min`] as Opt), "How often"))
        : this.row(this.label("Check every"), this.select("watch_seconds", [[5, "5 seconds"], [10, "10 seconds"], [15, "15 seconds"], [30, "30 seconds"], [60, "minute"]], "How often the browsers are checked for changes")),
      this.row(this.label("Show snapshots from the last"), this.select("keep_days", [[3, "3 days"], [7, "7 days"], [14, "14 days"], [30, "30 days"], [90, "90 days"], [365, "a year"]], "Older ones are hidden, never deleted")));
    const stored = stat.incognito_pages_stored ?? 0;
    box.append(el("div", "tabs-set-title mt-1", "Incognito"),
      this.row(this.label("Keep"), this.incognitoSelect()),
      this.row(this.label("Purge pages"), this.select("incognito_purge_hours", [[0, "never"], [1, "after 1 hour"], [24, "after 1 day"], [168, "after 1 week"], [720, "after 30 days"]], "Older incognito pages are removed automatically; counts stay")),
      this.row(this.label(`${plural(stored, "incognito page")} stored`)),
      this.purgeGroup(
        this.purgeButton("Purge incognito pages", "Purge pages", "pages", "Every saved incognito link and title is removed from the database for good (overwritten, not just hidden). How many incognito windows and tabs there were is kept.", "ti-file-x"),
        this.purgeButton("Forget incognito windows", "Forget windows", "windows", "Every saved incognito window is removed for good, pages and counts. Normal windows are not touched.", "ti-browser-x")));
    const ch = el("button", "btn btn-sm btn-ghost-danger"); ch.append(icon("ti-history-off", "me-1"), "Clear tab history…"); ch.onclick = () => this.d.clearHistory();
    box.append(el("div", "tabs-set-title mt-1", "History"), this.row(this.label(`${(stat.snapshots ?? 0).toLocaleString()} snapshots kept`), ch));
    return this;
  }

  private async save(patch: Partial<TabSettings>): Promise<boolean> {
    try {
      this.cfg = (await this.d.post<{ settings: TabSettings }>("/settings", patch)).settings;
      this.d.saved(this.cfg);
      return true;
    } catch (e) { this.d.toast(String((e as Error).message ?? e), "danger", "Not saved"); return false; }
  }

  private label(text: string): HTMLElement { return el("span", "text-secondary small", text); }
  private row(...kids: (HTMLElement | string)[]): HTMLElement { const r = el("div", "tabs-set-row"); r.append(...kids); return r; }

  private toggle(key: keyof TabSettings & string, label: string, tip: string): HTMLElement {
    const l = el("label", "form-check form-switch m-0 tabs-switch"), i = el("input", "form-check-input");
    i.type = "checkbox"; i.checked = !!this.cfg?.[key];
    i.onchange = async () => { if (!(await this.save({ [key]: i.checked }))) i.checked = !i.checked; else this.d.toast(label + (i.checked ? " on" : " off"), "success"); };
    l.append(i, el("span", "form-check-label", label)); l.dataset.tip = tip;
    return l;
  }

  private options(x: HTMLSelectElement, opts: Opt[]): void { for (const [v, t] of opts) { const o = el("option", null, t); o.value = String(v); x.append(o); } }

  private select(key: keyof TabSettings & string, opts: Opt[], tip: string): HTMLSelectElement {
    const x = el("select", "form-select form-select-sm w-auto"); this.options(x, opts);
    x.value = String(this.cfg?.[key]); x.dataset.tip = tip; x.onchange = () => void this.save({ [key]: Number(x.value) });
    return x;
  }

  private modeSelect(): HTMLSelectElement {
    const x = el("select", "form-select form-select-sm w-auto"); this.options(x, [["change", "as soon as tabs change"], ["interval", "on a schedule"]]);
    x.value = this.cfg?.save_mode ?? "change";
    x.dataset.tip = "As soon as tabs change: the browsers are checked every few seconds and a snapshot is stored the moment anything differs.";
    x.onchange = async () => { await this.save({ save_mode: x.value as "change" | "interval" }); this.paint(this.cfg, this.stat); };
    return x;
  }

  private incognitoSelect(): HTMLSelectElement {
    const x = el("select", "form-select form-select-sm w-auto"); this.options(x, [["pages", "Pages and counts"], ["counts", "Counts only"]]);
    x.value = this.cfg?.include_incognito ? "pages" : "counts";
    x.onchange = async () => {
      const on = x.value === "pages";
      if (on && !(await this.d.confirm({ title: "Keep incognito pages?", ok: "Keep pages", no: "Counts only", danger: false,
        body: "Incognito links and titles will be saved in the tabs database on this computer (never uploaded, never served), as well as the counts." }))) { x.value = "counts"; return; }
      if (!(await this.save({ include_incognito: on }))) x.value = on ? "counts" : "pages";
    };
    return x;
  }

  // Short label on the button, the full name in its tooltip and the confirm; the two sit together as one group.
  private purgeGroup(...kids: HTMLElement[]): HTMLElement {
    const g = el("div", "btn-group w-100 tabs-purge"); g.setAttribute("role", "group"); g.setAttribute("aria-label", "Remove saved incognito data"); g.append(...kids);
    return g;
  }

  private purgeButton(label: string, short: string, level: "pages" | "windows", body: string, ic: string): HTMLButtonElement {
    const b = el("button", "btn btn-sm btn-outline-danger"); b.type = "button"; b.append(icon(ic, "me-1"), short); b.dataset.tip = label;
    b.onclick = async () => {
      if (!(await this.d.confirm({ title: label + "?", ok: label, no: "Keep them", body }))) return;
      try {
        const r = await this.d.post<{ pages: number; windows: number }>("/purge", { level });
        this.d.toast(level === "pages" ? `${plural(r.pages, "incognito page")} removed from ${plural(r.windows, "window")}; counts kept.`
          : `${plural(r.windows, "incognito window")} forgotten, counts included.`, "success", "Purged");
        await this.d.purged();
      } catch (e) { this.d.toast(String((e as Error).message ?? e), "danger", "Purge failed"); }
    };
    return b;
  }
}
