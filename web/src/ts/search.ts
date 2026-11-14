// One search box in the header for every view. Each view keeps its own query and its own (hidden) input, which the
// view's code already reads; this box shows the current view's query and writes what you type into that input, so
// switching views never carries a bookmark search into the tabs list or back.
//
//   new ViewSearch(headerInput)
//     .view("bookmarks", bookmarkInput, "Search bookmarks…")
//     .view("tabs", tabsInput, "Search open tabs…")
//     .show("tabs");

interface Target { input: HTMLInputElement; placeholder: string }

export class ViewSearch {
  private readonly targets = new Map<string, Target>();
  private current = "";

  constructor(private readonly box: HTMLInputElement) {
    box.addEventListener("input", () => {
      const t = this.targets.get(this.current);
      if (!t) return;
      t.input.value = box.value;
      t.input.dispatchEvent(new Event("input", { bubbles: true }));
    });
    // Escape clears the search of the view you are in, then leaves the box.
    box.addEventListener("keydown", e => {
      if (e.key !== "Escape") return;
      if (box.value) { box.value = ""; box.dispatchEvent(new Event("input")); e.stopPropagation(); }
      else box.blur();
    });
  }

  view(name: string, input: HTMLInputElement | null, placeholder: string): this {
    if (input) this.targets.set(name, { input, placeholder });
    return this;
  }

  /** Point the box at a view: its placeholder, its current query. */
  show(name: string): this {
    this.current = name;
    const t = this.targets.get(name);
    this.box.disabled = !t;
    this.box.placeholder = t?.placeholder ?? "";
    this.box.setAttribute("aria-label", t?.placeholder.replace(/…$/, "") ?? "Search");
    this.box.value = t?.input.value ?? "";
    return this;
  }

  /** The view's own input changed without the box (a restored place, Clear filters): show it. */
  sync(): this { return this.show(this.current); }

  focus(): this { this.box.focus(); this.box.select(); return this; }
}
