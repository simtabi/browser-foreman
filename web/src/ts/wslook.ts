// How a workspace looks, so each is easy to tell apart: a colour (Tabler's named colours, as the server accepts in
// api.WorkspaceColors) and an icon (any Tabler icon; these are the ones offered). Used by the top-bar switcher, its
// menu, All workspaces and the Appearance dialog.
//
//   button.prepend(wsBadge(workspace));          // the icon on a tint of its colour
//   document.documentElement.dataset.ws = color; // the top bar's strip takes the colour

import { el, icon } from "./ui";

export interface WsLook { name: string; color?: string; icon?: string }

/** The colours a workspace may take, in the order the picker shows them; "" is the default. */
export const WS_COLORS = ["blue", "azure", "indigo", "purple", "pink", "red", "orange", "yellow", "lime", "green", "teal", "cyan"] as const;

/** The icons offered, with what each suggests. */
export const WS_ICONS: [string, string][] = [
  ["stack-2", "Default"], ["briefcase", "Work"], ["home", "Home"], ["user", "Personal"], ["users", "Team"],
  ["school", "Study"], ["code", "Code"], ["device-laptop", "Devices"], ["building", "Company"], ["shopping-cart", "Shopping"],
  ["book", "Reading"], ["news", "News"], ["music", "Music"], ["movie", "Video"], ["camera", "Photos"],
  ["palette", "Design"], ["flask", "Research"], ["rocket", "Projects"], ["plane", "Travel"], ["heart", "Favourites"],
  ["star", "Starred"], ["flag", "Goals"], ["coffee", "Leisure"], ["archive", "Archive"],
];

/** The workspace's badge: its icon on a tint of its colour (the default: a neutral stack). */
export function wsBadge(w: WsLook, cls = "avatar-xs"): HTMLElement {
  const b = el("span", `avatar ${cls} ws-badge ${w.color ? `bg-${w.color}-lt` : "bg-secondary-lt"}`);
  b.append(icon("ti-" + (w.icon || "stack-2")));
  b.setAttribute("aria-hidden", "true");
  return b;
}

/** The Appearance form: colour swatches and icon choices, calling `change` with what was picked. */
export function lookPicker(w: WsLook, change: (look: { color: string; icon: string }) => void): HTMLElement {
  let color = w.color ?? "", ic = w.icon ?? "";
  const box = el("div", "ws-look");
  const preview = el("div", "ws-look-preview");
  const paintPreview = () => { preview.replaceChildren(wsBadge({ name: w.name, color, icon: ic }, "avatar-md"), el("div", "fw-medium", w.name)); };
  const swatches = el("div", "row g-2");
  const pick = (c: string) => {
    const col = el("div", "col-auto"), lab = el("label", "form-colorinput"), inp = el("input", "form-colorinput-input");
    inp.type = "radio"; inp.name = "ws-color"; inp.value = c; inp.checked = c === color;
    inp.onchange = () => { color = c; paintPreview(); change({ color, icon: ic }); };
    const sw = el("span", `form-colorinput-color ${c ? `bg-${c}` : "bg-secondary-lt"}`); sw.setAttribute("aria-label", c || "Default");
    lab.dataset.tip = c ? c[0]!.toUpperCase() + c.slice(1) : "Default";
    lab.append(inp, sw); col.append(lab); swatches.append(col);
  };
  pick(""); for (const c of WS_COLORS) pick(c);
  const icons = el("div", "form-selectgroup ws-icons");
  for (const [name, label] of WS_ICONS) {
    const item = el("label", "form-selectgroup-item"), inp = el("input", "form-selectgroup-input");
    inp.type = "radio"; inp.name = "ws-icon"; inp.value = name; inp.checked = (ic || "stack-2") === name;
    inp.onchange = () => { ic = name === "stack-2" ? "" : name; paintPreview(); change({ color, icon: ic }); };
    const face = el("span", "form-selectgroup-label"); face.append(icon("ti-" + name)); face.dataset.tip = label; face.setAttribute("aria-label", label);
    item.append(inp, face); icons.append(item);
  }
  paintPreview();
  box.append(preview, el("div", "form-label mt-3", "Colour"), swatches, el("div", "form-label mt-3", "Icon"), icons);
  return box;
}
