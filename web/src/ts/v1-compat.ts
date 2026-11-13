// Translates the page's older calls (written for the Python server: api/tabs, api/open, api/rechecks…) into the
// /api/v1 routes, and their answers back into the shapes the page reads. One place, so the page's ~25 call sites
// stay as they are and the Python server keeps working with the same page. Pure: the network call is passed in.

export type Call = (method: string, path: string, body?: unknown) => Promise<any>;
export interface Answer { status: number; body: unknown }

/** The page snapshot preferences (Settings › Page snapshots): only these pass, both ways. */
const SNAPSHOT_KEYS = ["snapshot_width", "snapshot_height", "snapshot_wait_seconds", "snapshot_dir"] as const;
export function snapshotSettings(p: any): Record<string, unknown> {
  return Object.fromEntries(SNAPSHOT_KEYS.map(k => [k, p[k]]));
}
/** v1 preferences ⇄ the page's older setting names. */
export function legacySettings(p: any): Record<string, unknown> {
  return { autosave: p.autosave, interval: p.interval_seconds, include_incognito: p.include_incognito, keep_days: p.keep_days,
    save_mode: p.save_mode, watch_seconds: p.watch_seconds, recheck_days: p.recheck_days, tracked_browsers: p.tracked ?? null,
    open_with: p.open_with, incognito_purge_hours: p.incognito_purge_hours ?? 0, onboarded_browsers: true };
}
export function v1Settings(s: Record<string, any>): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  const map: Record<string, string> = { autosave: "autosave", interval: "interval_seconds", include_incognito: "include_incognito",
    keep_days: "keep_days", save_mode: "save_mode", watch_seconds: "watch_seconds", recheck_days: "recheck_days",
    tracked_browsers: "tracked", open_with: "open_with", incognito_purge_hours: "incognito_purge_hours" };
  for (const [k, v] of Object.entries(s)) if (k in map) out[map[k]!] = k === "open_with" && typeof v === "string" ? [v] : v;
  return out;
}

/** The browser to send a tab group to: the one asked for, else the first that opens links (the system default
 *  resolved), provided its extension is connected; null otherwise. */
async function groupTarget(asked: string | undefined, call: Call): Promise<string | null> {
  const [b, st] = await Promise.all([call("GET", "browsers"), call("GET", "extension/status")]);
  const paired = new Set<string>((st.extensions ?? []).map((x: { name: string }) => x.name.replace(/^extension:/, "")));
  let name = asked ?? (b.open_with ?? [])[0];
  if (name === "System default") name = b.default;
  return name && paired.has(name) ? name : null;
}

const NOT_YET = (what: string): Answer => ({ status: 501, body: { error: `${what} is not available with this server yet` } });

/** Answers one older call through the v1 routes, or null when it is not one this knows (it then goes out as is). */
export async function legacy(method: string, path: string, query: string, body: any, call: Call, workspace: string): Promise<Answer | null> {
  const q = query ? "?" + query : "";
  const ok = (b: unknown): Answer => ({ status: 200, body: b });
  const m = method.toUpperCase();
  if (m === "GET") {
    switch (path) {
      case "tabs": return ok(await call("GET", "tabs"));
      case "tabs/snapshots": return ok(await call("GET", "tabs/snapshots" + q));
      case "tabs/timeline": return ok(await call("GET", "tabs/timeline" + q));
      case "tabs/notes": return ok(await call("GET", "tabs/notes"));
      case "opened": return ok(await call("GET", `workspaces/${encodeURIComponent(workspace)}/opened`));
      case "snapshot-settings": return ok(snapshotSettings(await call("GET", "preferences")));
      case "tabs/status": {
        const [p, st] = await Promise.all([call("GET", "preferences"), call("GET", "tabs/stored").catch(() => ({}))]);
        return ok({ settings: legacySettings(p), last_auto: null, snapshots: st.snapshots ?? 0, incognito_pages_stored: st.incognito_pages ?? 0 });
      }
      case "tabs/browsers": {
        const b = await call("GET", "browsers");
        const installed = (b.installed ?? []).map(({ path: _p, ...rest }: any) => rest);
        return ok({ installed, tracked: b.tracked ?? installed.filter((x: any) => x.scriptable || x.session_file).map((x: any) => x.name),
          open_with: b.open_with, onboarded: true, system_default: b.default || null });
      }
      case "rechecks": {
        const [c, p] = await Promise.all([call("GET", "linkchecks"), call("GET", "preferences")]);
        return ok({ running: c.running, recheck_days: p.recheck_days, runs: (c.runs ?? []).map((r: any) => ({
          run_id: r.id, started: r.started, finished: r.done ? r.ended : undefined, total: r.total, checked: r.checked,
          newly_broken: r.newly_broken, fixed: r.fixed, state: r.done ? (r.state === "cancelled" ? "cancelled" : "done") : "running" })) });
      }
      case "history/access": {
        const a = await call("GET", "history/access");
        return ok({ ...a, python: a.binary });  // the panel's older name for the program that needs the access
      }
      case "profiles": return ok(await call("GET", "profiles"));
      case "token": return ok({ token: "" });
    }
    if (path.startsWith("tabs/snapshots/")) {
      const j = await call("GET", "tabs/snapshots/" + encodeURIComponent(path.slice("tabs/snapshots/".length)));
      return ok({ ...j.state, at: j.at, reason: j.reason });
    }
    if (path.startsWith("rechecks/")) {
      const j = await call("GET", "linkchecks/" + encodeURIComponent(path.slice("rechecks/".length)) + "?results=1");
      const res = Object.values(j.results ?? {}) as any[];
      return ok({ newly_broken: res.filter(r => r.status === "dead").map(r => ({ url: r.url, before: "", note: r.note ?? "" })), fixed: [] });
    }
    return null;
  }
  switch (path) {
    case "tabs": return { status: 200, body: await call("POST", "tabs/snapshots", { reason: "manual" }) };
    case "tabs/settings": {
      const p = await call("PUT", "preferences", v1Settings(body ?? {}));
      return ok({ settings: legacySettings(p) });
    }
    case "tabs/close": {
      const r = await call("POST", "tabs/close", { items: body?.items ?? [] });
      return ok({ closed: r.closed, skipped: (r.skipped ?? []).length, details: (r.skipped ?? []).map((why: string) => ({ why })), undo_snapshot: r.undo_snapshot });
    }
    case "tabs/ages": return ok(await call("POST", "tabs/ages", { urls: body?.urls ?? [] }));
    case "tabs/note": return ok((await call("PUT", "tabs/notes", { kind: body?.kind, key: body?.key, note: body?.note ?? "", tags: body?.tags ?? [] })).note);
    case "tabs/delete": return ok({ ...(await call("POST", "tabs/snapshots/delete", body ?? {})), files: 0 });
    case "tabs/purge": return ok(await call("POST", "tabs/purge", { level: body?.level ?? "pages", older_than_hours: Number(body?.older_than_hours) || 0 }));
    case "open": {
      const urls: string[] = body?.urls ?? [];
      const browsers = body?.browser ? [body.browser] : body?.browsers;
      if (body?.how === "grouped") {
        // A named tab group when the browser's extension is connected; otherwise a window of its own.
        const target = await groupTarget(body?.browser, call);
        if (target) {
          await call("POST", "extension/commands", { browser: target, kind: "open-group", urls, title: String(body?.title ?? "").slice(0, 100) });
          return ok({ opened: urls.length, grouped: true });
        }
      }
      const how = body?.how === "grouped" ? "window" : body?.how ?? "tabs";
      const r = await call("POST", "open", { urls, how, ...(browsers ? { browsers } : {}) });
      const done = (r.opened ?? []).filter((o: any) => o.ok).length;
      return ok({ opened: done * urls.length, results: r.opened });
    }
    case "rechecks/run": return ok(await call("POST", "linkchecks", { workspace }));
    case "rechecks/cancel": {
      const c = await call("GET", "linkchecks");
      return c.running ? ok(await call("POST", `linkchecks/${encodeURIComponent(c.running)}/cancel`)) : ok({});
    }
    case "history/access/open-settings": return ok(await call("POST", "history/access/open-settings"));
    case "snapshot-settings": {
      const want: Record<string, unknown> = {};
      for (const k of SNAPSHOT_KEYS) if (body && k in body) want[k] = body[k];
      return ok(snapshotSettings(await call("PUT", "preferences", want)));
    }
  }
  return null;
}
