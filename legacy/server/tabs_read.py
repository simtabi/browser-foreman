"""Read-only access to the saved open tabs (sessions/tabs.sqlite3, written by src/server/tabs.py) for the MCP server.

tabs.db() creates the schema, imports old files and sets the journal mode, so it writes. Opening through it would
make a read-only MCP server change files. This module opens the database read-only (SQLite `mode=ro`), runs only
SELECTs, and creates nothing when there is no database yet.

Incognito pages: returned only while the owner's current setting `include_incognito` is on. A snapshot taken while
it was on still has its incognito pages stored, but once the setting is off they come back as counts only, the same
redaction tabs.py applies to a live read.
"""
import json
import os
import re
import sqlite3

import tabs

SNAP_ID = re.compile(r"^\d{4}-\d{2}-\d{2}_\d{6}$")


def db_path():
    return os.environ.get("BM_TABS_DB") or tabs.DB_PATH


def _connect():
    p = db_path()
    if not os.path.isfile(p):
        return None
    try:
        c = sqlite3.connect(f"file:{p}?mode=ro", uri=True, timeout=10)
        c.execute("SELECT 1 FROM snapshots LIMIT 1")
    except sqlite3.Error:
        # A WAL database whose writer is not running has no -shm file, which mode=ro cannot create: read it as a
        # fixed file instead (nothing is writing it at that moment).
        c = sqlite3.connect(f"file:{p}?mode=ro&immutable=1", uri=True, timeout=10)
    c.row_factory = sqlite3.Row
    return c


def include_incognito(c):
    try:
        row = c.execute("SELECT value FROM settings WHERE key = 'include_incognito'").fetchone()
    except sqlite3.Error:
        return False
    return bool(json.loads(row["value"])) if row else bool(tabs.DEFAULTS["include_incognito"])


def _summary(r):
    return {k: r[k] for k in ("windows", "tabs", "incognito_windows", "incognito_tabs")}


def list_snapshots(limit=50):
    c = _connect()
    if c is None:
        return {"snapshots": [], "note": "No tab snapshots yet: the preview server takes them while it runs."}
    try:
        rows = c.execute("SELECT * FROM snapshots WHERE hidden = 0 ORDER BY id DESC LIMIT ?", (int(limit),)).fetchall()
    finally:
        c.close()
    return {"snapshots": [{"id": r["id"], "at": r["at"], "reason": r["reason"], "browsers": json.loads(r["browsers"]),
                           "summary": _summary(r)} for r in rows]}


def read_snapshot(snap_id, query=None):
    """One snapshot: browsers → windows → tabs. `query` keeps only tabs whose title or address contain every word."""
    if not isinstance(snap_id, str) or not SNAP_ID.match(snap_id):
        raise ValueError("a snapshot id looks like 2026-11-12_143000")
    c = _connect()
    if c is None:
        raise ValueError("no tab snapshots yet")
    try:
        s = c.execute("SELECT * FROM snapshots WHERE id = ?", (snap_id,)).fetchone()
        if not s:
            raise ValueError(f"no snapshot {snap_id}")
        keep_incognito = include_incognito(c)
        wins = c.execute("SELECT * FROM windows WHERE snapshot_id = ? ORDER BY seq", (snap_id,)).fetchall()
        by = {}
        for t in c.execute("SELECT window_seq, url, title FROM tabs WHERE snapshot_id = ? ORDER BY window_seq, pos",
                           (snap_id,)):
            by.setdefault(t["window_seq"], []).append({"url": t["url"], "title": t["title"]})
    finally:
        c.close()
    words = [w for w in (query or "").lower().split() if w]
    browsers = {}
    for w in wins:
        win = {"index": w["idx"], "mode": w["mode"], "active": w["active"]}
        if w["pages_kept"] and (w["mode"] != "incognito" or keep_incognito):
            win["tabs"] = [t for t in by.get(w["seq"], [])
                           if all(x in f"{t['title']} {t['url']}".lower() for x in words)]
        else:
            win["tab_count"], win["redacted"] = w["tab_count"], True
        browsers.setdefault(w["browser"], {"browser": w["browser"], "windows": []})["windows"].append(win)
    return {"id": s["id"], "at": s["at"], "reason": s["reason"], "summary": _summary(s),
            "incognito_pages": "included" if keep_incognito else "counts only (Keep incognito tabs is off)",
            "browsers": list(browsers.values())}


def open_now(live=False):
    """What is open: the newest snapshot (the server saves one every few minutes), or a live read when asked."""
    if live:
        c = _connect()
        keep = include_incognito(c) if c else bool(tabs.DEFAULTS["include_incognito"])
        if c:
            c.close()
        snap = tabs.redact(tabs.read_all(), keep)
        return {"source": "live read", **snap}
    latest = list_snapshots(1)["snapshots"]
    if not latest:
        return {"source": "none", "note": "No tab snapshots yet. Pass live: true to read the browsers now."}
    return {"source": "latest snapshot", **read_snapshot(latest[0]["id"])}
