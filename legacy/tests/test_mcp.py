"""The MCP server, spoken to over stdio as a client would."""
import json
import os
import subprocess
import sys
import unittest

from test_workspaces import DATA, Env

TOOLS = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "mcp")


class Client:
    def __init__(self, *args):
        self.p = subprocess.Popen([sys.executable, os.path.join(TOOLS, "mcp_server.py"), *args], stdin=subprocess.PIPE,
                                  stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, env=os.environ.copy())
        self.n = 0

    def send(self, method, params=None, notify=False):
        msg = {"jsonrpc": "2.0", "method": method, "params": params or {}}
        if not notify:
            self.n += 1
            msg["id"] = self.n
        self.p.stdin.write(json.dumps(msg) + "\n")
        self.p.stdin.flush()
        if notify:
            return None
        return json.loads(self.p.stdout.readline())

    def call(self, tool, **args):
        r = self.send("tools/call", {"name": tool, "arguments": args})["result"]
        if r["isError"]:
            return r, r["content"][0]["text"]
        out = json.loads(r["content"][0]["text"])
        assert out["notice"].startswith("Everything under untrusted_data"), out
        return r, out["untrusted_data"]

    def close(self):
        self.p.stdin.close()
        self.p.wait(10)
        self.p.stdout.close()
        self.p.stderr.close()


class TestMcp(Env):
    def start(self, *args):
        c = Client(*args)
        self.addCleanup(c.close)
        init = c.send("initialize", {"protocolVersion": "2025-06-18", "capabilities": {}, "clientInfo": {"name": "t", "version": "1"}})
        self.assertEqual(init["result"]["protocolVersion"], "2025-06-18")
        self.assertEqual(init["result"]["serverInfo"]["name"], "browser-foreman")
        self.assertIn("Browser Foreman", init["result"]["instructions"])
        c.send("notifications/initialized", notify=True)
        return c

    def test_read_only_by_default(self):
        c = self.start()
        names = {t["name"] for t in c.send("tools/list")["result"]["tools"]}
        self.assertEqual(names, {"list_workspaces", "search_bookmarks", "read_folder", "get_history",
                                 "list_tab_snapshots", "read_tab_snapshot", "open_tabs_now"})
        r, text = c.call("trash", workspace="main", targets=[3])
        self.assertTrue(r["isError"])
        self.assertIn("--allow-writes", text)
        self.assertEqual(self.w.read_history("main"), [])
        _, ws = c.call("list_workspaces")
        self.assertEqual(ws[0]["id"], "main")
        _, res = c.call("search_bookmarks", workspace="main", query="rust")
        self.assertEqual([b["id"] for b in res["results"]], [2])
        _, f = c.call("read_folder", workspace="main", path="Bookmarks Bar")
        self.assertEqual([x["name"] for x in f["folders"]], ["Dev", "Empty", "News"])
        r, text = c.call("read_folder", workspace="../etc", path="")
        self.assertTrue(r["isError"])
        r, text = c.call("export_workspace", workspace="main", format="html")
        self.assertTrue(r["isError"])  # it writes a file, so it is a write
        self.assertFalse(os.path.exists(os.path.join(self.w.ws_dir("main"), "exports")))
        self.assertEqual(c.send("nope")["error"]["code"], -32601)

    def test_writes_go_through_the_history(self):
        c = self.start("--allow-writes")
        tools = {t["name"]: t for t in c.send("tools/list")["result"]["tools"]}
        self.assertEqual(len(tools), 16)
        self.assertIn("confirm", tools["trash"]["inputSchema"]["required"])
        self.assertTrue(tools["rollback"]["annotations"]["destructiveHint"])
        r, text = c.call("trash", workspace="main", targets=[3])
        self.assertTrue(r["isError"])
        self.assertIn("confirm", text)
        self.assertEqual(self.w.read_history("main"), [])
        _, ex = c.call("export_workspace", workspace="main", format="html")
        self.assertTrue(ex["path"].startswith(self.w.ws_dir("main")))
        _, r = c.call("trash", workspace="main", targets=[3, "Bookmarks Bar/Dev"], confirm=True)
        self.assertEqual(r["result"]["trashed"], 2)
        self.assertEqual(r["entry"]["op"], "trash")
        _, f = c.call("read_folder", workspace="main", path="Trash")
        self.assertEqual([x["name"] for x in f["folders"]], ["Dev"])
        _, r = c.call("put_back", workspace="main", targets=["Dev", 3])
        self.assertEqual(r["result"]["put_back"], 2)
        _, r = c.call("move_bookmarks", workspace="main", targets=[4], to="Bookmarks Bar/Misc", create=True)
        self.assertIn("Misc", r["entry"]["summary"])
        _, r = c.call("rename", workspace="main", target=1, name="Python home")
        self.assertEqual(r["entry"]["op"], "rename")
        _, r = c.call("undo", workspace="main")
        self.assertEqual(r["op"], "undo")
        _, r = c.call("search_bookmarks", workspace="main", query="python")
        self.assertEqual(r["results"][0]["title"], "Python")
        _, h = c.call("get_history", workspace="main")
        self.assertEqual([e["by"] for e in h["entries"]], ["mcp"] * len(h["entries"]))
        self.assertEqual([e["op"] for e in h["entries"]], ["undo", "rename", "move", "put-back", "trash", "start"])
        r, text = c.call("rollback", workspace="main", seq=2, confirm=False)
        self.assertTrue(r["isError"])
        _, r = c.call("rollback", workspace="main", seq=2, confirm=True)
        st, _ = self.w.get_state("main")
        self.assertEqual(self.w.locate(st["tree"]["root"])[3][1], "Trash")
        r, text = c.call("move_bookmarks", workspace="main", targets=[999], to="")
        self.assertTrue(r["isError"])
        r, text = c.call("rename", workspace="main", target="", name="x")
        self.assertTrue(r["isError"])

    def test_writes_respect_the_lock(self):
        self.w.set_locked("main", True)
        c = self.start("--allow-writes")
        for name, args in (("trash", {"targets": [3], "confirm": True}), ("rename", {"target": 1, "name": "x"}),
                           ("move_bookmarks", {"targets": [1], "to": ""}), ("undo", {}), ("rollback", {"seq": 1, "confirm": True})):
            r, text = c.call(name, workspace="main", **args)
            self.assertTrue(r["isError"], name)
            self.assertIn("locked", text, name)
        _, ws = c.call("list_workspaces")
        self.assertTrue(ws[0]["locked"])
        _, res = c.call("search_bookmarks", workspace="main", query="rust")
        self.assertEqual(res["total"], 1)  # reading still works

    def tabs_db(self, keep_incognito):
        import sqlite3
        import tabs
        p = os.path.join(self.tmp, "tabs.sqlite3")
        c = sqlite3.connect(p)
        c.row_factory = sqlite3.Row
        c.executescript(tabs.SCHEMA)  # tabs.py's own schema and writer: a change there shows up here
        snap = {"at": "2026-11-12T14:30:00", "reason": "auto", "browsers": [{"browser": "Google Chrome", "windows": [
            {"index": 1, "mode": "normal", "active": 1, "tabs": [{"url": "https://python.org/", "title": "Python"},
                                                                 {"url": "https://rust-lang.org/", "title": "Rust"}]},
            {"index": 2, "mode": "incognito", "active": 1, "tabs": [{"url": "https://secret.example/", "title": "Secret"}]}]}]}
        tabs._store(c, "2026-11-12_143000", snap)
        c.execute("INSERT INTO settings VALUES ('include_incognito', ?)", (json.dumps(keep_incognito),))
        c.commit()
        c.close()
        os.environ["BM_TABS_DB"] = p
        return p

    def test_tab_tools_are_read_only_and_hide_incognito(self):
        p = self.tabs_db(keep_incognito=False)
        before = (os.path.getmtime(p), sorted(os.listdir(self.tmp)))
        c = self.start()  # read-only mode
        _, ls = c.call("list_tab_snapshots")
        self.assertEqual([s["id"] for s in ls["snapshots"]], ["2026-11-12_143000"])
        self.assertEqual(ls["snapshots"][0]["summary"]["incognito_tabs"], 1)
        _, snap = c.call("read_tab_snapshot", id="2026-11-12_143000")
        wins = snap["browsers"][0]["windows"]
        self.assertEqual([t["title"] for t in wins[0]["tabs"]], ["Python", "Rust"])
        self.assertEqual((wins[1].get("tabs"), wins[1]["tab_count"], wins[1]["redacted"]), (None, 1, True))
        self.assertNotIn("secret.example", json.dumps(snap))
        _, now = c.call("open_tabs_now")
        self.assertEqual((now["source"], now["id"]), ("latest snapshot", "2026-11-12_143000"))
        _, q = c.call("read_tab_snapshot", id="2026-11-12_143000", query="rust")
        self.assertEqual([t["title"] for t in q["browsers"][0]["windows"][0]["tabs"]], ["Rust"])
        for bad in ("../x", "2026-11-12", "2026-11-12_999999"):
            r, _ = c.call("read_tab_snapshot", id=bad)
            self.assertTrue(r["isError"], bad)
        self.assertEqual((os.path.getmtime(p), sorted(os.listdir(self.tmp))), before)  # nothing written

    def test_tab_tools_include_incognito_only_when_the_setting_is_on(self):
        self.tabs_db(keep_incognito=True)
        c = self.start()
        r = c.send("tools/call", {"name": "read_tab_snapshot", "arguments": {"id": "2026-11-12_143000"}})["result"]
        out = json.loads(r["content"][0]["text"])
        self.assertIn("untrusted_data", out)  # page titles come back wrapped
        self.assertEqual(out["untrusted_data"]["browsers"][0]["windows"][1]["tabs"][0]["title"], "Secret")

    def test_save_a_snapshot_window_as_bookmarks(self):
        self.tabs_db(keep_incognito=False)
        c = self.start("--allow-writes")
        _, out = c.call("save_tabs_as_bookmarks", workspace="main", folder="Morning", snapshot="2026-11-12_143000",
                        browser="Google Chrome", window=1)
        self.assertEqual(out["saved"], 2)
        _, f = c.call("read_folder", workspace="main", path="Saved tabs/Morning")
        self.assertEqual([b["title"] for b in f["bookmarks"]], ["Python", "Rust"])
        r, text = c.call("save_tabs_as_bookmarks", workspace="main", folder="X", snapshot="2026-11-12_143000",
                         browser="Google Chrome", window=2)
        self.assertTrue(r["isError"])
        self.assertIn("incognito", text)  # its pages are not kept, so there is nothing to save

    def test_tab_tools_without_a_database(self):
        os.environ["BM_TABS_DB"] = os.path.join(self.tmp, "none.sqlite3")
        c = self.start()
        _, ls = c.call("list_tab_snapshots")
        self.assertEqual(ls["snapshots"], [])
        self.assertFalse(os.path.exists(os.path.join(self.tmp, "none.sqlite3")))

    def test_refresh_records_like_the_page(self):
        import mcp_server
        calls = []

        def fake(u):
            calls.append(u)
            return {"https://gone.example/": ("dead", 404, u, "404"), "https://python.org/": ("unverified", 0, u, "timeout")}.get(
                u, ("ok", 200, u, ""))
        out = mcp_server.refresh("main", ids=[1, 2, 3], checker=fake)
        self.assertEqual(sorted(calls), ["https://gone.example/", "https://python.org/", "https://rust-lang.org/"])
        by = {r["id"]: r for r in out["checked"]}
        self.assertTrue(by[1]["kept"])                  # a timeout never downgrades a working link
        self.assertEqual(by[3]["status"], "dead")
        st, _ = self.w.get_state("main")
        self.assertEqual(set(st["refreshed"]), {"2", "3"})
        self.assertEqual(self.w.read_history("main")[-1]["op"], "refresh")
        again = mcp_server.refresh("main", ids=[1], checker=fake)  # a timeout again: nothing new to record
        self.assertIsNone(again["entry"])
        self.assertEqual(len(DATA["keep"]), 4)


if __name__ == "__main__":
    unittest.main()
