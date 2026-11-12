"""Serve the bookmarks preview, plus a refresh endpoint that re-checks links and re-takes snapshots.

Usage: python3 src/server/serve.py [port]   (serves Browser Foreman from the bookmarks-cleanup folder, on 127.0.0.1 only)

POST /api/refresh {"items": [{"id": 12, "u": "https://…"}]}  -> {"job": "…"}
GET  /api/job/<job>                                          -> progress and results
Results are appended to work/refresh-overrides.csv, which build.py applies on the next rebuild.

Workspaces (one bookmark set each, with its history), import and export are served by src/server/ws_api.py; every POST
needs the per-run token, sent as X-BM-Token. The page gets it embedded when it is served from here.
"""
import csv
import hashlib
import json
import urllib.parse
import posixpath
import os
import subprocess
import sys
import tempfile
import threading
import time
import signal
import uuid
from concurrent.futures import FIRST_COMPLETED, ThreadPoolExecutor, wait
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer

sys.path.insert(0, os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "lib"))
from config import env  # noqa: E402
import paths  # noqa: E402,F401  src/lib/paths.py: every folder, .env, and the src/ folders on the import path
import check_links as cl  # noqa: E402
import history_access  # noqa: E402  Settings › Browser history: Full Disk Access check
import tabs  # noqa: E402
import rechecks  # noqa: E402
from validate import DAY, RUN_ID, SNAPSHOT_ID, Invalid, V  # noqa: E402

# Request bodies, one rule each (src/lib/validate.py). Unknown fields are refused.
CLOSE_BODY = V.obj(items=V.list(V.obj(browser=V.enum(*sorted(tabs.CLOSABLE)), window_id=V.int().optional(),
                                      tab_id=V.int().optional(), pos=V.int(0, 100000), url=V.url()), max_items=1000, min_items=1))
DELETE_BODY = V.obj(ids=V.list(SNAPSHOT_ID, max_items=5000).optional(), before=DAY.optional(),
                    everything=V.bool().optional(False), confirm=V.str(0, 40).optional("")).where(
    lambda b: sum([bool(b["ids"]), bool(b["before"]), b["everything"]]) == 1, "give exactly one of ids, before, everything").where(
    lambda b: not b["everything"] or b["confirm"] == "DELETE ALL", 'wiping everything needs confirm: "DELETE ALL"')
AGES_BODY = V.obj(urls=V.list(V.str(1, 4096), max_items=5000))
NOTE_BODY = V.obj(kind=V.enum("tab", "window", "snapshot"), key=V.str(1, 4096), note=V.str(0, 2000).optional(""),
                  tags=V.list(V.str(1, 40), max_items=20).optional([]))
import ws_api  # noqa: E402  workspaces, history, import and export (src/server/ws_api.py)
from hostlimit import interleave  # noqa: E402

ROOT = paths.ROOT
PIPELINE = os.path.join(paths.SRC, "pipeline")
OVERRIDES = os.path.join(paths.WORK, "refresh-overrides.csv")
NODE = next((p for p in ("/usr/local/bin/node", "/opt/homebrew/bin/node") if os.path.exists(p)), "node")
MAX_ITEMS = 8000
JOBS, LOCK, CSV_LOCK = {}, threading.Lock(), threading.Lock()


def thumb_id(url):
    return hashlib.sha1(url.encode()).hexdigest()[:16]


def record(rows):
    with CSV_LOCK:
        new = not os.path.exists(OVERRIDES)
        with open(OVERRIDES, "a", newline="") as f:
            w = csv.writer(f)
            if new:
                w.writerow(["url", "status", "code", "final_url", "note"])
            w.writerows(rows)


def _gate(job):
    """Pause and cancel points: a paused job waits here; a cancelled one stops taking new work."""
    while job["state"] == "paused":
        time.sleep(0.3)
    return job["state"] != "cancelled"


def run_job(job_id, items):
    """Wrapper: whatever goes wrong, the job ends and says why, so the page never waits on it for ever."""
    job = JOBS[job_id]
    try:
        _run_job(job, items)
    except Exception as e:  # noqa: BLE001
        job["error"] = f"{type(e).__name__}: {e}"[:200]
        job.pop("_proc", None)
        job["phase"] = job["state"] = "failed"
        job["done"] = True


def _run_job(job, items):
    renderable = []

    def one(it):
        if not _gate(job):
            return None
        st, code, final, note = cl.check(it["u"])
        return it, st, code, final, note
    rows = []
    order = {u: k for k, u in enumerate(interleave([i["u"] for i in items]))}
    items = sorted(items, key=lambda i: order[i["u"]])  # round-robin by site, never a burst at one site
    # Collected as they finish, looking at the cancel flag twice a second, so a cancel never waits on a slow site.
    ex = ThreadPoolExecutor(6)
    pending = {ex.submit(one, it) for it in items}
    while pending and job["state"] != "cancelled":
        finished, pending = wait(pending, timeout=0.5, return_when=FIRST_COMPLETED)
        for fut in finished:
            try:
                res = fut.result()
            except Exception:  # one bad link never ends the job
                job["checked"] += 1
                continue
            if res is None:
                continue
            it, st, code, final, note = res
            note = (note + " (refreshed " + time.strftime("%Y-%m-%d") + ")").strip()
            job["results"][str(it["id"])] = {"s": st, "n": note, "final": final,
                                             "h": thumb_id(it["u"]) if st not in ("dead", "local", "skipped") else None}
            rows.append([it["u"], st, code, final, note])
            if st not in ("dead", "local", "skipped"):
                renderable.append({"h": thumb_id(it["u"]), "u": it["u"]})
            job["checked"] += 1
    ex.shutdown(wait=False, cancel_futures=True)  # checks still in flight finish on their own; nothing waits for them
    record(rows)
    job["phase"] = "snapshots"
    job["render_total"] = len(renderable)
    while job["state"] == "paused":  # a pause between checking and snapshots holds here
        time.sleep(0.3)
    if renderable and job["state"] != "cancelled":
        with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False) as f:
            json.dump(renderable, f)
            lst = f.name
        env = dict(os.environ, FORCE="1", NO_STATE_FILES="1")
        proc = subprocess.Popen([NODE, os.path.join(PIPELINE, "render.js"), lst, paths.THUMBS, "6"], cwd=paths.ROOT, env=env,
                                stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, start_new_session=True)
        job["_proc"] = proc
        if job["state"] == "paused":  # paused before the renderer existed: hold it now
            _signal(proc, signal.SIGSTOP)
        elif job["state"] == "cancelled":
            _signal(proc, signal.SIGTERM)
        for line in proc.stdout:
            if line.startswith("shot "):
                job["rendered"] += 1
        proc.wait()
        os.unlink(lst)
    job.pop("_proc", None)
    job["phase"] = "cancelled" if job["state"] == "cancelled" else "done"
    job["state"] = job["phase"]
    job["done"] = True


# The page answers at / and /index.html; /preview.html is its old address, kept so bookmarks to it still open.
PAGE_URLS = ("/", "/index.html", "/preview.html")


class Handler(SimpleHTTPRequestHandler):
    def __init__(self, *a, **kw):
        super().__init__(*a, directory=paths.PUBLIC, **kw)

    def translate_path(self, path):
        """URL → file, only through src/lib/paths.py: the page, and the folders in paths.SERVED. _static_ok has
        already refused everything else, so nothing outside those folders can be named here."""
        norm = posixpath.normpath(urllib.parse.unquote(urllib.parse.urlsplit(path).path))
        if norm in PAGE_URLS:
            return paths.PAGE
        for prefix, folder in paths.SERVED.items():
            if norm.startswith(prefix):
                return os.path.join(folder, *norm[len(prefix):].split("/"))
        return os.path.join(paths.PUBLIC, "_missing")

    def log_message(self, *a):
        pass

    def end_headers(self):
        # The page, its data and the workspace client change with every build or update: never cache them.
        fresh = self.path.startswith(("/api/", "/assets/js/")) or self.path.split("?")[0] in PAGE_URLS
        self.send_header("Cache-Control", "no-store" if fresh else "max-age=60")
        # Never sniff a type, never be framed by another site, never leak the address to linked sites.
        self.send_header("X-Content-Type-Options", "nosniff")
        self.send_header("X-Frame-Options", "DENY")
        self.send_header("Content-Security-Policy", "frame-ancestors 'none'")
        self.send_header("Referrer-Policy", "no-referrer")
        super().end_headers()

    def list_directory(self, path):  # no folder listings, ever
        self.send_error(404)
        return None

    def _static_ok(self):
        """Only these files are served, and only to a page addressed to this computer. Without the Host check a site
        in another tab could reach this server through DNS rebinding and read every file here."""
        if not ws_api.host_ok(self):
            self.send_error(403)
            return False
        raw = urllib.parse.unquote(urllib.parse.urlsplit(self.path).path)
        norm = posixpath.normpath(raw)
        if norm == ".":
            norm = "/"
        ok = (norm in PAGE_URLS
              or (norm.startswith(tuple(paths.SERVED)) and ".." not in norm.split("/")
                  and not any(part.startswith(".") for part in norm.split("/")[1:])
                  and (norm.endswith((".css", ".woff2", ".woff", ".ttf", ".jpg", ".png", ".ico", ".svg", ".gif", ".webp"))
                       or (norm.startswith("/assets/js/") and norm.endswith((".js", ".js.map"))))))
        if not ok:
            self.send_error(404)
            return False
        self.path = norm
        return True

    def _json(self, code, obj):
        body = json.dumps(obj).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _local_only(self):
        # Refuse requests from other pages (a site in another tab must not be able to drive this).
        origin = self.headers.get("Origin")
        host = self.headers.get("Host", "")
        if not host.startswith(("localhost:", "127.0.0.1:")):
            return False
        return origin is None or origin in (f"http://{host}",)

    def do_GET(self):
        if ws_api.handle_get(self):  # also refuses any /api/ read not addressed to localhost
            return
        if self.path.startswith("/api/job/"):
            if not ws_api.host_ok(self):
                return self._json(403, {"error": "forbidden"})
            job = JOBS.get(self.path.rsplit("/", 1)[-1])
            return self._json(200, {k: v for k, v in job.items() if not k.startswith("_")}) if job else self._json(404, {"error": "no such job"})
        if self.path == "/api/ping":
            return self._json(200, {"ok": True})
        if self.path.startswith("/api/tabs"):
            return self._tabs_get()
        if self.path.startswith("/api/rechecks"):
            return self._rechecks_get()
        if self.path == "/api/history/access":  # which browsers' history this server may read (counts nothing)
            return self._json(200, history_access.status())
        if self.path.startswith("/api/"):
            return self._json(404, {"error": "not found"})
        if self._static_ok():
            return super().do_GET()

    def do_HEAD(self):
        if self._static_ok():
            return super().do_HEAD()

    def do_POST(self):
        if ws_api.handle_post(self):
            return
        if self.path == "/api/history/access/open-settings":
            if not self._local_only() or not ws_api.post_allowed(self):
                return self._json(403, {"error": "forbidden"})
            return self._json(200, history_access.open_settings())
        if self.path in ("/api/rechecks/run", "/api/rechecks/cancel"):
            if not self._local_only() or not ws_api.post_allowed(self):
                return self._json(403, {"error": "forbidden"})
            return self._json(200, rechecks.start("manual") if self.path.endswith("/run") else rechecks.cancel())
        if self.path in ("/api/tabs/snapshot", "/api/tabs/settings", "/api/tabs/purge", "/api/tabs/close",
                         "/api/tabs/delete", "/api/tabs/ages", "/api/tabs/note"):
            if not self._local_only() or not ws_api.post_allowed(self):
                return self._json(403, {"error": "forbidden"})
            return self._tabs_post()
        ctl = self.path.startswith("/api/job/") and self.path.rsplit("/", 1)[-1] in ("pause", "resume", "cancel")
        if self.path not in ("/api/refresh", "/api/open") and not ctl:
            return self._json(404, {"error": "not found"})
        # Local origin, JSON, and the run's token (embedded only in the page this server serves).
        if not self._local_only() or not ws_api.post_allowed(self):
            return self._json(403, {"error": "forbidden"})
        if self.path == "/api/open":
            return self._open()
        if ctl:
            return self._control()
        try:
            data = _read_json(self)
            items = [{"id": int(i["id"]), "u": str(i["u"])} for i in data["items"]
                     if str(i.get("u", "")).startswith(("http://", "https://"))][:MAX_ITEMS]
        except (ValueError, KeyError, TypeError):
            return self._json(400, {"error": "bad request"})
        job_id = uuid.uuid4().hex[:12]
        with LOCK:
            JOBS[job_id] = {"total": len(items), "checked": 0, "rendered": 0, "render_total": 0, "phase": "checking",
                            "state": "running", "started": time.time(), "done": False, "results": {}}
        threading.Thread(target=run_job, args=(job_id, items), daemon=True).start()
        return self._json(200, {"job": job_id, "total": len(items)})


def _read_json(h, limit=8 * 1024 * 1024):
    """A request body as JSON, refusing a missing, negative or oversized length (a negative one would block a thread)."""
    n = int(h.headers.get("Content-Length") or 0)
    if n <= 0 or n > limit:
        raise ValueError("bad length")
    return json.loads(h.rfile.read(n))


def _signal(proc, sig):
    try:
        os.killpg(proc.pid, sig)
    except (ProcessLookupError, PermissionError):
        pass  # the renderer already exited


MAX_OPEN = 200
OPEN_BODY = V.obj(urls=V.list(V.url(), max_items=MAX_OPEN, min_items=1), how=V.enum("tabs", "window", "incognito").optional("tabs"),
                  browser=V.str(1, 60).optional(), browsers=V.list(V.str(1, 60), max_items=12).optional())


class OpenRefused(ValueError):
    pass


def _open_in(urls, how, browser=None):
    """Ask the browser itself to open the links: as tabs, in a new window, or in a new incognito/private window, using
    that browser's own launch option. A web page cannot do this; the browser's launch options can (tab groups need the
    extension). The browser is the one asked for, else the chosen ones (Browsers › Opens links), else the system's
    default browser. "System default" resolves to the browser macOS opens links in."""
    installed = {b["name"]: b for b in tabs.installed_browsers()}
    name = browser or next(iter(tabs.open_targets()), None)
    if name == tabs.SYSTEM_DEFAULT:
        name = tabs.default_browser()
        if not name:  # a default this tool does not know: macOS opens the links in it, as tabs, with no window options
            if how == "incognito":
                raise OpenRefused("The system default browser cannot be asked for a private window; choose a browser for incognito")
            return _launch(["open", *urls]), tabs.SYSTEM_DEFAULT
    b = installed.get(name)
    if not b:
        raise OpenRefused(f"{name or 'No supported browser'} is not installed")
    app = b["path"]
    if how == "tabs":
        cmd = ["open", "-a", app, *urls]
    elif how == "window":
        cmd = ["open", "-na", app, "--args", "--new-window", *urls] if b["engine"] in ("chromium", "gecko") else ["open", "-a", app, *urls]
    else:
        if not b["private_flag"]:
            raise OpenRefused(f"{b['name']} cannot be asked to open a private window; choose another browser for incognito")
        cmd = ["open", "-na", app, "--args", b["private_flag"], *urls]
    return _launch(cmd), b["name"]


LAUNCH_WAIT = 1.5  # seconds: long enough to catch an immediate refusal, short enough that a cold start never blocks


def _launch(cmd):
    """Start `open` and wait only briefly. `open` returns once the browser has taken the links, which for a browser that
    is not running means its whole start-up: waiting for that made every open as slow as the slowest browser (seen
    2026-11-12: links opening in Chrome, Brave and Safari waited for Brave to start from cold). A launch still running
    after LAUNCH_WAIT is under way and is reported as opened."""
    p = subprocess.Popen(cmd, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, text=True, start_new_session=True)
    try:
        _, err = p.communicate(timeout=LAUNCH_WAIT)
        return subprocess.CompletedProcess(cmd, p.returncode, "", err)
    except subprocess.TimeoutExpired:
        p.stderr.close()  # the launch carries on; nothing here reads it any more
        return subprocess.CompletedProcess(cmd, 0, "", "")


def _open_many(urls, how, names):
    """Open the same links in several browsers at the same time. Each browser answers on its own: one that cannot
    (not installed, no private window) is reported and the others still open."""
    def one(name):
        try:
            p, used = _open_in(urls, how, name)
        except OpenRefused as e:
            return {"browser": name, "ok": False, "error": str(e)}
        if p.returncode:
            return {"browser": used, "ok": False, "error": (p.stderr or p.stdout).strip()[:200] or f"{used} did not start"}
        return {"browser": used, "ok": True}
    with ThreadPoolExecutor(max_workers=max(1, len(names))) as pool:
        return list(pool.map(one, names))


def _open_handler(self):
    try:
        b = OPEN_BODY.check(_read_json(self))
    except (Invalid, ValueError) as e:
        return self._json(400, {"error": str(e) or "bad request"})
    names = [b["browser"]] if b["browser"] else (list(dict.fromkeys(b["browsers"] or [])) or tabs.open_targets())
    if not names:
        return self._json(400, {"error": "No supported browser is installed"})
    results = _open_many(b["urls"], b["how"], names)
    ok = [r["browser"] for r in results if r["ok"]]
    failed = [r for r in results if not r["ok"]]
    if not ok:
        return self._json(400 if all("cannot" in r["error"] or "not installed" in r["error"] for r in failed) else 500,
                          {"error": "; ".join(r["error"] for r in failed), "failed": failed})
    return self._json(200, {"opened": len(b["urls"]), "how": b["how"], "browser": ok[0], "browsers": ok, "failed": failed})


Handler._open = _open_handler


def _control_handler(self):
    """Pause, resume or cancel a refresh. Snapshots pause by stopping the renderer's process group, so Chrome
    holds still too; cancel ends it. Results gathered before a cancel are kept."""
    _, _, _, job_id, verb = self.path.split("/")
    job = JOBS.get(job_id)
    if not job or job.get("done"):
        return self._json(404, {"error": "no running job"})
    proc = job.get("_proc")
    if verb == "pause" and job["state"] == "running":
        job["state"] = "paused"
        if proc:
            _signal(proc, signal.SIGSTOP)
    elif verb == "resume" and job["state"] == "paused":
        job["state"] = "running"
        if proc:
            _signal(proc, signal.SIGCONT)
    elif verb == "cancel":
        job["state"] = "cancelled"
        if proc:
            _signal(proc, signal.SIGCONT)
            _signal(proc, signal.SIGTERM)
    return self._json(200, {"state": job["state"]})


Handler._control = _control_handler


# ---------- open tabs: live read, snapshots, settings (src/server/tabs.py does the work) ----------
def _scope(query):
    """?browsers=Google Chrome,Safari → those names; ?browsers= → none; absent → None (all browsers)."""
    q = urllib.parse.parse_qs(query, keep_blank_values=True)
    if "browsers" not in q:
        return None
    raw = (q["browsers"][0] or "").strip()
    return [n for n in (x.strip() for x in raw.split(",")) if n and len(n) <= 60][:20] if raw else []


def _tabs_get(self):
    path = urllib.parse.urlsplit(self.path).path
    scope = _scope(urllib.parse.urlsplit(self.path).query)
    cfg = tabs.settings()
    if path == "/api/tabs":
        return self._json(200, tabs.redact(tabs.read_all(), cfg["include_incognito"]))
    if path == "/api/tabs/timeline":
        q = urllib.parse.parse_qs(urllib.parse.urlsplit(self.path).query)
        try:
            days = max(0, min(3650, int((q.get("days") or ["30"])[0])))
        except ValueError:
            days = 30
        return self._json(200, tabs.timeline(days, browsers=scope))
    if path == "/api/tabs/browsers":
        cfg = tabs.settings()
        return self._json(200, {"installed": [{k: v for k, v in b.items() if k != "path"} for b in tabs.installed_browsers(refresh=True)],
                                "tracked": tabs.tracked_names(), "open_with": tabs.open_targets(), "onboarded": cfg.get("onboarded_browsers"),
                                "system_default": tabs.default_browser()})
    if path == "/api/tabs/browser-icon":  # ?name=Google Chrome: that installed browser's own app icon, as PNG
        name = (urllib.parse.parse_qs(urllib.parse.urlsplit(self.path).query).get("name") or [""])[0][:60]
        png = tabs.browser_icon(name)
        if not png:
            return self._json(404, {"error": "no icon for that browser"})
        self.send_response(200)
        self.send_header("Content-Type", "image/png")
        self.send_header("Content-Length", str(len(png)))
        self.send_header("Cache-Control", "max-age=86400")
        self.end_headers()
        self.wfile.write(png)
        return True
    if path == "/api/tabs/notes":
        return self._json(200, {"notes": tabs.notes()})
    if path == "/api/tabs/status":
        return self._json(200, tabs.status())
    if path == "/api/tabs/snapshots":
        return self._json(200, {"snapshots": tabs.list_snapshots(browsers=scope)})
    if path.startswith("/api/tabs/snapshots/"):
        snap = tabs.get_snapshot(path.rsplit("/", 1)[-1])
        return self._json(200, snap) if snap else self._json(404, {"error": "no such snapshot"})
    return self._json(404, {"error": "not found"})


def _tabs_post(self):
    try:
        data = _read_json(self, 64 * 1024)
    except ValueError:
        data = {}
    if self.path == "/api/tabs/settings":
        try:
            return self._json(200, {"settings": tabs.save_settings(data)})
        except (TypeError, ValueError):
            return self._json(400, {"error": "bad settings"})
    try:
        if self.path == "/api/tabs/close":
            return self._json(200, tabs.close_tabs(CLOSE_BODY.check(data)["items"]))
        if self.path == "/api/tabs/delete":
            b = DELETE_BODY.check(data)
            return self._json(200, tabs.delete_snapshots(ids=b["ids"], before=b["before"], everything=b["everything"]))
        if self.path == "/api/tabs/ages":
            return self._json(200, {"first_seen": tabs.first_seen(AGES_BODY.check(data)["urls"], tabs.settings()["include_incognito"])})
        if self.path == "/api/tabs/note":
            b = NOTE_BODY.check(data)
            return self._json(200, tabs.set_note(b["kind"], b["key"], b["note"], b["tags"]))
    except Invalid as e:
        return self._json(400, {"error": str(e)})
    if self.path == "/api/tabs/purge":
        try:
            return self._json(200, tabs.purge(str(data.get("level", "pages")), int(data.get("older_than_hours") or 0)))
        except (TypeError, ValueError) as e:
            return self._json(400, {"error": str(e)})
    return self._json(200, tabs.take("manual"))


def _rechecks_get(self):
    path = urllib.parse.urlsplit(self.path).path
    if path == "/api/rechecks":
        return self._json(200, rechecks.status())
    try:
        run_id = RUN_ID.check(path.rsplit("/", 1)[-1], "run")
    except Invalid as e:
        return self._json(404, {"error": str(e)})
    r = rechecks.report(run_id)
    return self._json(200, r) if r else self._json(404, {"error": "no such re-check"})


Handler._rechecks_get = _rechecks_get
Handler._tabs_get = _tabs_get
Handler._tabs_post = _tabs_post


if __name__ == "__main__":
    port = int(sys.argv[1]) if len(sys.argv) > 1 else env.int("BM_PORT", 8765)
    print(f"serving {ROOT} on http://localhost:{port}/ (workspaces in {ws_api.wsm.ws_root()})", flush=True)
    # Background work that reads the browsers or the network: off for test and throwaway servers
    # (BM_NO_TAB_AUTOSAVE=1), so they never write snapshots or start re-checks in the real database.
    if os.environ.get("BM_NO_TAB_AUTOSAVE") != "1":
        tabs.start_autosave()  # snapshots open tabs as they change (Open tabs › settings)
        rechecks.start_scheduler()  # weekly link re-checks by default (0 turns them off)
    else:
        print("tab autosave and link re-checks are off (BM_NO_TAB_AUTOSAVE=1)", flush=True)
    ThreadingHTTPServer(("127.0.0.1", port), Handler).serve_forever()
