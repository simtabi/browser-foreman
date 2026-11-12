"""Registering the MCP server with Claude, and the "Allow changes" setting."""
import json
import os
import stat
import sys

from test_mcp import Client
from test_workspaces import Env


class TestRegister(Env):
    def setUp(self):
        super().setUp()
        import mcp_register
        self.r = mcp_register
        self.folder = os.environ["BM_MCP_FOLDER_FILE"] = os.path.join(self.tmp, ".mcp.json")
        self.desk = os.environ["BM_MCP_DESKTOP_FILE"] = os.path.join(self.tmp, "claude_desktop_config.json")
        with open(self.desk, "w") as f:
            json.dump({"preferences": {"keep": True}, "mcpServers": {"other": {"command": "x"}}}, f)
        # A stand-in for the claude CLI that remembers what it was told, in a JSON file.
        self.store = os.path.join(self.tmp, "cli.json")
        cli = os.path.join(self.tmp, "claude")
        with open(cli, "w") as f:
            f.write(f"""#!{sys.executable}
import json, os, sys
p = {self.store!r}
d = json.load(open(p)) if os.path.exists(p) else {{}}
a = sys.argv[2:]
if a[0] == "add-json": d[a[3]] = json.loads(a[4])
elif a[0] == "remove":
    if a[-1] not in d: print("not found"); sys.exit(1)
    d.pop(a[-1])
elif a[0] == "get":
    if a[1] not in d: sys.exit(1)
    print(json.dumps(d[a[1]]))
json.dump(d, open(p, "w"))
""")
        os.chmod(cli, os.stat(cli).st_mode | stat.S_IEXEC)
        os.environ["BM_CLAUDE_CLI"] = cli
        for k in ("BM_MCP_FOLDER_FILE", "BM_MCP_DESKTOP_FILE", "BM_CLAUDE_CLI"):
            self.addCleanup(os.environ.pop, k, None)

    def test_add_status_remove_everywhere_keeps_other_entries_and_backs_up(self):
        self.assertEqual({t: v["registered"] for t, v in self.r.status().items()}, {"folder": False, "desktop": False, "code": False})
        for t in self.r.TARGETS:
            self.assertEqual(self.r.add(t), "registered")
        st = self.r.status()
        self.assertTrue(all(v["registered"] and not v["writes"] for v in st.values()), st)
        doc = json.load(open(self.desk))
        self.assertEqual(doc["preferences"], {"keep": True})
        self.assertEqual(set(doc["mcpServers"]), {"other", "bookmarks"})
        self.assertEqual(doc["mcpServers"]["bookmarks"]["args"], [self.r.SERVER])
        self.assertTrue(any(n.startswith("claude_desktop_config.json.bak-") for n in os.listdir(self.tmp)))
        self.assertEqual(self.r.add("desktop"), "already registered")
        self.assertEqual(self.r.add("folder", writes=True), "registered")
        self.assertTrue(self.r.status()["folder"]["writes"])
        for t in self.r.TARGETS:
            self.assertEqual(self.r.remove(t), "removed")
        self.assertEqual(set(json.load(open(self.desk))["mcpServers"]), {"other"})
        self.assertFalse(any(v["registered"] for v in self.r.status().values()))
        self.assertEqual(self.r.remove("desktop"), "not registered")


class TestWritesSetting(Env):
    def test_setting_turns_write_tools_on_and_off_without_restart(self):
        c = Client()
        self.addCleanup(c.close)
        c.send("initialize", {"protocolVersion": "2025-06-18"})
        names = lambda: {t["name"] for t in c.send("tools/list")["result"]["tools"]}  # noqa: E731
        self.assertNotIn("trash", names())
        self.w.save_settings({"mcp_allow_writes": True})
        self.assertIn("save_tabs_as_bookmarks", names())
        r, out = c.call("save_tabs_as_bookmarks", workspace="main", folder="From MCP",
                        links=[{"url": "https://mcp.example/", "title": "M"}, {"url": "chrome://flags", "title": "x"}])
        self.assertEqual((out["saved"], out["entry"]["op"]), (1, "add-links"))
        self.assertIn("Saved tabs/From MCP", out["entry"]["summary"])
        self.w.save_settings({"mcp_allow_writes": False})
        r, text = c.call("trash", workspace="main", targets=[3], confirm=True)
        self.assertTrue(r["isError"])
        for bad in ({"mcp_allow_writes": "yes"}, {"other": True}):
            with self.assertRaises(self.w.WsError):
                self.w.save_settings(bad)
