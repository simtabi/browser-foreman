"""Can the server read each browser's visit history? The check behind Settings › Browser history.

macOS keeps every browser's history file behind Full Disk Access, so a read answers "Operation not permitted" until
the owner grants it in System Settings › Privacy & Security › Full Disk Access. This module only finds out which
files are readable and opens that settings pane; it never reads a URL, a title or a visit. Reading history itself
waits for the Go port (one signed binary that can be granted access by name; see docs/architecture.md).

macOS grants access to the app that started the server (Terminal, iTerm, the Claude app), not to a script, so the
answer names that app as well as the Python binary.
"""
import os
import subprocess
import sys

_LIB = os.path.expanduser("~/Library")
# One profile per browser: the default one. Each is a SQLite file the browser keeps open while it runs.
FILES = {
    "Google Chrome": f"{_LIB}/Application Support/Google/Chrome/Default/History",
    "Brave Browser": f"{_LIB}/Application Support/BraveSoftware/Brave-Browser/Default/History",
    "Microsoft Edge": f"{_LIB}/Application Support/Microsoft Edge/Default/History",
    "Arc": f"{_LIB}/Application Support/Arc/User Data/Default/History",
    "Vivaldi": f"{_LIB}/Application Support/Vivaldi/Default/History",
    "Safari": f"{_LIB}/Safari/History.db",
}
SETTINGS_URL = "x-apple.systempreferences:com.apple.preference.security?Privacy_AllFiles"


def probe(path):
    """'readable', 'blocked' (no Full Disk Access) or 'missing'. Reads one byte, never the contents."""
    if not os.path.exists(path):
        return "missing"
    try:
        with open(path, "rb") as f:
            f.read(1)
        return "readable"
    except PermissionError:
        return "blocked"
    except OSError:
        return "blocked"


def status():
    browsers = [{"browser": name, "state": probe(path)} for name, path in FILES.items()]
    present = [b for b in browsers if b["state"] != "missing"]
    return {
        "browsers": browsers,
        "granted": bool(present) and all(b["state"] == "readable" for b in present),
        "blocked": sum(b["state"] == "blocked" for b in present),
        "python": os.path.realpath(sys.executable),
        "launched_by": os.environ.get("TERM_PROGRAM") or os.environ.get("__CFBundleIdentifier") or "",
        "settings_url": SETTINGS_URL,
    }


def open_settings():
    """Open System Settings at Full Disk Access. The owner adds the app there; nothing is granted from here."""
    r = subprocess.run(["open", SETTINGS_URL], capture_output=True, text=True, timeout=10)
    return {"ok": r.returncode == 0, "error": (r.stderr or "").strip()[:200]}
