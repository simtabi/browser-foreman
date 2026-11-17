// Settings › the server's own settings: the config files and .env in its config folder, as the server lists them
// (GET settings/fields). Secrets are write-only: the page shows whether one is set, never its value. A setting the
// environment sets, or one the files keep for themselves, shows why it cannot be changed here. Risky ones (the
// address, mode, storage, token) are editable only when the files say settings.edit "all", after a confirmation.
//
//   host.append(new ServerSettingsPanel(call).element);

import { askConfirm } from "./dialogs";
import { toast } from "./legacy";
import { el, icon } from "./ui";
import { skeleton } from "./busy";

export interface ServerField {
  key: string; group: string; env?: string; type: "string" | "choice" | "bool" | "int" | "duration" | "list" | "secret" | "map";
  choices?: string[]; value?: unknown; set: boolean; source: string; editable: boolean; locked?: string;
  risky?: boolean; live?: boolean; help?: string;
}
interface Fields { edit: "safe" | "all"; fields: ServerField[]; folder: string; local: boolean }
interface Saved extends Fields { result: { changed: string[]; restart: string[]; files: string[] } }
type Call = <T>(method: string, path: string, body?: unknown) => Promise<T>;

/** Groups in the order people look for them: AI and notifications first, the server's own last. */
export const GROUP_ORDER = ["ai", "notify", "mcp", "webhooks", "auth", "writeback", "server", "storage", "settings"];
/** The group headings. */
export const GROUP_TITLES: Record<string, string> = {
  ai: "AI", server: "Server", storage: "Storage", webhooks: "Webhooks", auth: "Sign-in", mcp: "Assistants (MCP)",
  writeback: "Write-back", notify: "Notifications", settings: "What this page may change",
};
const WORDS: Record<string, string> = { ai: "AI", api: "API", url: "URL", mcp: "MCP", ttl: "length", openai: "OpenAI", anthropic: "Anthropic", brofo: "", key: "key" };

/** A setting's label from its key: "ai.openai_url" → "OpenAI URL", "OPENAI_API_KEY" → "OpenAI API key". */
export function settingLabel(f: Pick<ServerField, "key" | "group">): string {
  const last = f.key.includes(".") ? f.key.split(".").pop()! : f.key.toLowerCase();
  const words = last.split("_").map(w => WORDS[w] ?? w).filter(Boolean);
  const s = words.join(" ");
  return s.charAt(0).toUpperCase() + s.slice(1);
}

/** Where a value comes from, in words. */
export function sourceText(f: ServerField): string {
  if (f.type === "secret") return f.set ? (f.source === "environment" ? "Set in the environment" : "Set in .env") : "Not set";
  return f.source === "default" ? "Default" : f.source === "environment" ? "From the environment" : `From ${f.source}`;
}

export class ServerSettingsPanel {
  readonly element: HTMLElement;
  private data: Fields | null = null;

  constructor(private readonly call: Call) {
    this.element = el("div");
    this.element.append(this.title("Server settings"), skeleton("Loading the server's settings…", 3));
    void this.load();
  }

  private title(t: string): HTMLElement { return el("div", "set-title", t); }

  private async load(): Promise<void> {
    try { this.data = await this.call<Fields>("GET", "settings/fields"); this.paint(); }
    catch (e) {
      const g = el("div", "set-group"); g.append(this.title("Server settings"), el("div", "set-help px-1", e instanceof Error ? e.message : "Only the owner of this server can see its settings."));
      this.element.replaceChildren(g);
    }
  }

  /** Save values; a risky one asks first. Answers whether it was saved. */
  async save(values: Record<string, unknown>, risky: boolean): Promise<boolean> {
    if (risky && !(await askConfirm({ title: "Change a server setting that matters for safety?", ok: "Change it", no: "Keep it",
      body: "This changes who can reach Browser Foreman or where its data lives. It takes effect when the server restarts; a wrong value can keep it from starting until you fix the file." }))) return false;
    try {
      const r = await this.call<Saved>("PUT", "settings", { values, confirm: risky });
      this.data = r; this.paint();
      const restart = r.result.restart ?? [], files = (r.result.files ?? []).join(" and ");
      toast(restart.length ? `Saved in ${files}. Restart Browser Foreman to use it (browser-foreman serve --replace, or browser-foreman service install).`
        : `Saved in ${files}. In use now.`, restart.length ? "warning" : "success", "Settings saved");
      return true;
    } catch (e) { toast(e instanceof Error ? e.message : String(e), "danger", "Not saved"); return false; }
  }

  private paint(): void {
    const d = this.data; if (!d) return;
    const groups = new Map<string, ServerField[]>(GROUP_ORDER.map(g => [g, []]));
    for (const f of d.fields) { if (!groups.has(f.group)) groups.set(f.group, []); groups.get(f.group)!.push(f); }
    const intro = el("div", "set-group");
    intro.append(this.title("Server settings"), el("div", "set-help px-1",
      `Saved in ${d.folder} (the *.json files and .env). Values set in the environment win and are not changed here. ` +
      (d.edit === "all" ? "The address, mode, storage and token can be changed here too, after a confirmation." :
        "The address, mode, storage and token are changed in the files only (settings.edit is \"safe\").") +
      (d.local ? "" : " Changes can be saved only from the computer the server runs on.")));
    const out: HTMLElement[] = [intro];
    for (const [g, fields] of groups) {
      if (!fields.length) continue;
      const sec = el("div", "set-group"); sec.dataset.group = g;
      sec.append(this.title(GROUP_TITLES[g] ?? g), ...fields.map(f => this.row(f, d.local)));
      out.push(sec);
    }
    this.element.replaceChildren(...out);
  }

  private row(f: ServerField, local: boolean): HTMLElement {
    const r = el("div", "set-row"), t = el("div", "min-w-0");
    r.dataset.key = f.key;
    const label = el("div", "set-label", settingLabel(f));
    const src = el("span", "badge bg-secondary-lt ms-2 fw-normal", sourceText(f));
    if (f.type === "secret") src.className = "badge ms-2 fw-normal " + (f.set ? "bg-green-lt" : "bg-secondary-lt");
    label.append(src);
    if (f.risky) { const b = el("span", "badge bg-orange-lt ms-1 fw-normal", "Restart, confirm"); label.append(b); }
    else if (f.live) label.append(el("span", "badge bg-blue-lt ms-1 fw-normal", "Applies at once"));
    t.append(label, el("div", "set-help", f.help ?? ""));
    const canEdit = f.editable && local;
    if (!canEdit) t.append(el("div", "set-help text-secondary", f.locked ?? "Changes can be saved only from this computer."));
    r.append(t, this.control(f, canEdit));
    return r;
  }

  private control(f: ServerField, on: boolean): HTMLElement {
    const risky = !!f.risky;
    const saveBtn = (label = "Save"): HTMLButtonElement => {
      const b = el("button", "btn btn-sm btn-primary"); b.type = "button"; b.append(icon("ti-device-floppy", "me-1"), label); b.disabled = !on; return b;
    };
    switch (f.type) {
      case "bool": {
        const box = el("span", "form-check form-switch m-0"), inp = el("input", "form-check-input");
        inp.type = "checkbox"; inp.checked = !!f.value; inp.disabled = !on; inp.setAttribute("aria-label", settingLabel(f));
        inp.onchange = async () => { if (!(await this.save({ [f.key]: inp.checked }, risky))) inp.checked = !inp.checked; };
        box.append(inp); return box;
      }
      case "choice": {
        const sel = el("select", "form-select form-select-sm w-auto"); sel.disabled = !on; sel.setAttribute("aria-label", settingLabel(f));
        for (const c of f.choices ?? []) sel.append(new Option(c, c));
        sel.value = String(f.value ?? "");
        const was = sel.value;
        sel.onchange = async () => { if (!(await this.save({ [f.key]: sel.value }, risky))) sel.value = was; };
        return sel;
      }
      case "map":
        return el("span", "set-help text-secondary", "In the file");
      case "secret": {
        const box = el("div", "snap-dir");
        const inp = el("input", "form-control form-control-sm"); inp.type = "password"; inp.autocomplete = "off"; inp.spellcheck = false;
        inp.placeholder = f.set ? "Enter a new value to replace it" : "Paste the key"; inp.disabled = !on; inp.setAttribute("aria-label", settingLabel(f));
        const save = saveBtn();
        save.onclick = async () => { const v = inp.value.trim(); if (!v) { inp.focus(); return; } if (await this.save({ [f.key]: v }, risky)) inp.value = ""; };
        inp.onkeydown = e => { if (e.key === "Enter") { e.preventDefault(); save.click(); } };
        box.append(inp, save);
        if (f.set && f.source !== "environment") {
          const clear = el("button", "btn btn-sm btn-ghost-danger"); clear.type = "button"; clear.textContent = "Remove"; clear.disabled = !on;
          clear.onclick = async () => {
            if (await askConfirm({ title: `Remove ${settingLabel(f)}?`, ok: "Remove", no: "Keep it", body: "It is emptied in .env. Anything that needs it stops working until you add one again." })) await this.save({ [f.key]: "" }, risky);
          };
          box.append(clear);
        }
        return box;
      }
    }
    // string, int, duration, list
    const box = el("div", "snap-dir");
    const list = f.type === "list";
    const inp = list ? el("textarea", "form-control form-control-sm") : el("input", "form-control form-control-sm");
    if (inp instanceof HTMLInputElement) inp.type = f.type === "int" ? "number" : "text";
    else inp.rows = 3;
    inp.spellcheck = false; inp.disabled = !on; inp.setAttribute("aria-label", settingLabel(f));
    inp.value = list ? ((f.value as string[] | undefined) ?? []).join("\n") : String(f.value ?? "");
    if (list) inp.placeholder = "One per line";
    const save = saveBtn();
    save.onclick = async () => {
      const raw = inp.value;
      const v = f.type === "int" ? Number(raw) : list ? raw.split("\n").map(x => x.trim()).filter(Boolean) : raw.trim();
      await this.save({ [f.key]: v }, risky);
    };
    if (!list) inp.onkeydown = e => { if (e.key === "Enter") { e.preventDefault(); save.click(); } };
    box.append(inp, save);
    return box;
  }
}
