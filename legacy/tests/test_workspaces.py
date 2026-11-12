"""Workspaces, history, importers, merge and export. Run: python3 -m unittest discover -s src/tests"""
import json
import os
import plistlib
import shutil
import sqlite3
import sys
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "lib"))
import paths  # noqa: E402,F401  puts src/server, src/mcp, src/pipeline and src/lib on the import path

DATA = {"keep": [
    {"id": 1, "a": 100, "f": "Bookmarks Bar/Dev", "g": "Bookmarks Bar/Dev", "t": "Python", "u": "https://python.org/", "s": "ok"},
    {"id": 2, "a": 200, "f": "Bookmarks Bar/Dev", "g": "Bookmarks Bar/Dev", "t": "Rust", "u": "https://rust-lang.org/", "s": "ok"},
    {"id": 3, "a": 300, "f": "Bookmarks Bar/News", "g": "Bookmarks Bar/News", "t": "Old news", "u": "https://gone.example/", "s": "dead"},
    {"id": 4, "a": 400, "f": "", "g": "", "t": "Loose", "u": "https://loose.example/a", "s": "ok"}],
    "dupes": [], "newMine": [], "folders": ["Bookmarks Bar", "Bookmarks Bar/Dev", "Bookmarks Bar/Empty"], "total": 4}


def fake_preview(path, data=None):
    blob = json.dumps(data or DATA)
    with open(path, "w") as f:
        f.write(f"<!doctype html><html><body><div id=edited></div>\n<script>\nconst DATA = {blob};\n</script></body></html>\n")


class Env(unittest.TestCase):
    """Each test gets its own workspaces folder and preview.html."""
    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        os.environ["BM_WORKSPACES"] = os.path.join(self.tmp, "ws")
        os.environ["BM_PREVIEW"] = os.path.join(self.tmp, "preview.html")
        # Never the real sessions/tabs.sqlite3: a restore test would move it aside.
        os.environ["BM_TABS_DB"] = os.path.join(self.tmp, "no-sessions", "tabs.sqlite3")
        fake_preview(os.environ["BM_PREVIEW"])
        import workspaces
        import bookmark_io
        self.w, self.io = workspaces, bookmark_io
        self.w._DATA_CACHE.clear()
        self.w.ensure_main()

    def tearDown(self):
        os.environ["BM_TABS_DB"] = os.path.join(self.tmp, "no-sessions", "tabs.sqlite3")
        shutil.rmtree(self.tmp)


def netscape(items):
    rows = "".join(f'<DT><A HREF="{u}" ADD_DATE="{a}">{t}</A>\n' for u, t, a in items)
    return ("<!DOCTYPE NETSCAPE-Bookmark-file-1>\n<DL><p>\n<DT><H3 PERSONAL_TOOLBAR_FOLDER=\"true\">Bookmarks bar</H3>\n"
            f"<DL><p>\n<DT><H3>Dev</H3>\n<DL><p>\n{rows}</DL><p>\n</DL><p>\n</DL><p>\n").encode()


def chrome_json():
    return json.dumps({"version": 1, "roots": {
        "bookmark_bar": {"type": "folder", "name": "Bookmarks bar", "children": [
            {"type": "folder", "name": "Dev/Tools", "children": [
                {"type": "url", "name": "Python docs", "url": "https://www.python.org/?utm_source=x",
                 "date_added": str((1600000000 + 11644473600) * 1000000)}]}]},
        "other": {"type": "folder", "name": "Other", "children": [{"type": "url", "name": "", "url": "https://example.com/#/app"}]},
        "synced": {"type": "folder", "name": "Mobile", "children": []}}}).encode()


def firefox_db(path):
    con = sqlite3.connect(path)
    con.executescript("""
    CREATE TABLE moz_places (id INTEGER PRIMARY KEY, url TEXT);
    CREATE TABLE moz_bookmarks (id INTEGER PRIMARY KEY, type INTEGER, fk INTEGER, parent INTEGER, position INTEGER,
                                title TEXT, dateAdded INTEGER, guid TEXT);
    INSERT INTO moz_places VALUES (1,'https://mozilla.org/'),(2,'place:sort=8'),(3,'https://example.com/#/app');
    INSERT INTO moz_bookmarks VALUES (1,2,NULL,0,0,'','0','root________'),(2,2,NULL,1,0,'toolbar',0,'toolbar_____'),
      (3,2,NULL,1,1,'unfiled',0,'unfiled_____'),(4,2,NULL,2,0,'Mozilla',0,'f1'),
      (5,1,1,4,0,'Mozilla home',1500000000000000,'b1'),(6,1,2,2,1,'Recent',0,'b2'),(7,1,3,3,0,'App route',1400000000000000,'b3');
    """)
    con.commit()
    con.close()


def _bytes(p):
    with open(p, "rb") as f:
        return f.read()


def safari_plist():
    return plistlib.dumps({"WebBookmarkType": "WebBookmarkTypeList", "Children": [
        {"WebBookmarkType": "WebBookmarkTypeProxy", "Title": "History"},
        {"WebBookmarkType": "WebBookmarkTypeList", "Title": "BookmarksBar", "Children": [
            {"WebBookmarkType": "WebBookmarkTypeLeaf", "URLString": "https://apple.com/", "URIDictionary": {"title": "Apple"}}]},
        {"WebBookmarkType": "WebBookmarkTypeList", "Title": "com.apple.ReadingList", "Children": [
            {"WebBookmarkType": "WebBookmarkTypeLeaf", "URLString": "https://python.org", "URIDictionary": {"title": "Py"}}]}]},
        fmt=plistlib.FMT_BINARY)


class TestIds(Env):
    def test_traversal_and_bad_ids_refused(self):
        for bad in ("../x", "..", "a/b", "A", "", "x" * 41, "-x", "a\x00", "%2e%2e", 5, None, "main/../../etc"):
            with self.assertRaises(self.w.WsError, msg=repr(bad)):
                self.w.ws_dir(bad)

    def test_symlink_out_is_refused(self):
        os.symlink(self.tmp, os.path.join(self.w.ws_root(), "escape"))
        with self.assertRaises(self.w.WsError):
            self.w.ws_dir("escape")


class TestHistory(Env):
    def edit(self, fn):
        data = self.w.load_data("main")
        st, head = self.w.get_state("main")
        st = self.w.editable(st, data)
        fn(st, data)
        return self.w.commit("main", st, by="test", base_seq=head)

    def test_main_starts_fresh_then_logs_every_edit(self):
        self.assertEqual(self.w.read_history("main"), [])
        e = self.edit(lambda st, d: self.w.op_trash(st, [3]))
        self.assertEqual((e["seq"], e["op"]), (2, "trash"))
        self.assertIn("Old news", e["summary"])
        self.assertEqual(self.w.read_history("main")[0]["op"], "start")
        e = self.edit(lambda st, d: self.w.op_move(st, [1], "Bookmarks Bar/News"))
        self.assertEqual(e["op"], "move")
        e = self.edit(lambda st, d: self.w.op_rename(st, 2, "Rust lang", d))
        self.assertEqual(e["op"], "rename")
        e = self.edit(lambda st, d: self.w.op_put_back(st, [3], d))
        self.assertEqual(e["op"], "put-back")
        st, _ = self.w.get_state("main")
        self.assertEqual(self.w.locate(st["tree"]["root"])[3][1], "Bookmarks Bar/News")
        self.assertEqual(len(self.w.read_history("main")), 5)

    def test_stale_base_is_refused_and_noop_appends_nothing(self):
        self.edit(lambda st, d: self.w.op_trash(st, [3]))
        st, head = self.w.get_state("main")
        with self.assertRaises(self.w.Conflict):
            self.w.commit("main", st, base_seq=head - 1)
        self.assertEqual(self.w.commit("main", st, base_seq=head)["seq"], head)

    def test_undo_walks_back_and_rollback_is_itself_undoable(self):
        self.edit(lambda st, d: self.w.op_trash(st, [3]))           # 2
        self.edit(lambda st, d: self.w.op_rename(st, 1, "Py", d))   # 3
        after_rename = self.w.get_state("main")[0]
        self.w.undo("main")                                          # 4 -> state of 2
        self.assertEqual(self.w.get_state("main")[0]["tree"]["titles"], {})
        self.w.undo("main")                                          # 5 -> state of 1 (start)
        self.assertIsNone(self.w.get_state("main")[0]["tree"])
        with self.assertRaises(self.w.WsError):
            self.w.undo("main")
        self.w.rollback("main", 3)                                   # 6 -> state of 3
        self.assertEqual(self.w.get_state("main")[0], after_rename)
        self.w.undo("main")                                          # undoing the rollback -> state of 5
        self.assertIsNone(self.w.get_state("main")[0]["tree"])
        sums = [e["summary"] for e in self.w.read_history("main") if e["op"] == "undo"]
        self.assertEqual(sums[:2], ["Undid: Renamed “Py”", "Undid: Moved to Trash: “Old news”"])
        ops = [e["op"] for e in self.w.read_history("main")]
        self.assertEqual(ops, ["start", "trash", "rename", "undo", "undo", "rollback", "undo"])

    def test_redo_brings_back_undone_changes_in_order_and_a_new_edit_ends_it(self):
        self.assertEqual(self.w.undo_state("main"), {"undo": None, "redo": None})  # nothing happened yet
        self.edit(lambda st, d: self.w.op_trash(st, [3]))           # 2
        self.edit(lambda st, d: self.w.op_rename(st, 1, "Py", d))   # 3
        after_trash, after_rename = self.w.state_at("main", 2)[0], self.w.get_state("main")[0]
        self.assertEqual(self.w.undo_state("main"), {"undo": "Renamed “Py”", "redo": None})
        with self.assertRaises(self.w.WsError):
            self.w.redo("main")                                      # nothing undone yet
        self.w.undo("main"); self.w.undo("main")                     # back to the start
        self.assertEqual(self.w.undo_state("main")["redo"], "Moved to Trash: “Old news”")
        self.w.redo("main")                                          # the trash comes back first
        self.assertEqual(self.w.get_state("main")[0], after_trash)
        self.assertEqual(self.w.undo_state("main")["redo"], "Renamed “Py”")
        self.w.redo("main")                                          # then the rename
        self.assertEqual(self.w.get_state("main")[0], after_rename)
        self.assertEqual(self.w.undo_state("main"), {"undo": "Renamed “Py”", "redo": None})
        self.w.undo("main")                                          # undo after redo undoes it again
        self.assertEqual(self.w.get_state("main")[0], after_trash)
        self.edit(lambda st, d: self.w.op_rename(st, 1, "New", d))  # a new edit ends the redo run
        self.assertIsNone(self.w.undo_state("main")["redo"])
        with self.assertRaises(self.w.WsError):
            self.w.redo("main")

    def test_workspace_rename_is_logged_but_not_undone(self):
        self.edit(lambda st, d: self.w.op_trash(st, [3]))
        self.w.rename_workspace("main", "Renamed")
        self.w.undo("main")
        self.assertEqual(self.w.read_meta("main")["name"], "Renamed")
        self.assertIsNone(self.w.get_state("main")[0]["tree"])

    def test_migrate_once(self):
        tree = {"root": self.w.default_tree(DATA), "titles": {"1": "Py"}, "notes": {}, "gone": [], "trashFrom": {}}
        e = self.w.migrate("main", {"tree": tree, "urls": {"2": "https://www.rust-lang.org/"}, "refreshed": {}})
        self.assertEqual(e["op"], "migrate")
        self.assertIn("Renamed", e["summary"])
        with self.assertRaises(self.w.Conflict):
            self.w.migrate("main", {"tree": None})

    def test_reverting_the_last_link_is_not_a_reset(self):
        self.w.commit("main", {"tree": None, "urls": {"2": "https://www.rust-lang.org/"}})
        e = self.w.commit("main", {"tree": None, "urls": {}})
        self.assertEqual(e["op"], "revert-link")
        e = self.w.commit("main", {"tree": None, "urls": {"2": "https://www.rust-lang.org/"}, "refreshed": {"1": {"s": "ok"}}})
        e = self.w.commit("main", self.w.empty_state(), op="reset")
        self.assertEqual(e["summary"], "Reset to the generated tree, clearing 1 link update, 1 refresh result")

    def test_bad_state_is_refused(self):
        for bad in ({"tree": {"root": {"n": "", "c": ["x"]}}}, {"tree": {"root": {"c": []}}}, {"urls": []}, []):
            with self.assertRaises(self.w.WsError):
                self.w.commit("main", bad)

    def test_trash_never_reached_by_plain_path_and_folder_into_itself_refused(self):
        self.edit(lambda st, d: self.w.op_trash(st, ["Bookmarks Bar/News"]))
        st = self.w.editable(self.w.get_state("main")[0], DATA)
        with self.assertRaises(self.w.WsError):
            self.w.op_move(st, ["Bookmarks Bar"], "Bookmarks Bar/Dev")
        res = self.w.search("main", "news")
        self.assertEqual(res["total"], 0)
        self.assertEqual(self.w.search("main", "news", include_trash=True)["total"], 1)
        self.edit(lambda st, d: self.w.op_put_back(st, ["News"], d))
        self.assertEqual(self.w.read_folder("main", "Bookmarks Bar/News")["bookmarks"][0]["id"], 3)


class TestImport(Env):
    def test_detect_each_format(self):
        db = os.path.join(self.tmp, "places.sqlite")
        firefox_db(db)
        self.assertEqual(self.io.detect(netscape([])), "netscape")
        self.assertEqual(self.io.detect(chrome_json()), "chrome")
        self.assertEqual(self.io.detect(_bytes(db)), "firefox")
        self.assertEqual(self.io.detect(safari_plist()), "safari")
        with self.assertRaises(self.w.WsError):
            self.io.detect(b"hello")

    def test_readers(self):
        _, it, fl = self.io.read_source(netscape([("https://a.example/", "A", 5)]))
        self.assertEqual(it[0]["folder"], "Bookmarks Bar/Dev")
        _, it, fl = self.io.read_source(chrome_json())
        self.assertEqual(it[0]["folder"], "Bookmarks Bar/Dev∕Tools")
        self.assertEqual(it[0]["add"], 1600000000)
        self.assertEqual(it[1]["folder"], "")
        db = os.path.join(self.tmp, "places.sqlite")
        firefox_db(db)
        _, it, fl = self.io.read_source(_bytes(db))
        self.assertEqual([(i["url"], i["folder"]) for i in it],
                         [("https://mozilla.org/", "Bookmarks Bar/Mozilla"), ("https://example.com/#/app", "")])
        self.assertEqual(it[0]["add"], 1500000000)
        _, it, fl = self.io.read_source(safari_plist())
        self.assertEqual([(i["title"], i["folder"]) for i in it], [("Apple", "Bookmarks Bar"), ("Py", "Reading List")])

    def test_read_path_copies_and_only_accepts_bookmark_files(self):
        db = os.path.join(self.tmp, "places.sqlite")
        firefox_db(db)
        before = _bytes(db)
        self.assertEqual(self.io.read_path(db), before)
        self.assertEqual(_bytes(db), before)
        for p in ("/etc/passwd", os.path.join(self.tmp, "x.txt")):
            with self.assertRaises(self.w.WsError):
                self.io.read_path(p)

    def test_merge_uses_build_rule_and_records_each_copy(self):
        db = os.path.join(self.tmp, "places.sqlite")
        firefox_db(db)
        ws = self.io.import_sources("Merged", [("chrome-Bookmarks", chrome_json()), ("places.sqlite", _bytes(db)),
                                               ("export.html", netscape([("https://python.org", "", 1)]))])
        data = self.w.load_data(ws)
        urls = sorted(i["u"] for i in data["keep"])
        # python.org twice (a tracking parameter, a trailing slash) -> once; the #/app route twice -> once.
        self.assertEqual(urls, ["https://example.com/#/app", "https://mozilla.org/", "https://python.org"])
        self.assertEqual(len(data["dupes"]), 2)
        py = next(i for i in data["keep"] if "python" in i["u"])
        self.assertEqual(py["g"], "Bookmarks Bar/Dev")  # same depth, so the earliest added wins
        self.assertEqual(py["a"], 1)
        self.assertEqual(py["t"], "Python docs")        # it had no title, so it takes the merged copy's
        app = next(i for i in data["keep"] if "#/app" in i["u"])
        self.assertEqual(app["t"], "App route")         # an empty title takes a real one
        self.assertEqual(app["g"], "")                  # both at the top: the earliest (Firefox, 2014) wins
        rows = self.w.read_dup_csv(os.path.join(self.w.ws_dir(ws), "duplicates.csv"))
        self.assertEqual(len(rows), 2)
        self.assertEqual({r["removed_source"] for r in rows}, {"chrome-Bookmarks"})  # the Chrome copies were the later ones both times
        meta = self.w.read_meta(ws)
        self.assertEqual([s["kind"] for s in meta["sources"]], ["chrome", "firefox", "netscape"])
        for s in meta["sources"]:
            self.assertTrue(os.path.exists(os.path.join(self.w.ws_dir(ws), s["stored"])))
        self.assertEqual(self.w.read_history(ws)[0]["op"], "import")

    def test_add_sources_keeps_existing_ids_and_edits(self):
        ws = self.io.import_sources("A", [("a.html", netscape([("https://a.example/", "A", 5)]))])
        data = self.w.load_data(ws)
        st, head = self.w.get_state(ws)
        st = self.w.editable(st, data)
        self.w.op_rename(st, data["keep"][0]["id"], "Renamed A", data)
        self.w.commit(ws, st, base_seq=head)
        self.io.add_sources(ws, [("b.html", netscape([("https://a.example", "A copy", 1), ("https://b.example/", "B", 2)]))])
        data2 = self.w.load_data(ws)
        self.assertEqual(data2["keep"][0]["id"], data["keep"][0]["id"])
        self.assertEqual(len(data2["keep"]), 2)
        self.assertEqual(len(data2["dupes"]), 1)
        self.assertEqual(self.w.search(ws, "renamed")["total"], 1)
        with self.assertRaises(self.w.WsError):
            self.io.add_sources("main", [("b.html", netscape([]))])

    def test_new_workspace_from_another(self):
        st, head = self.w.get_state("main")
        st = self.w.editable(st, DATA)
        self.w.op_trash(st, [3])
        self.w.commit("main", st, base_seq=head)
        ws = self.io.import_sources("Plus", [("b.html", netscape([("https://python.org/", "dup", 1)]))], base_ws="main")
        data = self.w.load_data(ws)
        self.assertEqual(len(data["keep"]), 3)  # 4 minus the trashed one; python.org merged
        self.assertEqual(len(data["dupes"]), 1)


class TestExport(Env):
    def test_html_and_chrome_leave_trash_out_and_apply_edits(self):
        st, head = self.w.get_state("main")
        st = self.w.editable(st, DATA)
        self.w.op_trash(st, [3])
        self.w.op_rename(st, 1, "Py <3", DATA)
        st["urls"] = {"2": "https://www.rust-lang.org/"}
        self.w.commit("main", st, base_seq=head)
        html, n = self.io.export_netscape("main")
        self.assertEqual(n, 3)
        self.assertIn("Py &lt;3", html)
        self.assertIn("https://www.rust-lang.org/", html)
        self.assertNotIn("gone.example", html)
        _, items, _ = self.io.read_source(html.encode())
        self.assertEqual(len(items), 3)  # it reads back
        js, n = self.io.export_chrome("main")
        doc = json.loads(js)
        self.assertEqual(n, 3)
        self.assertEqual(doc["checksum"], self.io.chrome_checksum(doc["roots"]))
        _, items, _ = self.io.read_source(js.encode())
        self.assertEqual(sorted(i["url"] for i in items),
                         ["https://loose.example/a", "https://python.org/", "https://www.rust-lang.org/"])
        p, _ = self.io.write_export("main", "chrome")
        self.assertTrue(p.startswith(self.w.ws_dir("main") + os.sep + "exports" + os.sep))
        p2, _ = self.io.write_export("main", "chrome")
        self.assertNotEqual(p, p2)


if __name__ == "__main__":
    unittest.main()
