# REST API

72 endpoints, all under `/api/v1`, all JSON. `GET /api/v1/openapi.json` describes every one (OpenAPI 3.1, with the
role each needs in `x-role`); a test fails if a route and the document ever disagree.

## Rules for every request

- Send it to a local host (`127.0.0.1`, `::1` or `localhost`) in `local` mode; any other Host is refused with `403`.
- From a browser, the Origin must be this server's own; another site gets `403`.
- Everything except health, version and sign-in needs a caller: `Authorization: Bearer <token>`, or a signed-in
  session cookie. Missing or wrong: `401`. In `local` mode the token is `BROFO_API_TOKEN` and acts as the owner; with
  none configured the server answers `503`. In `accounts` mode only a person's own tokens and sessions count.
- A change made with a session cookie must also send that session's `X-CSRF-Token` (from sign-in or `/auth/me`), or it
  is refused with `403`. Token requests need no CSRF header.
- Each endpoint needs a role: viewer < editor < owner. Too low: `403`.
- A browser extension (`chrome-extension://`, `moz-extension://`, `safari-web-extension://` origins) may call in; it
  still needs a paired token, or a one-time code to pair.
- A request another site's page makes from a browser (`Sec-Fetch-Site: cross-site`, anything but following a link)
  is refused, even one that sends no `Origin`, such as an image.
- Every change to a workspace is one history entry made on the head you last saw. Send that head as
  `If-Match: "12"` (a workspace read's `ETag`); if someone changed it since, you get `412` and nothing is applied.
  Without `If-Match` the change applies to the current head.
- Errors are `{"error": "message"}`.

## Endpoints

| Method | Path | Role | Answers |
|---|---|---|---|
| `GET` | `/api/v1/health` | none | `200 {"ok": true}` |
| `GET` | `/api/v1/version` | none | `200 {"version", "commit", "date"}` |
| `GET` | `/api/v1/openapi.json` | none | this API as OpenAPI 3.1 |
| `POST` | `/api/v1/auth/signin` | none | body `{username, password}`; `200 {"user", "csrf"}` and the `brofo_session` cookie; `401`, or `429` with `Retry-After` after 5 failures in 15 minutes |
| `POST` | `/api/v1/auth/signout` | none | `204`; ends the session |
| `GET` | `/api/v1/auth/me` | viewer | `200 {"user": {name, role, via}}`, plus `csrf` for a session |
| `GET` | `/api/v1/events` | viewer | Server-Sent Events; see [events](events.md) |
| `GET` | `/api/v1/tokens` | viewer | your tokens; an owner adds `?all=1` for everyone's. Secrets are never returned |
| `POST` | `/api/v1/tokens` | viewer | body `{name}`; `201 {"token", "secret"}`; the secret is shown this once |
| `DELETE` | `/api/v1/tokens/{id}` | viewer | `204`; your own token, or any for an owner; otherwise `404` |
| `GET` | `/api/v1/users` | owner | `200 {"users": [{name, role, disabled, created}]}` |
| `POST` | `/api/v1/users` | owner | body `{name, role, password}`; `201`; passwords need 12 characters |
| `PATCH` | `/api/v1/users/{name}` | owner | body any of `{role, password, disabled}`; `409` if it would leave no owner who can sign in |
| `DELETE` | `/api/v1/users/{name}` | owner | `204`, with that person's tokens and sessions; `409` for the last owner |
| `GET` | `/api/v1/webhooks` | owner | `200 {"webhooks": [{id, url, events, created, active}]}`; secrets are never returned |
| `POST` | `/api/v1/webhooks` | owner | body `{url, events, secret}`; `201` with the subscription, `400` with the reason |
| `DELETE` | `/api/v1/webhooks/{id}` | owner | `204`, or `404` |
| `POST` | `/api/v1/webhooks/{id}/test` | owner | sends a `webhook.test` event now; `200` with the delivery record |
| `GET` | `/api/v1/workspaces` | viewer | `200 {"workspaces": [{id, name, created, head, bookmarks, folders}]}` |
| `GET` | `/api/v1/workspaces/{id}` | viewer | the workspace, its last entry, its tree and `can` (what undo and redo would act on); `?at=N` for the tree after entry N |
| `GET` | `/api/v1/workspaces/{id}/history` | viewer | `200 {"history": [entries, oldest first]}` |
| `POST` | `/api/v1/workspaces` | editor | body `{id, name}`; `201`, or `409` if the id is taken |
| `POST` | `/api/v1/workspaces/{id}/import` | editor | the body is a bookmarks file (Chrome JSON or exported HTML, up to 64 MiB); replaces the tree as one entry; `?name=` labels it |
| `POST` | `/api/v1/workspaces/{id}/sources` | editor | merges files (the body, or multipart `file` parts, up to 20) and `?from=` another workspace into this one, as one entry; copies of a page go to the Trash as duplicates (a copy the Trash already holds as a duplicate of the same kept bookmark is not added again); `?separate=1` keeps each source's folders apart; answers `added`, `duplicates`, `already_in_trash`, `folders` |
| `GET` | `/api/v1/workspaces/{id}/duplicates` | viewer | bookmarks that lead to the same page, in groups, each naming the one to keep ([Workspaces](workspaces.md#finding-duplicates)) |
| `GET` | `/api/v1/workspaces/{id}/duplicates.csv` | viewer | every copy merged away, with the bookmark kept for it |
| `POST` | `/api/v1/workspaces/{id}/sort` | editor | file the bookmarks into folders by `sort-rules.json`: the plan, or `{"apply": true}` to make the moves ([Sorting](sorting.md)) |
| `GET` | `/api/v1/profiles` | owner | every browser profile's bookmarks file on this computer, with `readable` and `why` |
| `GET` | `/api/v1/workspaces/{id}/sources` | viewer | where the workspace's bookmarks came from: files (a copy is kept), other workspaces, files on this computer |
| `GET` | `/api/v1/workspaces/{id}/update` | viewer | what Update would do: `plan.new`, `plan.retitle` (titles changed upstream that you have not renamed here), `plan.gone` (no longer upstream; they stay), and `missing` sources |
| `POST` | `/api/v1/workspaces/{id}/update` | editor | reads the sources again and brings new bookmarks and titles in, as one entry; nothing is removed |
| `POST` | `/api/v1/workspaces/{id}/save-links` | editor | `{folder, parent, links: [{url, title}]}`: saves up to 2000 http(s) links as a new folder inside `parent` ("Saved tabs" when left out), as one entry |
| `POST` | `/api/v1/workspaces/{id}/reset` | editor | back to how it started (its first import or merge), as one entry undo takes back |
| `POST` | `/api/v1/workspaces/{id}/delete` | owner | `{"confirm": "<id>"}`: to the workspace Trash, history and all; not a locked one, nor the last in use. `GET /api/v1/workspaces?trash=1` lists the Trash |
| `POST` | `/api/v1/workspaces/{id}/restore` | owner | back from the workspace Trash |
| `POST` | `/api/v1/workspaces/{id}/purge` | owner | `{"confirm": "<id>"}`: removes a workspace in the Trash for good |
| `GET` | `/api/v1/workspaces/{id}/export` | viewer | a download: `?format=html` (default, every browser imports it) or `chrome`; `?at=N` for an earlier state |
| `POST` | `/api/v1/workspaces/{id}/changes` | editor | body `{base?, ops: [...]}`: several changes as one entry, all or none |
| `PUT` | `/api/v1/workspaces/{id}/tree` | editor | body `{root, trash?}`: the whole tree at once (the web interface saves this way); the server works out what changed, checks it by the same rules and publishes those events; an unchanged tree records nothing (`200`) |
| `POST` | `/api/v1/workspaces/{id}/nodes` | editor | body `{parent, index?, kind, title, url?, note?}`: add a bookmark or folder |
| `PATCH` | `/api/v1/workspaces/{id}/nodes/{node}` | editor | body any of `{title, url, note}` |
| `POST` | `/api/v1/workspaces/{id}/move` | editor | body `{ids, parent, index?}`; moved in the order given |
| `GET` | `/api/v1/workspaces/{id}/trash` | viewer | `200 {"trash": [{node, from, index, at, reason}]}` |
| `POST` | `/api/v1/workspaces/{id}/trash` | editor | body `{ids, reason?}` |
| `POST` | `/api/v1/workspaces/{id}/trash/restore` | editor | body `{ids}`; each goes back where it was (to the end of the first top folder if that folder is gone) |
| `DELETE` | `/api/v1/workspaces/{id}/trash` | editor | `?ids=a,b`, or everything; history still holds them, so undo brings them back |
| `POST` | `/api/v1/workspaces/{id}/undo` | editor | takes back the last change as a new entry; `409` when there is none |
| `POST` | `/api/v1/workspaces/{id}/redo` | editor | puts back the last undone change |
| `GET` | `/api/v1/workspaces/{id}/links` | viewer | `200 {"links": {url: result}}`: what the last checks found for this workspace's links |
| `POST` | `/api/v1/linkchecks` | editor | body `{workspace, nodes?}`: check the links (under `nodes`, or all) in the background; `202 {"job"}`, `409` while another runs. See [link checks](link-checks.md) |
| `GET` | `/api/v1/linkchecks/{id}` | viewer | progress; `?results=1` adds every result so far |
| `POST` | `/api/v1/linkchecks/{id}/pause`, `/resume`, `/cancel` | editor | `200`, or `409` when the check is not in a state to do that |
| `PATCH` | `/api/v1/workspaces/{id}` | editor | body any of `{name, notes, locked, archived, color, icon}` (`color`: blue, azure, indigo, purple, pink, red, orange, yellow, lime, green, teal or cyan; `icon`: a Tabler icon name; `""` resets either); a locked workspace accepts only `{"locked": false}` and its colour and icon (`423` otherwise) |
| `GET` | `/api/v1/webhooks/deliveries` | owner | the latest deliveries, newest first; `?subscription=`, `?limit=` (1–1000) |
| `GET` | `/api/v1/backup` | owner | every workspace with its history, and the link results, as gzipped JSON; see [backup](backup.md) |
| `POST` | `/api/v1/restore` | owner | the backup file as the body; adds, replaces nothing; `200 {"workspaces": {old: new}, "entries", "links"}` |
| `GET` | `/api/v1/browsers`, `/api/v1/browsers/icon?name=` | viewer, none | installed browsers and the default; a browser's icon (public, so an `<img>` can load it; other sites cannot) |
| `POST` | `/api/v1/open` | owner | open http(s) links in browsers; see [browsers and tabs](browsers-and-tabs.md) |
| `GET`, `PUT` | `/api/v1/preferences` | viewer, owner | browsers, tab snapshots, re-checks, page snapshots (`snapshot_width`, `snapshot_height`, `snapshot_wait_seconds`, `snapshot_dir`; see [page snapshots](page-snapshots.md)) and workspace pools (`tabs_own`, `active_workspace`) |
| `GET` | `/api/v1/workspaces/{id}/opened` | owner | when each bookmark was last opened and how often, from the browsers' own history; `no-store`, never kept. See [labels](labels.md) |
| `POST` | `/api/v1/workspaces/{id}/open` | editor | records the workspace in use, so background tab snapshots go into its pool; see [workspaces](workspaces.md) |
| `PUT` | `/api/v1/workspaces/{id}/tabs` | owner | body `{"own": true}` gives the workspace its own tab snapshots, `false` returns it to the shared ones; see [workspaces](workspaces.md) |
| `GET` | `/api/v1/tabs` and the `/api/v1/tabs/…` routes | viewer to owner | open tabs, snapshots, timeline (`?days=` or `?from=`/`?to=`), `tabs/stored` counts, restore, close; `?workspace=` picks the workspace's pool; see [browsers and tabs](browsers-and-tabs.md) |
| `GET` | `/api/v1/linkchecks` | viewer | the running check and earlier ones, with links newly broken and fixed |
| `POST`, `GET` | `/api/v1/extension/…` | see [browser extension](browser-extension.md) | pairing, tabs and groups, bookmarks, commands |
| `GET` | `/api/v1/settings` | owner | the configuration in force, without secrets; read-only |
| `POST` | `/api/v1/workspaces/{id}/rollback` | editor | body `{to}`: the state after entry `to` becomes the newest, as an entry you can undo |

Every change answers `201 {"entry", "events"}` with the new head as its `ETag`.

## Changes

`/changes` takes ops, applied in order to a copy of the tree; one refused op refuses them all (`400`, naming it).

| `op` | Fields |
|---|---|
| `create` | `parent` (folder id; `""` is the root), `index` (end when left out), `kind` (`bookmark` or `folder`), `title`, `url`, `note` |
| `update` | `id`, and any of `title`, `url`, `note`, `rating` (0 to 5; 0 clears), `tags` (up to 20, 40 characters each, no commas; `[]` clears) |
| `move` | `ids`, `parent`, `index` |
| `trash` | `ids`, `reason` (top folders such as Bookmarks Bar cannot go) |
| `restore` | `ids` |
| `purge` | `ids`, or none for the whole Trash |

URLs need a scheme; `javascript:`, `data:`, `vbscript:` and `blob:` are refused because they run code when opened.
Titles and notes are at most 2,000 characters.

## Example

```bash
curl -s http://127.0.0.1:8765/api/v1/webhooks -H "Authorization: Bearer $BROFO_API_TOKEN"
```

```json
{"webhooks": [{"id": "wh_8f2c1a9e0b7d4c3a", "url": "https://example.com/brofo", "events": ["bookmark.*"], "created": "2026-11-12T18:00:00Z", "active": true}]}
```

---
[← Docs index](../../README.md#documentation)
