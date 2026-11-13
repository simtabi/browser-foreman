// A bookmark's own labels: a 1–5 star rating and a set of tags. Two small controls used wherever a bookmark is shown
// in full (the details pane, the grid's cards), plus the rules both the page and the server keep (edit.CleanTags):
// trimmed, once each ignoring case, sorted, at most 20, each up to 40 characters, no commas.
//
//   pane.append(starRating(3, v => save(v)));      // click a star to set, click the same star again to clear
//   pane.append(tagEditor(["work"], tags => save(tags), allTags));

import { el, icon } from "./ui";

export const MAX_RATING = 5, MAX_TAGS = 20, MAX_TAG_LEN = 40;

/** The tags as the server keeps them, or an error to show. */
export function cleanTags(input: string[]): { tags: string[]; error?: string } {
  const seen = new Set<string>(), out: string[] = [];
  for (const raw of input) {
    const t = raw.split(/\s+/).filter(Boolean).join(" ");
    if (!t || seen.has(t.toLowerCase())) continue;
    if (t.includes(",")) return { tags: out, error: "A tag cannot hold a comma." };
    if (t.length > MAX_TAG_LEN) return { tags: out, error: `A tag is at most ${MAX_TAG_LEN} characters.` };
    seen.add(t.toLowerCase()); out.push(t);
  }
  if (out.length > MAX_TAGS) return { tags: out.slice(0, MAX_TAGS), error: `A bookmark has at most ${MAX_TAGS} tags.` };
  return { tags: out.sort((a, b) => a.toLowerCase().localeCompare(b.toLowerCase())) };
}

/** Read-only stars for a row or a card: "★★★☆☆", or nothing when not rated. */
export function starsText(v: number | undefined): string { return v ? "★".repeat(v) + "☆".repeat(MAX_RATING - v) : ""; }

/** Five star buttons. Clicking the current rating clears it; 1–5 and 0 on the keyboard do the same. */
export function starRating(value: number, onChange: (v: number) => void, opts: { disabled?: boolean } = {}): HTMLElement {
  const box = el("div", "star-rating"); box.setAttribute("role", "radiogroup"); box.setAttribute("aria-label", "Your rating");
  let cur = value;
  const paint = () => {
    Array.from(box.children).forEach((b, k) => {
      const on = k < cur; b.classList.toggle("on", on);
      b.textContent = "★";
      b.setAttribute("aria-checked", String(k + 1 === cur));
    });
    box.dataset.tip = cur ? `${cur} of ${MAX_RATING} stars. Click the same star to clear.` : "Not rated";
  };
  const set = (v: number) => { cur = v === cur ? 0 : v; paint(); onChange(cur); };
  for (let k = 1; k <= MAX_RATING; k++) {
    const b = el("button", "btn btn-ghost-secondary btn-icon btn-sm"); b.type = "button"; b.disabled = !!opts.disabled;
    b.setAttribute("role", "radio"); b.setAttribute("aria-label", `${k} star${k === 1 ? "" : "s"}`);
    b.onclick = () => set(k);
    b.onmouseenter = () => Array.from(box.children).forEach((x, j) => x.classList.toggle("hover", j < k));
    box.append(b);
  }
  box.onmouseleave = () => Array.from(box.children).forEach(x => x.classList.remove("hover"));
  box.onkeydown = e => {  // 1–5 sets that many stars, 0 clears
    const n = Number(e.key); if (e.key.length !== 1 || !(n >= 0 && n <= MAX_RATING) || opts.disabled) return;
    e.preventDefault(); if (n !== cur) { cur = n; paint(); onChange(cur); }
  };
  paint();
  return box;
}

/** The tags as removable badges and a field that adds one on Enter or comma, suggesting tags used elsewhere. */
export function tagEditor(tags: string[], onChange: (tags: string[]) => void, suggestions: string[] = [], opts: { disabled?: boolean } = {}): HTMLElement {
  const box = el("div", "tag-editor");
  let cur = [...tags];
  const list = el("datalist"); list.id = "tags-" + Math.random().toString(36).slice(2, 8);
  const input = el("input", "form-control form-control-sm tag-input"); input.type = "text"; input.placeholder = cur.length ? "Add a tag" : "Add tags: work, read later…";
  input.setAttribute("list", list.id); input.setAttribute("aria-label", "Add a tag"); input.maxLength = MAX_TAG_LEN + 10; input.disabled = !!opts.disabled;
  const err = el("div", "invalid-feedback d-block small"); err.hidden = true;
  const commit = (next: string[]) => {
    const r = cleanTags(next);
    err.hidden = !r.error; err.textContent = r.error ?? "";
    if (r.error) return false;
    cur = r.tags; paint(); onChange(cur); return true;
  };
  const paint = () => {
    const badges = cur.map(t => {
      const b = el("span", "badge bg-azure-lt tag-badge"); b.append(icon("ti-tag", "me-1"), t);
      if (!opts.disabled) { const x = el("button", "btn-close tag-x"); x.type = "button"; x.setAttribute("aria-label", `Remove ${t}`); x.onclick = () => commit(cur.filter(y => y !== t)); b.append(x); }
      return b;
    });
    box.replaceChildren(...badges, input, list, err);
    list.replaceChildren(...suggestions.filter(s => !cur.some(c => c.toLowerCase() === s.toLowerCase())).slice(0, 50).map(s => { const o = el("option"); o.value = s; return o; }));
  };
  input.onkeydown = e => {
    if ((e.key === "Enter" || e.key === ",") && input.value.trim()) { e.preventDefault(); if (commit([...cur, ...input.value.split(",")])) { input.value = ""; input.focus(); } }
    else if (e.key === "Backspace" && !input.value && cur.length) { e.preventDefault(); commit(cur.slice(0, -1)); input.focus(); }
  };
  input.onchange = () => { if (input.value.trim() && commit([...cur, ...input.value.split(",")])) { input.value = ""; } };
  paint();
  return box;
}
