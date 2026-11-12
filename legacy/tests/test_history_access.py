"""history_access: tells readable, blocked and missing apart, and never reads past the first byte."""
import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "lib"))
import paths  # noqa: E402,F401  puts src/server on the import path

import history_access as ha  # noqa: E402


class TestHistoryAccess(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.ok = os.path.join(self.tmp.name, "History")
        with open(self.ok, "wb") as f:
            f.write(b"SQLite format 3\0")
        self.locked = os.path.join(self.tmp.name, "Locked")
        with open(self.locked, "wb") as f:
            f.write(b"x")
        os.chmod(self.locked, 0)
        self.addCleanup(os.chmod, self.locked, 0o600)
        self.saved = dict(ha.FILES)
        self.addCleanup(lambda: (ha.FILES.clear(), ha.FILES.update(self.saved)))

    def test_states(self):
        self.assertEqual(ha.probe(self.ok), "readable")
        self.assertEqual(ha.probe(os.path.join(self.tmp.name, "none")), "missing")
        if os.geteuid() != 0:  # root reads a mode-0 file, so the blocked case cannot be made there
            self.assertEqual(ha.probe(self.locked), "blocked")

    def test_status_is_granted_only_when_every_present_file_reads(self):
        ha.FILES.clear()
        ha.FILES.update({"A": self.ok, "Gone": os.path.join(self.tmp.name, "none")})
        s = ha.status()
        self.assertTrue(s["granted"])
        self.assertEqual(s["blocked"], 0)
        self.assertEqual(len(s["browsers"]), 2)  # every browser is reported, installed or not
        ha.FILES["B"] = self.locked
        if os.geteuid() != 0:
            s = ha.status()
            self.assertFalse(s["granted"])
            self.assertEqual(s["blocked"], 1)

    def test_nothing_installed_is_not_granted(self):
        ha.FILES.clear()
        ha.FILES["Gone"] = os.path.join(self.tmp.name, "none")
        self.assertFalse(ha.status()["granted"])


if __name__ == "__main__":
    unittest.main()
