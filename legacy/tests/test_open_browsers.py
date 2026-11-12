"""Opening links in one, some or all browsers at once. No browser is started: serve._open_in is replaced."""
import os
import sys
import tempfile
import threading
import time
import unittest
from types import SimpleNamespace

sys.path.insert(0, os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "lib"))
import paths  # noqa: E402,F401
import serve  # noqa: E402
import tabs  # noqa: E402

INSTALLED = [{"name": "Google Chrome"}, {"name": "Brave Browser"}, {"name": "Safari"}]


class OpenWith(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.saved = (tabs.DB_PATH, tabs.SESSIONS, tabs._ready, tabs.installed_browsers, serve._open_in)
        tabs.SESSIONS = self.tmp.name
        tabs.DB_PATH = os.path.join(self.tmp.name, "tabs.sqlite3")
        tabs._ready = False
        tabs.installed_browsers = lambda refresh=False: INSTALLED
        self.addCleanup(self.restore)

    def restore(self):
        tabs.DB_PATH, tabs.SESSIONS, tabs._ready, tabs.installed_browsers, serve._open_in = self.saved

    def test_setting_is_a_list_and_reads_an_old_single_name(self):
        tabs.save_settings({"open_with": "Brave Browser"})  # the form saved before 2026-11-12
        self.assertEqual(tabs.settings()["open_with"], ["Brave Browser"])
        tabs.save_settings({"open_with": ["Safari", "Google Chrome"]})
        self.assertEqual(tabs.open_targets(), ["Google Chrome", "Safari"])  # registry order, not click order
        with self.assertRaises(ValueError):
            tabs.save_settings({"open_with": ["Netscape"]})

    def test_default_is_the_system_default_browser(self):
        # Until 2026-11-12 this was the first installed Chromium browser; the owner asked for the system default.
        self.assertEqual(tabs.open_targets(), [tabs.SYSTEM_DEFAULT])

    def test_system_default_stands_alone_and_is_accepted(self):
        tabs.save_settings({"open_with": ["Safari", tabs.SYSTEM_DEFAULT]})
        self.assertEqual(tabs.open_targets(), [tabs.SYSTEM_DEFAULT])

    def test_reads_the_system_default_from_launch_services(self):
        import plistlib
        tabs.installed_browsers = lambda refresh=False: [{"name": "Google Chrome", "bundle": "com.google.Chrome"},
                                                          {"name": "Safari", "bundle": "com.apple.Safari"}]
        f = os.path.join(self.tmp.name, "ls.plist")
        saved = tabs.LS_PLIST
        self.addCleanup(lambda: setattr(tabs, "LS_PLIST", saved))
        tabs.LS_PLIST = f
        with open(f, "wb") as fh:  # macOS stores the bundle id lower-cased
            plistlib.dump({"LSHandlers": [{"LSHandlerURLScheme": "mailto", "LSHandlerRoleAll": "com.apple.mail"},
                                          {"LSHandlerURLScheme": "https", "LSHandlerRoleAll": "com.apple.safari"}]}, fh)
        self.assertEqual(tabs.default_browser(), "Safari")
        os.remove(f)
        self.assertIsNone(tabs.default_browser())  # no file: unknown, not an error

    def test_system_default_opens_through_that_browser_or_plain_open(self):
        cmds = []
        saved = (tabs.default_browser, serve._launch)
        self.addCleanup(lambda: (setattr(tabs, "default_browser", saved[0]), setattr(serve, "_launch", saved[1])))
        serve._launch = lambda cmd: cmds.append(cmd) or SimpleNamespace(returncode=0, stdout="", stderr="")
        tabs.installed_browsers = lambda refresh=False: [{"name": "Google Chrome", "bundle": "com.google.Chrome", "path": "/Applications/Google Chrome.app",
                                                          "engine": "chromium", "private_flag": "--incognito"}]
        tabs.default_browser = lambda: "Google Chrome"
        _, used = serve._open_in(["https://a.example/"], "window", tabs.SYSTEM_DEFAULT)
        self.assertEqual(used, "Google Chrome")
        self.assertIn("--new-window", cmds[-1])
        tabs.default_browser = lambda: None  # a default this tool does not know
        _, used = serve._open_in(["https://a.example/"], "tabs", tabs.SYSTEM_DEFAULT)
        self.assertEqual(cmds[-1], ["open", "https://a.example/"])
        with self.assertRaises(serve.OpenRefused):
            serve._open_in(["https://a.example/"], "incognito", tabs.SYSTEM_DEFAULT)

    def test_browser_icons_only_for_installed_browsers_and_cached(self):
        saved = tabs.ICON_CACHE
        self.addCleanup(lambda: setattr(tabs, "ICON_CACHE", saved))
        tabs.ICON_CACHE = os.path.join(self.tmp.name, "icons")
        tabs.installed_browsers = lambda refresh=False: [{"name": "Safari", "bundle": "com.apple.Safari", "path": "/nonexistent/Safari.app"}]
        self.assertIsNone(tabs.browser_icon("Safari"))             # installed, but no icon file to read
        os.makedirs(tabs.ICON_CACHE)
        with open(os.path.join(tabs.ICON_CACHE, f"com.apple.Safari-{tabs.ICON_SIZE}.png"), "wb") as f:
            f.write(b"PNGDATA")
        self.assertEqual(tabs.browser_icon("Safari"), b"PNGDATA")  # a cached icon is served without converting again
        for bad in ("../../etc/passwd", "Safari ", "safari", ""):  # only an installed browser's exact name gets an icon
            self.assertIsNone(tabs.browser_icon(bad), bad)

    def test_a_slow_launch_does_not_block(self):
        t0 = time.monotonic()
        p = serve._launch(["sleep", "5"])  # a browser still starting
        self.assertLess(time.monotonic() - t0, 3)  # an absolute bound: a 5 s start-up must not hold the reply
        self.assertEqual(p.returncode, 0)
        p = serve._launch(["false"])  # a refusal that happens at once is still reported
        self.assertNotEqual(p.returncode, 0)

    def test_opens_in_every_browser_at_the_same_time(self):
        started, lock = [], threading.Lock()

        def fake(urls, how, name):
            with lock:
                started.append(time.monotonic())
            time.sleep(0.2)
            if name == "Safari" and how == "incognito":
                raise serve.OpenRefused("Safari cannot be asked to open a private window")
            return SimpleNamespace(returncode=0, stdout="", stderr=""), name
        serve._open_in = fake
        t0 = time.monotonic()
        res = serve._open_many(["https://a.example/"], "incognito", ["Google Chrome", "Brave Browser", "Safari"])
        self.assertLess(time.monotonic() - t0, 0.5)  # three 0.2 s opens, together, not one after another
        self.assertEqual(len(started), 3)
        self.assertEqual([r["browser"] for r in res if r["ok"]], ["Google Chrome", "Brave Browser"])
        self.assertIn("private window", next(r for r in res if not r["ok"])["error"])  # one refusal, the others open


if __name__ == "__main__":
    unittest.main()
