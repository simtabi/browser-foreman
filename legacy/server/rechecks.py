"""Scheduled link re-checks: every `recheck_days` days (Open tabs › settings, 0 = off) every bookmarked web link is
checked again, politely (the per-site limiter in hostlimit.py), and the result is compared with what was known
before, so the summary says exactly what broke and what came back.

Results live in the tabs database (tables link_checks and link_results). Nothing in the bookmarks changes on its own:
the page shows the summary and you decide what to do.

Baseline for "before": the last finished run's result for that address, else the status in work/links3.csv.
"""
import csv
import os
import threading
import time
import uuid
from concurrent.futures import FIRST_COMPLETED, ThreadPoolExecutor, wait

import check_links as cl
import tabs
from hostlimit import interleave

import paths  # noqa: E402

BASELINE_CSV = os.path.join(paths.WORK, "links3.csv")
BROKEN = {"dead"}
_current = {"run": None, "cancel": False}
_lock = threading.Lock()


def _baseline():
    """address → status, from the last finished run, else from the build's own link check."""
    base = {}
    if os.path.exists(BASELINE_CSV):
        with open(BASELINE_CSV, newline="") as f:
            for row in csv.DictReader(f):
                if row.get("url"):
                    base[row["url"]] = row.get("status") or ""
    c = tabs.db()
    try:
        last = c.execute("SELECT run_id FROM link_checks WHERE state = 'done' ORDER BY started DESC LIMIT 1").fetchone()
        if last:
            for r in c.execute("SELECT url, status FROM link_results WHERE run_id = ?", (last["run_id"],)):
                base[r["url"]] = r["status"]
    finally:
        c.close()
    return base


def start(reason="manual"):
    """Start a run in the background unless one is running. Returns the run id."""
    with _lock:
        if _current["run"]:
            return {"started": False, "why": "a re-check is already running", "run_id": _current["run"]}
        base = _baseline()
        urls = interleave([u for u in base if u.startswith(("http://", "https://"))])
        run_id = time.strftime("%Y-%m-%d_%H%M%S") + "-" + uuid.uuid4().hex[:4]
        c = tabs.db()
        try:
            c.execute("INSERT INTO link_checks (run_id, started, total, state) VALUES (?, ?, ?, 'running')",
                      (run_id, time.strftime("%Y-%m-%dT%H:%M:%S%z"), len(urls)))
            c.commit()
        finally:
            c.close()
        _current.update(run=run_id, cancel=False)
    threading.Thread(target=_run, args=(run_id, urls, base), daemon=True, name="link-recheck").start()
    return {"started": True, "run_id": run_id, "total": len(urls), "reason": reason}


def cancel():
    _current["cancel"] = True
    return {"cancelling": _current["run"]}


def _run(run_id, urls, base):
    checked = broken = fixed = 0
    ex = ThreadPoolExecutor(6)
    pending = {ex.submit(cl.check, u): u for u in urls}
    try:
        while pending and not _current["cancel"]:
            done, _ = wait(list(pending), timeout=1, return_when=FIRST_COMPLETED)
            rows = []
            for fut in done:
                u = pending.pop(fut)
                try:
                    st, code, final, note = fut.result()
                except Exception as e:  # noqa: BLE001 — one bad link never ends the run
                    st, note = "unverified", type(e).__name__
                before = base.get(u, "")
                # A timeout says nothing new: keep what was known rather than call a working link broken.
                if st == "unverified" and before:
                    st = before
                if st in BROKEN and before not in BROKEN:
                    broken += 1
                elif before in BROKEN and st not in BROKEN and st != "unverified":
                    fixed += 1
                rows.append((run_id, u, st, before, (note or "")[:300]))
                checked += 1
            if rows:
                c = tabs.db()
                try:
                    c.executemany("INSERT OR REPLACE INTO link_results VALUES (?, ?, ?, ?, ?)", rows)
                    c.execute("UPDATE link_checks SET checked = ?, newly_broken = ?, fixed = ? WHERE run_id = ?",
                              (checked, broken, fixed, run_id))
                    c.commit()
                finally:
                    c.close()
    finally:
        ex.shutdown(wait=False, cancel_futures=True)
        state = "cancelled" if _current["cancel"] else "done"
        c = tabs.db()
        try:
            c.execute("UPDATE link_checks SET state = ?, finished = ?, checked = ?, newly_broken = ?, fixed = ? WHERE run_id = ?",
                      (state, time.strftime("%Y-%m-%dT%H:%M:%S%z"), checked, broken, fixed, run_id))
            c.commit()
        finally:
            c.close()
        _current.update(run=None, cancel=False)


def runs(limit=20):
    c = tabs.db()
    try:
        return [dict(r) for r in c.execute("SELECT * FROM link_checks ORDER BY started DESC LIMIT ?", (limit,))]
    finally:
        c.close()


def report(run_id):
    """What broke and what came back in one run."""
    c = tabs.db()
    try:
        run = c.execute("SELECT * FROM link_checks WHERE run_id = ?", (run_id,)).fetchone()
        if not run:
            return None
        rows = c.execute("SELECT url, status, before, note FROM link_results WHERE run_id = ?", (run_id,)).fetchall()
    finally:
        c.close()
    broke = [dict(r) for r in rows if r["status"] in BROKEN and (r["before"] or "") not in BROKEN]
    back = [dict(r) for r in rows if (r["before"] or "") in BROKEN and r["status"] not in BROKEN]
    return {"run": dict(run), "newly_broken": broke[:2000], "fixed": back[:2000]}


def start_scheduler():
    """Once an hour: if re-checks are on and the last run is older than recheck_days, start one."""
    def loop():
        time.sleep(120)  # let the server settle after a start
        while True:
            try:
                cfg = tabs.settings()
                days = cfg.get("recheck_days", 0)
                if not cfg.get("recheck_epoch"):  # the schedule counts from the first time it was on, never from 1970
                    tabs.save_settings({"recheck_epoch": int(time.time())})
                    cfg = tabs.settings()
                if days and not _current["run"]:
                    last = runs(1)
                    started = last[0]["started"] if last else None
                    since = time.mktime(time.strptime(started[:19], "%Y-%m-%dT%H:%M:%S")) if started else cfg["recheck_epoch"]
                    if time.time() - since > days * 86400:
                        start("scheduled")
            except Exception:  # noqa: BLE001 — the scheduler must never die
                pass
            time.sleep(3600)
    threading.Thread(target=loop, daemon=True, name="recheck-scheduler").start()


def status():
    return {"running": _current["run"], "runs": runs(10), "recheck_days": tabs.settings().get("recheck_days", 0)}
