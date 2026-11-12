"""Reading and writing bookmark files: every browser's format in, Netscape HTML and Chrome JSON out.

Readers never write to what they read. A browser profile file is copied first (Firefox keeps places.sqlite open and
locked, so the copy is also the only safe way to read it), and an imported file is kept in the workspace's
sources/ folder, so the original is not needed again.

Sources, by what the file is rather than what it is called:
  Netscape HTML  any browser's "Export bookmarks" file
  Chrome JSON    the profile's "Bookmarks" file (Chrome, Edge, Brave, Vivaldi, Arc, Opera)
  Firefox        places.sqlite
  Safari         Bookmarks.plist (binary or XML)

Several sources merge into one set with build.py's duplicate rule (the normalised URL keeps "#fragment" and drops
tracking parameters): the copy in the deepest folder wins, then the earliest added; it takes the group's earliest
date and a real title if it had none; every copy merged away is recorded, as duplicates.csv records it for the main
set.
"""
import glob
import hashlib
import json
import os
import plistlib
import shutil
import sqlite3
import tempfile
from datetime import datetime, timezone
from urllib.parse import urlsplit

import workspaces as wsm
from parse import P as NetscapeParser

SLASH = wsm.SLASH
CHROME_EPOCH = 11644473600  # seconds from 1601-01-01 to 1970-01-01
MAX_SOURCE_BYTES = 200 * 1024 * 1024
ALLOWED_NAMES = ("Bookmarks", "places.sqlite", "Bookmarks.plist")
ALLOWED_EXT = (".html", ".htm", ".json", ".plist", ".sqlite")

# Where browsers keep their bookmarks on macOS. Reading them usually needs Full Disk Access, which this tool does not
# ask for: an export file chosen in the page is the main path.
PROFILE_GLOBS = {
    "Chrome": "~/Library/Application Support/Google/Chrome/*/Bookmarks",
    "Chrome Beta": "~/Library/Application Support/Google/Chrome Beta/*/Bookmarks",
    "Edge": "~/Library/Application Support/Microsoft Edge/*/Bookmarks",
    "Brave": "~/Library/Application Support/BraveSoftware/Brave-Browser/*/Bookmarks",
    "Vivaldi": "~/Library/Application Support/Vivaldi/*/Bookmarks",
    "Arc": "~/Library/Application Support/Arc/User Data/*/Bookmarks",
    "Firefox": "~/Library/Application Support/Firefox/Profiles/*/places.sqlite",
    "Safari": "~/Library/Safari/Bookmarks.plist",
}


def _norm(u):
    # build.py's rule, imported lazily: build.py is a large module that another tool run may be editing.
    from build import norm
    return norm(u)


def folder_name(n):
    return " ".join(str(n or "").split()).replace("/", SLASH)


# ---------- detecting and reading ----------
def detect(raw, filename=""):
    head = raw[:4096].lstrip()
    if raw.startswith(b"SQLite format 3\x00"):
        return "firefox"
    if raw.startswith(b"bplist") or b"<plist" in head[:400]:
        return "safari"
    if head.startswith(b"{"):
        return "chrome"
    low = head.lower()
    if b"netscape-bookmark-file" in low or b"<dl" in low or b"<dt>" in low:
        return "netscape"
    raise wsm.WsError(f"{filename or 'this file'} is not a bookmark file this tool can read "
                      "(Netscape HTML, Chrome Bookmarks JSON, Firefox places.sqlite, Safari Bookmarks.plist)")


def read_source(raw, filename=""):
    """bytes -> (kind, [{url, title, add, folder}], [folder paths])."""
    if len(raw) > MAX_SOURCE_BYTES:
        raise wsm.WsError("file too large")
    kind = detect(raw, filename)
    reader = {"netscape": read_netscape, "chrome": read_chrome, "firefox": read_firefox, "safari": read_safari}[kind]
    try:
        items, folders = reader(raw)
    except wsm.WsError:
        raise
    except Exception as e:  # noqa: BLE001 - a broken file is the user's input, not a server fault
        raise wsm.WsError(f"could not read {filename or 'the file'} as {kind}: {type(e).__name__}: {e}"[:300])
    return kind, items, folders


def read_netscape(raw):
    p = NetscapeParser()
    p.feed(raw.decode("utf-8", errors="replace"))
    items = [{"url": i["url"], "title": i["title"].strip(), "add": int(i["add"] or 0), "folder": _bar(i["folder"])}
             for i in p.items if i["url"]]
    return items, [_bar(f) for f in p.folders]


BAR_NAMES = {"bookmarks bar", "bookmarks toolbar", "favorites bar", "favourites bar", "personal toolbar folder"}


def _bar(path):
    """Every browser names its toolbar folder differently in an export; they are all the Bookmarks Bar here."""
    head, sep, rest = path.partition("/")
    return "Bookmarks Bar" + sep + rest if head.lower() in BAR_NAMES else path


CHROME_ROOTS = (("bookmark_bar", "Bookmarks Bar"), ("other", ""), ("synced", "Mobile bookmarks"))


def read_chrome(raw):
    doc = json.loads(raw.decode("utf-8"))
    roots = doc.get("roots")
    if not isinstance(roots, dict):
        raise wsm.WsError("no roots in this Bookmarks file")
    items, folders = [], []

    def walk(node, path):
        for ch in node.get("children") or []:
            if ch.get("type") == "folder":
                p = f"{path}/{folder_name(ch.get('name'))}" if path else folder_name(ch.get("name"))
                folders.append(p)
                walk(ch, p)
            elif ch.get("type") == "url" and ch.get("url"):
                items.append({"url": ch["url"], "title": (ch.get("name") or "").strip(),
                              "add": _from_chrome_time(ch.get("date_added")), "folder": path})
    for key, name in CHROME_ROOTS:
        if isinstance(roots.get(key), dict):
            if name:
                folders.append(name)
            walk(roots[key], name)
    return items, folders


def _from_chrome_time(v):
    try:
        return max(0, int(int(v) / 1_000_000 - CHROME_EPOCH))
    except (TypeError, ValueError):
        return 0


FIREFOX_ROOTS = {"toolbar_____": "Bookmarks Bar", "menu________": "Bookmarks Menu", "unfiled_____": "",
                 "mobile______": "Mobile bookmarks"}


def read_firefox(raw):
    """Read a copy, never the live file, opened read-only."""
    with tempfile.TemporaryDirectory() as tmp:
        db = os.path.join(tmp, "places.sqlite")
        with open(db, "wb") as f:
            f.write(raw)
        con = sqlite3.connect(f"file:{db}?mode=ro", uri=True)
        try:
            rows = con.execute("SELECT b.id, b.parent, b.type, b.title, b.position, b.dateAdded, b.guid, p.url "
                               "FROM moz_bookmarks b LEFT JOIN moz_places p ON b.fk = p.id ORDER BY b.parent, b.position").fetchall()
        finally:
            con.close()
    kids = {}
    by_guid = {}
    for r in rows:
        kids.setdefault(r[1], []).append(r)
        by_guid[r[6]] = r
    items, folders = [], []

    def walk(fid, path):
        for _id, _parent, typ, title, _pos, added, _guid, url in kids.get(fid, []):
            if typ == 2:
                p = f"{path}/{folder_name(title)}" if path else folder_name(title)
                folders.append(p)
                walk(_id, p)
            elif typ == 1 and url and not url.startswith("place:"):
                items.append({"url": url, "title": (title or "").strip(), "add": int((added or 0) / 1_000_000),
                              "folder": path})
    for guid, name in FIREFOX_ROOTS.items():
        r = by_guid.get(guid)
        if r:
            if name:
                folders.append(name)
            walk(r[0], name)
    return items, folders


SAFARI_ROOTS = {"BookmarksBar": "Bookmarks Bar", "BookmarksMenu": "Bookmarks Menu", "com.apple.ReadingList": "Reading List"}


def read_safari(raw):
    doc = plistlib.loads(raw)
    items, folders = [], []

    def walk(node, path):
        for ch in node.get("Children") or []:
            t = ch.get("WebBookmarkType")
            if t == "WebBookmarkTypeList":
                title = ch.get("Title") or ""
                name = SAFARI_ROOTS.get(title, folder_name(title)) if not path else folder_name(title)
                if title == "History" and not path:
                    continue
                p = f"{path}/{name}" if path else name
                folders.append(p)
                walk(ch, p)
            elif t == "WebBookmarkTypeLeaf" and ch.get("URLString"):
                rl = ch.get("ReadingList") or {}
                added = rl.get("DateAdded")
                items.append({"url": ch["URLString"], "title": ((ch.get("URIDictionary") or {}).get("title") or "").strip(),
                              "add": int(added.replace(tzinfo=timezone.utc).timestamp()) if isinstance(added, datetime) else 0,
                              "folder": path})
    walk(doc, "")
    return items, folders


def read_path(path):
    """Read a bookmark file named by path, from a copy. Only bookmark file names are accepted, so this cannot be
    used to read arbitrary files through the server."""
    path = os.path.realpath(os.path.expanduser(str(path)))
    base = os.path.basename(path)
    if base not in ALLOWED_NAMES and not base.lower().endswith(ALLOWED_EXT):
        raise wsm.WsError("only bookmark files can be read: Bookmarks, places.sqlite, Bookmarks.plist, .html, .json")
    if not os.path.isfile(path):
        raise wsm.NotFound(f"no file at {path}")
    try:
        with tempfile.TemporaryDirectory() as tmp:
            copy = os.path.join(tmp, base)
            shutil.copyfile(path, copy)
            for side in ("-wal", "-shm"):  # Firefox keeps recent changes beside the database
                if os.path.exists(path + side):
                    shutil.copyfile(path + side, copy + side)
            if base == "places.sqlite" and os.path.exists(copy + "-wal"):
                con = sqlite3.connect(copy)  # fold the WAL into the copy; the original is never opened
                con.execute("PRAGMA wal_checkpoint(TRUNCATE)")
                con.close()
            with open(copy, "rb") as f:
                return f.read()
    except PermissionError:
        raise wsm.WsError(f"macOS did not allow reading {path}. Export the bookmarks from the browser "
                          "(Bookmarks > Export) and import that file instead.")


def find_profiles():
    out = []
    for browser, pattern in PROFILE_GLOBS.items():
        try:
            paths = glob.glob(os.path.expanduser(pattern))
        except OSError:
            paths = []
        for p in sorted(paths):
            readable = os.access(p, os.R_OK)
            try:
                with open(p, "rb") as f:
                    f.read(1)
            except OSError:
                readable = False
            out.append({"browser": browser, "path": p, "profile": os.path.basename(os.path.dirname(p)),
                        "readable": readable})
        if not paths:
            parent = os.path.expanduser(pattern.split("*")[0])
            if os.path.exists(parent) and not os.access(parent, os.R_OK | os.X_OK):
                out.append({"browser": browser, "path": pattern, "profile": None, "readable": False,
                            "why": "macOS blocks this folder for this app"})
    return out


# ---------- merging ----------
def merge(sources, existing=None, separate=False):
    """sources: [(source name, items, folders)] -> (DATA, duplicate rows). Existing items keep their ids and always
    win over a new copy of the same page, so adding a source never moves what was already being cleaned."""
    old_keep = (existing or {}).get("keep", [])
    old_dupes = (existing or {}).get("dupes", [])
    next_id = max([i["id"] for i in old_keep + old_dupes] + [0]) + 1
    folders = set((existing or {}).get("folders", []))
    cands = []
    for k, it in enumerate(old_keep):
        cands.append({"id": it["id"], "url": it["u"], "title": it["t"], "add": it.get("a") or 0, "folder": it["g"],
                      "source": it.get("src", ""), "old": True, "rec": it, "order": k})
    for name, items, fl in sources:
        pre = folder_name(name) if separate else ""
        for p in fl:
            folders.add(f"{pre}/{p}" if pre and p else (pre or p))
        for it in items:
            folder = f"{pre}/{it['folder']}" if pre and it["folder"] else (pre or it["folder"])
            cands.append({"id": next_id, "url": it["url"], "title": it["title"], "add": it["add"], "folder": folder,
                          "source": name, "old": False, "order": len(cands)})
            next_id += 1
    groups = {}
    for c in cands:
        key = _norm(c["url"]) if urlsplit(c["url"]).scheme in ("http", "https") else c["url"]
        groups.setdefault(key, []).append(c)
    keep, dupes, rows = [], list(old_dupes), []
    for g in groups.values():
        g.sort(key=lambda c: (not c["old"], c["folder"] == "", -c["folder"].count("/"), c["add"] or 1 << 62, c["order"]))
        w, losers = g[0], g[1:]
        if losers and not w["old"]:
            dates = [c["add"] for c in g if c["add"]]
            if dates and min(dates) != w["add"]:
                w["add_was"], w["add"] = w["add"], min(dates)
            if not w["title"] or w["title"] == w["url"]:
                better = next((c["title"] for c in losers if c["title"] and c["title"] != c["url"]), None)
                if better:
                    w["title_was"], w["title"] = w["title"], better
        rec = w["rec"] if w["old"] else _rec(w)
        keep.append(rec)
        for d in losers:
            if d["old"]:
                continue  # never demote something already in the set
            dupes.append(dict(_rec(d), k=rec["f"], kg=rec["g"], dupOf=rec["id"]))
            rows.append({"kept_title": rec["t"], "kept_url": rec["u"], "kept_in": wsm.display(rec["g"]),
                         "removed_title": d["title"], "removed_url": d["url"],
                         "removed_was_in": wsm.display(d["folder"]) or "(Other bookmarks)",
                         "removed_added": _day(d["add"]), "kept_took_earlier_date": "yes" if "add_was" in w else "",
                         "kept_took_title": "yes" if "title_was" in w else "", "removed_source": d["source"]})
    keep.sort(key=lambda r: (r["g"], r["id"]))
    for r in keep:
        p = r["g"]
        while p:
            folders.add(p)
            p = p.rpartition("/")[0]
    data = {"keep": keep, "dupes": dupes, "newMine": [], "folders": sorted(f for f in folders if f),
            "total": len(keep) + len(dupes)}
    return data, rows


def _day(t):
    return datetime.fromtimestamp(t, timezone.utc).date().isoformat() if t else ""


def _rec(c):
    """One bookmark in the page's DATA shape."""
    return {"id": c["id"], "a": c["add"], "f": c["folder"], "g": c["folder"], "t": c["title"], "u": c["url"],
            "ou": c["url"], "s": "unverified", "n": "imported, not checked yet; Refresh checks it",
            "o": wsm.display(c["folder"]) or "(Other bookmarks)", "w": f"imported from {c['source']}",
            "p": 0, "m": 0, "src": c["source"]}


def flatten_workspace(ws_id):
    """A workspace's current bookmarks as a source: edits applied, the Trash left out."""
    data = wsm.load_data(ws_id)
    st = wsm.editable(wsm.get_state(ws_id)[0], data)
    items = wsm.items_by_id(data)
    root = st["tree"]["root"]
    bin_ = wsm.trash_folder(root)
    out, folders = [], []
    for f, path in wsm.walk(root):
        if bin_ is not None and (f is bin_ or path.split("/")[0] == wsm.TRASH):
            continue
        if path:
            folders.append(path)
        for x in f["c"]:
            if not isinstance(x, dict) and x in items:
                v = wsm.item_view(items[x], st, path)
                out.append({"url": v["url"], "title": v["title"], "add": items[x].get("a") or 0, "folder": path})
    return out, folders


def stash_source(ws_dir, name, raw):
    """Keep a copy of every imported file in the workspace, with its checksum."""
    os.makedirs(os.path.join(ws_dir, "sources"), exist_ok=True)
    sha = hashlib.sha256(raw).hexdigest()
    safe = "".join(ch if ch.isalnum() or ch in "._-" else "_" for ch in os.path.basename(name))[:80] or "source"
    path = os.path.join(ws_dir, "sources", f"{sha[:12]}-{safe}")
    if not os.path.exists(path):
        with open(path, "wb") as f:
            f.write(raw)
    return sha, os.path.relpath(path, ws_dir)


def import_sources(name, files, base_ws=None, separate=False, by="page"):
    """Create a workspace from files [(filename, bytes)], optionally starting from another workspace's current set."""
    sources, metas, raws = [], [], []
    if base_ws:
        bitems, bfolders = flatten_workspace(base_ws)
        bname = wsm.read_meta(base_ws)["name"]
        sources.append((f"workspace {bname}", bitems, bfolders))
        metas.append({"name": f"workspace {bname}", "kind": "workspace", "workspace": base_ws, "count": len(bitems),
                      "added": wsm.now_iso()})
    for filename, raw in files:
        kind, items, folders = read_source(raw, filename)
        label = os.path.basename(filename) or kind
        sources.append((label, items, folders))
        raws.append((label, raw))
        metas.append({"name": label, "kind": kind, "count": len(items), "added": wsm.now_iso()})
    if not sources:
        raise wsm.WsError("choose at least one bookmark file")
    data, rows = merge(sources, separate=separate)
    for m in metas:
        m["separate"] = bool(separate)  # so Update re-reads the sources the way they were first merged
    ws_id = wsm.create_workspace(name, data, metas, rows, by=by)
    d = wsm.ws_dir(ws_id)
    for m, (label, raw) in zip([m for m in metas if m["kind"] != "workspace"], raws):
        m["sha256"], m["stored"] = stash_source(d, label, raw)
    meta = wsm.read_meta(ws_id)
    meta["sources"] = metas
    wsm.write_json(os.path.join(d, "meta.json"), meta)
    return ws_id


def add_sources(ws_id, files, separate=False, by="page"):
    """Merge more files into an imported workspace. Existing bookmarks keep their ids, places and edits."""
    meta = wsm.read_meta(ws_id)
    wsm.refuse_if_locked(ws_id)
    if meta["kind"] != "import":
        raise wsm.WsError("the built workspace is rebuilt by src/pipeline/build.py; import into a new workspace instead "
                          "(it can start from this one)")
    parsed = []
    for filename, raw in files:
        kind, items, folders = read_source(raw, filename)
        parsed.append((os.path.basename(filename) or kind, kind, raw, items, folders))
    with wsm.locked(ws_id) as d:
        data = wsm.load_data(ws_id)
        before = len(data["keep"])
        new, rows = merge([(n, it, fl) for n, _, _, it, fl in parsed], existing=data, separate=separate)
        wsm.write_json(os.path.join(d, "data.json"), new)
        wsm.write_dup_csv(os.path.join(d, "duplicates.csv"), wsm.read_dup_csv(os.path.join(d, "duplicates.csv")) + rows)
        for n, kind, raw, items, _ in parsed:
            sha, stored = stash_source(d, n, raw)
            meta["sources"].append({"name": n, "kind": kind, "count": len(items), "added": wsm.now_iso(),
                                    "sha256": sha, "stored": stored})
        wsm._save_meta(d, meta)
        head = wsm.head_entry(ws_id)
        state = wsm.read_snapshot(d, head["snap"]) if head else wsm.empty_state()
        added = len(new["keep"]) - before
        e = wsm._append(d, state, "add-source", f"Added {', '.join(p[0] for p in parsed)}: {added} new bookmarks, "
                        f"{len(rows)} duplicate copies merged", by, base=head and head["seq"], undoable=False)
    return e


# ---------- update: re-read where a workspace came from ----------
def _key(u):
    return _norm(u) if urlsplit(u).scheme in ("http", "https") else u


def _upstream(ws_id, meta):
    """The workspace's sources read again: the stored copy of each imported file, and the current set of a workspace
    it started from. Returns ([(name, items, folders, separate)], [names that could not be read])."""
    d = wsm.existing_dir(ws_id)
    out, missing = [], []
    for src in meta.get("sources", []):
        try:
            if src.get("kind") == "workspace":
                items, folders = flatten_workspace(src["workspace"])
            else:
                with open(os.path.join(d, src["stored"]), "rb") as f:
                    _, items, folders = read_source(f.read(), src["name"])
            out.append((src["name"], items, folders, bool(src.get("separate"))))
        except (KeyError, OSError, wsm.WsError):
            missing.append(src.get("name", "?"))
    return out, missing


def _tree_ids(state):
    return set(wsm.locate(state["tree"]["root"])) if state.get("tree") else None


def update_preview(ws_id):
    """What Update would do, without doing it: new bookmarks, ones gone upstream, ones retitled upstream."""
    meta = wsm.read_meta(ws_id)
    data = wsm.load_data(ws_id)
    if meta["kind"] == "build":
        state, _ = wsm.get_state(ws_id)
        ids = _tree_ids(state)
        if ids is None:
            return {"kind": "build", "new": [], "removed": [], "changed": [], "missing": [],
                    "note": "No edits yet, so this workspace already shows the latest build."}
        gone = set(state["tree"].get("gone") or [])
        dupes = {i["id"] for i in data["dupes"]}
        new = [i for i in data["keep"] if i["id"] not in ids and i["id"] not in gone]
        removed = sorted(i for i in ids if i not in {k["id"] for k in data["keep"]} and i not in dupes)
        return {"kind": "build", "new": [_brief(i) for i in new], "removed": [{"id": i} for i in removed],
                "changed": [], "missing": [], "note": "Compared with the current build (src/pipeline/build.py writes it)."}
    srcs, missing = _upstream(ws_id, meta)
    fresh = {}
    for name, items, _, sep in srcs:
        for it in items:
            fresh.setdefault(_key(it["url"]), (name, it))
    have = {_key(i["u"]) for i in data["keep"] + data["dupes"]}
    read_names = {n for n, *_ in srcs}
    new = [{"title": it["title"], "url": it["url"], "folder": wsm.display(it["folder"]), "source": n}
           for k, (n, it) in fresh.items() if k not in have]
    removed = [_brief(i) for i in data["keep"] if i.get("src") in read_names and _key(i["u"]) not in fresh]
    changed = [dict(_brief(i), new_title=fresh[_key(i["u"])][1]["title"]) for i in data["keep"]
               if _key(i["u"]) in fresh and fresh[_key(i["u"])][1]["title"]
               and fresh[_key(i["u"])][1]["title"] != i["t"] and fresh[_key(i["u"])][0] == i.get("src")]
    return {"kind": "import", "new": new, "removed": removed, "changed": changed, "missing": missing,
            "note": "Removed upstream means no longer in the source; they stay here until you trash them."}


def _brief(i):
    return {"id": i["id"], "title": i["t"], "url": i["u"], "folder": wsm.display(i.get("g", ""))}


def update_apply(ws_id, by="page"):
    """Bring the upstream changes in: new bookmarks are added where they belong, titles changed upstream are taken
    (a title you renamed here still wins), and nothing is removed. Edits, places and history are kept."""
    wsm.refuse_if_locked(ws_id)
    meta = wsm.read_meta(ws_id)
    pv = update_preview(ws_id)
    summary = (f"Updated from source: {len(pv['new'])} new, {len(pv['removed'])} gone upstream (kept), "
               f"{len(pv['changed'])} retitled upstream")
    if meta["kind"] == "build":
        state, head = wsm.get_state(ws_id)
        if state.get("tree") is None or not (pv["new"] or pv["removed"]):
            return None, pv
        data = wsm.load_data(ws_id)
        st = wsm.editable(state, data)
        root = st["tree"]["root"]
        drop = {r["id"] for r in pv["removed"]}
        for f, _ in wsm.walk(root):
            f["c"] = [x for x in f["c"] if isinstance(x, dict) or x not in drop]
        for b in pv["new"]:
            wsm._folder_outside_trash(root, next(i["g"] for i in data["keep"] if i["id"] == b["id"]))["c"].append(b["id"])
        e = wsm.commit(ws_id, st, by=by, base_seq=head, op="update", summary=summary.replace(
            "gone upstream (kept)", "no longer in the build"))
        return e, pv
    srcs, _ = _upstream(ws_id, meta)
    with wsm.locked(ws_id) as d:
        data = wsm.load_data(ws_id)
        retitle = {c["id"]: c["new_title"] for c in pv["changed"]}
        for i in data["keep"]:
            if i["id"] in retitle:
                i["t"] = retitle[i["id"]]
        have = {_key(i["u"]) for i in data["keep"] + data["dupes"]}
        rows = []
        if pv["new"]:
            fresh = [(n, [it for it in items if _key(it["url"]) not in have], fl) for n, items, fl, _ in srcs]
            data, rows = merge([f for f in fresh if f[1]], existing=data, separate=any(x[3] for x in srcs))
        wsm.write_json(os.path.join(d, "data.json"), data)
        if rows:
            wsm.write_dup_csv(os.path.join(d, "duplicates.csv"),
                              wsm.read_dup_csv(os.path.join(d, "duplicates.csv")) + rows)
        head = wsm.head_entry(ws_id)
        state = wsm.read_snapshot(d, head["snap"]) if head else wsm.empty_state()
        e = wsm._append(d, state, "update", summary, by, base=head and head["seq"], undoable=False)
    return e, pv


# ---------- writing ----------
def _esc(s):
    return str(s).replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;").replace('"', "&quot;")


def _export_tree(ws_id):
    data = wsm.load_data(ws_id)
    st = wsm.editable(wsm.get_state(ws_id)[0], data)
    return st, wsm.items_by_id(data), wsm.read_meta(ws_id)


def export_netscape(ws_id):
    """The same file the page's Download builds: edits applied, Trash left out, descriptions as <DD>."""
    st, items, meta = _export_tree(ws_id)
    root = st["tree"]["root"]
    bin_ = wsm.trash_folder(root)
    day = datetime.now(timezone.utc).date().isoformat()
    out = ["<!DOCTYPE NETSCAPE-Bookmark-file-1>", '<META HTTP-EQUIV="Content-Type" CONTENT="text/html; charset=UTF-8">',
           "<TITLE>Bookmarks</TITLE>", "<H1>Bookmarks</H1>", "<DL><p>",
           f"    <DT><H3>{_esc(meta['name'])} (exported {day})</H3>", "    <DL><p>"]
    count = 0

    def walk(f, path, depth):
        nonlocal count
        ind = "    " * depth
        for x in f["c"]:
            if isinstance(x, dict):
                if x is bin_:
                    continue
                out.append(f"{ind}<DT><H3>{_esc(wsm.display(x['n']))}</H3>")
                if x.get("d"):
                    out.append(f"{ind}<DD>{_esc(x['d'])}")
                out.append(f"{ind}<DL><p>")
                walk(x, f"{path}/{x['n']}" if path else x["n"], depth + 1)
                out.append(f"{ind}</DL><p>")
            elif x in items:
                v = wsm.item_view(items[x], st, path)
                count += 1
                out.append(f'{ind}<DT><A HREF="{_esc(v["url"])}" ADD_DATE="{items[x].get("a") or 0}">{_esc(v["title"])}</A>')
                note = st["tree"]["notes"].get(str(x))
                if note:
                    out.append(f"{ind}<DD>{_esc(note)}")
    walk(root, "", 2)
    out += ["    </DL><p>", "</DL><p>", ""]
    return "\n".join(out), count


def export_chrome(ws_id):
    """A Chrome "Bookmarks" JSON file. It is written to the workspace's exports folder, never into a profile:
    Chrome overwrites that file while it runs, and replacing it by hand risks the profile."""
    st, items, _ = _export_tree(ws_id)
    root = st["tree"]["root"]
    bin_ = wsm.trash_folder(root)
    seq = [0]

    def nid():
        seq[0] += 1
        return str(seq[0])
    now = str(int((datetime.now(timezone.utc).timestamp() + CHROME_EPOCH) * 1_000_000))
    count = 0

    def node(f, path):
        nonlocal count
        kids = []
        for x in f["c"]:
            if isinstance(x, dict):
                if x is bin_:
                    continue
                kids.append(node(x, f"{path}/{x['n']}" if path else x["n"]))
            elif x in items:
                v = wsm.item_view(items[x], st, path)
                count += 1
                a = items[x].get("a") or 0
                kids.append({"date_added": str(int((a + CHROME_EPOCH) * 1_000_000)) if a else now, "id": nid(),
                             "name": v["title"], "type": "url", "url": v["url"]})
        return {"children": kids, "date_added": now, "date_modified": now, "id": nid(), "name": wsm.display(f["n"]),
                "type": "folder"}
    bar = next((x for x in root["c"] if isinstance(x, dict) and x["n"] == "Bookmarks Bar"), None)
    mobile = next((x for x in root["c"] if isinstance(x, dict) and x["n"] == "Mobile bookmarks"), None)
    rest = {"n": "Other bookmarks", "c": [x for x in root["c"] if x is not bar and x is not mobile]}
    roots = {"bookmark_bar": node(bar or {"n": "Bookmarks bar", "c": []}, "Bookmarks Bar"),
             "other": node(rest, ""), "synced": node(mobile or {"n": "Mobile bookmarks", "c": []}, "Mobile bookmarks")}
    roots["bookmark_bar"]["name"] = "Bookmarks bar"
    roots["other"]["name"] = "Other bookmarks"
    doc = {"checksum": chrome_checksum(roots), "roots": roots, "version": 1}
    return json.dumps(doc, ensure_ascii=False, indent=3), count


def chrome_checksum(roots):
    """Chrome's own checksum (MD5 over id, UTF-16 name, type and URL, in tree order), so Chrome reads the file as
    intact rather than as damaged."""
    md5 = hashlib.md5()

    def upd(n):
        md5.update(n["id"].encode())
        md5.update(n["name"].encode("utf-16-le"))
        if n["type"] == "url":
            md5.update(b"url")
            md5.update(n["url"].encode())
        else:
            md5.update(b"folder")
            for c in n["children"]:
                upd(c)
    for k in ("bookmark_bar", "other", "synced"):
        upd(roots[k])
    return md5.hexdigest()


def write_export(ws_id, fmt):
    """Write an export into the workspace's exports folder. A new file each time: nothing is overwritten."""
    text, count = export_netscape(ws_id) if fmt == "html" else export_chrome(ws_id) if fmt == "chrome" else (None, 0)
    if text is None:
        raise wsm.WsError("format is html or chrome")
    d = os.path.join(wsm.existing_dir(ws_id), "exports")
    os.makedirs(d, exist_ok=True)
    stamp = datetime.now(timezone.utc).strftime("%Y%m%d-%H%M%S")
    path = os.path.join(d, f"{ws_id}-{stamp}." + ("html" if fmt == "html" else "Bookmarks.json"))
    n = 2
    while os.path.exists(path):
        path = path.replace(f"-{stamp}", f"-{stamp}-{n}", 1)
        n += 1
    wsm.write_atomic(path, text)
    return path, count
