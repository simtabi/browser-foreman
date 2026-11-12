// DOM hooks for scripts use data-brofo-* attributes ("brofo" = BROwser FOreman), never classes or ids that styling
// also uses. One prefix, so a search for "data-brofo-" finds every element a script reaches for, and a restyle
// cannot break behaviour.
//
//   <button data-brofo-view="tabs">           Dom.all("view")              every element with that hook
//   Dom.one("view", "tabs")                   the element whose hook has that value
//   Dom.read(button, "view")  → "tabs"        Dom.write(button, "view", "timeline") returns the element

export const PREFIX = "brofo";

export const attr = (name: string): string => `data-${PREFIX}-${name}`;

export const selector = (name: string, value?: string): string =>
  value === undefined ? `[${attr(name)}]` : `[${attr(name)}="${CSS.escape(value)}"]`;

export const Dom = {
  all<T extends Element = HTMLElement>(name: string, value?: string, root: ParentNode = document): T[] {
    return [...root.querySelectorAll<T>(selector(name, value))];
  },
  one<T extends Element = HTMLElement>(name: string, value?: string, root: ParentNode = document): T | null {
    return root.querySelector<T>(selector(name, value));
  },
  read(el: Element, name: string): string | null {
    return el.getAttribute(attr(name));
  },
  write<T extends Element>(el: T, name: string, value: string | null): T {
    if (value === null) el.removeAttribute(attr(name));
    else el.setAttribute(attr(name), value);
    return el;
  },
};
