// Settings › Page snapshots: the window a page is drawn in (a taller one shows more of the page), how long it may
// run before the picture, and the folder pictures and site icons are kept in. Saved on the server (Preferences),
// so every browser and the command line take snapshots the same way.
//
//   host.append(new SnapshotSettingsPanel().element);

import { api } from "./api";
import { toast } from "./legacy";
import { el, icon } from "./ui";

export interface SnapshotSettings { snapshot_width: number; snapshot_height: number; snapshot_wait_seconds: number; snapshot_dir: string }

/** The window sizes offered; any other saved size shows as "Custom". */
export const SNAPSHOT_SIZES: [string, string][] = [
  ["1024x640", "Small · 1024 × 640"], ["1280x800", "Standard · 1280 × 800"], ["1440x900", "Large · 1440 × 900"],
  ["1920x1080", "Full HD · 1920 × 1080"], ["1280x2400", "Tall · 1280 × 2400 (more of the page)"]];
export const SNAPSHOT_WAITS = [3, 5, 8, 12, 20, 30];

export class SnapshotSettingsPanel {
  readonly element: HTMLElement;
  private cur: SnapshotSettings | null = null;

  constructor() {
    this.element = el("div", "set-group");
    this.element.append(el("div", "set-title", "Page snapshots"), el("div", "set-help px-1", "Loading…"));
    void this.load();
  }

  private async load(): Promise<void> {
    try { this.cur = await api.path("snapshot-settings").get<SnapshotSettings>(); this.paint(); }
    catch { this.element.replaceChildren(el("div", "set-title", "Page snapshots"), el("div", "set-help px-1", "Only the owner of this server can change how snapshots are taken.")); }
  }

  private async save(change: Partial<SnapshotSettings>, said: string): Promise<boolean> {
    try { this.cur = await api.path("snapshot-settings").post<SnapshotSettings>(change); toast(said, "success"); this.paint(); return true; }
    catch (e) { toast(e instanceof Error ? e.message : String(e), "danger", "Not saved"); return false; }
  }

  private row(label: string, help: string, control: HTMLElement): HTMLElement {
    const r = el("div", "set-row"), t = el("div", "min-w-0"); t.append(el("div", "set-label", label), el("div", "set-help", help));
    r.append(t, control); return r;
  }

  private paint(): void {
    const c = this.cur; if (!c) return;
    // Window size.
    const size = el("select", "form-select form-select-sm w-auto"), key = `${c.snapshot_width}x${c.snapshot_height}`;
    for (const [v, t] of SNAPSHOT_SIZES) size.append(new Option(t, v));
    if (!SNAPSHOT_SIZES.some(([v]) => v === key)) size.append(new Option(`Custom · ${c.snapshot_width} × ${c.snapshot_height}`, key));
    size.value = key;
    size.onchange = () => { const [w, h] = size.value.split("x").map(Number); void this.save({ snapshot_width: w!, snapshot_height: h! }, "Snapshots use the new size from the next one taken"); };
    // Wait.
    const wait = el("select", "form-select form-select-sm w-auto");
    for (const s of SNAPSHOT_WAITS) wait.append(new Option(`${s} seconds`, String(s)));
    if (!SNAPSHOT_WAITS.includes(c.snapshot_wait_seconds)) wait.append(new Option(`${c.snapshot_wait_seconds} seconds`, String(c.snapshot_wait_seconds)));
    wait.value = String(c.snapshot_wait_seconds);
    wait.onchange = () => void this.save({ snapshot_wait_seconds: Number(wait.value) }, `Pages get ${wait.value} seconds before their picture`);
    // Folder.
    const box = el("div", "snap-dir");
    const input = el("input", "form-control form-control-sm"); input.type = "text"; input.value = c.snapshot_dir; input.spellcheck = false;
    input.placeholder = "Default: data folder/media"; input.setAttribute("aria-label", "Folder for snapshots and site icons");
    const save = el("button", "btn btn-sm btn-primary"); save.type = "button"; save.append(icon("ti-device-floppy", "me-1"), "Save");
    const reset = el("button", "btn btn-sm btn-ghost-secondary"); reset.type = "button"; reset.textContent = "Use default"; reset.hidden = !c.snapshot_dir;
    save.onclick = () => void this.save({ snapshot_dir: input.value.trim() }, input.value.trim() ? "Snapshots and icons are kept in the new folder" : "Snapshots and icons are kept in the data folder");
    reset.onclick = () => { input.value = ""; save.click(); };
    input.onkeydown = e => { if (e.key === "Enter") { e.preventDefault(); save.click(); } };
    box.append(input, save, reset);
    this.element.replaceChildren(el("div", "set-title", "Page snapshots"),
      this.row("Window size", "The browser window each page is drawn in. Tall shows more of a long page; the picture kept is half the size.", size),
      this.row("Wait before the picture", "How long a page may load and run first. Longer suits slow or script-heavy sites.", wait),
      this.row("Folder", "Where snapshots and site icons are kept: a full path on this computer. Pictures already taken stay where they are.", box));
  }
}
