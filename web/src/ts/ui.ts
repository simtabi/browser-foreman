// Small DOM builders every view uses, typed once. The page script (page/page.ts) and the typed view modules
// (views/*.ts) import these instead of each defining its own.
//
//   el("div", "card", "Title")   icon("ti-trash", "me-1")   kbd("⌘Z")   svg("rect", {x: 1})   plural(3, "tab")

export const $ = <T extends Element = HTMLElement>(sel: string, root: ParentNode = document): T | null => root.querySelector<T>(sel);

export function el<K extends keyof HTMLElementTagNameMap>(tag: K, cls?: string | null, text?: string | number | null): HTMLElementTagNameMap[K] {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text != null) e.textContent = String(text);
  return e;
}

/** A Tabler icon by class name, or with "browser:<name>" that browser's own app icon (served by the local server from
 *  the installed app; a globe if it cannot be had). Every menu, palette row and card gets real browser icons this way. */
export const icon = (name: string, cls = ""): HTMLElement => name.startsWith("browser:") ? browserIcon(name.slice(8), cls) : el("i", `ti ${name} ${cls}`.trim());
// An <img> cannot go through the page's fetch, so the icon's address names the server's own route. Icons drawn before
// the workspace bridge has loaded use the Go server's route too; only the Python tool's page (a BM_WS without v1)
// keeps its own.
export const browserIconUrl = (name: string): string => {
  const ws = (window as unknown as { BM_WS?: { v1?: boolean } }).BM_WS;
  return (ws && !ws.v1 ? "api/tabs/browser-icon" : "api/v1/browsers/icon") + `?name=${encodeURIComponent(name)}`;
};
export function browserIcon(name: string, cls = ""): HTMLElement {
  const img = el("img", `browser-ico ${cls}`.trim()); img.src = browserIconUrl(name); img.alt = ""; img.width = img.height = 16;
  img.onerror = () => img.replaceWith(el("i", `ti ti-world ${cls}`.trim()));
  return img;
}
export const kbd = (k: string): HTMLElement => el("kbd", null, k);

const SVG_NS = "http://www.w3.org/2000/svg";
export function svg<K extends keyof SVGElementTagNameMap>(tag: K, attrs: Record<string, string | number> = {}, text?: string | number): SVGElementTagNameMap[K] {
  const e = document.createElementNS(SVG_NS, tag);
  for (const [k, v] of Object.entries(attrs)) e.setAttribute(k, String(v));
  if (text != null) e.textContent = String(text);
  return e;
}

/** "1 tab", "3 tabs"; a word whose plural is not +s passes it. */
export const plural = (n: number, one: string, many = one + "s"): string => `${n.toLocaleString()} ${n === 1 ? one : many}`;

/** A Tabler badge with an optional icon: badge("untouched", "bg-green-lt", "ti-lock"). */
export function badge(text: string, cls: string, ic?: string): HTMLElement {
  const b = el("span", "badge " + cls); if (ic) b.append(icon(ic, "me-1")); b.append(text); return b;
}
/** A Unix time (seconds) as a date and time, or a date, in the reader's own format. */
export const fmtDate = (t?: number): string => t ? new Date(t * 1000).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" }) : "unknown";
export const fmtDay = (t?: number): string => t ? new Date(t * 1000).toLocaleDateString(undefined, { dateStyle: "medium" }) : "";
