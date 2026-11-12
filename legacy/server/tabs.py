"""Open tabs: read every window and tab of the browsers running on this Mac, and keep timed snapshots on disk, so a
crash or a closed window never loses track of what was open.

How it reads: macOS lets a local program ask a scriptable browser for its windows and tabs (Apple Events, through
`osascript -l JavaScript`). Chrome, Edge and Brave answer with every window's tabs and whether the window is incognito;
Safari answers with its windows and tabs but says nothing about private windows. Firefox is not scriptable this way.
The first read asks macOS for permission ("python3 wants to control Google Chrome"); until it is granted the reader
reports `needs-permission` for that browser and reads nothing.

Nothing here opens, closes or changes a tab. Incognito and private pages are kept only as a count unless the owner
turns on "Keep incognito tabs" in the Open tabs settings; even then they live only in the snapshot files on this
computer, which the preview server never serves.

Snapshots live in one SQLite database, sessions/tabs.sqlite3 (snapshots, windows, tabs, settings), one row set per
change (an identical read is not stored again). Snapshots older than `keep_days` are hidden from the timeline, never
deleted. Incognito pages can be purged, now or automatically after a set age, keeping the counts; with
secure_delete on, purged rows are overwritten in the file, not just unlinked. Snapshot files from before the
database are imported once and moved to sessions/_imported/.

Usage: python3 tabs.py            print what is open now, as JSON
       python3 tabs.py --save     take a snapshot now
"""
import hashlib
import json
import os
import re
import sqlite3
import subprocess
import sys
import threading
import time

import paths  # noqa: E402  every folder lives in src/lib/paths.py

ROOT = paths.ROOT
SESSIONS = paths.SESSIONS
SETTINGS_PATH = os.path.join(SESSIONS, "settings.json")  # read once, to bring old settings into the database
# BM_TABS_DB points the database elsewhere (tests, tools), so nothing but the real server touches the real one.
DB_PATH = os.environ.get("BM_TABS_DB") or paths.TABS_DB
# include_incognito: keep incognito pages (links and titles) as well as counts.
# incognito_purge_hours: 0 = never; otherwise incognito pages older than this are purged (counts stay).
# save_mode "change": read the browsers every watch_seconds and store as soon as anything differs (an unchanged read is
# never stored); "interval": store every `interval` seconds.
DEFAULTS = {"autosave": True, "interval": 300, "include_incognito": False, "keep_days": 14, "incognito_purge_hours": 0,
            "save_mode": "change", "watch_seconds": 15, "recheck_days": 7, "recheck_epoch": 0,
            "tracked_browsers": None, "open_with": None, "onboarded_browsers": False}

# Every browser this tool knows, what macOS lets it do with each, and how to open links in it.
#   scriptable: windows and tabs can be read (and closed) through Apple Events
#   private_known: the reader can tell incognito/private windows apart
#   private_flag: the launch option for a new incognito/private window, or None when the browser has none
BROWSERS = [
    {"name": "Google Chrome", "bundle": "com.google.Chrome", "engine": "chromium", "scriptable": True, "private_known": True, "private_flag": "--incognito"},
    {"name": "Google Chrome Canary", "bundle": "com.google.Chrome.canary", "engine": "chromium", "scriptable": True, "private_known": True, "private_flag": "--incognito"},
    {"name": "Chromium", "bundle": "org.chromium.Chromium", "engine": "chromium", "scriptable": True, "private_known": True, "private_flag": "--incognito"},
    {"name": "Brave Browser", "bundle": "com.brave.Browser", "engine": "chromium", "scriptable": True, "private_known": True, "private_flag": "--incognito"},
    {"name": "Microsoft Edge", "bundle": "com.microsoft.edgemac", "engine": "chromium", "scriptable": True, "private_known": True, "private_flag": "--inprivate"},
    {"name": "Vivaldi", "bundle": "com.vivaldi.Vivaldi", "engine": "chromium", "scriptable": True, "private_known": True, "private_flag": "--incognito"},
    {"name": "Opera", "bundle": "com.operasoftware.Opera", "engine": "chromium", "scriptable": True, "private_known": True, "private_flag": "--private"},
    {"name": "Arc", "bundle": "company.thebrowser.Browser", "engine": "chromium", "scriptable": True, "private_known": False, "private_flag": None},
    {"name": "Safari", "bundle": "com.apple.Safari", "engine": "webkit", "scriptable": True, "private_known": False, "private_flag": None},
    {"name": "Firefox", "bundle": "org.mozilla.firefox", "engine": "gecko", "scriptable": False, "private_known": False, "private_flag": "--private-window"},
    {"name": "Zen", "bundle": "app.zen-browser.zen", "engine": "gecko", "scriptable": False, "private_known": False, "private_flag": "--private-window"},
    {"name": "Orion", "bundle": "com.kagi.kagimacOS", "engine": "webkit", "scriptable": False, "private_known": False, "private_flag": None},
]
BY_NAME = {b["name"]: b for b in BROWSERS}
APP_DIRS = ["/Applications", "/Applications/Utilities", os.path.expanduser("~/Applications"), "/System/Volumes/Preboot/Cryptexes/App/System/Applications"]
_installed_cache = {"at": 0, "list": []}


def installed_browsers(refresh=False):
    """The browsers actually installed: every .app in the usual folders whose bundle id is a known browser."""
    import plistlib
    if not refresh and time.time() - _installed_cache["at"] < 600:
        return _installed_cache["list"]
    by_bundle = {b["bundle"]: b for b in BROWSERS}
    found = {}
    for d in APP_DIRS:
        try:
            names = os.listdir(d)
        except OSError:
            continue
        for n in names:
            if not n.endswith(".app"):
                continue
            try:
                with open(os.path.join(d, n, "Contents", "Info.plist"), "rb") as f:
                    bid = plistlib.load(f).get("CFBundleIdentifier", "")
            except (OSError, ValueError, plistlib.InvalidFileException):
                continue
            if bid in by_bundle and bid not in found:
                found[bid] = {**by_bundle[bid], "path": os.path.join(d, n)}
    out = [found[b["bundle"]] for b in BROWSERS if b["bundle"] in found]
    _installed_cache.update(at=time.time(), list=out)
    return out


def tracked_names():
    """The scriptable installed browsers the owner chose to track (all of them until a choice is made)."""
    chosen = settings().get("tracked_browsers")
    ok = [b["name"] for b in installed_browsers() if b["scriptable"]]
    return [n for n in ok if chosen is None or n in chosen]


SAFARI = "Safari"

# JavaScript for Automation: never launches a browser (running() is checked first), reads in bulk, returns JSON.
JXA = r"""
function run(argv) {
  const name = argv[0], safari = name === "Safari";
  let app;
  try { app = Application(name); } catch (e) { return JSON.stringify({installed: false}); }
  if (!app.running()) return JSON.stringify({installed: true, running: false});
  const out = [];
  const wins = app.windows();
  for (let i = 0; i < wins.length; i++) {
    const w = wins[i];
    let urls = [], titles = [], ids = [], mode = "normal", active = null, wid = null;
    try {
      urls = w.tabs.url();
      titles = safari ? w.tabs.name() : w.tabs.title();
    } catch (e) { continue; }  // not a browser window (Safari's downloads, settings)
    try { wid = w.id(); } catch (e) {}
    if (!safari) { try { ids = w.tabs.id(); } catch (e) {} }
    if (!safari) { try { mode = w.mode(); } catch (e) {} try { active = w.activeTabIndex(); } catch (e) {} }
    else { try { active = w.currentTab().index(); } catch (e) {} }
    out.push({index: i + 1, id: wid, mode: mode, active: active,
              tabs: urls.map((u, k) => ({url: u || "", title: titles[k] || "", id: ids[k] == null ? null : ids[k]}))});
  }
  return JSON.stringify({installed: true, running: true, windows: out});
}
"""

_lock = threading.Lock()

# Close tabs by the browser's own window and tab ids (Chromium) or by position (Safari), and only when the tab still
# shows the address we expect: a tab that navigated since the read is left alone and reported.
CLOSE_JXA = r"""
function run(argv) {
  const name = argv[0], items = JSON.parse(argv[1]), safari = name === "Safari";
  const app = Application(name);
  if (!app.running()) return JSON.stringify({closed: 0, skipped: items.length, reasons: ["not running"]});
  let closed = 0; const skipped = [];
  // Highest positions first, so closing one never shifts the next.
  items.sort((a, b) => (b.pos || 0) - (a.pos || 0));
  for (const it of items) {
    try {
      let tab;
      if (!safari && it.window_id != null && it.tab_id != null) tab = app.windows.byId(it.window_id).tabs.byId(it.tab_id);
      else tab = app.windows.byId(it.window_id).tabs[it.pos];
      if (tab.url() !== it.url) { skipped.push({url: it.url, why: "changed since it was read"}); continue; }
      tab.close(); closed++;
    } catch (e) { skipped.push({url: it.url, why: "already closed or window gone"}); }
  }
  return JSON.stringify({closed: closed, skipped: skipped.length, details: skipped.slice(0, 50)});
}
"""
CLOSABLE = {b["name"] for b in BROWSERS if b["scriptable"]}


def read_browser(name, timeout=20):
    """One browser's windows and tabs. Never raises: problems come back as {"error": ...}."""
    try:
        p = subprocess.run(["osascript", "-l", "JavaScript", "-e", JXA, name],
                           capture_output=True, text=True, timeout=timeout)
    except subprocess.TimeoutExpired:
        # macOS holds the request while its permission dialog is up, so a first read often times out.
        return {"browser": name, "error": "needs-permission",
                "hint": f"macOS may be asking whether this tool can read {name}: look for the dialog and choose Allow, "
                        f"or turn it on in System Settings › Privacy & Security › Automation. Then read again."}
    except OSError as e:
        return {"browser": name, "error": f"could not ask {name}: {type(e).__name__}"}
    err = (p.stderr or "").strip()
    if p.returncode != 0:
        if "-1743" in err or "Not authorized" in err or "not allowed" in err.lower():
            return {"browser": name, "error": "needs-permission",
                    "hint": f"Allow it in System Settings › Privacy & Security › Automation (python3 → {name})."}
        if "-600" in err or "isn't running" in err:
            return {"browser": name, "running": False}
        return {"browser": name, "error": err.splitlines()[-1][:200] if err else f"exit {p.returncode}"}
    try:
        data = json.loads(p.stdout.strip() or "{}")
    except ValueError:
        return {"browser": name, "error": "unreadable answer"}
    if not data.get("installed", True):
        return None  # not installed: leave it out entirely
    data["browser"] = name
    data.pop("installed", None)
    if not BY_NAME.get(name, {}).get("private_known"):
        for w in data.get("windows", []):
            w["mode"] = "normal" if w.get("mode") in (None, "", "normal") else w["mode"]
    return data


def close_tabs(items):
    """Close the given tabs. A snapshot is stored first (always, even if nothing changed), so every close can be
    undone from the timeline. items: [{browser, window_id, tab_id?, pos, url}] — validated by the caller."""
    before = take("before-close", force=True)
    by = {}
    for it in items:
        by.setdefault(it["browser"], []).append(it)
    total, skipped, details = 0, 0, []
    for browser, its in by.items():
        try:
            p = subprocess.run(["osascript", "-l", "JavaScript", "-e", CLOSE_JXA, browser, json.dumps(its)],
                               capture_output=True, text=True, timeout=60)
            r = json.loads(p.stdout.strip() or "{}") if p.returncode == 0 else {"closed": 0, "skipped": len(its), "details": [{"why": (p.stderr or "").strip()[-200:]}]}
        except (OSError, subprocess.TimeoutExpired, ValueError) as e:
            r = {"closed": 0, "skipped": len(its), "details": [{"why": type(e).__name__}]}
        total += r.get("closed", 0); skipped += r.get("skipped", 0); details += r.get("details", [])
    after = take("after-close")
    return {"closed": total, "skipped": skipped, "details": details[:50], "undo_snapshot": before.get("id"), "after": after.get("id")}


def read_all():
    """Every scriptable browser that is installed, in a fixed order, read in parallel."""
    names = tracked_names()
    res = [None] * len(names)

    def one(k, n):
        res[k] = read_browser(n)
    ts = [threading.Thread(target=one, args=(k, n)) for k, n in enumerate(names)]
    for t in ts:
        t.start()
    for t in ts:
        t.join()
    found = [r for r in res if r]
    return {"at": time.strftime("%Y-%m-%dT%H:%M:%S%z"), "browsers": found, "summary": summarise(found)}


def summarise(browsers):
    s = {"windows": 0, "tabs": 0, "incognito_windows": 0, "incognito_tabs": 0, "running": 0}
    for b in browsers:
        if b.get("running"):
            s["running"] += 1
        for w in b.get("windows", []):
            n = len(w.get("tabs", [])) if "tabs" in w else w.get("tab_count", 0)
            if w.get("mode") == "incognito":
                s["incognito_windows"] += 1
                s["incognito_tabs"] += n
            else:
                s["windows"] += 1
                s["tabs"] += n
    return s


def redact(snap, include_incognito):
    """Incognito windows keep only their size unless the owner chose to keep them."""
    if include_incognito:
        return snap
    out = json.loads(json.dumps(snap))
    for b in out["browsers"]:
        for w in b.get("windows", []):
            if w.get("mode") == "incognito":
                w["tab_count"] = len(w.pop("tabs", []))
                w["redacted"] = True
    return out


# ---------- database ----------
SCHEMA = """
CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS snapshots (
  id TEXT PRIMARY KEY, at TEXT NOT NULL, reason TEXT NOT NULL, fingerprint TEXT NOT NULL,
  windows INTEGER NOT NULL, tabs INTEGER NOT NULL, incognito_windows INTEGER NOT NULL, incognito_tabs INTEGER NOT NULL,
  browsers TEXT NOT NULL, hidden INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS windows (
  snapshot_id TEXT NOT NULL REFERENCES snapshots(id), seq INTEGER NOT NULL, browser TEXT NOT NULL, idx INTEGER,
  mode TEXT NOT NULL, active INTEGER, tab_count INTEGER NOT NULL, pages_kept INTEGER NOT NULL,
  PRIMARY KEY (snapshot_id, seq));
CREATE TABLE IF NOT EXISTS tabs (
  snapshot_id TEXT NOT NULL, window_seq INTEGER NOT NULL, pos INTEGER NOT NULL, url TEXT NOT NULL, title TEXT NOT NULL,
  PRIMARY KEY (snapshot_id, window_seq, pos));
CREATE INDEX IF NOT EXISTS tabs_url ON tabs(url);
CREATE TABLE IF NOT EXISTS notes (
  kind TEXT NOT NULL CHECK (kind IN ('tab', 'window', 'snapshot')), key TEXT NOT NULL,
  note TEXT NOT NULL DEFAULT '', tags TEXT NOT NULL DEFAULT '[]', updated TEXT NOT NULL, PRIMARY KEY (kind, key));
CREATE TABLE IF NOT EXISTS link_checks (
  run_id TEXT PRIMARY KEY, started TEXT NOT NULL, finished TEXT, total INTEGER NOT NULL, checked INTEGER NOT NULL DEFAULT 0,
  newly_broken INTEGER NOT NULL DEFAULT 0, fixed INTEGER NOT NULL DEFAULT 0, state TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS link_results (
  run_id TEXT NOT NULL, url TEXT NOT NULL, status TEXT NOT NULL, before TEXT, note TEXT, PRIMARY KEY (run_id, url));
CREATE INDEX IF NOT EXISTS snapshots_at ON snapshots(at);
"""
_dbinit = threading.Lock()
_ready = False


def db():
    """A connection per call (the server is threaded); the schema and the one-off import run on first use."""
    global _ready
    os.makedirs(SESSIONS, exist_ok=True)
    c = sqlite3.connect(DB_PATH, timeout=30)
    c.row_factory = sqlite3.Row
    c.execute("PRAGMA journal_mode=WAL")
    c.execute("PRAGMA secure_delete=ON")  # purged pages are overwritten in the file, not left in free space
    c.execute("PRAGMA foreign_keys=ON")
    if not _ready:
        with _dbinit:
            if not _ready:
                c.executescript(SCHEMA)
                _import_old(c)
                _ready = True
    return c


def _import_old(c):
    """Bring settings.json and the per-snapshot JSON files from before the database in, once; move the files aside."""
    if os.path.exists(SETTINGS_PATH) and not c.execute("SELECT 1 FROM settings LIMIT 1").fetchone():
        try:
            with open(SETTINGS_PATH) as f:
                old = json.load(f)
            for k, v in old.items():
                if k in DEFAULTS:
                    c.execute("INSERT OR REPLACE INTO settings VALUES (?, ?)", (k, json.dumps(v)))
            c.commit()
        except (OSError, ValueError):
            pass
    for day in sorted(os.listdir(SESSIONS)):
        d = os.path.join(SESSIONS, day)
        if not (re.fullmatch(r"\d{4}-\d{2}-\d{2}", day) and os.path.isdir(d)):
            continue
        for fn in sorted(os.listdir(d)):
            if not fn.endswith(".json"):
                continue
            try:
                with open(os.path.join(d, fn)) as f:
                    snap = json.load(f)
                _store(c, f"{day}_{fn[:-5]}", snap)
            except (OSError, ValueError, sqlite3.Error):
                continue
        dest = os.path.join(SESSIONS, "_imported", day)
        os.makedirs(os.path.dirname(dest), exist_ok=True)
        if not os.path.exists(dest):
            os.replace(d, dest)
    c.commit()


# ---------- settings ----------
def reopen():
    """After the database file was replaced (a restore), re-run the schema and the one-off import on next use."""
    global _ready
    _ready = False


def settings():
    c = db()
    try:
        rows = {r["key"]: json.loads(r["value"]) for r in c.execute("SELECT key, value FROM settings")}
    finally:
        c.close()
    return {**DEFAULTS, **{k: v for k, v in rows.items() if k in DEFAULTS}}


# Each browser's own app icon, for the page's browser lists. Read from the installed app (its CFBundleIconFile .icns),
# converted once with macOS's sips to a small PNG and kept in storage/cache/browser-icons. Only installed browsers:
# a name never becomes a path.
ICON_CACHE = os.path.join(paths.STORAGE, "cache", "browser-icons")
ICON_SIZE = 64


def browser_icon(name):
    """PNG bytes of an installed browser's app icon, or None (not installed, no icon file, or the conversion failed)."""
    import plistlib
    b = next((x for x in installed_browsers() if x["name"] == name), None)
    if not b:
        return None
    out = os.path.join(ICON_CACHE, f"{b['bundle']}-{ICON_SIZE}.png")
    if os.path.exists(out):
        with open(out, "rb") as f:
            return f.read()
    try:
        with open(os.path.join(b["path"], "Contents", "Info.plist"), "rb") as f:
            icon = plistlib.load(f).get("CFBundleIconFile") or ""
    except (OSError, ValueError, plistlib.InvalidFileException):
        return None
    src = os.path.join(b["path"], "Contents", "Resources", icon if icon.endswith(".icns") else icon + ".icns")
    if not icon or not os.path.isfile(src):
        return None
    os.makedirs(ICON_CACHE, exist_ok=True)
    tmp = out + ".tmp.png"
    try:
        p = subprocess.run(["sips", "-s", "format", "png", "-Z", str(ICON_SIZE), src, "--out", tmp], capture_output=True, timeout=15)
    except (OSError, subprocess.TimeoutExpired):
        return None
    if p.returncode or not os.path.exists(tmp):
        return None
    os.replace(tmp, out)
    with open(out, "rb") as f:
        return f.read()


# The browser macOS opens web links in (System Settings › Desktop & Dock › Default web browser). Chosen as a link
# browser by name; resolved at the moment a link opens, so changing the system default needs no change here.
SYSTEM_DEFAULT = "System default"
LS_PLIST = os.path.expanduser("~/Library/Preferences/com.apple.LaunchServices/com.apple.launchservices.secure.plist")


def default_browser():
    """The installed browser macOS opens https links in, or None when it is unknown or not one this tool knows."""
    import plistlib
    try:
        with open(LS_PLIST, "rb") as f:
            handlers = plistlib.load(f).get("LSHandlers", [])
    except (OSError, ValueError, plistlib.InvalidFileException):
        return None
    bid = next((h.get("LSHandlerRoleAll") for h in handlers if h.get("LSHandlerURLScheme") == "https"), None)
    if not bid:
        return None
    return next((b["name"] for b in installed_browsers() if b["bundle"].lower() == str(bid).lower()), None)


def open_targets():
    """The browsers that open links: the chosen ones that are installed, else the first installed Chromium browser,
    else any installed browser. Settings saved before 2026-11-12 hold one name; it reads as a list of one."""
    chosen = settings().get("open_with")
    chosen = [chosen] if isinstance(chosen, str) else (chosen or [])
    installed = [b["name"] for b in installed_browsers()]
    picked = [n for n in chosen if n in installed or n == SYSTEM_DEFAULT]
    # Nothing chosen (or nothing chosen is installed): the system's own default browser. Until 2026-11-12 this fell
    # back to the first installed Chromium browser, ignoring the system default (owner: "respect the default os browser").
    return picked or [SYSTEM_DEFAULT]


def save_settings(new):
    cur = settings()
    for k, v in (new or {}).items():
        if k not in DEFAULTS:
            continue
        if k == "interval":
            v = max(60, min(3600, int(v)))
        elif k == "keep_days":
            v = max(1, min(3650, int(v)))
        elif k == "incognito_purge_hours":
            v = max(0, min(24 * 365, int(v)))
        elif k == "watch_seconds":
            v = max(5, min(300, int(v)))
        elif k == "recheck_days":
            v = max(0, min(90, int(v)))
        elif k == "recheck_epoch":
            v = max(0, int(v))
        elif k == "tracked_browsers":
            if v is not None:
                if not isinstance(v, list) or any(n not in BY_NAME for n in v):
                    raise ValueError("tracked_browsers must list known browsers")
                v = [n for n in BY_NAME if n in v]
        elif k == "open_with":  # the browsers links open in, all at once; one name (the old form) is a list of one
            v = [v] if isinstance(v, str) else v
            if v is not None:
                if not isinstance(v, list) or any(n not in BY_NAME and n != SYSTEM_DEFAULT for n in v):
                    raise ValueError("open_with must list known browsers or \"System default\"")
                # System default stands alone: it already means "whichever browser macOS opens links in".
                v = [SYSTEM_DEFAULT] if SYSTEM_DEFAULT in v else ([n for n in BY_NAME if n in v] or None)
        elif k == "save_mode":
            if v not in ("change", "interval"):
                raise ValueError("save_mode must be change or interval")
        elif k in ("autosave", "include_incognito", "onboarded_browsers"):
            v = bool(v)
        cur[k] = v
    c = db()
    try:
        for k, v in cur.items():
            c.execute("INSERT OR REPLACE INTO settings VALUES (?, ?)", (k, json.dumps(v)))
        c.commit()
    finally:
        c.close()
    return cur


# ---------- snapshots ----------
def _fingerprint(snap):
    tabs = [(b["browser"], w.get("mode"), w.get("index"), [t["url"] for t in w.get("tabs", [])], w.get("tab_count"))
            for b in snap["browsers"] for w in b.get("windows", [])]
    return hashlib.sha256(json.dumps(tabs, sort_keys=True).encode()).hexdigest()[:16]


def _store(c, snap_id, snap):
    if c.execute("SELECT 1 FROM snapshots WHERE id = ?", (snap_id,)).fetchone():
        return
    s = snap.get("summary") or summarise(snap.get("browsers", []))
    browsers = [b["browser"] for b in snap.get("browsers", []) if b.get("windows")]
    c.execute("INSERT INTO snapshots (id, at, reason, fingerprint, windows, tabs, incognito_windows, incognito_tabs, browsers)"
              " VALUES (?,?,?,?,?,?,?,?,?)",
              (snap_id, snap.get("at") or "", snap.get("reason") or "auto", snap.get("fingerprint") or _fingerprint(snap),
               s["windows"], s["tabs"], s["incognito_windows"], s["incognito_tabs"], json.dumps(browsers)))
    seq = 0
    for b in snap.get("browsers", []):
        for w in b.get("windows", []):
            tabs = w.get("tabs") or []
            n = len(tabs) if "tabs" in w else int(w.get("tab_count") or 0)
            c.execute("INSERT INTO windows VALUES (?,?,?,?,?,?,?,?)",
                      (snap_id, seq, b["browser"], w.get("index"), w.get("mode") or "normal", w.get("active"), n,
                       1 if "tabs" in w else 0))
            c.executemany("INSERT INTO tabs VALUES (?,?,?,?,?)",
                          [(snap_id, seq, k, t.get("url") or "", t.get("title") or "") for k, t in enumerate(tabs)])
            seq += 1


def path_of(snap_id):
    """Kept for callers of the file era: a valid id or None. Ids are `YYYY-MM-DD_HHMMSS`, nothing else."""
    return snap_id if re.fullmatch(r"\d{4}-\d{2}-\d{2}_\d{6}", snap_id or "") else None


def take(reason="manual", force=False):
    """Read the browsers and store a snapshot, unless nothing changed since the last one."""
    cfg = settings()
    live = read_all()
    readable = [b for b in live["browsers"] if b.get("windows")]
    if not readable:
        return {"stored": False, "why": "no readable browser windows", "live": redact(live, cfg["include_incognito"])}
    snap = redact(live, cfg["include_incognito"])
    snap["reason"] = reason
    snap["fingerprint"] = _fingerprint(snap)
    with _lock:
        c = db()
        try:
            last = c.execute("SELECT id, fingerprint FROM snapshots ORDER BY id DESC LIMIT 1").fetchone()
            if last and last["fingerprint"] == snap["fingerprint"] and not force:
                return {"stored": False, "why": "unchanged since the last snapshot", "id": last["id"], "live": snap}
            snap_id = time.strftime("%Y-%m-%d_%H%M%S")
            if c.execute("SELECT 1 FROM snapshots WHERE id = ?", (snap_id,)).fetchone():
                return {"stored": False, "why": "a snapshot was just taken", "id": snap_id, "live": snap}
            _store(c, snap_id, snap)
            c.commit()
        finally:
            c.close()
    housekeeping(cfg)
    return {"stored": True, "id": snap_id, "live": snap}


def housekeeping(cfg=None):
    """Hide snapshots older than keep_days (never deleted); purge incognito pages older than the chosen age."""
    cfg = cfg or settings()
    cutoff = time.strftime("%Y-%m-%d", time.localtime(time.time() - cfg["keep_days"] * 86400))
    c = db()
    try:
        c.execute("UPDATE snapshots SET hidden = 1 WHERE substr(id, 1, 10) < ? AND hidden = 0", (cutoff,))
        c.commit()
    finally:
        c.close()
    if cfg.get("incognito_purge_hours"):
        purge("pages", older_than_hours=cfg["incognito_purge_hours"])


def purge(level="pages", older_than_hours=0):
    """Forget incognito data. level "pages": links and titles go, the window and tab counts stay.
    level "windows": the incognito windows go entirely, and the counts with them. Normal windows are never touched."""
    if level not in ("pages", "windows"):
        raise ValueError("level must be pages or windows")
    where, args = "", []
    if older_than_hours:
        cutoff = time.strftime("%Y-%m-%d_%H%M%S", time.localtime(time.time() - older_than_hours * 3600))
        where, args = " AND w.snapshot_id < ?", [cutoff]
    with _lock:
        c = db()
        try:
            sel = f"SELECT w.snapshot_id, w.seq, w.tab_count FROM windows w WHERE w.mode = 'incognito'{where}"
            wins = c.execute(sel, args).fetchall()
            pages = 0
            for w in wins:
                pages += c.execute("DELETE FROM tabs WHERE snapshot_id = ? AND window_seq = ?", (w["snapshot_id"], w["seq"])).rowcount
                if level == "windows":
                    c.execute("DELETE FROM windows WHERE snapshot_id = ? AND seq = ?", (w["snapshot_id"], w["seq"]))
                    c.execute("UPDATE snapshots SET incognito_windows = incognito_windows - 1, incognito_tabs = incognito_tabs - ? WHERE id = ?",
                              (w["tab_count"], w["snapshot_id"]))
                else:
                    c.execute("UPDATE windows SET pages_kept = 0 WHERE snapshot_id = ? AND seq = ?", (w["snapshot_id"], w["seq"]))
            c.commit()
            # With write-ahead logging the old pages can sit in the -wal file; fold it back and truncate it now.
            c.execute("PRAGMA wal_checkpoint(TRUNCATE)")
        finally:
            c.close()
    _purge_imported_files(level)
    return {"level": level, "windows": len(wins), "pages": pages}


def _purge_imported_files(level):
    """The snapshot files from before the database are kept, so a purge must reach them too."""
    base = os.path.join(SESSIONS, "_imported")
    if not os.path.isdir(base):
        return
    for day in os.listdir(base):
        d = os.path.join(base, day)
        for fn in os.listdir(d) if os.path.isdir(d) else []:
            p = os.path.join(d, fn)
            try:
                with open(p) as f:
                    snap = json.load(f)
            except (OSError, ValueError):
                continue
            changed = False
            for b in snap.get("browsers", []):
                keep = []
                for w in b.get("windows", []):
                    if w.get("mode") == "incognito":
                        if level == "windows":
                            changed = True
                            continue
                        if "tabs" in w:
                            w["tab_count"] = len(w.pop("tabs")); w["redacted"] = True; changed = True
                    keep.append(w)
                b["windows"] = keep
            if changed:
                snap["summary"] = summarise(snap.get("browsers", []))
                with open(p + ".tmp", "w") as f:
                    json.dump(snap, f)
                os.replace(p + ".tmp", p)


def _browser_filter(browsers, col="browser"):
    """SQL for "only these browsers": ("", []) when browsers is None (all of them), a clause that matches nothing
    when it is an empty list (none), else an IN clause with its parameters."""
    if browsers is None:
        return "", []
    names = [str(b) for b in browsers][:20]
    if not names:
        return " AND 0", []
    return f" AND {col} IN ({','.join('?' * len(names))})", names


def _summaries(c, ids, browsers):
    """Per-snapshot counts recomputed from the window rows, for the chosen browsers only."""
    where, params = _browser_filter(browsers)
    out = {i: {"windows": 0, "tabs": 0, "incognito_windows": 0, "incognito_tabs": 0} for i in ids}
    if not ids:
        return out
    for r in c.execute(f"SELECT snapshot_id, mode, COUNT(*) w, SUM(tab_count) t FROM windows WHERE snapshot_id IN "
                       f"({','.join('?' * len(ids))}){where} GROUP BY snapshot_id, mode", [*ids, *params]):
        s = out[r["snapshot_id"]]
        if r["mode"] == "incognito":
            s["incognito_windows"] += r["w"]; s["incognito_tabs"] += r["t"] or 0
        else:
            s["windows"] += r["w"]; s["tabs"] += r["t"] or 0
    return out


def list_snapshots(limit=300, include_hidden=False, browsers=None):
    """Saved snapshots, newest first. browsers: None for all, or the names to count (an empty list counts none);
    the summary then covers those browsers only, and snapshots with none of their windows are left out."""
    c = db()
    try:
        rows = c.execute("SELECT * FROM snapshots" + ("" if include_hidden else " WHERE hidden = 0") +
                         " ORDER BY id DESC LIMIT ?", (limit,)).fetchall()
        kept = {r["snapshot_id"]: r["n"] for r in c.execute(
            "SELECT snapshot_id, COUNT(*) n FROM windows WHERE mode = 'incognito' AND pages_kept = 1 GROUP BY snapshot_id")}
        sums = _summaries(c, [r["id"] for r in rows], browsers) if browsers is not None else None
    finally:
        c.close()
    out = []
    for r in rows:
        summary = sums[r["id"]] if sums is not None else {"windows": r["windows"], "tabs": r["tabs"],
                                                           "incognito_windows": r["incognito_windows"], "incognito_tabs": r["incognito_tabs"]}
        if sums is not None and not (summary["windows"] or summary["incognito_windows"]):
            continue
        names = json.loads(r["browsers"])
        out.append({"id": r["id"], "at": r["at"], "reason": r["reason"],
                    "browsers": names if browsers is None else [b for b in names if b in browsers],
                    "incognito_pages_kept": bool(kept.get(r["id"])), "summary": summary})
    return out


def get_snapshot(snap_id):
    """A snapshot in the same shape as a live read: browsers → windows → tabs."""
    if not path_of(snap_id):
        return None
    c = db()
    try:
        s = c.execute("SELECT * FROM snapshots WHERE id = ?", (snap_id,)).fetchone()
        if not s:
            return None
        wins = c.execute("SELECT * FROM windows WHERE snapshot_id = ? ORDER BY seq", (snap_id,)).fetchall()
        tabs_by = {}
        for t in c.execute("SELECT window_seq, url, title FROM tabs WHERE snapshot_id = ? ORDER BY window_seq, pos", (snap_id,)):
            tabs_by.setdefault(t["window_seq"], []).append({"url": t["url"], "title": t["title"]})
    finally:
        c.close()
    browsers = {}
    for w in wins:
        win = {"index": w["idx"], "mode": w["mode"], "active": w["active"]}
        if w["pages_kept"]:
            win["tabs"] = tabs_by.get(w["seq"], [])
        else:
            win["tab_count"] = w["tab_count"]; win["redacted"] = True
        browsers.setdefault(w["browser"], {"browser": w["browser"], "running": True, "windows": []})["windows"].append(win)
    return {"at": s["at"], "reason": s["reason"], "fingerprint": s["fingerprint"], "browsers": list(browsers.values()),
            "summary": {"windows": s["windows"], "tabs": s["tabs"], "incognito_windows": s["incognito_windows"],
                        "incognito_tabs": s["incognito_tabs"]}}


def timeline(days=30, limit_items=40, browsers=None):
    """Snapshots oldest first within `days`, each with what opened and closed since the snapshot before it.
    Tabs are compared by address, per mode (a page moving from normal to incognito counts as closed and opened).
    Incognito pages appear in the lists only where they were kept; otherwise only the counts change."""
    since = time.strftime("%Y-%m-%d_%H%M%S", time.localtime(time.time() - days * 86400)) if days else "0"
    where, params = _browser_filter(browsers, "w.browser")
    c = db()
    try:
        snaps = c.execute("SELECT * FROM snapshots WHERE id >= ? ORDER BY id", (since,)).fetchall()
        prev_row = c.execute("SELECT id FROM snapshots WHERE id < ? ORDER BY id DESC LIMIT 1", (since,)).fetchone()
        ids = ([prev_row["id"]] if prev_row else []) + [s["id"] for s in snaps]
        sums = _summaries(c, [s["id"] for s in snaps], browsers) if browsers is not None else None
        tabs_of = {}
        for i in ids:
            rows = c.execute("SELECT w.mode, t.url, t.title FROM tabs t JOIN windows w ON w.snapshot_id = t.snapshot_id AND w.seq = t.window_seq WHERE t.snapshot_id = ?" + where, (i, *params)).fetchall()
            tabs_of[i] = {(r["mode"], r["url"]): r["title"] for r in rows}
    finally:
        c.close()
    out, prev = [], (tabs_of.get(prev_row["id"]) if prev_row else None)
    for s in snaps:
        cur = tabs_of.get(s["id"], {})
        summary = sums[s["id"]] if sums is not None else {"windows": s["windows"], "tabs": s["tabs"], "incognito_windows": s["incognito_windows"], "incognito_tabs": s["incognito_tabs"]}
        if sums is not None and not (summary["windows"] or summary["incognito_windows"]):
            prev = cur
            continue  # none of the chosen browsers had a window open then
        names = json.loads(s["browsers"])
        item = {"id": s["id"], "at": s["at"], "reason": s["reason"],
                "browsers": names if browsers is None else [b for b in names if b in browsers], "summary": summary}
        if prev is not None:
            opened = [k for k in cur if k not in prev]
            closed = [k for k in prev if k not in cur]
            item["opened_n"], item["closed_n"] = len(opened), len(closed)
            item["opened"] = [{"mode": m, "url": u, "title": cur[(m, u)]} for m, u in opened[:limit_items]]
            item["closed"] = [{"mode": m, "url": u, "title": prev[(m, u)]} for m, u in closed[:limit_items]]
        out.append(item)
        prev = cur
    return {"days": days, "browsers": browsers, "snapshots": out}


# ---------- deleting history (the owner asked for these; each is confirmed in the page first) ----------
def delete_snapshots(ids=None, before=None, everything=False):
    """Delete snapshots for good: a list of ids, everything before a date (YYYY-MM-DD), or everything.
    Rows are overwritten (secure_delete) and the write-ahead log is truncated. Notes stay; they are keyed by address."""
    with _lock:
        c = db()
        try:
            if everything:
                sel = c.execute("SELECT id FROM snapshots").fetchall()
            elif before:
                sel = c.execute("SELECT id FROM snapshots WHERE substr(id, 1, 10) < ?", (before,)).fetchall()
            else:
                sel = [{"id": i} for i in (ids or []) if path_of(i)]
            gone = [r["id"] for r in sel]
            for i in gone:
                c.execute("DELETE FROM tabs WHERE snapshot_id = ?", (i,))
                c.execute("DELETE FROM windows WHERE snapshot_id = ?", (i,))
                c.execute("DELETE FROM snapshots WHERE id = ?", (i,))
            c.commit()
            c.execute("PRAGMA wal_checkpoint(TRUNCATE)")
        finally:
            c.close()
    files = 0
    if everything:  # the pre-database snapshot files hold the same history: they go too
        base = os.path.join(SESSIONS, "_imported")
        if os.path.isdir(base):
            for day in os.listdir(base):
                d = os.path.join(base, day)
                for fn in (os.listdir(d) if os.path.isdir(d) else []):
                    os.remove(os.path.join(d, fn)); files += 1
                if os.path.isdir(d):
                    os.rmdir(d)
    return {"deleted": len(gone), "files": files}


# ---------- how long each open tab has been open ----------
def first_seen(urls, include_incognito=False):
    """For each address, the earliest snapshot it appears in (normal windows; incognito only if pages are kept)."""
    urls = [u for u in urls if isinstance(u, str)][:5000]
    if not urls:
        return {}
    c = db()
    try:
        out = {}
        for k in range(0, len(urls), 500):
            chunk = urls[k:k + 500]
            q = ("SELECT t.url, MIN(s.at) first FROM tabs t JOIN windows w ON w.snapshot_id = t.snapshot_id AND w.seq = t.window_seq "
                 "JOIN snapshots s ON s.id = t.snapshot_id WHERE t.url IN (%s)%s GROUP BY t.url") % (
                ",".join("?" * len(chunk)), "" if include_incognito else " AND w.mode = 'normal'")
            for r in c.execute(q, chunk):
                out[r["url"]] = r["first"]
    finally:
        c.close()
    return out


# ---------- notes and tags on tabs (by address), windows (by browser and window id) and snapshots ----------
def set_note(kind, key, note="", tags=()):
    tags = sorted({t.strip()[:40] for t in tags if isinstance(t, str) and t.strip()})[:20]
    c = db()
    try:
        if not note.strip() and not tags:
            c.execute("DELETE FROM notes WHERE kind = ? AND key = ?", (kind, key))
        else:
            c.execute("INSERT OR REPLACE INTO notes VALUES (?, ?, ?, ?, ?)",
                      (kind, key, note.strip()[:2000], json.dumps(tags), time.strftime("%Y-%m-%dT%H:%M:%S%z")))
        c.commit()
    finally:
        c.close()
    return {"kind": kind, "key": key, "note": note.strip()[:2000], "tags": tags}


def notes(kind=None):
    c = db()
    try:
        rows = c.execute("SELECT * FROM notes" + (" WHERE kind = ?" if kind else "") + " ORDER BY updated DESC", (kind,) if kind else ()).fetchall()
    finally:
        c.close()
    return [{"kind": r["kind"], "key": r["key"], "note": r["note"], "tags": json.loads(r["tags"]), "updated": r["updated"]} for r in rows]


def stats():
    c = db()
    try:
        one = lambda q: c.execute(q).fetchone()[0]  # noqa: E731
        out = {"snapshots": one("SELECT COUNT(*) FROM snapshots WHERE hidden = 0"),
               "hidden": one("SELECT COUNT(*) FROM snapshots WHERE hidden = 1"),
               "tabs_stored": one("SELECT COUNT(*) FROM tabs"),
               "incognito_pages_stored": one("SELECT COUNT(*) FROM tabs t JOIN windows w ON w.snapshot_id = t.snapshot_id AND w.seq = t.window_seq WHERE w.mode = 'incognito'"),
               "incognito_windows_stored": one("SELECT COUNT(*) FROM windows WHERE mode = 'incognito'")}
    finally:
        c.close()
    out["db_bytes"] = os.path.getsize(DB_PATH) if os.path.exists(DB_PATH) else 0
    return out


# ---------- autosave ----------
_auto = {"thread": None, "last": None, "last_stored": None, "last_error": None}


def start_autosave():
    """A background thread that snapshots every `interval` seconds while autosave is on, then does housekeeping."""
    if _auto["thread"]:
        return

    def loop():
        last_house = 0
        while True:
            cfg = settings()
            try:
                if cfg["autosave"]:
                    r = take("auto")  # stores only when something changed since the last snapshot
                    _auto["last"] = time.time()
                    if r.get("stored"):
                        _auto["last_stored"] = time.time()
                    _auto["last_error"] = None if r.get("stored") or r.get("why", "").startswith(("unchanged", "a snapshot")) else r.get("why")
                elif time.time() - last_house > 600:
                    housekeeping(cfg); last_house = time.time()
            except Exception as e:  # noqa: BLE001 — the loop must never die
                _auto["last_error"] = f"{type(e).__name__}: {e}"[:200]
            time.sleep(cfg["watch_seconds"] if cfg["autosave"] and cfg["save_mode"] == "change" else cfg["interval"])
    t = threading.Thread(target=loop, daemon=True, name="tabs-autosave")
    t.start()
    _auto["thread"] = t


def status():
    return {"settings": settings(), "last_auto": _auto["last"], "last_stored": _auto["last_stored"], "last_error": _auto["last_error"], **stats()}


if __name__ == "__main__":
    if "--save" in sys.argv:
        r = take("manual")
        print(json.dumps({k: r.get(k) for k in ("stored", "why", "id")}))
    elif "--stats" in sys.argv:
        print(json.dumps(stats(), indent=2))
    else:
        print(json.dumps(read_all(), indent=2))
