"""Saving tabs as bookmarks, and backup / restore of the whole tool."""
import io
import json
import os
import sqlite3
import zipfile

from test_workspaces import Env, netscape


class TestAddLinks(Env):
    LINKS = [{"url": "https://a.example/x", "title": "A"}, {"url": "https://b.example/", "title": ""}]

    def test_adds_items_folder_and_one_undoable_entry(self):
        e = self.w.add_links("main", "Saved tabs/Work", self.LINKS)
        self.assertEqual((e["op"], e["summary"]), ("add-links", "Saved 2 tabs as bookmarks in Saved tabs/Work"))
        st, _ = self.w.get_state("main")
        self.assertEqual(sorted(r["id"] for r in st["added"].values()), [5, 6])  # after the highest id (4)
        f = self.w.read_folder("main", "Saved tabs/Work")
        self.assertEqual([(b["title"], b["url"]) for b in f["bookmarks"]],
                         [("A", "https://a.example/x"), ("https://b.example/", "https://b.example/")])
        self.assertEqual(self.w.search("main", "a.example")["total"], 1)  # searchable like any bookmark
        e2 = self.w.add_links("main", "Saved tabs/Work", self.LINKS[:1])  # same name: a new folder beside it
        self.assertIn("Saved tabs/Work (2)", e2["summary"])
        self.assertEqual(sorted(int(k) for k in self.w.get_state("main")[0]["added"]), [5, 6, 7])
        self.w.undo("main")
        self.w.undo("main")
        self.assertNotIn("added", self.w.get_state("main")[0])
        self.assertEqual(self.w.search("main", "a.example")["total"], 0)
        html, n = self.io.export_netscape("main")
        self.assertNotIn("a.example", html)

    def test_validation_and_lock(self):
        bad = [[], [{"url": "javascript:alert(1)", "title": "x"}], [{"url": "file:///etc/passwd"}],
               [{"url": "https://x.example/" + "a" * 4100}], [{"url": "https://x.example/", "title": "t" * 501}],
               [{"url": "https://x.example/"}] * 2001, "nope"]
        for links in bad:
            with self.assertRaises(self.w.WsError):
                self.w.add_links("main", "Saved tabs/X", links)
        for folder in ("", "Trash", "/", None):
            with self.assertRaises(self.w.WsError):
                self.w.add_links("main", folder, self.LINKS)
        self.w.set_locked("main", True)
        with self.assertRaises(self.w.Locked):
            self.w.add_links("main", "Saved tabs/X", self.LINKS)

    def test_bad_added_in_a_state_is_refused(self):
        for added in ({"5": {"id": 6, "u": "https://a/", "t": ""}}, {"5": {"id": 5, "u": "ftp://a/", "t": ""}}, []):
            with self.assertRaises(self.w.WsError):
                self.w.commit("main", {"tree": None, "added": added})


class TestBackup(Env):
    def setUp(self):
        super().setUp()
        import backup
        self.b = backup
        os.environ["BM_TABS_DB"] = self.tabs = os.path.join(self.tmp, "sessions", "tabs.sqlite3")
        os.makedirs(os.path.dirname(self.tabs))
        c = sqlite3.connect(self.tabs)
        c.execute("PRAGMA journal_mode=WAL")
        c.execute("CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT)")
        c.execute("INSERT INTO settings VALUES ('include_incognito', 'false')")
        c.commit()
        self.addCleanup(c.close)  # left open, as the running server would: the backup API still copies it
        
    def make(self):
        self.ws = self.io.import_sources("Laptop", [("a.html", netscape([("https://a.example/", "A", 1)]))])
        self.w.commit("main", {"tree": None, "urls": {"1": "https://www.python.org/"}})
        tid = self.w.delete_workspace(self.io.import_sources("Old", [("b.html", netscape([("https://b.example/", "B", 1)]))]))
        return tid, self.b.backup_bytes()

    def test_round_trip_keeps_everything_and_moves_the_current_data_aside(self):
        self.w.save_settings({"mcp_allow_writes": True})
        tid, raw = self.make()
        z = zipfile.ZipFile(io.BytesIO(raw))
        self.assertIn("browser-foreman.json", z.namelist())
        names = set(z.namelist())
        self.assertIn("sessions/tabs.sqlite3", names)
        self.assertIn(f"workspaces/_trash/{tid}/meta.json", names)  # the workspace trash too
        self.assertFalse(any(n.endswith(".lock") for n in names))
        manifest = json.loads(z.read("manifest.json"))
        self.assertEqual((manifest["format"], manifest["version"]), ("browser-foreman-backup", 1))
        # Change things after the backup, then restore it.
        self.w.rename_workspace(self.ws, "Changed")
        self.w.commit("main", {"tree": None, "urls": {}})
        self.w.save_settings({"mcp_allow_writes": False})
        res = self.b.restore(raw)
        self.assertTrue(self.w.read_settings()["mcp_allow_writes"])
        self.assertEqual(self.w.read_meta(self.ws)["name"], "Laptop")
        self.assertEqual(self.w.get_state("main")[0]["urls"], {"1": "https://www.python.org/"})
        self.assertEqual([t["trash_id"] for t in self.w.list_trash()], [tid])
        c = sqlite3.connect(self.tabs)
        self.assertEqual(c.execute("SELECT value FROM settings").fetchone()[0], "false")
        c.close()
        # Nothing current was deleted: it was backed up, and moved aside.
        self.assertTrue(os.path.isfile(res["auto_backup"]))
        self.assertEqual(json.loads(zipfile.ZipFile(res["auto_backup"]).read("manifest.json"))["format"], "browser-foreman-backup")
        aside = res["previous_data"]
        with open(os.path.join(aside, "workspaces", self.ws, "meta.json")) as f:
            self.assertEqual(json.load(f)["name"], "Changed")
        self.assertTrue(os.path.isfile(os.path.join(aside, "sessions", "tabs.sqlite3")))

    def tampered(self, raw, edit):
        src = zipfile.ZipFile(io.BytesIO(raw))
        out = io.BytesIO()
        with zipfile.ZipFile(out, "w") as z:
            for i in src.infolist():
                data = src.read(i.filename)
                name, data = edit(i.filename, data)
                if name:
                    z.writestr(name, data)
        return out.getvalue()

    def test_damaged_or_hostile_backups_change_nothing(self):
        _, raw = self.make()
        before = sorted(os.listdir(self.w.ws_root()))
        cases = {
            "not a zip": b"hello",
            "checksum": self.tampered(raw, lambda n, d: (n, d + b" " if n.endswith("data.json") else d)),
            "traversal": self.tampered(raw, lambda n, d: ("workspaces/../../evil" if n.endswith("data.json") else n, d)),
            "absolute": self.tampered(raw, lambda n, d: ("/etc/evil" if n.endswith("data.json") else n, d)),
            "other path": self.tampered(raw, lambda n, d: ("src/server/serve.py" if n.endswith("data.json") else n, d)),
            "no manifest": self.tampered(raw, lambda n, d: (None if n == "manifest.json" else n, d)),
            "newer": self.tampered(raw, lambda n, d: (n, json.dumps({**json.loads(d), "version": 99}).encode()
                                                      if n == "manifest.json" else d)),
        }
        for why, bad in cases.items():
            with self.assertRaises(self.w.WsError, msg=why):
                self.b.restore(bad)
        self.assertEqual(sorted(os.listdir(self.w.ws_root())), before)
        self.assertFalse(os.path.exists(os.path.join(self.b.data_root(), "_before-restore")))
