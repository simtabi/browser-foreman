"""Back up and restore all of Browser Foreman's own data in one .zip.

What a backup holds:
    manifest.json            format, version, time, and the SHA-256 and size of every other file
    workspaces/...           every workspace, the workspace trash (_trash/) included
    browser-foreman.json     the tool's own settings (MCP: allow changes)
    sessions/tabs.sqlite3    the saved open tabs and their settings, copied with SQLite's backup API (the live file
                             is in WAL mode, so a plain file copy can miss recent writes or catch a half-written page)

Not in it: the original export (backup-2026-11-10/, already read-only), the build outputs, snapshots of pages
(thumbs/), and settings kept only in a browser's localStorage.

A restore checks the manifest and every checksum, and refuses any name that is absolute, climbs out with "..", or is
not one of the paths above, before it changes anything. Then it writes an automatic backup of the current data to
backups/, moves the current workspaces/ and tabs database into _before-restore/<time>/ (never deleted), and unpacks.
"""
import hashlib
import io
import json
import os
import shutil
import sqlite3
import tempfile
import time
import zipfile

import tabs_read
import workspaces as wsm

FORMAT, VERSION = "browser-foreman-backup", 1
# Backups made before the 2026-11-12 rename (Browser Foreman) still restore: same layout, old names.
OLD_FORMATS = ("browser-keeper-backup",)
OLD_SETTINGS_NAMES = ("browser-keeper.json",)
TABS_NAME = "sessions/tabs.sqlite3"
SETTINGS_NAME = "browser-foreman.json"
MAX_FILES, MAX_TOTAL = 200000, 4 * 1024 ** 3  # refuse zip bombs: file count and unpacked size


def data_root():
    """The folder that holds workspaces/ (and gets backups/ and _before-restore/ beside it)."""
    return os.path.dirname(wsm.ws_root())


def _sha(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def _copy_tabs_db(dest):
    """A consistent copy of the tabs database through SQLite's backup API, or False when there is none."""
    src = tabs_read.db_path()
    if not os.path.isfile(src):
        return False
    s = sqlite3.connect(f"file:{src}?mode=ro", uri=True, timeout=30)
    d = sqlite3.connect(dest)
    try:
        s.backup(d)
    finally:
        d.close()
        s.close()
    return True


def make_backup(out_path):
    """Write the backup zip to out_path. Returns the manifest."""
    files = {}
    with tempfile.TemporaryDirectory() as tmp, zipfile.ZipFile(out_path, "w", zipfile.ZIP_DEFLATED) as z:
        root = wsm.ws_root()
        if os.path.isdir(root):
            with wsm._LOCK:
                for dirpath, dirnames, filenames in os.walk(root):
                    dirnames.sort()
                    for fn in sorted(filenames):
                        if fn == ".lock" or ".tmp-" in fn:
                            continue
                        p = os.path.join(dirpath, fn)
                        if os.path.islink(p):
                            continue
                        name = "workspaces/" + os.path.relpath(p, root).replace(os.sep, "/")
                        files[name] = {"sha256": _sha(p), "size": os.path.getsize(p)}
                        z.write(p, name)
        sp = wsm.settings_path()
        if os.path.isfile(sp):
            files[SETTINGS_NAME] = {"sha256": _sha(sp), "size": os.path.getsize(sp)}
            z.write(sp, SETTINGS_NAME)
        copy = os.path.join(tmp, "tabs.sqlite3")
        if _copy_tabs_db(copy):
            files[TABS_NAME] = {"sha256": _sha(copy), "size": os.path.getsize(copy)}
            z.write(copy, TABS_NAME)
        manifest = {"format": FORMAT, "version": VERSION, "created": wsm.now_iso(), "app": "Browser Foreman",
                    "workspaces": [m["id"] for m in wsm.list_workspaces()] if os.path.isdir(root) else [],
                    "files": files}
        z.writestr("manifest.json", json.dumps(manifest, indent=1))
    return manifest


def backup_bytes():
    with tempfile.TemporaryDirectory() as tmp:
        p = os.path.join(tmp, "backup.zip")
        make_backup(p)
        with open(p, "rb") as f:
            return f.read()


def _allowed(name):
    if name.startswith("/") or "\\" in name or any(part in ("", ".", "..") for part in name.split("/")):
        return False
    return name in ("manifest.json", TABS_NAME, SETTINGS_NAME, *OLD_SETTINGS_NAMES) or name.startswith("workspaces/")


def check(raw):
    """Validate a backup without changing anything. Returns (zipfile, manifest) or raises WsError."""
    try:
        z = zipfile.ZipFile(io.BytesIO(raw))
    except zipfile.BadZipFile:
        raise wsm.WsError("this is not a Browser Foreman backup (not a zip file)")
    infos = z.infolist()
    if len(infos) > MAX_FILES or sum(i.file_size for i in infos) > MAX_TOTAL:
        raise wsm.WsError("the backup is too large to restore")
    for i in infos:
        if not _allowed(i.filename) or i.is_dir() or (i.external_attr >> 16) & 0o170000 == 0o120000:
            raise wsm.WsError(f"refused: unexpected entry {i.filename!r} in the backup")
    try:
        manifest = json.loads(z.read("manifest.json"))
    except (KeyError, ValueError):
        raise wsm.WsError("the backup has no readable manifest.json")
    if manifest.get("format") not in (FORMAT, *OLD_FORMATS) or not isinstance(manifest.get("version"), int) or manifest["version"] > VERSION:
        raise wsm.WsError("this backup was made by a different or newer tool")
    listed = manifest.get("files") or {}
    names = {i.filename for i in infos} - {"manifest.json"}
    if set(listed) != names:
        raise wsm.WsError("the backup's files do not match its manifest")
    for name, meta in listed.items():
        if hashlib.sha256(z.read(name)).hexdigest() != meta.get("sha256"):
            raise wsm.WsError(f"checksum mismatch for {name}: the backup is damaged")
    return z, manifest


def restore(raw):
    """Replace the current data with the backup's. Nothing current is deleted: it is backed up and moved aside."""
    z, manifest = check(raw)
    root, tabs_db = wsm.ws_root(), tabs_read.db_path()
    stamp = time.strftime("%Y%m%d-%H%M%S", time.gmtime())
    keep = os.path.join(data_root(), "backups")
    aside = os.path.join(data_root(), "_before-restore", stamp)
    os.makedirs(keep, exist_ok=True)
    auto = os.path.join(keep, f"auto-before-restore-{stamp}.zip")
    with wsm._LOCK:
        make_backup(auto)
        os.makedirs(aside, exist_ok=True)
        if os.path.isdir(root):
            os.replace(root, os.path.join(aside, "workspaces"))
        if os.path.exists(wsm.settings_path()) and any(n in manifest["files"] for n in (SETTINGS_NAME, *OLD_SETTINGS_NAMES)):
            os.replace(wsm.settings_path(), os.path.join(aside, SETTINGS_NAME))
        moved_tabs = False
        for side in ("", "-wal", "-shm"):
            if os.path.exists(tabs_db + side):
                os.makedirs(os.path.join(aside, "sessions"), exist_ok=True)
                os.replace(tabs_db + side, os.path.join(aside, "sessions", os.path.basename(tabs_db) + side))
                moved_tabs = True
        os.makedirs(root, exist_ok=True)
        for name in manifest["files"]:
            dest = (os.path.join(root, *name.split("/")[1:]) if name.startswith("workspaces/")
                    else wsm.settings_path() if name in (SETTINGS_NAME, *OLD_SETTINGS_NAMES) else tabs_db)
            real = os.path.realpath(dest)
            if not (real in (os.path.realpath(tabs_db), os.path.realpath(wsm.settings_path()))
                    or real.startswith(os.path.realpath(root) + os.sep)):
                raise wsm.WsError(f"refused: {name} would land outside the data folders")
            os.makedirs(os.path.dirname(dest), exist_ok=True)
            with z.open(name) as src, open(dest, "wb") as out:
                shutil.copyfileobj(src, out)
        import tabs
        tabs.reopen()  # tabs.py's supported way to open the restored database afresh
    wsm._DATA_CACHE.clear()
    return {"restored": len(manifest["files"]), "from": manifest.get("created"), "workspaces": manifest.get("workspaces"),
            "auto_backup": auto, "previous_data": aside, "tabs_replaced": moved_tabs or TABS_NAME in manifest["files"]}
