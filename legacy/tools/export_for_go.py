"""Write everything the Python tool keeps as one Browser Foreman backup, for `browser-foreman restore`.

    python3 legacy/tools/export_for_go.py --root ~/path/to/the/python/checkout --out ~/browser-foreman-from-python.json.gz
    browser-foreman restore ~/browser-foreman-from-python.json.gz

It only reads. Every workspace comes across with its whole history: each entry's saved page state is rebuilt into a
tree the way the page rebuilds it (bookmarks no longer in the data drop out, ones the saved tree lacks go into their
own folder path, titles, addresses and notes edited in the page apply, the top Trash folder becomes the Trash).
Duplicates the cleanup merged go into the Trash with the reason "duplicate", so they can still be put back. Link check
results, every tab snapshot (with the pages the owner chose to keep) and the tab settings come across too.

Python's own undo and redo entries carry no link to the entry they took back, so they arrive as ordinary steps named
legacy-undo and legacy-redo: every state is kept, and undo in Browser Foreman steps back one entry at a time.
"""
import argparse
import gzip
import hashlib
import json
import os
import re
import sqlite3
import sys
from datetime import datetime, timezone

TRASH_NAMES = ("Trash", "_Removed (not exported)")
NONE = (None, "", "None")


def iso(s):
    """'2026-11-12T12:46:58-0500' (no colon in the offset) or an epoch, as RFC 3339."""
    if s in NONE:
        return datetime.now(timezone.utc).isoformat()
    if isinstance(s, (int, float)):
        return datetime.fromtimestamp(s, timezone.utc).isoformat()
    s = str(s)
    m = re.match(r"^(.*[T ]\d\d:\d\d:\d\d(?:\.\d+)?)([+-]\d\d)(\d\d)$", s)
    if m:
        s = f"{m.group(1)}{m.group(2)}:{m.group(3)}"
    return datetime.fromisoformat(s).isoformat()


def build_data(root):
    with open(os.path.join(root, "public", "index.html"), encoding="utf-8") as f:
        for line in f:
            if line.startswith("const DATA = ") and line.rstrip().endswith(";"):
                return json.loads(line[len("const DATA = "):].rstrip().rstrip(";"))
    raise SystemExit("public/index.html has no DATA line: is --root the Python checkout?")


def default_tree(data):
    root, idx = {"n": "", "c": []}, {"": None}
    idx[""] = root

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
        folder_at(i.get("g") or "")["c"].append(i["id"])

    # As Python and the page order it: links first, then folders (Bookmarks Bar first, "_" folders, Trash last).
    def rank(n):
        return 0 if n == "Bookmarks Bar" else 3 if n == "Trash" else 2 if n.startswith("_") else 1

    def tidy(f):
        links = [x for x in f["c"] if not isinstance(x, dict)]
        subs = sorted((x for x in f["c"] if isinstance(x, dict)), key=lambda x: (rank(x["n"]), x["n"].strip().lower()))
        f["c"] = links + subs
        for x in subs:
            tidy(x)
    tidy(root)
    return root


def restore(saved, items, gone):
    """The page's restoreTree: unknown, gone and repeated ids drop out; bookmarks the tree lacks go to their path."""
    seen = set()

    def prune(f):
        out = []
        for x in f.get("c", []):
            if isinstance(x, dict):
                prune(x)
                out.append(x)
                continue
            k = str(x)
            if k in items and k not in gone and k not in seen:
                seen.add(k)
                out.append(k)
        f["c"] = out
    root = json.loads(json.dumps(saved))
    for f in root.get("c", []):
        if isinstance(f, dict) and f.get("n") == "_Removed (not exported)":
            f["n"] = "Trash"
    prune(root)
    for k, i in items.items():
        if k in seen or k in gone:
            continue
        f = root
        for part in (i.get("g") or "").split("/"):
            if not part:
                continue
            nxt = next((x for x in f["c"] if isinstance(x, dict) and x.get("n") == part), None)
            if nxt is None:
                nxt = {"n": part, "c": []}
                f["c"].append(nxt)
            f = nxt
        f["c"].append(k)
    return root


def val(x):
    return None if x in NONE else x


class Converter:
    def __init__(self, data):
        self.data = data

    def tree(self, state, at):
        at_unix = int(datetime.fromisoformat(at).timestamp())  # the Trash keeps Unix seconds
        added = list((state.get("added") or {}).values())
        items = {str(i["id"]): i for i in self.data["keep"] + added}
        st = state.get("tree")
        saved = st["root"] if st else default_tree(self.data)
        gone = {str(x) for x in (st or {}).get("gone", [])}
        titles = {str(k): v for k, v in (st or {}).get("titles", {}).items()}
        notes = {str(k): v for k, v in (st or {}).get("notes", {}).items()}
        trash_from = {str(k): v for k, v in (st or {}).get("trashFrom", {}).items()}
        urls = {str(k): v for k, v in (state.get("urls") or {}).items()}
        root = restore(saved, items, gone)
        used = {}

        def folder_id(path):
            n = used.get(path, 0)
            used[path] = n + 1
            h = hashlib.sha1(path.encode()).hexdigest()[:12]
            return f"pf-{h}" + (f"-{n}" if n else "")

        def bookmark(k):
            i = items[k]
            node = {"id": f"py-{k}", "kind": "bookmark", "title": titles.get(k, i.get("t") or ""), "url": urls.get(k, i.get("u") or "")}
            if notes.get(k):
                node["note"] = notes[k]
            if val(i.get("ds")):
                node["description"] = i["ds"]
            a = str(i.get("a") or "")
            if a.isdigit():
                node["added"] = int(a)
            return node

        paths = {}

        def conv(f, path):
            name = f.get("n", "")
            p = f"{path}/{name}" if path else name
            node = {"id": folder_id(p), "kind": "folder", "title": name.replace("∕", "/"), "children": []}
            paths.setdefault(p, node["id"])
            if f.get("d"):
                node["note"] = f["d"]
            for x in f.get("c", []):
                node["children"].append(conv(x, p) if isinstance(x, dict) else bookmark(x))
            return node
        top = {"id": "root", "kind": "folder", "title": "", "children": []}
        bin_ = None
        for x in root.get("c", []):
            if isinstance(x, dict) and x.get("n") in TRASH_NAMES and bin_ is None:
                bin_ = x
                continue
            top["children"].append(conv(x, "") if isinstance(x, dict) else bookmark(x))
        trash = []
        for x in (bin_ or {}).get("c", []):
            if isinstance(x, dict):
                frm = x.get("from") or {}
                node = conv(x, "Trash")
            else:
                frm = trash_from.get(x) or {}
                node = bookmark(x)
            path = frm.get("path", "") if isinstance(frm, dict) else ""
            trash.append({"node": node, "from": paths.get(path, "root"), "index": int(frm.get("index", 0)) if isinstance(frm, dict) else 0, "at": at_unix})
        ids = {k for k in items}
        for d in self.data.get("dupes") or []:
            k = str(d["id"])
            nid = f"py-{k}" if k not in ids else f"py-dup-{k}"
            node = {"id": nid, "kind": "bookmark", "title": d.get("t") or "", "url": d.get("u") or ""}
            trash.append({"node": node, "from": paths.get(d.get("g") or "", "root"), "index": 0, "at": at_unix,
                          "reason": f"duplicate of py-{d.get('k')}" if val(d.get("k")) else "duplicate"})
        out = {"root": top}
        if trash:
            out["trash"] = trash
        return out, items, urls

    def links(self, items, urls, refreshed, built, head_at):
        out = {}
        for k, i in items.items():
            u = urls.get(k, i.get("u") or "")
            if not u.startswith(("http://", "https://")) or val(i.get("s")) is None:
                continue
            r = {"url": u, "status": i["s"], "checked": built}
            if val(i.get("n")):
                r["note"] = i["n"]
            if val(i.get("dk")):
                r["kind"] = i["dk"]
            if i["s"] == "moved" and val(i.get("nu")):
                r["final"] = i["nu"]
            out[u] = r
        for k, v in (refreshed or {}).items():
            i = items.get(str(k))
            if not i or not isinstance(v, dict) or val(v.get("s")) is None:
                continue
            u = urls.get(str(k), i.get("u") or "")
            r = {"url": u, "status": v["s"], "checked": head_at}
            if val(v.get("n")):
                r["note"] = v["n"]
            if val(v.get("dk")):
                r["kind"] = v["dk"]
            out[u] = r
        return out


def read_snapshot(ws_dir, h):
    for name, opener in ((f"{h}.json.gz", gzip.open), (f"{h}.json", open)):
        p = os.path.join(ws_dir, "snapshots", name)
        if os.path.exists(p):
            with opener(p, "rt", encoding="utf-8") as f:
                return json.load(f)
    raise SystemExit(f"{ws_dir}: snapshot {h} is missing")


def workspaces(root, data, backup):
    wroot = os.path.join(root, "storage", "workspaces")
    conv = Converter(data)
    built = iso(data.get("built"))
    for wid in sorted(os.listdir(wroot)):
        d = os.path.join(wroot, wid)
        if not os.path.isfile(os.path.join(d, "meta.json")):
            continue
        with open(os.path.join(d, "meta.json"), encoding="utf-8") as f:
            meta = json.load(f)
        if meta.get("kind") != "build":
            with open(os.path.join(d, "data.json"), encoding="utf-8") as f:
                conv = Converter(json.load(f))
        else:
            conv = Converter(data)
        with open(os.path.join(d, "history.jsonl"), encoding="utf-8") as f:
            hist = [json.loads(x) for x in f if x.strip()]
        entries = []
        state = {}
        for e in hist:
            state = read_snapshot(d, e["snap"])
            at = iso(e.get("at") or e.get("t"))
            tree, items, urls = conv.tree(state, at)
            backup["trees"][e["snap"]] = tree
            op = {"undo": "legacy-undo", "redo": "legacy-redo"}.get(e.get("op"), e.get("op") or "edit")
            entries.append({"seq": len(entries) + 1, "at": at, "op": op, "summary": e.get("summary") or "", "base": len(entries), "snap": e["snap"]})
        if entries:
            backup["links"].update(conv.links(items, urls, state.get("refreshed"), built, entries[-1]["at"]))
        ws = {"id": wid, "name": meta.get("name") or wid, "created": iso(meta.get("created")), "head": len(entries),
              "notes": meta.get("notes") or "", "locked": bool(meta.get("locked")), "archived": bool(meta.get("archived"))}
        backup["workspaces"].append({"workspace": ws, "history": entries})


def tabs(root, backup):
    path = os.path.join(root, "storage", "sessions", "tabs.sqlite3")
    if not os.path.exists(path):
        return
    c = sqlite3.connect(f"file:{path}?mode=ro", uri=True)
    c.row_factory = sqlite3.Row
    settings = {r["key"]: json.loads(r["value"]) for r in c.execute("SELECT key, value FROM settings")}
    names = {"autosave": "autosave", "interval": "interval_seconds", "include_incognito": "include_incognito", "keep_days": "keep_days",
             "save_mode": "save_mode", "watch_seconds": "watch_seconds", "recheck_days": "recheck_days",
             "tracked_browsers": "tracked", "open_with": "open_with"}
    prefs = {names[k]: v for k, v in settings.items() if k in names}
    if isinstance(prefs.get("open_with"), str):
        prefs["open_with"] = [prefs["open_with"]]
    if prefs:
        backup["prefs"] = prefs
    for s in c.execute("SELECT * FROM snapshots ORDER BY id"):
        tabs_by = {}
        for t in c.execute("SELECT window_seq, url, title FROM tabs WHERE snapshot_id = ? ORDER BY window_seq, pos", (s["id"],)):
            tabs_by.setdefault(t["window_seq"], []).append({"url": t["url"], "title": t["title"]})
        browsers = {}
        for w in c.execute("SELECT * FROM windows WHERE snapshot_id = ? ORDER BY seq", (s["id"],)):
            win = {"index": w["idx"] or 0, "mode": w["mode"] or "normal"}
            if w["active"]:
                win["active"] = w["active"]
            if w["pages_kept"]:
                win["tabs"] = tabs_by.get(w["seq"], [])
            else:
                win["tab_count"], win["redacted"] = w["tab_count"], True
            browsers.setdefault(w["browser"], {"browser": w["browser"], "running": True, "windows": []})["windows"].append(win)
        at = iso(s["at"])
        doc = {"at": at, "browsers": list(browsers.values()),
               "summary": {"windows": s["windows"], "tabs": s["tabs"], "incognito_windows": s["incognito_windows"],
                           "incognito_tabs": s["incognito_tabs"], "running": len(browsers)},
               "fingerprint": f"py-{s['fingerprint']}"}
        backup["tabs"].append({"id": s["id"], "at": at, "reason": s["reason"] or "auto", "windows": doc})
    c.close()


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--root", required=True, help="the Python tool's checkout (it holds public/ and storage/)")
    ap.add_argument("--out", required=True, help="the backup file to write (never overwritten)")
    a = ap.parse_args(argv)
    root = os.path.expanduser(a.root)
    out = os.path.expanduser(a.out)
    if os.path.exists(out):
        raise SystemExit(f"{out} exists: choose another name")
    backup = {"format": "browser-foreman-backup", "version": 1, "created": datetime.now(timezone.utc).isoformat(),
              "workspaces": [], "trees": {}, "links": {}, "tabs": []}
    data = build_data(root)
    workspaces(root, data, backup)
    tabs(root, backup)
    fd = os.open(out, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with gzip.open(os.fdopen(fd, "wb"), "wt", encoding="utf-8") as f:
        json.dump(backup, f)
    n_entries = sum(len(w["history"]) for w in backup["workspaces"])
    print(f"wrote {out}: {len(backup['workspaces'])} workspace(s), {n_entries} history entries, {len(backup['trees'])} trees, "
          f"{len(backup['links'])} link results, {len(backup['tabs'])} tab snapshots" + (", settings" if backup.get("prefs") else ""))


if __name__ == "__main__":
    sys.exit(main())
