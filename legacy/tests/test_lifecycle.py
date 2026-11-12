"""Lock, reset, update from source, delete to the workspace trash, restore and empty."""
import json
import os
import unittest

from test_workspaces import DATA, Env, fake_preview, netscape


class TestLifecycle(Env):
    def imported(self, items=(("https://a.example/", "A", 5), ("https://b.example/", "B", 6))):
        return self.io.import_sources("Laptop", [("a.html", netscape(list(items)))])

    def test_lock_refuses_every_change_until_unlocked(self):
        ws = self.imported()
        st, head = self.w.get_state(ws)
        self.w.set_locked(ws, True)
        for fn in (lambda: self.w.commit(ws, {"tree": None, "urls": {"1": "x"}}, base_seq=head),
                   lambda: self.w.rollback(ws, 1), lambda: self.w.undo(ws), lambda: self.w.rename_workspace(ws, "x"),
                   lambda: self.w.set_notes(ws, "x"), lambda: self.w.set_archived(ws, True), lambda: self.w.reset_workspace(ws),
                   lambda: self.io.update_apply(ws), lambda: self.io.add_sources(ws, [("c.html", netscape([]))]),
                   lambda: self.w.delete_workspace(ws)):
            with self.assertRaises(self.w.Locked):
                fn()
        self.assertEqual(self.w.Locked.code, 423)
        self.assertEqual(self.w.search(ws, "a")["total"] >= 1, True)  # reading still works
        self.w.set_locked(ws, False)
        self.w.rename_workspace(ws, "Unlocked")
        self.assertEqual([e["op"] for e in self.w.read_history(ws)][-3:], ["lock", "unlock", "rename-workspace"])

    def test_main_lock_refuses_migrate(self):
        self.w.set_locked("main", True)
        with self.assertRaises(self.w.Locked):
            self.w.migrate("main", {"tree": None, "urls": {"1": "x"}})

    def test_reset_is_one_undoable_entry(self):
        ws = self.imported()
        data = self.w.load_data(ws)
        st, head = self.w.get_state(ws)
        st = self.w.editable(st, data)
        self.w.op_trash(st, [data["keep"][0]["id"]])
        self.w.commit(ws, st, base_seq=head)
        e = self.w.reset_workspace(ws)
        self.assertEqual(e["op"], "reset")
        self.assertTrue(e["summary"].startswith("Reset to how it was imported"))
        self.assertIsNone(self.w.get_state(ws)[0]["tree"])
        self.w.undo(ws)
        self.assertIsNotNone(self.w.get_state(ws)[0]["tree"])
        self.w.commit("main", {"tree": None, "urls": {"1": "https://www.python.org/"}})
        self.assertTrue(self.w.reset_workspace("main")["summary"].startswith("Reset to the generated tree"))

    def test_update_from_stored_source_keeps_edits(self):
        ws = self.imported()
        data = self.w.load_data(ws)
        a = next(i for i in data["keep"] if "a.example" in i["u"])
        st, head = self.w.get_state(ws)
        st = self.w.editable(st, data)
        self.w.op_rename(st, a["id"], "My A", data)
        self.w.commit(ws, st, base_seq=head)
        stored = os.path.join(self.w.ws_dir(ws), self.w.read_meta(ws)["sources"][0]["stored"])
        with open(stored, "wb") as f:  # upstream: A retitled, B gone, C new
            f.write(netscape([("https://a.example/", "A2", 5), ("https://c.example/", "C", 7)]))
        pv = self.io.update_preview(ws)
        self.assertEqual(([b["url"] for b in pv["new"]], [b["url"] for b in pv["removed"]], [c["new_title"] for c in pv["changed"]]),
                         (["https://c.example/"], ["https://b.example/"], ["A2"]))
        e, _ = self.io.update_apply(ws)
        self.assertEqual(e["op"], "update")
        data = self.w.load_data(ws)
        self.assertEqual(sorted(i["u"] for i in data["keep"]), ["https://a.example/", "https://b.example/", "https://c.example/"])
        self.assertEqual(self.w.search(ws, "a.example")["results"][0]["title"], "My A")  # the rename still wins
        self.assertEqual(self.io.update_preview(ws)["new"], [])

    def test_update_main_follows_a_rebuild(self):
        self.assertEqual(self.io.update_preview("main")["new"], [])  # no edits: nothing to do
        st = self.w.editable(self.w.get_state("main")[0], DATA)
        self.w.op_rename(st, 1, "Py", DATA)
        self.w.commit("main", st)
        data = json.loads(json.dumps(DATA))
        data["keep"] = [i for i in data["keep"] if i["id"] != 2] + [dict(data["keep"][0], id=9, u="https://new.example/", t="New")]
        fake_preview(os.environ["BM_PREVIEW"], data)
        pv = self.io.update_preview("main")
        self.assertEqual(([b["id"] for b in pv["new"]], [b["id"] for b in pv["removed"]]), ([9], [2]))
        e, _ = self.io.update_apply("main")
        ids = set(self.w.locate(self.w.get_state("main")[0]["tree"]["root"]))
        self.assertEqual(ids, {1, 3, 4, 9})
        self.assertEqual(self.w.get_state("main")[0]["tree"]["titles"], {"1": "Py"})
        self.w.undo("main")
        self.assertIn(2, set(self.w.locate(self.w.get_state("main")[0]["tree"]["root"])))

    def test_delete_restore_and_empty(self):
        ws = self.imported()
        with self.assertRaises(self.w.WsError):
            self.w.delete_workspace("main")
        tid = self.w.delete_workspace(ws)
        self.assertFalse(os.path.exists(self.w.ws_dir(ws)))
        self.assertTrue(os.path.isdir(os.path.join(self.w.trash_root(), tid)))
        self.assertNotIn(ws, [m["id"] for m in self.w.list_workspaces()])
        self.assertEqual([t["trash_id"] for t in self.w.list_trash()], [tid])
        ws2 = self.imported()  # takes the free id
        self.assertEqual(ws2, ws)
        back = self.w.restore_workspace(tid)
        self.assertNotEqual(back, ws)  # the id was taken, so it came back under a new one
        self.assertEqual(self.w.read_meta(back)["id"], back)
        self.assertEqual(self.w.read_history(back)[-1]["op"], "restore")
        tid2 = self.w.delete_workspace(back)
        for bad in ("../x", "main", "x-1", tid2 + "/.."):
            with self.assertRaises(self.w.WsError):
                self.w.empty_trash([bad])
        self.assertEqual(self.w.empty_trash(), [tid2])
        self.assertEqual(self.w.list_trash(), [])
        self.assertTrue(os.path.isdir(self.w.ws_dir(ws2)))  # only the trash was emptied


if __name__ == "__main__":
    unittest.main()
