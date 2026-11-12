"""serve.py's workspace endpoints, through a real server on a free local port."""
import base64
import http.client
import json
import os
import sys
import threading
import unittest
from http.server import ThreadingHTTPServer

from test_workspaces import Env, chrome_json, netscape

sys.path.insert(0, os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "lib"))
import paths  # noqa: E402,F401  puts src/server, src/mcp, src/pipeline and src/lib on the import path


class TestServer(Env):
    def setUp(self):
        super().setUp()
        import serve
        import ws_api
        self.api = ws_api
        self.srv = ThreadingHTTPServer(("127.0.0.1", 0), serve.Handler)
        self.port = self.srv.server_address[1]
        threading.Thread(target=self.srv.serve_forever, daemon=True).start()
        self.host = f"localhost:{self.port}"

    def tearDown(self):
        self.srv.shutdown()
        self.srv.server_close()
        super().tearDown()

    def req(self, method, path, body=None, token=True, headers=None, host=None):
        c = http.client.HTTPConnection("127.0.0.1", self.port, timeout=10)
        h = {"Host": host or self.host}
        if body is not None:
            h["Content-Type"] = "application/json"
        if token:
            h["X-BM-Token"] = self.api.TOKEN
        h.update(headers or {})
        c.request(method, path, body=None if body is None else json.dumps(body), headers=h)
        r = c.getresponse()
        raw = r.read()
        c.close()
        try:
            return r.status, json.loads(raw)
        except ValueError:
            return r.status, raw

    def test_reads_need_a_local_host_and_same_origin(self):
        self.assertEqual(self.req("GET", "/api/workspaces")[0], 200)
        self.assertEqual(self.req("GET", "/api/workspaces", host="evil.example:80")[0], 403)  # DNS rebinding
        self.assertEqual(self.req("GET", "/api/ws/main/state", headers={"Origin": "http://evil.example"})[0], 403)
        self.assertEqual(self.req("GET", "/api/ws/main/state", headers={"Sec-Fetch-Site": "cross-site"})[0], 403)
        self.assertEqual(self.req("GET", "/preview.html", host="evil.example")[0], 403)

    def test_writes_need_the_token_json_and_origin(self):
        body = {"state": {"tree": None}, "base": 0}
        self.assertEqual(self.req("POST", "/api/ws/main/commit", body, token=False)[0], 403)
        self.assertEqual(self.req("POST", "/api/ws/main/commit", body, headers={"X-BM-Token": "wrong"})[0], 403)
        self.assertEqual(self.req("POST", "/api/ws/main/commit", body, headers={"Origin": "http://evil.example"})[0], 403)
        self.assertEqual(self.req("POST", "/api/ws/main/commit", body, headers={"Content-Type": "text/plain"})[0], 403)
        self.assertEqual(self.req("POST", "/api/refresh", {"items": []}, token=False)[0], 403)
        self.assertEqual(self.req("POST", "/api/open", {"urls": []}, token=False)[0], 403)

    def test_traversal_ids_are_refused(self):
        for bad in ("..", "..%2f..", "%2e%2e", "MAIN", "a.b"):
            code, _ = self.req("GET", f"/api/ws/{bad}/state")
            self.assertIn(code, (400, 404), bad)
            code, _ = self.req("POST", f"/api/ws/{bad}/rename", {"name": "x"})
            self.assertIn(code, (400, 404), bad)

    def test_page_embeds_state_and_token(self):
        code, raw = self.req("GET", "/preview.html")
        self.assertEqual(code, 200)
        html = raw.decode()
        self.assertIn("window.BM_WS = ", html)
        self.assertIn(self.api.TOKEN, html)
        # BM_WS is set before the page's data; the workspace client itself is bundled in assets/js/app.js.
        self.assertLess(html.index("window.BM_WS = "), html.index("const DATA"))
        self.assertNotIn("ws_client.js", html)
        for page in ("/", "/index.html"):  # the page's addresses; /preview.html is kept for old bookmarks
            self.assertIn("window.BM_WS = ", self.req("GET", page)[1].decode(), page)

    def test_commit_history_undo_rollback(self):
        _, s = self.req("GET", "/api/ws/main/state")
        self.assertEqual(s["head"], 0)
        tree = {"root": {"n": "", "c": [{"n": "Bookmarks Bar", "c": [1, 2, 3]}, 4]}, "titles": {"1": "Py"}}
        code, r = self.req("POST", "/api/ws/main/commit", {"state": {"tree": tree}, "base": 0})
        self.assertEqual(code, 200)
        self.assertEqual(r["head"], 2)
        self.assertEqual(self.req("POST", "/api/ws/main/commit", {"state": {"tree": None}, "base": 0})[0], 409)
        _, h = self.req("GET", "/api/ws/main/history")
        self.assertEqual([e["op"] for e in h["entries"]], [r["entry"]["op"], "start"])
        code, u = self.req("POST", "/api/ws/main/undo", {})
        self.assertEqual((code, u["state"]["tree"]), (200, None))
        code, rb = self.req("POST", "/api/ws/main/rollback", {"seq": 2})
        self.assertEqual(rb["state"]["tree"]["titles"], {"1": "Py"})
        code, at = self.req("GET", "/api/ws/main/history/1")
        self.assertEqual(at["state"]["tree"], None)
        self.assertEqual(self.req("POST", "/api/ws/main/commit", {"state": {"tree": {"root": {"n": 1}}}, "base": 4})[0], 400)
        # What WS.factoryReset() sends: back to the generated tree, as one more (undoable) entry.
        code, r = self.req("POST", "/api/ws/main/commit", {"state": {"tree": None, "urls": {}, "refreshed": {}}, "base": 4,
                                                             "reset": True})
        self.assertEqual((code, r["entry"]["op"]), (200, "reset"))
        self.assertEqual(r["entry"]["summary"], "Reset to the generated tree, clearing the tree's edits")
        code, u = self.req("POST", "/api/ws/main/undo", {})
        self.assertEqual(u["state"]["tree"]["titles"], {"1": "Py"})

    def test_commit_with_only_the_changed_parts(self):
        self.req("POST", "/api/ws/main/commit", {"state": {"tree": None, "urls": {"2": "https://www.rust-lang.org/"},
                                                           "refreshed": {"1": {"s": "ok"}}}, "base": 0})
        tree = {"root": {"n": "", "c": [{"n": "Bookmarks Bar", "c": [1, 2, 3]}, 4]}, "titles": {"1": "Py"}}
        code, r = self.req("POST", "/api/ws/main/commit", {"changes": {"tree": tree, "evil": 1}, "base": 2})
        self.assertEqual((code, r["head"]), (200, 3))
        st = self.req("GET", "/api/ws/main/state")[1]["state"]
        self.assertEqual((st["tree"]["titles"], st["urls"], st["refreshed"]),
                         ({"1": "Py"}, {"2": "https://www.rust-lang.org/"}, {"1": {"s": "ok"}}))  # the rest was kept
        self.assertNotIn("evil", st)
        self.assertEqual(self.req("POST", "/api/ws/main/commit", {"changes": {"urls": {}}, "base": 2})[0], 409)
        self.assertEqual(self.req("POST", "/api/ws/main/commit", {"changes": {"urls": {}}, "base": None})[0], 200)

    def test_migrate_rename_archive_notes(self):
        code, r = self.req("POST", "/api/ws/main/migrate", {"state": {"tree": None, "urls": {"1": "https://www.python.org/"}}})
        self.assertEqual((code, r["entry"]["op"]), (200, "migrate"))
        self.assertEqual(self.req("POST", "/api/ws/main/migrate", {"state": {}})[0], 409)
        self.assertEqual(self.req("POST", "/api/ws/main/rename", {"name": "Chrome"})[1]["workspace"]["name"], "Chrome")
        self.assertTrue(self.req("POST", "/api/ws/main/archive", {"archived": True})[1]["workspace"]["archived"])
        self.assertEqual(self.req("POST", "/api/ws/main/notes", {"notes": "half done"})[1]["workspace"]["notes"], "half done")
        _, ws = self.req("GET", "/api/workspaces")
        self.assertTrue(ws["workspaces"][0]["archived"])

    def test_import_upload_page_export_and_duplicates(self):
        files = [{"name": "Bookmarks", "data": base64.b64encode(chrome_json()).decode()},
                 {"name": "export.html", "data": base64.b64encode(netscape([("https://python.org", "", 1)])).decode()}]
        code, r = self.req("POST", "/api/ws", {"name": "Laptop", "files": files})
        self.assertEqual(code, 200, r)
        ws = r["id"]
        self.assertEqual(ws, "laptop")
        code, raw = self.req("GET", f"/preview.html?ws={ws}")
        self.assertEqual(code, 200)
        self.assertIn('"id": "laptop"', raw.decode())
        self.assertIn("python.org", raw.decode())  # its own data, not the main set's
        code, raw = self.req("GET", f"/api/ws/{ws}/export?format=html")
        self.assertEqual(code, 200)
        self.assertIn(b"NETSCAPE-Bookmark-file-1", raw)
        code, doc = self.req("GET", f"/api/ws/{ws}/export?format=chrome")
        self.assertIn("bookmark_bar", doc["roots"])
        code, raw = self.req("GET", f"/api/ws/{ws}/duplicates.csv")
        self.assertEqual(raw.count(b"\n"), 2)
        code, r = self.req("POST", f"/api/ws/{ws}/sources", {"files": [{"name": "more.html",
                           "data": base64.b64encode(netscape([("https://new.example/", "New", 3)])).decode()}]})
        self.assertEqual(code, 200, r)
        self.assertIn("1 new bookmarks", r["entry"]["summary"])
        code, r = self.req("POST", f"/api/ws/{ws}/export", {"format": "chrome"})
        self.assertTrue(os.path.exists(r["path"]))
        self.assertEqual(self.req("POST", "/api/ws", {"name": "x", "files": [{"name": "a", "data": "!!"}]})[0], 400)
        self.assertEqual(self.req("POST", "/api/ws", {"name": "x", "paths": ["/etc/passwd"]})[0], 400)
        self.assertEqual(self.req("POST", "/api/ws", {"name": "x", "files": [{"name": "a.html",
                         "data": base64.b64encode(b"not bookmarks").decode()}]})[0], 400)

    def test_path_tricks_still_get_the_workspace_page(self):
        for p in ("/./preview.html", "//preview.html", "/x/../preview.html", "/%2e/preview.html"):
            code, raw = self.req("GET", p)
            self.assertEqual(code, 200, p)
            self.assertIn(b"window.BM_WS = ", raw, p)
            self.assertEqual(self.req("GET", p, host="evil.example")[0], 403, p)

    def test_embedded_json_cannot_break_the_script(self):
        evil = '<!--<script></script>&\u2028'
        files = [{"name": "x.html", "data": base64.b64encode(netscape([("https://a.example/", evil, 1)])).decode()}]
        code, r = self.req("POST", "/api/ws", {"name": "Evil " + evil, "files": files})
        self.assertEqual(code, 200, r)
        code, raw = self.req("GET", f"/preview.html?ws={r['id']}")
        html = raw.decode()
        boot = html[html.index("window.BM_WS = "):html.index(";</script>")]
        data = html[html.index("const DATA = "):]
        data = data[:data.index("\n")]
        for chunk in (boot, data):
            for bad in ("<", ">", "&", "\u2028"):
                self.assertNotIn(bad, chunk.split("=", 1)[1])

    def test_odd_tokens_and_lengths_are_refused_not_crashed(self):
        self.assertEqual(self.req("POST", "/api/ws/main/commit", {"state": {}}, headers={"X-BM-Token": "t\u00e9st"})[0], 403)
        c = http.client.HTTPConnection("127.0.0.1", self.port, timeout=5)
        c.putrequest("POST", "/api/ws/main/notes", skip_host=True)
        for k, v in (("Host", self.host), ("Content-Type", "application/json"), ("X-BM-Token", self.api.TOKEN),
                     ("Content-Length", "-5")):
            c.putheader(k, v)
        c.endheaders()
        self.assertEqual(c.getresponse().status, 400)
        c.close()
        self.assertEqual(self.req("GET", "/api/ping")[0], 200)  # still serving

    def test_workspace_files_are_never_served(self):
        files = [{"name": "evil.html", "data": base64.b64encode(netscape([("https://a.example/", "A", 1)])).decode()}]
        ws = self.req("POST", "/api/ws", {"name": "S", "files": files})[1]["id"]
        stored = self.w.read_meta(ws)["sources"][0]["stored"]
        for p in (f"/workspaces/{ws}/{stored}", f"/workspaces/{ws}/meta.json", f"/./workspaces/{ws}/data.json",
                  "/workspaces/", "/src/server/ws_api.py", "/storage/sessions/tabs.sqlite3", "/.env", "/public/index.html",
                  "/assets/../src/lib/paths.py", "/thumbs/../../.env", "/package.json"):
            self.assertIn(self.req("GET", p)[0], (403, 404), p)
        code, raw = self.req("GET", f"/api/ws/{ws}/export?format=html")
        self.assertEqual(code, 200)

    def test_build_workspace_serves_the_build_duplicates_or_404(self):
        self.assertEqual(self.req("GET", "/api/ws/main/duplicates.csv")[0], 404)  # none written yet: not an empty 200
        with open(os.path.join(os.path.dirname(os.environ["BM_PREVIEW"]), "duplicates.csv"), "w") as f:
            f.write("kept_title,kept_url\nA,https://a.example/\n")
        code, raw = self.req("GET", "/api/ws/main/duplicates.csv")
        self.assertEqual((code, raw.count(b"\n")), (200, 2))

    def test_lock_delete_trash_reset_update_endpoints(self):
        files = [{"name": "a.html", "data": base64.b64encode(netscape([("https://a.example/", "A", 1)])).decode()}]
        ws = self.req("POST", "/api/ws", {"name": "Tmp", "files": files})[1]["id"]
        self.assertTrue(self.req("POST", f"/api/ws/{ws}/lock", {"locked": True})[1]["workspace"]["locked"])
        for path, body in ((f"/api/ws/{ws}/commit", {"state": {"tree": None, "urls": {"1": "x"}}}),
                           (f"/api/ws/{ws}/rollback", {"seq": 1}), (f"/api/ws/{ws}/undo", {}), (f"/api/ws/{ws}/reset", {}),
                           (f"/api/ws/{ws}/update", {}), (f"/api/ws/{ws}/delete", {"confirm": ws}), (f"/api/ws/{ws}/rename", {"name": "x"})):
            self.assertEqual(self.req("POST", path, body)[0], 423, path)
        self.assertIn(b'"locked": true', self.req("GET", f"/preview.html?ws={ws}")[1])
        self.req("POST", f"/api/ws/{ws}/lock", {"locked": False})
        self.assertEqual(self.req("GET", f"/api/ws/{ws}/update")[1]["new"], [])
        self.assertEqual(self.req("POST", f"/api/ws/{ws}/reset", {})[0], 200)
        self.assertEqual(self.req("POST", f"/api/ws/{ws}/delete", {})[0], 400)            # no confirm
        self.assertEqual(self.req("POST", "/api/ws/main/delete", {"confirm": "main"})[0], 400)
        code, r = self.req("POST", f"/api/ws/{ws}/delete", {"confirm": ws})
        self.assertEqual(code, 200)
        tid = r["trash_id"]
        self.assertEqual(self.req("GET", f"/api/ws/{ws}/state")[0], 404)
        self.assertEqual([t["trash_id"] for t in self.req("GET", "/api/trash")[1]["trash"]], [tid])
        self.assertEqual(self.req("GET", f"/workspaces/_trash/{tid}/meta.json")[0], 404)  # never served
        self.assertEqual(self.req("POST", "/api/trash/empty", {"trash_ids": [tid]})[0], 400)  # no confirm words
        self.assertEqual(self.req("POST", "/api/trash/empty", {"confirm": "empty the workspace trash"}, token=False)[0], 403)
        self.assertEqual(self.req("POST", "/api/trash/restore", {"trash_id": "../../x"})[0], 400)
        self.assertEqual(self.req("POST", "/api/trash/restore", {"trash_id": tid})[1]["id"], ws)
        tid = self.req("POST", f"/api/ws/{ws}/delete", {"confirm": ws})[1]["trash_id"]
        code, r = self.req("POST", "/api/trash/empty", {"confirm": "empty the workspace trash", "trash_ids": [tid]})
        self.assertEqual((code, r["removed"]), (200, [tid]))

    def test_add_links_backup_restore_endpoints(self):
        code, r = self.req("POST", "/api/ws/main/add-links", {"folder": "Saved tabs/Now",
                           "links": [{"url": "https://saved.example/", "title": "Saved <b>"}]})
        self.assertEqual((code, r["entry"]["op"]), (200, "add-links"))
        html = self.req("GET", "/preview.html")[1].decode()
        data = html[html.index("const DATA = "):]
        self.assertIn("saved.example", data[:data.index(";\n")])  # the built page's data now holds it
        self.assertNotIn("<b>", data[:data.index(";\n")])
        self.assertEqual(self.req("POST", "/api/ws/main/add-links", {"folder": "X", "links": [{"url": "javascript:x"}]})[0], 400)
        self.assertEqual(self.req("POST", "/api/ws/main/add-links", {"folder": "X", "links": [{"url": "https://a/"}]},
                                  token=False)[0], 403)
        code, raw = self.req("GET", "/api/backup")
        self.assertEqual(code, 200)
        self.assertEqual(raw[:2], b"PK")
        self.assertEqual(self.req("GET", "/api/backup", host="evil.example")[0], 403)
        body = {"data": base64.b64encode(raw).decode()}
        self.assertEqual(self.req("POST", "/api/restore", body)[0], 400)  # no typed confirmation
        self.assertEqual(self.req("POST", "/api/restore", {**body, "confirm": "restore"}, token=False)[0], 403)
        self.req("POST", "/api/ws/main/undo", {})
        code, r = self.req("POST", "/api/restore", {**body, "confirm": "restore"})
        self.assertEqual(code, 200, r)
        self.assertEqual(self.req("GET", "/api/ws/main/state")[1]["state"]["added"]["5"]["u"], "https://saved.example/")

    def test_profiles_lists_without_failing(self):
        code, r = self.req("GET", "/api/profiles")
        self.assertEqual(code, 200)
        self.assertIsInstance(r["profiles"], list)


if __name__ == "__main__":
    unittest.main()
