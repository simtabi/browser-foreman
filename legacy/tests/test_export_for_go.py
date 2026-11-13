"""The exporter that brings the Python tool's data to Browser Foreman (legacy/tools/export_for_go.py), on a small
synthetic checkout: page state rebuilt as the page does, duplicates into the Trash, tabs and settings carried."""
import gzip
import json
import os
import sqlite3
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "tools"))
import export_for_go as X  # noqa: E402


def make_root(tmp):
    data = {"built": "2026-11-12T12:46:58-0500", "total": 5, "folders": ["Bookmarks Bar", "Bookmarks Bar/Dev"],
            "keep": [
                {"id": "1", "u": "https://go.dev/", "t": "Go", "a": "1700000000", "g": "Bookmarks Bar", "s": "ok", "n": "page read normally", "dk": "None", "nu": "None", "ds": "The Go site"},
                {"id": "2", "u": "https://old.example/", "t": "Old", "a": "1700000001", "g": "Bookmarks Bar/Dev", "s": "dead", "n": "HTTP 404", "dk": "page", "nu": "None"},
                {"id": "3", "u": "https://moved.example/", "t": "Moved", "a": "x", "g": "Bookmarks Bar/Dev", "s": "moved", "n": "redirects", "dk": "None", "nu": "https://new.example/"},
                {"id": "4", "u": "https://new-in-build.example/", "t": "New", "a": "1", "g": "Bookmarks Bar/Dev", "s": "ok", "n": "", "dk": "None", "nu": "None"},
            ],
            # As build.py writes them: k and kg are the kept copy's folder, not its id; the kept copy is found by address.
            "dupes": [{"id": "9", "u": "https://www.go.dev?utm_source=x", "t": "Go (copy)", "g": "Bookmarks Bar", "k": "1 Dev/Tools", "kg": "Bookmarks Bar"}]}
    os.makedirs(os.path.join(tmp, "public"))
    with open(os.path.join(tmp, "public", "index.html"), "w") as f:
        f.write("<html>\nconst DATA = " + json.dumps(data) + ";\n</html>\n")
    ws = os.path.join(tmp, "storage", "workspaces", "main")
    os.makedirs(os.path.join(ws, "snapshots"))
    with open(os.path.join(ws, "meta.json"), "w") as f:
        json.dump({"id": "main", "name": "Chrome cleanup", "kind": "build", "created": "2026-11-12T11:43:59+00:00", "archived": False, "notes": "n"}, f)
    first = {"tree": None, "urls": {}, "refreshed": {}}
    # Later: Go renamed, Old deleted for good, Moved in the Trash, a new address for Go; item 4 is not in the saved tree.
    second = {"tree": {"root": {"n": "", "c": [{"n": "Bookmarks Bar", "c": [1, {"n": "Dev", "c": [2]}]},
                                                {"n": "Trash", "c": [3]}]},
                       "titles": {"1": "The Go site"}, "notes": {"1": "start here"}, "gone": [2],
                       "trashFrom": {"3": {"path": "Bookmarks Bar/Dev", "index": 1}}},
              "urls": {"1": "https://go.dev/doc/"}, "refreshed": {"3": {"s": "ok", "n": "fixed"}}}
    hist = []
    for k, st in enumerate([first, second], 1):
        h = f"h{k}"
        with gzip.open(os.path.join(ws, "snapshots", h + ".json.gz"), "wt") as f:
            json.dump(st, f)
        hist.append({"seq": k, "at": f"2026-11-12T1{k}:00:00+00:00", "op": "build" if k == 1 else "undo", "summary": f"s{k}", "base": k - 1, "snap": h})
    with open(os.path.join(ws, "history.jsonl"), "w") as f:
        f.write("\n".join(json.dumps(e) for e in hist) + "\n")
    os.makedirs(os.path.join(tmp, "storage", "sessions"))
    c = sqlite3.connect(os.path.join(tmp, "storage", "sessions", "tabs.sqlite3"))
    c.executescript("""CREATE TABLE settings (key TEXT, value TEXT);
      CREATE TABLE snapshots (id TEXT, at TEXT, reason TEXT, fingerprint TEXT, windows INT, tabs INT, incognito_windows INT, incognito_tabs INT, browsers TEXT, hidden INT);
      CREATE TABLE windows (snapshot_id TEXT, seq INT, browser TEXT, idx INT, mode TEXT, active INT, tab_count INT, pages_kept INT);
      CREATE TABLE tabs (snapshot_id TEXT, window_seq INT, pos INT, url TEXT, title TEXT);
      CREATE TABLE notes (kind TEXT, key TEXT, note TEXT, tags TEXT, updated TEXT);""")
    c.execute("INSERT INTO notes VALUES ('tab', 'https://go.dev/', 'read later', '[\"go\"]', '2026-11-12T07:30:00-0500')")
    c.executemany("INSERT INTO settings VALUES (?, ?)", [("interval", "120"), ("include_incognito", "false"), ("open_with", '"Safari"'), ("onboarded_browsers", "true")])
    c.execute("INSERT INTO snapshots VALUES ('2026-11-12_072137', '2026-11-12T07:21:37-0500', 'auto', 'fp', 1, 1, 1, 2, '[]', 0)")
    c.executemany("INSERT INTO windows VALUES (?,?,?,?,?,?,?,?)", [("2026-11-12_072137", 0, "Google Chrome", 1, "normal", 1, 1, 1), ("2026-11-12_072137", 1, "Google Chrome", 2, "incognito", 1, 2, 0)])
    c.execute("INSERT INTO tabs VALUES ('2026-11-12_072137', 0, 0, 'https://go.dev/', 'Go')")
    c.commit(); c.close()


class ExportTest(unittest.TestCase):
    def test_everything_comes_across(self):
        with tempfile.TemporaryDirectory() as tmp:
            make_root(tmp)
            out = os.path.join(tmp, "out.json.gz")
            X.main(["--root", tmp, "--out", out])
            b = json.load(gzip.open(out, "rt"))
            w = b["workspaces"][0]
            self.assertEqual([e["op"] for e in w["history"]], ["build", "legacy-undo"])
            self.assertEqual(w["workspace"]["notes"], "n")
            first = b["trees"]["h1"]
            bar = first["root"]["children"][0]
            self.assertEqual(bar["children"][0]["description"], "The Go site")
            head = b["trees"]["h2"]
            bar = head["root"]["children"][0]
            go = bar["children"][0]
            self.assertEqual((go["title"], go["url"], go["note"], go["id"]), ("The Go site", "https://go.dev/doc/", "start here", "py-1"))
            dev = bar["children"][1]
            self.assertEqual([c["id"] for c in dev["children"]], ["py-4"], "Old is gone for good; New (not in the saved tree) is placed at its path")
            own = [x for x in head["trash"] if not x.get("reason")]
            self.assertEqual((own[0]["node"]["id"], own[0]["from"], own[0]["index"]), ("py-3", dev["id"], 1))
            dup = [x for x in head["trash"] if x.get("reason", "").startswith("duplicate")]
            self.assertEqual((len(dup), dup[0]["reason"], dup[0]["from"]), (1, "duplicate of py-1", bar["id"]))
            self.assertIsInstance(own[0]["at"], int)
            links = b["links"]
            self.assertEqual(links["https://moved.example/"]["status"], "ok", "a later re-check wins over the build's result")
            self.assertEqual(links["https://go.dev/doc/"]["status"], "ok")
            self.assertTrue(links["https://go.dev/doc/"]["checked"].endswith("-05:00"))
            snap = b["tabs"][0]["windows"]
            wins = snap["browsers"][0]["windows"]
            self.assertEqual(wins[0]["tabs"][0]["url"], "https://go.dev/")
            self.assertEqual((wins[1].get("tabs"), wins[1]["tab_count"], wins[1]["redacted"]), (None, 2, True), "pages not kept stay out")
            self.assertEqual(b["prefs"], {"interval_seconds": 120, "include_incognito": False, "open_with": ["Safari"]})
            self.assertEqual(b["tab_notes"], [{"kind": "tab", "key": "https://go.dev/", "note": "read later", "tags": ["go"], "updated": "2026-11-12T07:30:00-05:00"}])
            with self.assertRaises(SystemExit):
                X.main(["--root", tmp, "--out", out])  # never overwrites


if __name__ == "__main__":
    unittest.main()
