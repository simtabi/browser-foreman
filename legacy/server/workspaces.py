"""Workspaces: each one is a bookmark set being cleaned, kept on disk with an append-only history.

Layout, one folder per workspace (the id is the folder name, checked against ID_RE and kept inside the root):

    workspaces/<id>/meta.json        name, kind, archived, notes, the sources it was built from
    workspaces/<id>/data.json        the bookmarks themselves, in the page's DATA shape (imported workspaces only;
                                     the "build" workspace reads the DATA embedded in preview.html)
    workspaces/<id>/history.jsonl    one line per change, never rewritten: seq, time, who, what, and the state after
    workspaces/<id>/snapshots/       each distinct state once, gzipped, named by its SHA-256
    workspaces/<id>/sources/         a copy of every file imported, so the original is never needed (or touched) again
    workspaces/<id>/exports/         files written by an export
    workspaces/<id>/duplicates.csv   one row per copy merged away at import, as build.py writes for the main set

A state is what the page keeps: {"tree": {root, titles, notes, gone, trashFrom} or None for the generated tree,
"urls": {id: new address}, "refreshed": {id: {s, n, h}}}. Rolling back appends a new entry whose state is an older
one, so nothing in the log is ever lost, rollbacks included.
"""
import contextlib
import csv
import fcntl
import gzip
import hashlib
import json
import os
import re
import threading
import time
from datetime import datetime, timezone

import paths  # noqa: E402  every folder lives in src/lib/paths.py

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = paths.ROOT
ID_RE = re.compile(r"^[a-z0-9][a-z0-9-]{0,39}$")
MAIN = "main"
TRASH = "Trash"
SLASH = "∕"  # a "/" that is part of a folder name, as parse.py stores it
MAX_HISTORY_READ = 100000
_LOCK = threading.RLock()


class WsError(Exception):
    code = 400


class NotFound(WsError):
    code = 404


class Conflict(WsError):
    code = 409


class Locked(WsError):
    code = 423


def refuse_if_locked(ws_id):
    """A locked workspace is read-only: every change to it is refused until it is unlocked."""
    if read_meta(ws_id).get("locked"):
        raise Locked("this workspace is locked (read-only); unlock it to change it")


# ---------- paths ----------
def ws_root():
    return os.path.realpath(os.environ.get("BM_WORKSPACES") or os.path.join(paths.STORAGE, "workspaces"))


def preview_path():
    return os.environ.get("BM_PREVIEW") or os.path.join(paths.PUBLIC, "index.html")


def check_id(ws_id):
    if not isinstance(ws_id, str) or not ID_RE.match(ws_id):
        raise WsError("bad workspace id")
    return ws_id


def ws_dir(ws_id):
    """The workspace's folder. The id is a plain slug, and the resolved path must sit directly in the root."""
    check_id(ws_id)
    base = ws_root()
    d = os.path.realpath(os.path.join(base, ws_id))
    if os.path.dirname(d) != base:
        raise WsError("bad workspace id")
    return d


def existing_dir(ws_id):
    d = ws_dir(ws_id)
    if not os.path.isfile(os.path.join(d, "meta.json")):
        raise NotFound(f"no workspace {ws_id}")
    return d


def write_atomic(path, data):
    tmp = f"{path}.tmp-{os.getpid()}-{threading.get_ident()}"
    with open(tmp, "wb") as f:
        f.write(data if isinstance(data, bytes) else data.encode("utf-8"))
    os.replace(tmp, path)


def write_json(path, obj):
    write_atomic(path, json.dumps(obj, ensure_ascii=False, indent=1))


@contextlib.contextmanager
def locked(ws_id):
    """One writer at a time per workspace, across threads and processes (the page's server and the MCP server)."""
    d = ws_dir(ws_id)
    os.makedirs(d, exist_ok=True)
    with _LOCK, open(os.path.join(d, ".lock"), "a") as lf:
        fcntl.flock(lf, fcntl.LOCK_EX)
        try:
            yield d
        finally:
            fcntl.flock(lf, fcntl.LOCK_UN)


def now_iso(t=None):
    return datetime.fromtimestamp(t or time.time(), timezone.utc).isoformat(timespec="seconds")


def slugify(name):
    s = re.sub(r"[^a-z0-9]+", "-", name.lower()).strip("-")[:30] or "workspace"
    return s if s[0].isalnum() else "w" + s


# ---------- data (the bookmarks) ----------
_DATA_CACHE = {}


def load_build_data(path=None):
    """The DATA object build.py embedded in preview.html, read without running the page."""
    path = path or preview_path()
    st = os.stat(path)
    hit = _DATA_CACHE.get(path)
    if hit and hit[0] == st.st_mtime_ns:
        return hit[1]
    with open(path, encoding="utf-8") as f:
        for line in f:
            if line.startswith("const DATA = ") and line.rstrip().endswith(";"):
                data = json.loads(line[len("const DATA = "):].rstrip().rstrip(";"))
                _DATA_CACHE[path] = (st.st_mtime_ns, data)
                return data
    raise WsError("preview.html has no DATA line; run src/pipeline/build.py")


def load_data(ws_id, added=None):
    """The workspace's bookmarks, with the ones added in it (saved tabs) merged in. Those live in the state, under
    "added", so undo and rollback take them away again; `added` overrides the head's when given."""
    d = existing_dir(ws_id)
    meta = read_meta(ws_id)
    if meta["kind"] == "build":
        data = load_build_data()
    else:
        with open(os.path.join(d, "data.json"), encoding="utf-8") as f:
            data = json.load(f)
    if added is None:
        added = get_state(ws_id)[0].get("added") or {}
    if not added:
        return data
    have = {i["id"] for i in data["keep"]}
    extra = [r for r in added.values() if r["id"] not in have]
    folders = set(data.get("folders") or []) | {r["g"] for r in extra if r.get("g")}
    return {**data, "keep": data["keep"] + extra, "folders": sorted(folders)}


def items_by_id(data):
    return {i["id"]: i for i in data["keep"]}


# ---------- meta ----------
def read_meta(ws_id):
    with open(os.path.join(existing_dir(ws_id), "meta.json"), encoding="utf-8") as f:
        return json.load(f)


def _save_meta(d, meta):
    meta["updated"] = now_iso()
    write_json(os.path.join(d, "meta.json"), meta)


def ensure_main():
    """The cleaned Chrome set is the first workspace. It is created on first use and starts with no history, so the
    page can bring its browser-kept edits in as the first entry (see the "migrate" op)."""
    d = ws_dir(MAIN)
    if os.path.isfile(os.path.join(d, "meta.json")) or not os.path.exists(preview_path()):
        return
    with locked(MAIN):
        if os.path.isfile(os.path.join(d, "meta.json")):
            return
        _save_meta(d, {"id": MAIN, "name": "Chrome cleanup (Nov 2026)", "kind": "build", "created": now_iso(),
                       "archived": False, "notes": "", "sources": [{"name": "preview.html (built by src/pipeline/build.py "
                       "from backup-2026-11-10/)", "kind": "build"}]})


def list_workspaces(include_archived=True):
    ensure_main()
    base = ws_root()
    out = []
    if not os.path.isdir(base):
        return out
    for name in sorted(os.listdir(base)):
        if not ID_RE.match(name) or not os.path.isfile(os.path.join(base, name, "meta.json")):
            continue
        meta = read_meta(name)
        if meta.get("archived") and not include_archived:
            continue
        head = head_entry(name)
        out.append({**meta, "head": head["seq"] if head else 0, "last_change": head and head["at"]})
    out.sort(key=lambda m: (m.get("archived", False), m["id"] != MAIN, m.get("created", "")))
    return out


def new_id(name):
    base = slugify(name)
    root = ws_root()
    cand, n = base, 2
    while os.path.exists(os.path.join(root, cand)):
        cand, n = f"{base}-{n}", n + 1
    return check_id(cand[:40])


def create_workspace(name, data, sources, dup_rows, by="page", notes=""):
    """A new imported workspace: its data, its first history entry, and its duplicates record."""
    name = clean_name(name)
    os.makedirs(ws_root(), exist_ok=True)
    with _LOCK:
        ws_id = new_id(name)
        with locked(ws_id) as d:
            write_json(os.path.join(d, "data.json"), data)
            write_dup_csv(os.path.join(d, "duplicates.csv"), dup_rows)
            _save_meta(d, {"id": ws_id, "name": name, "kind": "import", "created": now_iso(), "archived": False,
                           "notes": notes, "sources": sources})
            _append(d, empty_state(), "import", f"Imported {len(data['keep'])} bookmarks from "
                    f"{len(sources)} source{'s' if len(sources) != 1 else ''}"
                    + (f", {len(data['dupes'])} duplicate copies merged" if data["dupes"] else ""), by, base=None)
    return ws_id


def clean_name(name):
    name = " ".join(str(name or "").split())[:80]
    if not name:
        raise WsError("a workspace needs a name")
    return name


def rename_workspace(ws_id, name, by="page"):
    refuse_if_locked(ws_id)
    with locked(ws_id) as d:
        meta = read_meta(ws_id)
        old, meta["name"] = meta["name"], clean_name(name)
        _save_meta(d, meta)
    _log_meta(ws_id, "rename-workspace", f"Renamed the workspace from “{old}” to “{meta['name']}”", by)
    return meta


def set_archived(ws_id, archived, by="page"):
    refuse_if_locked(ws_id)
    with locked(ws_id) as d:
        meta = read_meta(ws_id)
        meta["archived"] = bool(archived)
        _save_meta(d, meta)
    _log_meta(ws_id, "archive" if archived else "unarchive", "Archived the workspace" if archived
              else "Brought the workspace back from the archive", by)
    return meta


def set_notes(ws_id, notes, by="page"):
    refuse_if_locked(ws_id)
    with locked(ws_id) as d:
        meta = read_meta(ws_id)
        meta["notes"] = str(notes or "")[:20000]
        _save_meta(d, meta)
    _log_meta(ws_id, "notes", "Edited the workspace notes", by)
    return meta


def _log_meta(ws_id, op, summary, by):
    """Workspace-level changes are progress too: they go in the log with the state unchanged."""
    with locked(ws_id) as d:
        head = head_entry(ws_id)
        state = read_snapshot(d, head["snap"]) if head else empty_state()
        if not head:
            head = _append(d, empty_state(), "start", "Started keeping history", by, base=None)
        _append(d, state, op, summary, by, base=head["seq"], undoable=False)


def set_locked(ws_id, on, by="page"):
    """Lock (read-only) or unlock. The only change a locked workspace accepts is being unlocked."""
    with locked(ws_id) as d:
        meta = read_meta(ws_id)
        if bool(meta.get("locked")) == bool(on):
            return meta
        meta["locked"] = bool(on)
        _save_meta(d, meta)
    _log_meta(ws_id, "lock" if on else "unlock", "Locked the workspace (read-only)" if on
              else "Unlocked the workspace", by)
    return meta


def reset_workspace(ws_id, by="page"):
    """Back to the starting point: the generated tree for the built set, the state it was imported in for an imported
    one (files added later stay in it). One history entry, so Undo or History brings everything back."""
    with locked(ws_id):
        refuse_if_locked(ws_id)
        hist = read_history(ws_id)
        start = next((e for e in hist if e["op"] in ("import", "start")), None)
    target = read_snapshot(existing_dir(ws_id), start["snap"]) if start else empty_state()
    cur, head = get_state(ws_id)
    return commit(ws_id, target, by=by, base_seq=head, op="reset", summary=reset_summary(cur).replace(
        "Reset to the generated tree", "Reset to the generated tree" if read_meta(ws_id)["kind"] == "build"
        else "Reset to how it was imported"))


# ---------- deleting: into the workspace trash, never straight off the disk ----------
TRASH_DIR = "_trash"  # not a valid workspace id, so it is never listed or opened as one
TRASH_ID_RE = re.compile(r"^[a-z0-9][a-z0-9-]{0,39}-\d{8}-\d{6}(-\d+)?$")


def trash_root():
    return os.path.join(ws_root(), TRASH_DIR)


def _trash_entry_dir(trash_id):
    if not isinstance(trash_id, str) or not TRASH_ID_RE.match(trash_id):
        raise WsError("bad trash entry")
    base = os.path.realpath(trash_root())
    d = os.path.realpath(os.path.join(base, trash_id))
    if os.path.dirname(d) != base or not os.path.isfile(os.path.join(d, "meta.json")):
        raise NotFound("no such entry in the workspace trash")
    return d


def delete_workspace(ws_id, by="page"):
    """Move a workspace into workspaces/_trash/<id>-<time>/. Nothing is removed from the disk here; Restore brings it
    back whole, history included. The built workspace cannot be deleted, and a locked one must be unlocked first."""
    meta = read_meta(ws_id)
    if meta["kind"] == "build" or ws_id == MAIN:
        raise WsError("the built workspace cannot be deleted")
    refuse_if_locked(ws_id)
    os.makedirs(trash_root(), exist_ok=True)
    with locked(ws_id) as d:
        stamp = time.strftime("%Y%m%d-%H%M%S", time.gmtime())
        tid, n = f"{ws_id}-{stamp}", 2
        while os.path.exists(os.path.join(trash_root(), tid)):
            tid, n = f"{ws_id}-{stamp}-{n}", n + 1
        write_json(os.path.join(d, "deleted.json"), {"id": ws_id, "name": meta["name"], "deleted": now_iso(), "by": by})
        os.replace(d, os.path.join(trash_root(), tid))
    return tid


def list_trash():
    root = trash_root()
    out = []
    if not os.path.isdir(root):
        return out
    for name in sorted(os.listdir(root), reverse=True):
        if not TRASH_ID_RE.match(name):
            continue
        d = os.path.join(root, name)
        try:
            with open(os.path.join(d, "meta.json"), encoding="utf-8") as f:
                meta = json.load(f)
            with open(os.path.join(d, "deleted.json"), encoding="utf-8") as f:
                gone = json.load(f)
        except (OSError, ValueError):
            continue
        out.append({"trash_id": name, "id": meta["id"], "name": meta["name"], "deleted": gone.get("deleted"),
                    "sources": [s.get("name") for s in meta.get("sources", [])]})
    return out


def restore_workspace(trash_id, by="page"):
    """Bring a deleted workspace back, under its old id when that is free."""
    d = _trash_entry_dir(trash_id)
    with open(os.path.join(d, "meta.json"), encoding="utf-8") as f:
        meta = json.load(f)
    with _LOCK:
        ws_id = meta["id"] if ID_RE.match(meta.get("id", "")) and not os.path.exists(os.path.join(ws_root(), meta["id"])) \
            else new_id(meta["name"])
        os.replace(d, os.path.join(ws_root(), ws_id))
    target = ws_dir(ws_id)
    os.remove(os.path.join(target, "deleted.json"))
    if ws_id != meta["id"]:
        meta["id"] = ws_id
        _save_meta(target, meta)
    _log_meta(ws_id, "restore", "Restored the workspace from the workspace trash", by)
    return ws_id


def empty_trash(trash_ids=None):
    """Permanently remove workspaces from the workspace trash: all of them, or the ones named. The only call in this
    module that removes anything from the disk; the page and the server ask twice before calling it."""
    import shutil
    names = trash_ids if trash_ids is not None else [t["trash_id"] for t in list_trash()]
    removed = []
    for tid in names:
        d = _trash_entry_dir(tid)
        shutil.rmtree(d)
        removed.append(tid)
    return removed


# ---------- tool settings (browser-foreman.json, beside workspaces/) ----------
# Named browser-foreman.json before the 2026-11-12 rename to Browser Foreman: read when the new file is not there yet.
OLD_SETTINGS_FILE = "browser-keeper.json"
SETTINGS_DEFAULTS = {"mcp_allow_writes": False}


def settings_path():
    return os.path.join(os.path.dirname(ws_root()), "browser-foreman.json")


def read_settings():
    got = {}
    for path in (settings_path(), os.path.join(os.path.dirname(ws_root()), OLD_SETTINGS_FILE)):
        try:
            with open(path, encoding="utf-8") as f:
                got = json.load(f)
            break
        except (OSError, ValueError):
            continue
    return {**SETTINGS_DEFAULTS, **{k: got[k] for k in SETTINGS_DEFAULTS if k in got}}


def save_settings(new):
    cur = read_settings()
    for k, v in (new or {}).items():
        if k not in SETTINGS_DEFAULTS or type(v) is not type(SETTINGS_DEFAULTS[k]):
            raise WsError(f"unknown or bad setting {k}")
        cur[k] = v
    with _LOCK:
        write_json(settings_path(), cur)
    return cur


# ---------- state and history ----------
def empty_state():
    return {"tree": None, "urls": {}, "refreshed": {}}


def validate_state(state):
    """Shape check on anything that will be written to the log, so a bad client cannot corrupt a workspace."""
    if not isinstance(state, dict):
        raise WsError("state must be an object")
    out = empty_state()
    tree = state.get("tree")
    if tree is not None:
        if not isinstance(tree, dict) or not isinstance(tree.get("root"), dict):
            raise WsError("tree.root missing")
        _check_folder(tree["root"], 0)
        for k in ("titles", "notes", "trashFrom"):
            if not isinstance(tree.get(k, {}), dict):
                raise WsError(f"tree.{k} must be an object")
        if not isinstance(tree.get("gone", []), list):
            raise WsError("tree.gone must be a list")
        out["tree"] = {"root": tree["root"], "titles": tree.get("titles", {}), "notes": tree.get("notes", {}),
                       "gone": tree.get("gone", []), "trashFrom": tree.get("trashFrom", {})}
    for k in ("urls", "refreshed"):
        v = state.get(k)
        v = {} if v is None else v
        if not isinstance(v, dict):
            raise WsError(f"{k} must be an object")
        out[k] = v
    added = state.get("added")
    if added is not None and added != {}:  # left out when empty, so states from before saved tabs existed compare equal
        if not isinstance(added, dict) or len(added) > MAX_ADDED:
            raise WsError("added must be an object of at most %d bookmarks" % MAX_ADDED)
        for k, r in added.items():
            if (not isinstance(r, dict) or not isinstance(r.get("id"), int) or str(r["id"]) != k
                    or not _ok_link(r.get("u"), r.get("t")) or not isinstance(r.get("g", ""), str)):
                raise WsError("bad added bookmark")
        out["added"] = added
    return out


MAX_ADDED, MAX_LINKS, MAX_TITLE, MAX_URL = 50000, 2000, 500, 4096


def _ok_link(u, t):
    return (isinstance(u, str) and u.startswith(("http://", "https://")) and len(u) <= MAX_URL
            and isinstance(t, str) and len(t) <= MAX_TITLE)


def add_links(ws_id, folder, links, by="page"):
    """Save links (open tabs) as new bookmarks: new ids, recorded in the state under "added", placed in a new folder
    at the top of the tree. One history entry; undo takes them away again."""
    if not isinstance(links, list) or not links:
        raise WsError("no links to save")
    if len(links) > MAX_LINKS:
        raise WsError(f"at most {MAX_LINKS} links at a time")
    clean = []
    for x in links:
        if not isinstance(x, dict):
            raise WsError("each link is {url, title}")
        u, t = str(x.get("url") or "").strip(), " ".join(str(x.get("title") or "").split())
        if not _ok_link(u, t):
            raise WsError("only http(s) links, addresses up to %d characters and titles up to %d" % (MAX_URL, MAX_TITLE))
        clean.append((u, t or u))
    parts = [stored_name(p) for p in str(folder or "").split("/") if p.strip()]
    if not parts or len(parts) > 8 or any(len(p) > 200 for p in parts) or parts[0] == TRASH:
        raise WsError("a folder name is needed (not the Trash)")
    refuse_if_locked(ws_id)
    cur, head = get_state(ws_id)
    data = load_data(ws_id, added=cur.get("added") or {})
    st = editable(cur, data)
    root = st["tree"]["root"]
    parent = root
    for p in parts[:-1]:  # "Saved tabs/<name>": the parents are shared, the last part is always new
        parent = next((x for x in parent["c"] if isinstance(x, dict) and x["n"] == p and x["n"] != TRASH), None) \
            or _new_folder(parent, p)
    names = {x["n"] for x in parent["c"] if isinstance(x, dict)}
    name, n = parts[-1], 2
    while name in names:
        name, n = f"{parts[-1]} ({n})", n + 1
    target = _new_folder(parent, name)
    path = "/".join(parts[:-1] + [name])
    next_id = max([i["id"] for i in data["keep"]] + [i["id"] for i in data.get("dupes", [])] + [0]) + 1
    added = dict(st.get("added") or {})
    now = int(time.time())
    for u, t in clean:
        rec = {"id": next_id, "a": now, "f": path, "g": path, "t": t, "u": u, "ou": u, "s": "unverified",
               "n": "saved from open tabs, not checked yet", "o": display(path), "w": "saved from open tabs",
               "p": 0, "m": 0, "src": "open tabs"}
        added[str(next_id)] = rec
        target["c"].append(next_id)
        next_id += 1
    st["added"] = added
    return commit(ws_id, st, by=by, base_seq=head, op="add-links",
                  summary=f"Saved {len(clean)} tab{'s' if len(clean) != 1 else ''} as bookmarks in {display(path)}")


def _new_folder(parent, name):
    f = {"n": name, "c": []}
    parent["c"].append(f)
    return f


def _check_folder(f, depth):
    if depth > 64 or not isinstance(f.get("n"), str) or not isinstance(f.get("c"), list):
        raise WsError("bad folder in tree")
    for x in f["c"]:
        if isinstance(x, dict):
            _check_folder(x, depth + 1)
        elif not isinstance(x, int) or isinstance(x, bool):
            raise WsError("tree entries are folders or bookmark ids")


def canon(state):
    return json.dumps(state, sort_keys=True, ensure_ascii=False, separators=(",", ":")).encode("utf-8")


def store_snapshot(d, state):
    raw = canon(state)
    h = hashlib.sha256(raw).hexdigest()
    snaps = os.path.join(d, "snapshots")
    os.makedirs(snaps, exist_ok=True)
    p = os.path.join(snaps, h + ".json.gz")
    if not os.path.exists(p):
        write_atomic(p, gzip.compress(raw, 6))
    return h


def read_snapshot(d, h):
    if not re.fullmatch(r"[0-9a-f]{64}", h or ""):
        raise WsError("bad snapshot")
    with gzip.open(os.path.join(d, "snapshots", h + ".json.gz")) as f:
        return json.loads(f.read())


def read_history(ws_id):
    p = os.path.join(existing_dir(ws_id), "history.jsonl")
    if not os.path.exists(p):
        return []
    out = []
    with open(p, encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if line:
                out.append(json.loads(line))
            if len(out) > MAX_HISTORY_READ:
                break
    return out


def head_entry(ws_id):
    h = read_history(ws_id)
    return h[-1] if h else None


def _append(d, state, op, summary, by, base, **extra):
    hist = os.path.join(d, "history.jsonl")
    seq = 1
    if os.path.exists(hist):
        with open(hist, "rb") as f:
            lines = [ln for ln in f.read().splitlines() if ln.strip()]
            seq = json.loads(lines[-1])["seq"] + 1 if lines else 1
    t = time.time()
    entry = {"seq": seq, "t": round(t, 3), "at": now_iso(t), "op": op, "summary": summary, "by": by, "base": base,
             "snap": store_snapshot(d, state), **extra}
    with open(hist, "a", encoding="utf-8") as f:
        f.write(json.dumps(entry, ensure_ascii=False) + "\n")
        f.flush()
        os.fsync(f.fileno())
    return entry


def get_state(ws_id):
    d = existing_dir(ws_id)
    head = head_entry(ws_id)
    return (read_snapshot(d, head["snap"]) if head else empty_state()), (head["seq"] if head else 0)


def state_at(ws_id, seq):
    d = existing_dir(ws_id)
    for e in read_history(ws_id):
        if e["seq"] == seq:
            return read_snapshot(d, e["snap"]), e
    raise NotFound(f"no entry {seq}")


def commit(ws_id, state, by="page", base_seq=None, op=None, summary=None):
    """Append a new state. base_seq is the head the writer last saw; a stale one is refused (409), so an edit made
    on an old view never silently replaces a newer one. A state equal to the head appends nothing."""
    state = validate_state(state)
    with locked(ws_id) as d:
        existing_dir(ws_id)
        refuse_if_locked(ws_id)
        head = head_entry(ws_id)
        cur_seq = head["seq"] if head else 0
        if base_seq is not None and int(base_seq) != cur_seq:
            raise Conflict(f"workspace changed since {base_seq} (now {cur_seq})")
        cur = read_snapshot(d, head["snap"]) if head else empty_state()
        if canon(cur) == canon(state):
            return head
        if not head:
            head = _append(d, empty_state(), "start", "Started keeping history", by, base=None)
        if op == "reset" and summary is None:
            summary = reset_summary(cur)
        if op is None or summary is None:
            dop, dsum = describe(cur, state, load_data(ws_id, added={**(cur.get("added") or {}),
                                                                     **(state.get("added") or {})}))
            op, summary = op or dop, summary or dsum
        return _append(d, state, op, summary, by, base=head["seq"])


def migrate(ws_id, state, by="page"):
    """Bring edits kept in a browser's localStorage in as this workspace's first change. Only while it has none."""
    state = validate_state(state)
    with locked(ws_id) as d:
        refuse_if_locked(ws_id)
        if head_entry(ws_id):
            raise Conflict("this workspace already has history; nothing was migrated")
        start = _append(d, empty_state(), "start", "Started keeping history", by, base=None)
        if canon(state) == canon(empty_state()):
            return start
        _, dsum = describe(empty_state(), state, load_data(ws_id))
        return _append(d, state, "migrate", "Brought in the edits this browser had kept: " + dsum, by,
                       base=start["seq"])


def rollback(ws_id, seq, by="page"):
    """Go back to the state after entry `seq`. Appended as a new entry, so the rollback can be undone too."""
    with locked(ws_id) as d:
        refuse_if_locked(ws_id)
        target, entry = state_at(ws_id, int(seq))
        head = head_entry(ws_id)
        return _append(d, target, "rollback", f"Rolled back to #{entry['seq']} ({entry['summary']})", by,
                       base=head["seq"], to=entry["seq"])


def undo(ws_id, by="page"):
    """Step back one change. Each entry names the entry it was made on (base); undo returns there, and an undo's
    own base is its target's base, so pressing it again keeps walking back rather than redoing."""
    with locked(ws_id) as d:
        refuse_if_locked(ws_id)
        hist = read_history(ws_id)
        if not hist:
            raise WsError("nothing to undo")
        by_seq = {e["seq"]: e for e in hist}
        cur = hist[-1]
        while cur and cur.get("undoable") is False:  # workspace renames and notes are not tree edits
            cur = by_seq.get(cur["base"])
        if not cur or cur.get("base") is None:
            raise WsError("nothing to undo")
        target = by_seq[cur["base"]]
        while target.get("undoable") is False and target.get("base") is not None:
            target = by_seq[target["base"]]
        # After an undo that went back to T, the next one reverses T's own change.
        undone = by_seq[cur["to"]] if cur["op"] in ("undo", "redo") else cur  # a redo names the change it brought back
        # `undid`: the entry whose state Redo brings back. `prev_undo`: the undo before this one in an unbroken run,
        # so a second Redo continues the run. A new edit ends the run (Redo is then unavailable).
        return _append(d, read_snapshot(d, target["snap"]), "undo", f"Undid: {undone['summary']}", by,
                       base=target.get("base"), to=target["seq"], undid=undone["seq"],
                       prev_undo=hist[-1]["seq"] if hist[-1]["op"] == "undo" else None)


def _redo_source(hist):
    """The undo entry Redo would reverse, or None. Redo is available straight after one or more undos (and after
    redos that continue the same run); any other change makes it unavailable."""
    by_seq = {e["seq"]: e for e in hist}
    last = next((e for e in reversed(hist) if e.get("undoable") is not False), None)
    if not last:
        return None
    if last["op"] == "undo" and last.get("undid") is not None:
        return last
    if last["op"] == "redo" and last.get("next_undo") is not None:
        return by_seq.get(last["next_undo"])
    return None


def redo(ws_id, by="page"):
    """Bring back what the last undo took away. Appended as a new entry; Undo after it undoes the change again."""
    with locked(ws_id) as d:
        refuse_if_locked(ws_id)
        hist = read_history(ws_id)
        u = _redo_source(hist)
        if not u:
            raise WsError("nothing to redo")
        by_seq = {e["seq"]: e for e in hist}
        again = by_seq[u["undid"]]
        prev = by_seq.get(u.get("prev_undo")) if u.get("prev_undo") else None
        return _append(d, read_snapshot(d, again["snap"]), "redo", f"Redid: {again['summary']}", by,
                       base=again.get("base"), to=again["seq"],
                       next_undo=prev["seq"] if prev and prev["op"] == "undo" and prev.get("undid") is not None else None)


def undo_state(ws_id):
    """What Undo and Redo would do now, for enabling the buttons: {"undo": summary or None, "redo": summary or None}.
    Reads the history only; changes nothing."""
    hist = read_history(ws_id)
    by_seq = {e["seq"]: e for e in hist}
    cur = hist[-1] if hist else None
    while cur and cur.get("undoable") is False:
        cur = by_seq.get(cur["base"])
    undo_label = None
    if cur and cur.get("base") is not None:
        undone = by_seq.get(cur["to"]) if cur["op"] in ("undo", "redo") else cur
        undo_label = (undone or cur)["summary"]
    u = _redo_source(hist)
    redo_label = by_seq[u["undid"]]["summary"] if u and u.get("undid") in by_seq else None
    return {"undo": undo_label, "redo": redo_label}


# ---------- the tree, materialised the way the page builds it ----------
def _rank(n):
    return 0 if n == "Bookmarks Bar" else 3 if n == TRASH else 2 if n.startswith("_") else 1


def default_tree(data, key="g"):
    root = {"n": "", "c": []}
    idx = {"": root}

    def folder_at(path):
        if path in idx:
            return idx[path]
        cut = path.rfind("/")
        parent = folder_at("" if cut < 0 else path[:cut])
        f = {"n": path[cut + 1:], "c": []}
        parent["c"].append(f)
        idx[path] = f
        return f
    for p in data.get("folders") or []:
        folder_at(p)
    for i in data["keep"]:
        folder_at(i.get(key) or "")["c"].append(i["id"])

    def tidy(f):
        links = [x for x in f["c"] if not isinstance(x, dict)]
        subs = sorted((x for x in f["c"] if isinstance(x, dict)), key=lambda x: (_rank(x["n"]), x["n"].strip().lower()))
        f["c"] = links + subs
        for s in subs:
            tidy(s)
    tidy(root)
    return root


def editable(state, data):
    """A deep copy of the state with the tree materialised, ready for the tree operations below."""
    st = json.loads(json.dumps(state))
    if st["tree"] is None:
        st["tree"] = {"root": default_tree(data), "titles": {}, "notes": {}, "gone": [], "trashFrom": {}}
    return st


def walk(f, path=""):
    """Yield (folder, path) for every folder under f, f included."""
    yield f, path
    for x in f["c"]:
        if isinstance(x, dict):
            yield from walk(x, f"{path}/{x['n']}" if path else x["n"])


def locate(root):
    """bookmark id -> (parent folder, folder path)."""
    out = {}
    for f, p in walk(root):
        for x in f["c"]:
            if not isinstance(x, dict):
                out[x] = (f, p)
    return out


def trash_folder(root, create=False):
    t = next((x for x in root["c"] if isinstance(x, dict) and x["n"] == TRASH), None)
    if t is None and create:
        t = {"n": TRASH, "c": []}
        root["c"].append(t)
    return t


def display(n):
    return str(n).replace(SLASH, "/")


def stored_name(n):
    return " ".join(str(n).split()).replace("/", SLASH)


def find_folder(root, path, create=False):
    """A folder by its path ("Bookmarks Bar/Dev"; "" is the top). The Trash is only reached by naming it first."""
    f = root
    if not path:
        return f
    parts = [p for p in path.split("/") if p != ""]
    for k, part in enumerate(parts):
        want = stored_name(part)
        s = next((x for x in f["c"] if isinstance(x, dict) and x["n"] in (want, part)
                  and (x["n"] != TRASH or k > 0 or f is not root or want == TRASH)), None)
        if s is None:
            if not create:
                raise NotFound(f"no folder {path!r}")
            s = {"n": want, "c": []}
            f["c"].append(s)
        f = s
    return f


def parent_of(root, node):
    for f, p in walk(root):
        for x in f["c"]:
            if x is node or (not isinstance(node, dict) and x == node):
                return f, p
    return None, None


def resolve_targets(st, targets):
    """Targets are bookmark ids (numbers) or folder paths (strings)."""
    root = st["tree"]["root"]
    out = []
    for t in targets:
        if isinstance(t, bool):
            raise WsError("bad target")
        if isinstance(t, int) or (isinstance(t, str) and t.isdigit()):
            t = int(t)
            if t not in locate(root):
                raise NotFound(f"no bookmark {t} in this workspace's tree")
            out.append(t)
        elif isinstance(t, str) and t.strip("/"):
            f = find_folder(root, t.strip("/"))
            if f is root or f is trash_folder(root):
                raise WsError("the top folder and the Trash itself cannot be moved")
            out.append(f)
        else:
            raise WsError("bad target")
    return out


def _detach(root, x):
    p, _ = parent_of(root, x)
    if p is not None:
        p["c"].remove(x)
    return p


def _contains(folder, x):
    return any(y is x or (not isinstance(x, dict) and y == x) for f, _ in walk(folder) for y in f["c"])


def op_move(st, targets, to, create=False):
    root = st["tree"]["root"]
    xs = resolve_targets(st, targets)
    dest = find_folder(root, to.strip("/"), create=create)
    for x in xs:
        if isinstance(x, dict) and (x is dest or _contains(x, dest)):
            raise WsError("cannot move a folder into itself")
    for x in xs:
        _detach(root, x)
        _forget_from(st, x)
        dest["c"].append(x)
    return len(xs)


def _forget_from(st, x):
    if isinstance(x, dict):
        x.pop("from", None)
    else:
        st["tree"]["trashFrom"].pop(str(x), None)


def op_rename(st, target, name, data):
    name = " ".join(str(name).split())
    if not name:
        raise WsError("a name cannot be empty")
    (x,) = resolve_targets(st, [target])
    if isinstance(x, dict):
        x["n"] = stored_name(name)
    else:
        item = items_by_id(data).get(x)
        if item and item["t"] == name:
            st["tree"]["titles"].pop(str(x), None)
        else:
            st["tree"]["titles"][str(x)] = name


def op_trash(st, targets):
    root = st["tree"]["root"]
    xs = resolve_targets(st, targets)
    bin_ = trash_folder(root, create=True)
    n = 0
    for x in xs:
        if _contains(bin_, x):
            continue
        p, path = parent_of(root, x)
        frm = {"path": path, "index": p["c"].index(x)}
        if isinstance(x, dict):
            x["from"] = frm
        else:
            st["tree"]["trashFrom"][str(x)] = frm
        p["c"].remove(x)
        bin_["c"].append(x)
        n += 1
    return n


def op_put_back(st, targets, data):
    root = st["tree"]["root"]
    bin_ = trash_folder(root)
    if bin_ is None:
        raise WsError("the Trash is empty")
    xs = resolve_targets(st, [t if not isinstance(t, str) or t.isdigit() or t.startswith(TRASH + "/")
                              else f"{TRASH}/{t}" for t in targets])
    items = items_by_id(data)
    for x in xs:
        if not any(y is x or (not isinstance(x, dict) and y == x) for y in bin_["c"]):
            raise WsError("only things directly inside the Trash can be put back")
    for x in xs:
        frm = x.get("from") if isinstance(x, dict) else st["tree"]["trashFrom"].get(str(x))
        path = frm["path"] if frm else ("" if isinstance(x, dict) else items.get(x, {}).get("g", ""))
        bin_["c"].remove(x)
        dest = _folder_outside_trash(root, path)
        _forget_from(st, x)
        at = min(frm["index"], len(dest["c"])) if frm else len(dest["c"])
        dest["c"].insert(at, x)
    return len(xs)


def _folder_outside_trash(root, path):
    f = root
    for part in [p for p in (path or "").split("/") if p]:
        s = next((x for x in f["c"] if isinstance(x, dict) and x["n"] == part and not (f is root and x["n"] == TRASH)), None)
        if s is None:
            s = {"n": part, "c": []}
            f["c"].append(s)
        f = s
    return f


# ---------- reading ----------
def item_view(item, st, path):
    sid = str(item["id"])
    tree = st["tree"] or {}
    ref = (st.get("refreshed") or {}).get(sid) or {}
    return {"id": item["id"], "title": (tree.get("titles") or {}).get(sid, item["t"]),
            "url": (st.get("urls") or {}).get(sid, item["u"]), "folder": display(path),
            "status": ref.get("s", item.get("s")), "note": (tree.get("notes") or {}).get(sid, item.get("ds")),
            "added": item.get("a")}


def search(ws_id, query="", folder=None, status=None, include_trash=False, limit=50):
    data = load_data(ws_id)
    st = editable(get_state(ws_id)[0], data)
    items = items_by_id(data)
    words = [w for w in str(query or "").lower().split() if w]
    root = st["tree"]["root"]
    scope = find_folder(root, folder.strip("/")) if folder else root
    bin_ = trash_folder(root)
    out, total = [], 0
    for f, path in walk(scope, (folder or "").strip("/")):
        if not include_trash and bin_ is not None and (f is bin_ or (path.split("/")[0] == TRASH and scope is root)):
            continue
        for x in f["c"]:
            if isinstance(x, dict) or x not in items:
                continue
            v = item_view(items[x], st, path)
            hay = f"{v['title']} {v['url']} {v['note'] or ''} {v['folder']}".lower()
            if words and not all(w in hay for w in words):
                continue
            if status and v["status"] != status:
                continue
            total += 1
            if len(out) < limit:
                out.append(v)
    return {"total": total, "results": out}


def read_folder(ws_id, path=""):
    data = load_data(ws_id)
    st = editable(get_state(ws_id)[0], data)
    items = items_by_id(data)
    f = find_folder(st["tree"]["root"], (path or "").strip("/"))

    def count(g):
        return sum(1 for h, _ in walk(g) for x in h["c"] if not isinstance(x, dict))
    return {"path": display((path or "").strip("/")), "description": f.get("d"),
            "folders": [{"name": display(x["n"]), "bookmarks": count(x)} for x in f["c"] if isinstance(x, dict)],
            "bookmarks": [item_view(items[x], st, (path or "").strip("/")) for x in f["c"]
                          if not isinstance(x, dict) and x in items]}


# ---------- describing a change ----------
def describe(old, new, data):
    """One line saying what changed between two states, and the op it is filed under."""
    items = items_by_id(data)
    o, n = editable(old, data), editable(new, data)
    ot, nt = o["tree"], n["tree"]
    oloc, nloc = locate(ot["root"]), locate(nt["root"])
    obin, nbin = trash_folder(ot["root"]), trash_folder(nt["root"])

    def in_bin(loc, bin_, i):
        p = loc.get(i)
        return p is not None and bin_ is not None and (p[0] is bin_ or p[1].split("/")[0] == TRASH)
    trashed = [i for i in nloc if in_bin(nloc, nbin, i) and not in_bin(oloc, obin, i)]
    restored = [i for i in nloc if in_bin(oloc, obin, i) and not in_bin(nloc, nbin, i)]
    gone = set(nt["gone"]) - set(ot["gone"])
    ofold = {p for _, p in walk(ot["root"]) if p and p != TRASH}
    nfold = {p for _, p in walk(nt["root"]) if p and p != TRASH}
    lost = ofold - nfold  # a bookmark whose folder was renamed or moved changed path without being "moved" itself
    moved = [i for i in nloc if i in oloc and oloc[i][1] != nloc[i][1] and i not in trashed and i not in restored
             and not any(oloc[i][1] == a or oloc[i][1].startswith(a + "/") for a in lost)]
    renamed = [k for k in set(ot["titles"]) | set(nt["titles"]) if ot["titles"].get(k) != nt["titles"].get(k)]
    noted = [k for k in set(ot["notes"]) | set(nt["notes"]) if ot["notes"].get(k) != nt["notes"].get(k)]
    upd = [k for k in n["urls"] if o["urls"].get(k) != n["urls"][k]]
    rev = [k for k in o["urls"] if k not in n["urls"]]
    ref = [k for k in n["refreshed"] if o["refreshed"].get(k) != n["refreshed"][k]]
    parts = []

    def name(i):
        it = items.get(int(i))
        return f"“{(nt['titles'].get(str(i)) or (it and it['t']) or i)}”"

    def few(ids, word):
        ids = list(ids)
        return f"{word} {name(ids[0])}" if len(ids) == 1 else f"{word} {len(ids)} bookmarks"
    if new["tree"] is None and old["tree"] is not None:
        return "reset", reset_summary(old)
    if trashed:
        parts.append(("trash", few(trashed, "Moved to Trash:")))
    if restored:
        parts.append(("put-back", few(restored, "Put back")))
    if gone:
        parts.append(("delete", f"Deleted {len(gone)} bookmark{'s' if len(gone) != 1 else ''} from the Trash for good"))
    if moved:
        dest = {display(nloc[i][1]) or "the top" for i in moved}
        parts.append(("move", few(moved, "Moved") + (f" to {next(iter(dest))}" if len(dest) == 1 else "")))
    add, rem = sorted(nfold - ofold), sorted(ofold - nfold)
    if add and rem and len(add) == len(rem) and not moved:
        parts.append(("rename-folder", f"Renamed folder {display(rem[0])} to {display(add[0])}"
                      if len(add) == 1 else f"Renamed or moved {len(add)} folders"))
    else:
        if add:
            parts.append(("folder", f"New folder {display(add[0])}" if len(add) == 1 else f"{len(add)} new folders"))
        if rem:
            parts.append(("folder", f"Removed folder {display(rem[0])}" if len(rem) == 1 else f"{len(rem)} folders removed"))
    if renamed:
        parts.append(("rename", few(renamed, "Renamed")))
    if noted:
        parts.append(("note", few(noted, "Edited the description of")))
    if upd:
        parts.append(("update-link", f"Updated {len(upd)} link{'s' if len(upd) != 1 else ''} to the new address"))
    if rev:
        parts.append(("revert-link", f"Reverted {len(rev)} link{'s' if len(rev) != 1 else ''} to the old address"))
    if ref:
        parts.append(("refresh", f"Refresh results for {len(ref)} link{'s' if len(ref) != 1 else ''}"))
    if not parts:
        if canon(ot["root"]) != canon(nt["root"]):
            return "reorder", "Reordered or edited folders"
        return "edit", "Edited"
    return parts[0][0], "; ".join(p[1] for p in parts)


def reset_summary(old):
    """What a reset to the generated tree throws away (all of it stays in the history)."""
    parts = ["the tree's edits" if old.get("tree") is not None else ""]
    for k, word in (("urls", "link update"), ("refreshed", "refresh result")):
        n = len(old.get(k) or {})
        parts.append(f"{n} {word}{'s' if n != 1 else ''}" if n else "")
    parts = [p for p in parts if p]
    return "Reset to the generated tree" + (", clearing " + ", ".join(parts) if parts else "")


# ---------- duplicates record ----------
def duplicates_path(ws_id):
    """Where a workspace's merged copies are listed: its own duplicates.csv when it was imported, and the one
    build.py writes beside preview.html for the built set (read live, so a rebuild is reflected)."""
    d = existing_dir(ws_id)
    if read_meta(ws_id)["kind"] == "build":
        # the build writes it to storage/reports; a test or tool pointing BM_PREVIEW elsewhere keeps it beside that page
        return os.path.join(os.path.dirname(os.path.abspath(os.environ["BM_PREVIEW"])) if os.environ.get("BM_PREVIEW") else paths.REPORTS, "duplicates.csv")
    return os.path.join(d, "duplicates.csv")



DUP_COLS = ["kept_title", "kept_url", "kept_in", "removed_title", "removed_url", "removed_was_in", "removed_added",
            "kept_took_earlier_date", "kept_took_title", "removed_source"]


def write_dup_csv(path, rows):
    with open(path, "w", newline="", encoding="utf-8") as f:
        w = csv.writer(f)
        w.writerow(DUP_COLS)
        w.writerows([[r.get(c, "") for c in DUP_COLS] for r in rows])


def read_dup_csv(path):
    if not os.path.exists(path):
        return []
    with open(path, newline="", encoding="utf-8") as f:
        return list(csv.DictReader(f))
