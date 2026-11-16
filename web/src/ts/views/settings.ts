// The Settings dialog: the preference rows (one table of what can be set), then the sections the page supplies
// (browser history access, assistants, the danger zone). The page owns the values and what a change sets off; this
// builds the rows and reports changes. Moved from the page script on 2026-11-12; same markup.
//
//   renderSettings(body, { get: k => SET[k], set: (k, v) => {...}, defaults: SETTINGS_DEFAULT }, [historySection, dangerZone]);

import { el, icon } from "../ui";

export type SettingValue = string | number | boolean;
export type SettingOption = [SettingValue, string];
/** One preference: [key, label, help, "switch"] or [key, label, help, "select", options]. */
export type SettingRow = [string, string, string, "switch"] | [string, string, string, "select", SettingOption[]];
export type SettingGroup = [string, SettingRow[]];

export const SETTING_GROUPS: SettingGroup[] = [
  ["Appearance", [
    ["theme", "Theme", "Light, dark, or follow the system setting.", "select", [["system", "Follow the system"], ["light", "Light"], ["dark", "Dark"]]],
    ["treeThumbs", "Snapshots in the tree", "A small picture of the page beside each bookmark.", "switch"],
    ["treeDesc", "Descriptions in the tree", "The page's description or your note under each title.", "switch"],
    ["treeDates", "Dates in the tree", "When each bookmark was added.", "switch"],
    ["pageSize", "Rows per page", "How many rows a long list shows per page, until you pick another size in that list's own footer.", "select", [[10, "10"], [25, "25"], [50, "50"], [100, "100"], [0, "All"]]],
    ["tileLimit", "Cards per page", "How many bookmark cards a folder overview shows per page, until you pick another size in its footer.", "select", [[60, "60"], [120, "120"], [240, "240"], [500, "500"], [0, "All"]]],
  ]],
  ["Help and messages", [
    ["tooltips", "Tooltips", "Hints when the pointer rests on something.", "switch"],
    ["tipDelay", "Tooltip delay", "How long the pointer rests before a hint shows.", "select", [[150, "Short"], [400, "Normal"], [800, "Long"]]],
    ["toastSecs", "Notifications stay for", "Errors always stay until you close them.", "select", [[3, "3 seconds"], [4.5, "4–5 seconds"], [8, "8 seconds"]]],
  ]],
  ["Open tabs", [
    ["liveMs", "Refresh from the browsers every", "How often the page reads your browsers again while it is open, to show tabs and windows that opened, closed or changed. Reading takes a moment for each browser, so very short times use more processor.", "select", [[0, "Off"], [2000, "2 seconds"], [5000, "5 seconds"], [10000, "10 seconds"], [30000, "30 seconds"], [60000, "1 minute"]]],
  ]],
  ["Startup", [
    ["bootLoader", "Loading screen", "Cover the whole page while it loads, until everything is ready. Off: each panel shows its own placeholder as it fills in. Takes effect the next time the page loads.", "switch"],
    ["rememberPlace", "Remember where I was", "Reopen at the same view, filter, search, folder or bookmark and tab snapshot after a refresh or on the next visit. Off: always start at Bookmarks.", "switch"],
  ]],
  ["Browser extensions", [
    ["extApprove", "Approve a connecting browser", "Strict: a browser asking to connect is approved only in Browser Foreman opened in that same browser, so Chrome never asks you about Brave. Loose: from any browser. The terminal (browser-foreman pair --approve) works either way.", "select", [["strict", "Only in that browser"], ["loose", "In any browser"]]],
  ]],
  ["Opening links", [
    ["openWarn", "Ask before opening more than", "Opening many tabs at once asks first.", "select", [[5, "5 links"], [10, "10 links"], [20, "20 links"], [50, "50 links"], [100000, "Never ask"]]],
  ]],
];

export interface SettingsStore {
  get: (key: string) => SettingValue | undefined;
  /** Store a changed value; the page saves it and sets off what depends on it. */
  set: (key: string, value: SettingValue) => void;
  /** Defaults, so a select stores a number where the default is one. */
  defaults: Record<string, SettingValue>;
}

/** A select's chosen text, typed like the setting's default. */
export const settingValue = (raw: string, def: SettingValue | undefined): SettingValue => typeof def === "number" ? Number(raw) : raw;

function control(row: SettingRow, s: SettingsStore): HTMLElement {
  const [k, , , kind] = row;
  if (kind === "switch") {
    const ctl = el("span", "form-check form-switch m-0"), inp = el("input", "form-check-input");
    inp.type = "checkbox"; inp.checked = !!s.get(k); inp.onchange = () => s.set(k, inp.checked); ctl.append(inp);
    return ctl;
  }
  const ctl = el("select", "form-select form-select-sm w-auto");
  for (const [v, t] of row[4]) { const o = el("option", null, t); o.value = String(v); ctl.append(o); }
  ctl.value = String(s.get(k));
  ctl.onchange = () => s.set(k, settingValue(ctl.value, s.defaults[k]));
  return ctl;
}

/** A section: its title and rows. */
export function settingsGroup(title: string, rows: HTMLElement[] = [], cls = ""): HTMLElement {
  const sec = el("div", ("set-group " + cls).trim()); sec.append(el("div", "set-title", title), ...rows); return sec;
}

/** One action row: label and explanation on the left, the button on the right. */
export function actionRow(label: string, help: string, btnLabel: string, ic: string, cls: string, fn: () => unknown): HTMLElement {
  const r = el("div", "set-row"), t = el("div", "min-w-0"); t.append(el("div", "set-label", label), el("div", "set-help", help));
  const b = el("button", ("btn btn-sm " + cls).trim()); b.append(icon(ic, "me-1"), btnLabel); b.onclick = () => fn();
  r.append(t, b); return r;
}

/** Fill the dialog body: every preference group, then the page's own sections in order. */
export function renderSettings(body: HTMLElement, s: SettingsStore, extra: HTMLElement[] = [], groups = SETTING_GROUPS): void {
  body.replaceChildren();
  for (const [title, rows] of groups) {
    body.append(settingsGroup(title, rows.map(row => {
      const r = el("label", "set-row"), txt = el("div", "min-w-0");
      txt.append(el("div", "set-label", row[1]), el("div", "set-help", row[2]));
      r.append(txt, control(row, s)); return r;
    })));
  }
  body.append(...extra);
}
