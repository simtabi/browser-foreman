// Entry point: registers the Alpine components and gives the page's existing script one small API (window.BF)
// to open them, then starts Alpine. Built by src/web/build.mjs into assets/js/app.js.
import "./ws_client.js";  // the workspace store first: the page script reads and writes through window.BM_WS
import "./page/page";  // the page script first: everything else builds on what it sets up
import Alpine from "alpinejs";
import { browserPicker, closeWithConfirm, historyTools, moveToMode, noteEditor, rechecks, tabCleanup, tabHistory } from "./components";
import type { OpenTab } from "./tabs";

Alpine.data("tabCleanup", tabCleanup);
Alpine.data("historyTools", historyTools);
Alpine.data("noteEditor", noteEditor);
Alpine.data("rechecks", rechecks);
Alpine.data("browserPicker", browserPicker);

type Opener = (...args: never[]) => unknown;
const call = (id: string, method: string, ...args: unknown[]): unknown => {
  const el = document.getElementById(id) as (HTMLElement & { _x_dataStack?: Record<string, Opener>[] }) | null;
  const data = el?._x_dataStack?.[0];
  return (data?.[method] as ((...a: unknown[]) => unknown) | undefined)?.call(data, ...args);
};

declare global { interface Window { BF: Record<string, (...args: never[]) => unknown> } }
window.BF = {
  cleanup: (group?: string) => call("bkCleanup", "show", group),
  clearHistory: () => call("bkHistory", "show"),
  browsers: () => call("bkBrowsers", "show"),
  closeTabs: (list: OpenTab[]) => closeWithConfirm(list),
  moveToMode: (list: OpenTab[], to: "normal" | "incognito") => moveToMode(list, to),
  tabHistory: () => tabHistory,
  editNote: (kind: string, key: string, label: string, note?: string, tags?: string[], onSaved?: () => void) => call("bkNote", "show", kind, key, label, note, tags, onSaved),
};

Alpine.start();
