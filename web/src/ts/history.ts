// Settings › Browser history: whether this server may read each browser's visit history, and the way to fix it.
// macOS keeps those files behind Full Disk Access; the panel says which browsers are blocked, opens the right
// System Settings pane, and checks again. Reading the history itself comes with the Go port (docs/architecture.md).
//
//   new HistoryAccessPanel(container).refresh();

import { api } from "./api";
import { Dom } from "./dom";
import { toast } from "./legacy";

export type AccessState = "readable" | "blocked" | "missing";
export interface AccessStatus {
  browsers: { browser: string; state: AccessState }[];
  granted: boolean;
  blocked: number;
  python: string;
  launched_by: string;
}

const LABEL: Record<AccessState, string> = { readable: "can be read", blocked: "blocked by macOS", missing: "not installed" };

const node = <K extends keyof HTMLElementTagNameMap>(tag: K, cls = "", text = ""): HTMLElementTagNameMap[K] => {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text) e.textContent = text;
  return e;
};

export class HistoryAccessPanel {
  constructor(private readonly root: HTMLElement) {}

  async refresh(): Promise<this> {
    this.root.replaceChildren(node("div", "set-title", "Browser history"), node("div", "set-help", "Checking…"));
    try {
      this.render(await api.path("history", "access").get<AccessStatus>());
    } catch {
      this.root.replaceChildren(node("div", "set-title", "Browser history"),
        node("div", "set-help", "Needs the local server (src/server/serve.py)."));
    }
    return this;
  }

  private render(s: AccessStatus): void {
    const present = s.browsers.filter(b => b.state !== "missing");
    const head = node("div", "set-row"), txt = node("div", "min-w-0");
    txt.append(
      node("div", "set-label", s.granted ? "Full Disk Access granted" : `${s.blocked} browser${s.blocked === 1 ? "" : "s"} blocked`),
      node("div", "set-help", s.granted
        ? "The server can read your browsers' visit history. Nothing is read until the history view is built."
        : "macOS keeps every browser's history behind Full Disk Access. Add the app that runs the server"
          + (s.launched_by ? ` (${s.launched_by})` : "") + " in System Settings › Privacy & Security › Full Disk Access, "
          + "then quit and reopen it and start the server again. Until then the history view stays off; the Go version "
          + "will ask for this access by its own name."));
    const acts = node("div", "d-flex gap-2 flex-wrap");
    if (!s.granted) acts.append(this.button("Open Full Disk Access", "ti-shield-lock", "open-fda", () => this.openSettings(), "btn-primary"));
    acts.append(this.button("Check again", "ti-refresh", "recheck-access", () => void this.refresh(), "btn-outline-secondary"));
    head.append(txt, acts);
    const list = node("div", "set-help");
    list.textContent = present.map(b => `${b.browser}: ${LABEL[b.state]}`).join(" · ") || "No supported browser found.";
    const py = node("div", "set-help text-truncate", `Python: ${s.python}`);
    py.title = s.python;
    this.root.replaceChildren(node("div", "set-title", "Browser history"), head, list, py);
  }

  private button(label: string, ic: string, action: string, fn: () => void, cls: string): HTMLButtonElement {
    const b = Dom.write(node("button", `btn btn-sm ${cls}`), "action", action);
    b.type = "button";
    b.append(node("i", `ti ${ic} me-1`), label);
    b.onclick = fn;
    return b;
  }

  private async openSettings(): Promise<void> {
    try {
      const r = await api.path("history", "access", "open-settings").post<{ ok: boolean; error: string }>();
      if (r.ok) toast("Add the app that runs the server, then press Check again.", "info", "System Settings opened");
      else toast(r.error || "System Settings did not open", "danger");
    } catch (e) {
      toast(String((e as Error).message || e), "danger", "System Settings did not open");
    }
  }
}
