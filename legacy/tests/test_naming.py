"""The tool is Browser Foreman (renamed from Browser Keeper on 2026-11-12). The old names must not come back, except
where files written under them are still read: old backups, the old settings file. Those lines carry OLD_."""
import hashlib
import io
import json
import os
import re
import unittest
import zipfile

from test_workspaces import Env

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
OLD = re.compile(r"Browser Keeper|browser-keeper|data-broke-|\bBK_[A-Z]|\bBK\b|__bk\b|bk:[a-z]|\bbk-modal\b", re.I)
SCAN = ("src", "docs", "README.md", "package.json", ".env.example")
SKIP = ("node_modules", "__pycache__")
THIS = os.path.abspath(__file__)


def files():
    for top in SCAN:
        p = os.path.join(ROOT, top)
        if os.path.isfile(p):
            yield p
            continue
        for d, ds, fs in os.walk(p):
            ds[:] = [x for x in ds if x not in SKIP]
            for f in fs:
                if f.endswith((".ts", ".js", ".mjs", ".py", ".md", ".html", ".json", ".css", ".txt")):
                    yield os.path.join(d, f)


class Naming(unittest.TestCase):
    def test_no_old_names(self):
        seen, bad = 0, []
        for f in files():
            if os.path.abspath(f) == THIS:
                continue
            seen += 1
            for n, line in enumerate(open(f, encoding="utf-8", errors="replace"), 1):
                if OLD.search(line) and "OLD_" not in line:
                    bad.append(f"{os.path.relpath(f, ROOT)}:{n}: {line.strip()[:100]}")
        self.assertGreater(seen, 80, f"only {seen} files read: the scan is not seeing the project")
        self.assertEqual(bad, [])


class OldFilesStillLoad(Env):
    def test_an_old_backup_passes_the_check(self):
        import backup
        settings = json.dumps({"mcp_allow_writes": True}).encode()
        manifest = {"format": "browser-keeper-backup", "version": 1, "files": {
            "browser-keeper.json": {"sha256": hashlib.sha256(settings).hexdigest(), "size": len(settings)}}}
        buf = io.BytesIO()
        with zipfile.ZipFile(buf, "w") as z:
            z.writestr("manifest.json", json.dumps(manifest))
            z.writestr("browser-keeper.json", settings)
        _, m = backup.check(buf.getvalue())
        self.assertEqual(m["format"], "browser-keeper-backup")

    def test_the_old_settings_file_is_still_read(self):
        old = os.path.join(os.path.dirname(self.w.ws_root()), self.w.OLD_SETTINGS_FILE)
        with open(old, "w", encoding="utf-8") as f:
            json.dump({"mcp_allow_writes": True}, f)
        self.addCleanup(lambda: os.path.exists(old) and os.remove(old))
        self.assertTrue(self.w.read_settings()["mcp_allow_writes"])
        self.w.save_settings({"mcp_allow_writes": False})  # writes the new file, which wins from then on
        self.assertTrue(os.path.exists(self.w.settings_path()))
        self.assertFalse(self.w.read_settings()["mcp_allow_writes"])


if __name__ == "__main__":
    unittest.main()
