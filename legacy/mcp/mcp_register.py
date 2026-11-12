"""Register Browser Foreman's MCP server with Claude, in any of three places, or see where it is registered.

    python3 src/mcp/mcp_register.py status
    python3 src/mcp/mcp_register.py add    [--target folder|code|desktop|all] [--writes]
    python3 src/mcp/mcp_register.py remove [--target folder|code|desktop|all]

Targets:
    folder   .mcp.json in the bookmarks-cleanup folder: Claude Code sessions started there offer it (approve it once)
    code     Claude Code, user-wide (through `claude mcp add-json --scope user`), for every folder
    desktop  the Claude desktop app's claude_desktop_config.json, for regular chats (restart the app afterwards)

The server is registered under the key "bookmarks" (kept from before the Browser Foreman name, so existing
configurations keep working). Without --writes it starts read-only, unless the "Allow changes" setting is on in the
page (browser-foreman.json); with --writes it always allows changes.

Each config file this edits is copied to <file>.bak-<time> first, and only the "bookmarks" entry is added, replaced
or removed: everything else in the file is left as it was.
"""
import argparse
import json
import os
import shutil
import subprocess
import sys
import time

sys.path.insert(0, os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "lib"))
import paths  # noqa: E402,F401  src/lib/paths.py: every folder, .env, and the src/ folders on the import path

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = paths.ROOT
KEY = "bookmarks"
SERVER = os.path.join(HERE, "mcp_server.py")
PYTHON = next((p for p in ("/opt/homebrew/bin/python3", "/usr/local/bin/python3", "/usr/bin/python3")
               if os.path.exists(p)), sys.executable)
TARGETS = ("folder", "code", "desktop")


def folder_file():
    return os.environ.get("BM_MCP_FOLDER_FILE") or os.path.join(ROOT, ".mcp.json")


def desktop_file():
    return os.environ.get("BM_MCP_DESKTOP_FILE") or os.path.expanduser(
        "~/Library/Application Support/Claude/claude_desktop_config.json")


def claude_cli():
    return os.environ.get("BM_CLAUDE_CLI") or shutil.which("claude") or os.path.expanduser("~/.local/bin/claude")


def entry(writes=False):
    return {"command": PYTHON, "args": [SERVER] + (["--allow-writes"] if writes else [])}


def _read(path):
    if not os.path.exists(path):
        return {}
    with open(path, encoding="utf-8") as f:
        text = f.read()
    return json.loads(text) if text.strip() else {}


def _write(path, doc):
    if os.path.exists(path):
        shutil.copy2(path, f"{path}.bak-{time.strftime('%Y%m%d-%H%M%S')}")
    tmp = f"{path}.tmp-{os.getpid()}"
    with open(tmp, "w", encoding="utf-8") as f:
        json.dump(doc, f, indent=2)
        f.write("\n")
    if os.path.exists(path):
        shutil.copymode(path, tmp)
    os.replace(tmp, path)


def _file_set(path, value):
    doc = _read(path)
    servers = doc.get("mcpServers") if isinstance(doc.get("mcpServers"), dict) else {}
    if value is None:
        if KEY not in servers:
            return "not registered"
        servers.pop(KEY)
    else:
        if servers.get(KEY) == value:
            return "already registered"
        servers[KEY] = value
    doc["mcpServers"] = servers
    _write(path, doc)
    return "removed" if value is None else "registered"


def _cli(*args):
    p = subprocess.run([claude_cli(), "mcp", *args], capture_output=True, text=True, timeout=60)
    return p.returncode, (p.stdout + p.stderr).strip()


def add(target, writes=False):
    if target == "folder":
        return _file_set(folder_file(), entry(writes))
    if target == "desktop":
        return _file_set(desktop_file(), entry(writes))
    if target == "code":
        _cli("remove", "--scope", "user", KEY)  # replace, never duplicate; "not found" is fine
        code, out = _cli("add-json", "--scope", "user", KEY, json.dumps({"type": "stdio", **entry(writes)}))
        return "registered" if code == 0 else f"failed: {out[:300]}"
    raise ValueError(target)


def remove(target):
    if target in ("folder", "desktop"):
        return _file_set(folder_file() if target == "folder" else desktop_file(), None)
    code, out = _cli("remove", "--scope", "user", KEY)
    return "removed" if code == 0 else "not registered"


def status():
    """Where the server is registered, and whether each registration allows changes."""
    out = {}
    for t, path in (("folder", folder_file()), ("desktop", desktop_file())):
        try:
            e = (_read(path).get("mcpServers") or {}).get(KEY)
        except (OSError, ValueError) as err:
            out[t] = {"registered": False, "error": f"{type(err).__name__}: {err}"[:200], "file": path}
            continue
        out[t] = {"registered": bool(e), "file": path, "writes": bool(e and "--allow-writes" in e.get("args", [])),
                  "current": e == entry("--allow-writes" in (e or {}).get("args", [])) if e else None}
    try:
        code, text = _cli("get", KEY)
        # Registered is "the CLI knows it"; current is "and it points at this server". A moved checkout leaves a
        # registration that is there but stale, which must not read as absent.
        out["code"] = {"registered": code == 0, "writes": "--allow-writes" in text,
                       "current": (SERVER in text) if code == 0 else None, "file": "~/.claude.json (user scope)"}
    except (OSError, subprocess.TimeoutExpired) as err:
        out["code"] = {"registered": False, "error": f"claude CLI not available: {type(err).__name__}"}
    return out


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("action", choices=["status", "add", "remove"])
    ap.add_argument("--target", choices=[*TARGETS, "all"], default="all")
    ap.add_argument("--writes", action="store_true", help="register it with --allow-writes")
    a = ap.parse_args(argv)
    if a.action == "status":
        print(json.dumps(status(), indent=2))
        return
    for t in TARGETS if a.target == "all" else (a.target,):
        print(f"{t}: {add(t, a.writes) if a.action == 'add' else remove(t)}")
    if a.action == "add" and a.target in ("desktop", "all"):
        print("Restart the Claude desktop app to load it there.")


if __name__ == "__main__":
    main()
