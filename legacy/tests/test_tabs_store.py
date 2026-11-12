"""The tabs database: storing, purging, deleting history, first-seen ages, notes, link re-checks and the validator.
Every test runs against a fresh database in a temporary folder; no browser is asked anything."""
import json
import os
import sys
import tempfile
import time
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "lib"))
import paths  # noqa: E402,F401  puts src/server, src/mcp, src/pipeline and src/lib on the import path
import rechecks  # noqa: E402
import tabs  # noqa: E402
import validate  # noqa: E402
from validate import Invalid, V  # noqa: E402


def snap(*windows, at="2026-11-12T08:00:00"):
    return {"at": at, "reason": "auto", "browsers": [{"browser": "Google Chrome", "windows": list(windows)}]}


def win(mode, *urls):
    return {"index": 1, "mode": mode, "tabs": [{"url": u, "title": u.split("/")[-1]} for u in urls]}


class Store(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        tabs.SESSIONS = self.tmp.name
        tabs.DB_PATH = os.path.join(self.tmp.name, "tabs.sqlite3")
        tabs.SETTINGS_PATH = os.path.join(self.tmp.name, "settings.json")
        tabs._ready = False
        self.put("2026-11-10_100000", snap(win("normal", "https://a.example/1"), win("incognito", "https://secret.example/x"), at="2026-11-10T10:00:00"))
        self.put("2026-11-12_080000", snap(win("normal", "https://a.example/1", "https://b.example/2")))

    def tearDown(self):
        self.tmp.cleanup()

    def put(self, sid, s):
        c = tabs.db()
        tabs._store(c, sid, s)
        c.commit()
        c.close()

    def test_snapshots_and_timeline_follow_the_chosen_browsers(self):
        both = {"at": "2026-11-12T09:00:00", "reason": "auto", "browsers": [
            {"browser": "Google Chrome", "windows": [win("normal", "https://a.example/1", "https://c.example/3")]},
            {"browser": "Safari", "windows": [win("normal", "https://s.example/1")]}]}
        self.put("2026-11-12_090000", both)
        self.put("2026-11-12_091000", snap(win("normal", "https://a.example/1"), at="2026-11-12T09:10:00"))  # Chrome only
        every = {x["id"]: x["summary"]["tabs"] for x in tabs.list_snapshots()}
        self.assertEqual(every["2026-11-12_090000"], 3)
        chrome = {x["id"]: x for x in tabs.list_snapshots(browsers=["Google Chrome"])}
        self.assertEqual(chrome["2026-11-12_090000"]["summary"]["tabs"], 2)
        self.assertEqual(chrome["2026-11-12_090000"]["browsers"], ["Google Chrome"])
        safari = [x["id"] for x in tabs.list_snapshots(browsers=["Safari"])]
        self.assertEqual(safari, ["2026-11-12_090000"])  # snapshots with no Safari window are left out
        self.assertEqual(tabs.list_snapshots(browsers=[]), [])  # none chosen: nothing
        tl = {x["id"]: x for x in tabs.timeline(0, browsers=["Google Chrome"])["snapshots"]}
        self.assertEqual(tl["2026-11-12_091000"]["closed_n"], 1)  # c.example closed in Chrome; Safari's tab is not counted
        self.assertEqual(tabs.timeline(0, browsers=[])["snapshots"], [])

    def raw(self):
        out = b""
        for p in (tabs.DB_PATH, tabs.DB_PATH + "-wal"):
            if os.path.exists(p):
                with open(p, "rb") as f:
                    out += f.read()
        return out

    # ---- history deletion ----
    def test_delete_one_snapshot(self):
        self.assertEqual(tabs.delete_snapshots(ids=["2026-11-10_100000"])["deleted"], 1)
        self.assertEqual([s["id"] for s in tabs.list_snapshots(include_hidden=True)], ["2026-11-12_080000"])

    def test_delete_before_a_date_is_exclusive(self):
        self.assertEqual(tabs.delete_snapshots(before="2026-11-12")["deleted"], 1)
        self.assertIsNotNone(tabs.get_snapshot("2026-11-12_080000"))

    def test_wipe_all_leaves_nothing_in_the_file(self):
        self.assertGreater(self.raw().count(b"secret.example"), 0)  # control: it is there before
        self.assertEqual(tabs.delete_snapshots(everything=True)["deleted"], 2)
        self.assertEqual(tabs.list_snapshots(include_hidden=True), [])
        self.assertEqual(self.raw().count(b"secret.example"), 0)

    def test_bad_ids_are_ignored_not_executed(self):
        self.assertEqual(tabs.delete_snapshots(ids=["1 OR 1=1", "../x"])["deleted"], 0)
        self.assertEqual(len(tabs.list_snapshots(include_hidden=True)), 2)

    # ---- ages, notes ----
    def test_first_seen_uses_the_earliest_snapshot_and_skips_incognito(self):
        fs = tabs.first_seen(["https://a.example/1", "https://b.example/2", "https://secret.example/x"])
        self.assertEqual(fs["https://a.example/1"], "2026-11-10T10:00:00")  # in both snapshots: the earlier wins
        self.assertEqual(fs["https://b.example/2"], "2026-11-12T08:00:00")
        self.assertNotIn("https://secret.example/x", fs)
        self.assertIn("https://secret.example/x", tabs.first_seen(["https://secret.example/x"], include_incognito=True))

    def test_notes_set_read_and_clear(self):
        tabs.set_note("tab", "https://a.example/1", "read later", ["work", " work ", "", "x" * 60])
        n = tabs.notes("tab")
        self.assertEqual(n[0]["note"], "read later")
        self.assertEqual(n[0]["tags"], ["work", "x" * 40])  # trimmed, deduplicated, capped
        tabs.set_note("tab", "https://a.example/1", "", [])
        self.assertEqual(tabs.notes("tab"), [])

    # ---- purge (regression) ----
    def test_purge_pages_keeps_counts(self):
        r = tabs.purge("pages")
        self.assertEqual(r["pages"], 1)
        self.assertEqual(tabs.get_snapshot("2026-11-10_100000")["summary"]["incognito_tabs"], 1)

    # ---- link re-checks ----
    def test_recheck_reports_newly_broken_and_fixed(self):
        base = os.path.join(self.tmp.name, "links.csv")
        with open(base, "w") as f:
            f.write("url,status\nhttps://ok.example/,ok\nhttps://dead.example/,dead\nhttps://slow.example/,ok\n")
        rechecks.BASELINE_CSV = base
        results = {"https://ok.example/": "dead", "https://dead.example/": "ok", "https://slow.example/": "unverified"}
        real = rechecks.cl.check
        rechecks.cl.check = lambda u: (results[u], 0, u, "")
        try:
            r = rechecks.start("test")
            for _ in range(100):
                if not rechecks._current["run"]:
                    break
                time.sleep(0.05)
            rep = rechecks.report(r["run_id"])
        finally:
            rechecks.cl.check = real
        self.assertEqual(rep["run"]["state"], "done")
        self.assertEqual([x["url"] for x in rep["newly_broken"]], ["https://ok.example/"])
        self.assertEqual([x["url"] for x in rep["fixed"]], ["https://dead.example/"])
        # a timeout keeps the old status instead of calling a working link broken
        self.assertEqual(rep["run"]["newly_broken"], 1)


class Validator(unittest.TestCase):
    CLOSE = V.obj(items=V.list(V.obj(browser=V.enum("Google Chrome"), window_id=V.int().optional(), tab_id=V.int().optional(),
                                     pos=V.int(0, 100000), url=V.url()), max_items=3, min_items=1))

    def ok(self, **item):
        base = {"browser": "Google Chrome", "window_id": 1, "tab_id": 2, "pos": 0, "url": "https://a.example/"}
        return {"items": [{**base, **item}]}

    def test_accepts_a_good_body(self):
        self.assertEqual(self.CLOSE.check(self.ok())["items"][0]["pos"], 0)

    def test_refuses(self):
        cases = [self.ok(url="javascript:alert(1)"), self.ok(url="file:///etc/passwd"), self.ok(browser="Finder"),
                 self.ok(pos=-1), self.ok(pos=True), {"items": []}, {"items": [self.ok()["items"][0]] * 4},
                 {"items": self.ok()["items"], "extra": 1}, self.ok(url="https://a.example/\nx"), "nope"]
        for body in cases:
            with self.subTest(body=str(body)[:60]):
                with self.assertRaises(Invalid):
                    self.CLOSE.check(body)

    def test_snapshot_id_and_day(self):
        validate.SNAPSHOT_ID.check("2026-11-12_080000")
        for bad in ["2026-11-12", "../../x", "2026-11-12_080000;"]:
            with self.assertRaises(Invalid):
                validate.SNAPSHOT_ID.check(bad)


if __name__ == "__main__":
    unittest.main()
