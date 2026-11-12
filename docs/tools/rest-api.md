# REST API

42 endpoints, all under `/api/v1`, all JSON. `GET /api/v1/openapi.json` describes every one (OpenAPI 3.1, with the
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
| `POST` | `/api/v1/workspaces/{id}/rollback` | editor | body `{to}`: the state after entry `to` becomes the newest, as an entry you can undo |

Every change answers `201 {"entry", "events"}` with the new head as its `ETag`.

## Changes

`/changes` takes ops, applied in order to a copy of the tree; one refused op refuses them all (`400`, naming it).

| `op` | Fields |
|---|---|
| `create` | `parent` (folder id; `""` is the root), `index` (end when left out), `kind` (`bookmark` or `folder`), `title`, `url`, `note` |
| `update` | `id`, and any of `title`, `url`, `note` |
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
