// Settings › the server's own settings: the config files and .env in its config folder, as the server lists them
// (GET settings/fields). Secrets are write-only: the page shows whether one is set, never its value.
//
// AI comes first and is nested: choose who answers AI jobs, and only that provider's settings show beneath it (its
// key, a model chosen from the models the provider itself offers, effort for Anthropic, and its address under
// Advanced). One button saves provider and model together, so a provider is never left without the model it needs.
// Every other group is a collapsible section whose summary shows its current values.
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
interface ModelList { provider: string; models: { id: string; name?: string }[]; default?: string; key_set?: boolean; error?: string }
type Call = <T>(method: string, path: string, body?: unknown) => Promise<T>;

/** Groups after AI, in the order people look for them; the server's own last. */
export const GROUP_ORDER = ["notify", "mcp", "webhooks", "auth", "writeback", "server", "storage", "settings"];
/** The group headings. */
export const GROUP_TITLES: Record<string, string> = {
  ai: "AI", server: "Server", storage: "Storage", webhooks: "Webhooks", auth: "Sign-in", mcp: "Assistants (MCP)",
  writeback: "Write-back", notify: "Notifications", settings: "What this page may change",
};
/** Who answers AI jobs: the choice, its name and what it means. */
export const PROVIDERS: { id: string; name: string; about: string }[] = [
  { id: "none", name: "Your assistant (MCP)", about: "No key here: jobs wait until an assistant connected over MCP takes them." },
  { id: "anthropic", name: "Anthropic (Claude)", about: "Answered by Anthropic's API with your key, as soon as a job is made." },
  { id: "openai", name: "OpenAI", about: "Answered by OpenAI's API, or any server with the same API, with your key." },
  { id: "ollama", name: "Ollama, on this computer", about: "Answered by a local model; nothing leaves this computer." },
];
const WORDS: Record<string, string> = { addr: "address", ai: "AI", api: "API", url: "URL", mcp: "MCP", ttl: "length", openai: "OpenAI", anthropic: "Anthropic", brofo: "", key: "key" };

/** A setting's label from its key: "ai.openai_url" → "OpenAI URL", "OPENAI_API_KEY" → "OpenAI API key". */
export function settingLabel(f: Pick<ServerField, "key" | "group">): string {
  const last = f.key.includes(".") ? f.key.split(".").pop()! : f.key.toLowerCase();
  const words = last.split("_").map(w => WORDS[w] ?? w).filter(Boolean);
  const s = words.join(" ");
  return s.charAt(0).toUpperCase() + s.slice(1);
}

/** Where a value comes from, in words. */
export function sourceText(f: ServerField): string {
  if (f.type === "secret") return f.set ? (f.source === "environment" ? "set in the environment" : "saved in .env") : "not set";
  return f.source === "default" ? "default" : f.source === "environment" ? "from the environment" : `from ${f.source}`;
}

/** A value as one short phrase, for a group's summary. */
export function shortValue(f: ServerField): string {
  if (f.type === "secret") return f.set ? "set" : "not set";
  if (f.type === "bool") return f.value ? "on" : "off";
  if (f.type === "list") { const l = (f.value as string[] | undefined) ?? []; return l.length ? `${l.length}` : "none"; }
  if (f.type === "map") return "in the file";
  return String(f.value ?? "") || "default";
}

export class ServerSettingsPanel {
  readonly element: HTMLElement;
  private data: Fields | null = null;
  private chosen: string | null = null; // the provider picked in the dropdown, before it is saved
  private models = new Map<string, ModelList>();
  private open = new Set<string>(); // groups the reader opened, kept open across a repaint

  constructor(private readonly call: Call) {
    this.element = el("div");
    this.element.append(this.title("Server settings"), skeleton("Loading the server's settings…", 3));
    void this.load();
  }

  private title(t: string): HTMLElement { return el("div", "set-title", t); }
  private f(key: string): ServerField | undefined { return this.data?.fields.find(x => x.key === key); }

  private async load(): Promise<void> {
    try { this.data = await this.call<Fields>("GET", "settings/fields"); this.paint(); }
    catch (e) {
      const g = el("div", "set-group"); g.append(this.title("Server settings"), el("div", "set-help px-1", e instanceof Error ? e.message : "Only the owner of this server can see its settings."));
      this.element.replaceChildren(g);
    }
  }

  /** Save values together; a risky one asks first. Answers whether it was saved. */
  async save(values: Record<string, unknown>, risky = false, said = ""): Promise<boolean> {
    if (risky && !(await askConfirm({ title: "Change a server setting that matters for safety?", ok: "Change it", no: "Keep it",
      body: "This changes who can reach Browser Foreman, where its data lives, or where an API key is sent. A wrong value can keep the server from starting, or send the key to the wrong place, until you fix the file." }))) return false;
    try {
      const r = await this.call<Saved>("PUT", "settings", { values, confirm: risky });
      this.data = r;
      const restart = r.result.restart ?? [], files = (r.result.files ?? []).join(" and ");
      toast(restart.length ? `${said || "Saved"} in ${files}. Restart Browser Foreman to use it (browser-foreman serve --replace, or browser-foreman service install).`
        : `${said || "Saved"} in ${files}. In use now.`, restart.length ? "warning" : "success", "Settings saved");
      this.paint();
      return true;
    } catch (e) { toast(e instanceof Error ? e.message : String(e), "danger", "Not saved"); return false; }
  }

  private paint(): void {
    const d = this.data; if (!d) return;
    const intro = el("div", "set-group");
    intro.append(this.title("Server settings"), el("div", "set-help px-1",
      `Saved in ${d.folder} (the config files and .env); keys are never shown back.` + (d.local ? "" : " Changes can be saved only from the computer the server runs on.")));
    const out: HTMLElement[] = [intro, this.aiSection()];
    const groups = new Map<string, ServerField[]>(GROUP_ORDER.map(g => [g, []]));
    for (const f of d.fields) if (f.group !== "ai" && groups.has(f.group)) groups.get(f.group)!.push(f);
    for (const [g, fields] of groups) if (fields.length) out.push(this.group(g, fields, d.local));
    this.element.replaceChildren(...out);
  }

  // ---- AI: nested by provider ----

  private aiSection(): HTMLElement {
    const sec = el("div", "set-group"); sec.dataset.group = "ai";
    const saved = String(this.f("ai.provider")?.value ?? "none");
    const provider = this.chosen ?? saved;
    const local = !!this.data?.local;
    const modelNow = String(this.f("ai.model")?.value ?? "");
    const nameOf = (id: string) => PROVIDERS.find(p => p.id === id)?.name ?? id;
    // What is in use now, in one line.
    const now = el("div", "set-help px-1 mb-2");
    now.append(icon(saved === "none" ? "ti-robot-off" : "ti-robot", "me-1"),
      saved === "none" ? "In use: AI jobs wait for your assistant." : `In use: ${nameOf(saved)}${saved === "anthropic" ? ` · ${modelNow || "claude-opus-5-5 (default)"}` : modelNow ? ` · ${modelNow}` : ""}.`);
    // Who answers.
    const row = el("div", "set-row"), txt = el("div", "min-w-0");
    txt.append(el("div", "set-label", "Who answers AI jobs"), el("div", "set-help", PROVIDERS.find(p => p.id === provider)?.about ?? ""));
    const sel = el("select", "form-select form-select-sm w-auto"); sel.setAttribute("aria-label", "Who answers AI jobs"); sel.disabled = !local || !this.f("ai.provider")?.editable;
    for (const p of PROVIDERS) sel.append(new Option(p.name, p.id));
    sel.value = provider;
    sel.onchange = () => { this.chosen = sel.value; this.paint(); };
    row.append(txt, sel);
    sec.append(this.title("AI"), now, row);
    if (provider !== "none") sec.append(this.providerPanel(provider, saved, local));
    else if (saved !== "none") {
      const b = el("button", "btn btn-sm btn-primary"); b.type = "button"; b.append(icon("ti-check", "me-1"), "Use your assistant");
      b.onclick = () => void this.save({ "ai.provider": "none" }, false, "AI jobs now wait for your assistant").then(ok => { if (ok) this.chosen = null; });
      const r = el("div", "set-row justify-content-end"); r.append(b); sec.append(r);
    }
    return sec;
  }

  private providerPanel(provider: string, saved: string, local: boolean): HTMLElement {
    const box = el("div", "set-nested ms-3 ps-3 border-start");
    const keyField = this.f(provider === "anthropic" ? "ANTHROPIC_API_KEY" : provider === "openai" ? "OPENAI_API_KEY" : "");
    if (keyField) box.append(this.keyRow(keyField, provider, local));
    // Model, from the provider's own list.
    const modelRow = el("div", "set-row"), mt = el("div", "min-w-0");
    const list = this.models.get(provider);
    mt.append(el("div", "set-label", "Model"), el("div", "set-help", list?.error
      ? `Could not list the models: ${list.error}`
      : provider === "ollama" ? "The models pulled into Ollama (ollama pull <name> adds one)." : "The models your key can use, as the provider lists them."));
    const msel = el("select", "form-select form-select-sm w-auto"); msel.setAttribute("aria-label", "Model"); msel.disabled = !local;
    const other = el("input", "form-control form-control-sm mt-1"); other.placeholder = "Model name"; other.hidden = true; other.spellcheck = false;
    const current = String(this.f("ai.model")?.value ?? "");
    if (!list) { msel.append(new Option("Loading the models…", "")); void this.loadModels(provider); }
    else {
      for (const m of list.models) msel.append(new Option(m.name && m.name !== m.id ? `${m.name} (${m.id})` : m.id, m.id));
      msel.append(new Option("Other…", "__other"));
      const pick = (saved === provider && current) || list.default || list.models[0]?.id || "";
      if (pick && !list.models.some(m => m.id === pick)) msel.prepend(new Option(pick, pick));
      msel.value = pick || "__other"; other.hidden = msel.value !== "__other";
    }
    msel.onchange = () => { other.hidden = msel.value !== "__other"; if (!other.hidden) other.focus(); };
    const refresh = el("button", "btn btn-sm btn-ghost-secondary btn-icon"); refresh.type = "button"; refresh.setAttribute("aria-label", "List the models again"); refresh.dataset.tip = "List the models again";
    refresh.append(icon("ti-refresh")); refresh.onclick = () => { this.models.delete(provider); this.paint(); };
    const mctl = el("div", "d-flex gap-1 align-items-start flex-wrap justify-content-end"); const mcol = el("div"); mcol.append(msel, other); mctl.append(mcol, refresh);
    modelRow.append(mt, mctl); box.append(modelRow);
    // Effort (Anthropic).
    let esel: HTMLSelectElement | null = null;
    if (provider === "anthropic") {
      const er = el("div", "set-row"), et = el("div", "min-w-0");
      et.append(el("div", "set-label", "Effort"), el("div", "set-help", "How hard the model thinks: low is quickest and cheapest."));
      esel = el("select", "form-select form-select-sm w-auto"); esel.setAttribute("aria-label", "Effort"); esel.disabled = !local;
      for (const v of ["low", "medium", "high"]) esel.append(new Option(v, v));
      esel.value = String(this.f("ai.effort")?.value || "low");
      er.append(et, esel); box.append(er);
    }
    // Address, under Advanced.
    const urlKey = provider === "ollama" ? "ai.ollama_url" : provider === "openai" ? "ai.openai_url" : "";
    const uf = urlKey ? this.f(urlKey) : undefined;
    if (uf) {
      const adv = el("details", "mt-1"); const sum = el("summary", "set-help", "Advanced");
      adv.append(sum, this.row(uf, local));
      box.append(adv);
    }
    // One button: provider, model and effort together.
    const go = el("button", "btn btn-sm btn-primary"); go.type = "button";
    const name = PROVIDERS.find(p => p.id === provider)?.name ?? provider;
    go.append(icon("ti-check", "me-1"), saved === provider ? "Save" : `Use ${name}`);
    go.disabled = !local || (!!keyField && !keyField.set && provider !== "anthropic");
    if (go.disabled && local) go.dataset.tip = "Add the key first";
    go.onclick = () => {
      const model = msel.value === "__other" ? other.value.trim() : msel.value;
      if (provider !== "anthropic" && !model) { toast("Choose a model first.", "warning"); return; }
      const values: Record<string, unknown> = { "ai.provider": provider, "ai.model": model };
      if (esel) values["ai.effort"] = esel.value;
      void this.save(values, false, `AI jobs now go to ${name}${model ? ` (${model})` : ""}`).then(ok => { if (ok) this.chosen = null; });
    };
    const act = el("div", "set-row justify-content-end"); act.append(go); box.append(act);
    return box;
  }

  private keyRow(f: ServerField, provider: string, local: boolean): HTMLElement {
    const r = el("div", "set-row"), t = el("div", "min-w-0");
    const label = el("div", "set-label", provider === "anthropic" ? "Anthropic API key" : "OpenAI API key");
    label.append(el("span", "badge ms-2 fw-normal " + (f.set ? "bg-green-lt" : "bg-secondary-lt"), f.set ? "Saved" : "Not set"));
    t.append(label, el("div", "set-help", f.set ? "Kept in .env and never shown again. Paste a new key to replace it."
      : provider === "anthropic" ? "Kept in .env, never shown again. Without one, the default model is offered and a profile from ant auth login is used if there is one." : "Kept in .env, never shown again."));
    const box = el("div", "snap-dir");
    const inp = el("input", "form-control form-control-sm"); inp.type = "password"; inp.autocomplete = "off"; inp.spellcheck = false;
    inp.placeholder = f.set ? "New key to replace it" : "Paste the key"; inp.disabled = !local || !f.editable; inp.setAttribute("aria-label", label.textContent ?? "API key");
    const save = el("button", "btn btn-sm btn-primary"); save.type = "button"; save.append(icon("ti-key", "me-1"), f.set ? "Replace" : "Save key"); save.disabled = inp.disabled;
    save.onclick = async () => {
      const v = inp.value.trim(); if (!v) { inp.focus(); return; }
      if (await this.save({ [f.key]: v }, false, "Key saved")) { this.models.delete(provider); this.paint(); }
    };
    inp.onkeydown = e => { if (e.key === "Enter") { e.preventDefault(); save.click(); } };
    box.append(inp, save);
    if (f.set && f.source !== "environment") {
      const rm = el("button", "btn btn-sm btn-ghost-danger"); rm.type = "button"; rm.textContent = "Remove"; rm.disabled = inp.disabled;
      rm.onclick = async () => {
        if (await askConfirm({ title: "Remove this key?", ok: "Remove", no: "Keep it", body: "It is emptied in .env. Jobs for this provider fail until you add one again." })) await this.save({ [f.key]: "" }, false, "Key removed");
      };
      box.append(rm);
    }
    if (!f.editable && f.locked) t.append(el("div", "set-help text-secondary", f.locked));
    r.append(t, box);
    return r;
  }

  private async loadModels(provider: string): Promise<void> {
    try { this.models.set(provider, await this.call<ModelList>("GET", `ai/models?provider=${encodeURIComponent(provider)}`)); }
    catch (e) { this.models.set(provider, { provider, models: [], error: e instanceof Error ? e.message : String(e) }); }
    if ((this.chosen ?? String(this.f("ai.provider")?.value ?? "none")) === provider) this.paint();
  }

  // ---- every other group: collapsible, summarised ----

  private group(g: string, fields: ServerField[], local: boolean): HTMLElement {
    const det = el("details", "set-group set-collapse"); det.dataset.group = g;
    det.open = this.open.has(g);
    det.addEventListener("toggle", () => { if (det.open) this.open.add(g); else this.open.delete(g); });
    const sum = el("summary", "d-flex align-items-baseline gap-2");
    const chev = icon("ti-chevron-right", "set-chev flex-shrink-0");
    const shown = fields.filter(f => f.type !== "map").slice(0, 3).map(f => `${settingLabel(f).toLowerCase()} ${shortValue(f)}`).join(" · ");
    sum.append(chev, el("span", "set-title m-0 text-nowrap flex-shrink-0", GROUP_TITLES[g] ?? g), el("span", "set-help text-truncate min-w-0", shown));
    det.append(sum);
    const restart = fields.some(f => !f.live && f.editable);
    if (restart) det.append(el("div", "set-help px-1 mb-1", "Changes here take effect when Browser Foreman restarts."));
    det.append(...fields.map(f => this.row(f, local)));
    return det;
  }

  private row(f: ServerField, local: boolean): HTMLElement {
    const r = el("div", "set-row"), t = el("div", "min-w-0");
    r.dataset.key = f.key;
    const label = el("div", "set-label", settingLabel(f));
    label.append(el("span", "set-help ms-2 d-inline", sourceText(f)));
    t.append(label, el("div", "set-help", f.help ?? ""));
    const canEdit = f.editable && local;
    if (!canEdit) {
      const why = el("div", "set-help text-secondary"); why.append(icon("ti-lock", "me-1"), f.locked ?? "Changes can be saved only from this computer.");
      t.append(why);
    }
    r.append(t, this.control(f, canEdit));
    return r;
  }

  private control(f: ServerField, on: boolean): HTMLElement {
    const risky = !!f.risky;
    const saveBtn = (): HTMLButtonElement => { const b = el("button", "btn btn-sm btn-primary"); b.type = "button"; b.append(icon("ti-device-floppy", "me-1"), "Save"); b.disabled = !on; return b; };
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
        sel.value = String(f.value ?? ""); const was = sel.value;
        sel.onchange = async () => { if (!(await this.save({ [f.key]: sel.value }, risky))) sel.value = was; };
        return sel;
      }
      case "map":
        return el("span", "set-help text-secondary", "In the file");
      case "secret": {
        const box = el("div", "snap-dir");
        const inp = el("input", "form-control form-control-sm"); inp.type = "password"; inp.autocomplete = "off"; inp.spellcheck = false;
        inp.placeholder = f.set ? "New value to replace it" : "Paste the value"; inp.disabled = !on; inp.setAttribute("aria-label", settingLabel(f));
        const save = saveBtn();
        save.onclick = async () => { const v = inp.value.trim(); if (!v) { inp.focus(); return; } if (await this.save({ [f.key]: v }, risky)) inp.value = ""; };
        box.append(inp, save); return box;
      }
    }
    const box = el("div", "snap-dir");
    const list = f.type === "list";
    const inp = list ? el("textarea", "form-control form-control-sm") : el("input", "form-control form-control-sm");
    if (inp instanceof HTMLInputElement) inp.type = f.type === "int" ? "number" : "text"; else inp.rows = 3;
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
