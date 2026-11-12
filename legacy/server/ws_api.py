"""The workspace endpoints of src/server/serve.py, and the page it serves for a workspace.

Reads (GET) answer only requests addressed to localhost, so a site that rebinds its own name to 127.0.0.1 cannot
read bookmarks through a victim's browser. Writes (POST) also need the per-run token, which reaches only the page
this server itself served (it is embedded in that page) and is sent back in the X-BM-Token header; a page from any
other origin cannot read it, and cannot send a custom header here without a CORS preflight this server never answers.

GET  /api/token                          the token, to a local same-origin caller (tests, tools)
GET  /api/workspaces                     every workspace, archived ones included, with its head
GET  /api/ws/<id>                        meta and head
GET  /api/ws/<id>/state                  the current state and its seq
GET  /api/ws/<id>/history[?limit=&before=]  entries, newest first
GET  /api/ws/<id>/history/<seq>          the state after that entry
GET  /api/ws/<id>/export?format=html|chrome   a download of the current set
GET  /api/ws/<id>/duplicates.csv         the copies merged away at import
GET  /api/profiles                       browser bookmark files on this Mac, and whether they can be read
POST /api/ws                             {name, files:[{name, data (base64)}], paths:[...], base, separate}
POST /api/ws/<id>/sources                {files, paths, separate}   merge more files in
POST /api/ws/<id>/rename                 {name}
POST /api/ws/<id>/archive                {archived}
POST /api/ws/<id>/notes                  {notes}
POST /api/ws/<id>/commit                 {state | changes, base}   409 when base is not the head; `changes` holds
                                         only the keys that changed (tree, urls, refreshed)
POST /api/ws/<id>/migrate                {state}             the browser-kept edits, once, as the first entry
POST /api/ws/<id>/undo                   {}
POST /api/ws/<id>/redo                   {}                  bring back what the last undo took away
GET  /api/ws/<id>/undo-state             {undo, redo}: what each would do now, or null
POST /api/ws/<id>/rollback               {seq}
POST /api/ws/<id>/export                 {format}            write a file into the workspace's exports folder
POST /api/ws/<id>/add-links              {folder, links:[{url, title}]}   save tabs as new bookmarks in a new folder
POST /api/ws/<id>/lock                   {locked}            read-only on/off; a locked workspace answers 423 to edits
POST /api/ws/<id>/reset                  {}                  back to its starting point, as one undoable entry
GET  /api/ws/<id>/update                 what Update would bring in (new, gone upstream, retitled)
POST /api/ws/<id>/update                 {}                  apply it; nothing is removed, edits are kept
POST /api/ws/<id>/delete                 {confirm: <id>}     move it to workspaces/_trash/ (not the built one)
GET  /api/trash                          deleted workspaces
GET  /api/settings                       tool settings, and where the MCP server is registered with Claude
POST /api/settings                       {settings: {mcp_allow_writes}}
POST /api/mcp                            {target: folder|code|desktop, action: add|remove, writes}
GET  /api/backup                         a .zip of all workspaces and the tabs database, with a checksummed manifest
POST /api/restore                        {data (base64 zip), confirm: "restore"}   current data is backed up and moved
                                         to _before-restore/<time>/ first
POST /api/trash/restore                  {trash_id}
POST /api/trash/empty                    {confirm: "empty the workspace trash", trash_ids?}   permanent
"""
import base64
import hmac
import json
import os
import posixpath
import re
import secrets
from urllib.parse import parse_qs, unquote

import backup
import paths
import bookmark_io as bio
import mcp_register
import workspaces as wsm

TOKEN = os.environ.get("BM_TOKEN") or secrets.token_urlsafe(24)
MAX_BODY = 260 * 1024 * 1024  # base64 of the largest source file, plus room
HERE = os.path.dirname(os.path.abspath(__file__))
WS_PATH = re.compile(r"^/api/ws/([^/]+)(?:/(.*))?$")


def host_ok(h):
    host = h.headers.get("Host", "")
    return re.fullmatch(r"(localhost|127\.0\.0\.1)(:\d+)?", host) is not None


def origin_ok(h):
    origin = h.headers.get("Origin")
    if origin is not None and origin != f"http://{h.headers.get('Host', '')}":
        return False
    return h.headers.get("Sec-Fetch-Site", "same-origin") in ("same-origin", "none")


def token_ok(h):
    # Compared as bytes: compare_digest refuses a str with non-ASCII characters, which a header can carry.
    given = h.headers.get("X-BM-Token", "").encode("utf-8", "surrogateescape")
    return hmac.compare_digest(given, TOKEN.encode())


def clean_path(raw):
    """The request path as the server will treat it: unquoted, "." and ".." resolved, repeated slashes collapsed, so
    "/./preview.html", "//preview.html" and "/x/../api/…" cannot slip past an exact match."""
    p = posixpath.normpath(unquote(raw.split("?", 1)[0].split("#", 1)[0]) or "/")  # not urlsplit: "//x" is a host to it
    return "/" + p.lstrip("/") if p != "." else "/"


def embed_json(obj):
    """JSON that is safe inside a <script> element: no "<", ">" or "&" (so neither "</script>" nor "<!--" in a title
    can end or change the script), and no U+2028/U+2029 line separators. Same escaping as build.py."""
    return (json.dumps(obj, ensure_ascii=False).replace("<", "\\u003c").replace(">", "\\u003e").replace("&", "\\u0026")
            .replace("\u2028", "\\u2028").replace("\u2029", "\\u2029"))


def _body(h):
    try:
        n = int(h.headers.get("Content-Length") or 0)
    except ValueError:
        raise wsm.WsError("bad Content-Length")
    if n < 0:
        raise wsm.WsError("bad Content-Length")  # a negative length would make read() wait for ever
    if n > MAX_BODY:
        raise wsm.WsError("request too large")
    raw = h.rfile.read(n) if n else b"{}"
    try:
        data = json.loads(raw or b"{}")
    except ValueError:
        raise wsm.WsError("body is not JSON")
    if not isinstance(data, dict):
        raise wsm.WsError("body must be an object")
    return data


def _files(data):
    files = []
    for f in data.get("files") or []:
        try:
            files.append((str(f["name"])[:200], base64.b64decode(f["data"], validate=True)))
        except (KeyError, TypeError, ValueError):
            raise wsm.WsError("each file needs a name and base64 data")
    for p in data.get("paths") or []:
        files.append((os.path.basename(str(p)), bio.read_path(p)))
    return files


def _ws_brief():
    return [{k: m.get(k) for k in ("id", "name", "kind", "archived", "locked", "head", "last_change")}
            for m in wsm.list_workspaces()]


# ---------- the page ----------
def page_html(ws_id):
    """The editor for a workspace: preview.html for the built set, the template with the workspace's own data for an
    imported one; either way with the workspace's state and the token embedded before the page's script runs."""
    meta = wsm.read_meta(ws_id)
    if meta["kind"] == "build":
        with open(wsm.preview_path(), encoding="utf-8") as f:
            html = f.read()
        if wsm.get_state(ws_id)[0].get("added"):  # bookmarks saved from tabs join the built set's data
            start = html.index("\nconst DATA = ") + 1
            end = html.index(";\n", start)
            html = html[:start] + "const DATA = " + embed_json(wsm.load_data(ws_id)) + html[end:]
    else:
        with open(paths.TEMPLATE, encoding="utf-8") as f:
            tpl = f.read()
        data = embed_json(wsm.load_data(ws_id))
        html = tpl.replace("/*DATA*/null", data, 1)
    hist = wsm.read_history(ws_id)
    state, head = wsm.get_state(ws_id)
    boot = {"id": ws_id, "name": meta["name"], "kind": meta["kind"], "archived": meta.get("archived", False),
            "locked": bool(meta.get("locked")),
            "token": TOKEN, "head": head, "fresh": not hist, "state": state, "workspaces": _ws_brief()}
    # The workspace client is bundled first in assets/js/app.js (src/web/ts/main.ts); it only needs BM_WS set.
    inject = "<script>window.BM_WS = " + embed_json(boot) + ";</script>\n"
    marker = "<script>\nconst DATA"
    if marker not in html:
        raise wsm.WsError("the page has no DATA script to attach the workspace to")
    return html.replace(marker, inject + marker, 1)


# ---------- dispatch ----------
def handle_get(h):
    """True when this module answered the request."""
    path, q = clean_path(h.path), parse_qs(h.path.split("?", 1)[1] if "?" in h.path else "")
    if path in ("/", "/index.html", "/preview.html") and os.environ.get("BM_NO_WS") != "1":
        if not host_ok(h):
            h._json(403, {"error": "forbidden"})
            return True
        try:
            ws_id = (q.get("ws") or [wsm.MAIN])[0]
            wsm.ensure_main()
            body = page_html(ws_id).encode("utf-8")
        except (wsm.WsError, FileNotFoundError) as e:
            if path != "/" and "ws" not in q:
                return False  # no workspace yet: plain file
            h._json(getattr(e, "code", 404), {"error": str(e)})
            return True
        h.send_response(200)
        h.send_header("Content-Type", "text/html; charset=utf-8")
        h.send_header("Content-Length", str(len(body)))
        h.end_headers()
        h.wfile.write(body)
        return True
    if not path.startswith("/api/"):
        return False
    if not host_ok(h) or not origin_ok(h):
        h._json(403, {"error": "forbidden"})
        return True
    try:
        if path == "/api/token":
            return h._json(200, {"token": TOKEN}) or True
        if path == "/api/workspaces":
            return h._json(200, {"workspaces": wsm.list_workspaces()}) or True
        if path == "/api/profiles":
            return h._json(200, {"profiles": bio.find_profiles()}) or True
        if path == "/api/trash":
            return h._json(200, {"trash": wsm.list_trash()}) or True
        if path == "/api/settings":
            return h._json(200, {"settings": wsm.read_settings(), "mcp": mcp_register.status(),
                                 "mcp_command": f"{mcp_register.PYTHON} {mcp_register.SERVER}"}) or True
        if path == "/api/backup":
            stamp = wsm.now_iso().replace(":", "").replace("+0000", "")[:15]
            return _send(h, backup.backup_bytes(), "application/zip", f"browser-foreman-backup-{stamp}.zip")
        m = WS_PATH.match(path)
        if not m:
            return False
        ws_id, rest = m.group(1), m.group(2) or ""
        wsm.existing_dir(ws_id)
        if rest == "":
            st, head = wsm.get_state(ws_id)
            return h._json(200, {**wsm.read_meta(ws_id), "head": head}) or True
        if rest == "state":
            st, head = wsm.get_state(ws_id)
            return h._json(200, {"state": st, "head": head}) or True
        if rest == "undo-state":  # what Undo and Redo would do now (labels or null), for enabling the buttons
            return h._json(200, wsm.undo_state(ws_id)) or True
        if rest == "history":
            hist = wsm.read_history(ws_id)
            before = int((q.get("before") or [0])[0] or 0)
            limit = max(1, min(int((q.get("limit") or [200])[0]), 2000))
            if before:
                hist = [e for e in hist if e["seq"] < before]
            return h._json(200, {"entries": list(reversed(hist))[:limit], "total": len(hist)}) or True
        if rest.startswith("history/"):
            st, e = wsm.state_at(ws_id, int(rest.split("/", 1)[1]))
            return h._json(200, {"state": st, "entry": e}) or True
        if rest == "export":
            fmt = (q.get("format") or ["html"])[0]
            text, _ = bio.export_netscape(ws_id) if fmt == "html" else bio.export_chrome(ws_id) if fmt == "chrome" else (None, 0)
            if text is None:
                raise wsm.WsError("format is html or chrome")
            name = f"{ws_id}-bookmarks.html" if fmt == "html" else f"{ws_id}-Bookmarks.json"
            return _send(h, text.encode("utf-8"), "text/html" if fmt == "html" else "application/json", name)
        if rest == "update":
            return h._json(200, bio.update_preview(ws_id)) or True
        if rest == "duplicates.csv":
            p = wsm.duplicates_path(ws_id)
            if not os.path.isfile(p):
                raise wsm.NotFound("no duplicates record for this workspace")
            with open(p, "rb") as f:
                return _send(h, f.read(), "text/csv", f"{ws_id}-duplicates.csv")
        return False
    except wsm.WsError as e:
        h._json(e.code, {"error": str(e)})
        return True
    except ValueError:
        h._json(400, {"error": "bad request"})
        return True


def _send(h, body, ctype, filename):
    h.send_response(200)
    h.send_header("Content-Type", ctype if ctype == "application/zip" else ctype + "; charset=utf-8")
    h.send_header("Content-Disposition", f'attachment; filename="{filename}"')
    # An export carries other people's titles and addresses: never let it run here, even if a browser renders it.
    h.send_header("Content-Security-Policy", "sandbox; default-src 'none'")
    h.send_header("Content-Length", str(len(body)))
    h.end_headers()
    h.wfile.write(body)
    return True


def post_allowed(h):
    """Every POST: addressed to localhost, from this origin, as JSON, carrying the run's token."""
    return (host_ok(h) and origin_ok(h) and token_ok(h)
            and h.headers.get("Content-Type", "").split(";")[0].strip() == "application/json")


def handle_post(h):
    path = clean_path(h.path)
    if not (path in ("/api/ws", "/api/trash/restore", "/api/trash/empty", "/api/restore", "/api/settings", "/api/mcp")
            or WS_PATH.match(path)):
        return False
    if not post_allowed(h):
        h._json(403, {"error": "forbidden"})
        return True
    try:
        data = _body(h)
        if path == "/api/settings":
            return h._json(200, {"settings": wsm.save_settings(data.get("settings"))}) or True
        if path == "/api/mcp":
            # Registers (or removes) the MCP server with Claude: edits a Claude config file, after backing it up.
            target, action = data.get("target"), data.get("action")
            if target not in mcp_register.TARGETS or action not in ("add", "remove"):
                raise wsm.WsError("target is folder, code or desktop; action is add or remove")
            res = mcp_register.add(target, bool(data.get("writes"))) if action == "add" else mcp_register.remove(target)
            return h._json(200, {"result": res, "mcp": mcp_register.status()}) or True
        if path == "/api/restore":
            # Replaces all of the tool's data; the page asks for the word typed out, and the body must carry it.
            if data.get("confirm") != "restore":
                raise wsm.WsError('restoring needs confirm: "restore"')
            try:
                raw = base64.b64decode(data.get("data") or "", validate=True)
            except ValueError:
                raise wsm.WsError("the backup must be sent as base64")
            return h._json(200, backup.restore(raw)) or True
        if path == "/api/trash/restore":
            return h._json(200, {"id": wsm.restore_workspace(str(data.get("trash_id")))}) or True
        if path == "/api/trash/empty":
            # Permanent: the page asks twice, and the body must say so in words.
            if data.get("confirm") != "empty the workspace trash":
                raise wsm.WsError('emptying needs confirm: "empty the workspace trash"')
            ids = data.get("trash_ids")
            return h._json(200, {"removed": wsm.empty_trash(None if ids is None else [str(i) for i in ids])}) or True
        if path == "/api/ws":
            ws_id = bio.import_sources(data.get("name") or "Imported bookmarks", _files(data),
                                       base_ws=data.get("base") or None, separate=bool(data.get("separate")))
            return h._json(200, {"id": ws_id, "workspace": wsm.read_meta(ws_id)}) or True
        ws_id, verb = WS_PATH.match(path).groups()
        wsm.existing_dir(ws_id)
        if verb == "sources":
            e = bio.add_sources(ws_id, _files(data), separate=bool(data.get("separate")))
            return h._json(200, {"entry": e}) or True
        if verb == "rename":
            return h._json(200, {"workspace": wsm.rename_workspace(ws_id, data.get("name"))}) or True
        if verb == "archive":
            return h._json(200, {"workspace": wsm.set_archived(ws_id, data.get("archived", True))}) or True
        if verb == "notes":
            return h._json(200, {"workspace": wsm.set_notes(ws_id, data.get("notes", ""))}) or True
        if verb == "commit":
            base = data.get("base")
            state = data.get("state")
            if state is None and isinstance(data.get("changes"), dict):
                # Only the parts that changed (tree, urls, refreshed), so a save fits a keepalive request (64 KB),
                # which is what lets the last edit survive the tab closing. The rest comes from the current head.
                state = {**wsm.get_state(ws_id)[0], **{k: v for k, v in data["changes"].items()
                                                       if k in ("tree", "urls", "refreshed")}}
            e = wsm.commit(ws_id, state, by="page", base_seq=None if base is None else int(base),
                           op="reset" if data.get("reset") is True else None)
            return h._json(200, {"entry": e, "head": e["seq"] if e else 0}) or True
        if verb == "migrate":
            e = wsm.migrate(ws_id, data.get("state"))
            return h._json(200, {"entry": e, "head": e["seq"]}) or True
        if verb in ("undo", "redo", "rollback"):
            e = wsm.undo(ws_id) if verb == "undo" else wsm.redo(ws_id) if verb == "redo" else wsm.rollback(ws_id, int(data["seq"]))
            st, head = wsm.get_state(ws_id)
            return h._json(200, {"entry": e, "head": head, "state": st, "can": wsm.undo_state(ws_id)}) or True
        if verb == "export":
            p, n = bio.write_export(ws_id, data.get("format", "html"))
            return h._json(200, {"path": p, "bookmarks": n}) or True
        if verb == "add-links":
            e = wsm.add_links(ws_id, data.get("folder"), data.get("links"))
            return h._json(200, {"entry": e, "head": e["seq"], "added": len(data.get("links") or [])}) or True
        if verb == "lock":
            return h._json(200, {"workspace": wsm.set_locked(ws_id, data.get("locked", True))}) or True
        if verb == "delete":
            if data.get("confirm") != ws_id:  # the caller repeats the id: a delete is never a stray click
                raise wsm.WsError("deleting needs confirm set to the workspace id")
            return h._json(200, {"trash_id": wsm.delete_workspace(ws_id)}) or True
        if verb == "reset":
            e = wsm.reset_workspace(ws_id)
            st, head = wsm.get_state(ws_id)
            return h._json(200, {"entry": e, "head": head, "state": st}) or True
        if verb == "update":
            e, pv = bio.update_apply(ws_id)
            st, head = wsm.get_state(ws_id)
            return h._json(200, {"entry": e, "preview": pv, "head": head, "state": st}) or True
        h._json(404, {"error": "not found"})
        return True
    except wsm.WsError as e:
        h._json(e.code, {"error": str(e)})
        return True
    except (ValueError, KeyError, TypeError):
        h._json(400, {"error": "bad request"})
        return True
