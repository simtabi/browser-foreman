# REST API

Twenty endpoints today, all under `/api/v1`, all JSON. Phase 4 of the [roadmap](../roadmap.md) adds bookmarks,
folders, workspaces, history, tabs and settings.

## Rules for every request

- Send it to a local host (`127.0.0.1`, `::1` or `localhost`) in `local` mode; any other Host is refused with `403`.
- From a browser, the Origin must be this server's own; another site gets `403`.
- Everything except health, version and sign-in needs a caller: `Authorization: Bearer <token>`, or a signed-in
  session cookie. Missing or wrong: `401`. In `local` mode the token is `BROFO_API_TOKEN` and acts as the owner; with
  none configured the server answers `503`. In `accounts` mode only a person's own tokens and sessions count.
- A change made with a session cookie must also send that session's `X-CSRF-Token` (from sign-in or `/auth/me`), or it
  is refused with `403`. Token requests need no CSRF header.
- Each endpoint needs a role: viewer < editor < owner. Too low: `403`.
- Errors are `{"error": "message"}`.

## Endpoints

| Method | Path | Role | Answers |
|---|---|---|---|
| `GET` | `/api/v1/health` | none | `200 {"ok": true}` |
| `GET` | `/api/v1/version` | none | `200 {"version", "commit", "date"}` |
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
| `GET` | `/api/v1/workspaces/{id}` | viewer | the workspace, its last entry and its tree; `?at=N` for the tree after entry N |
| `GET` | `/api/v1/workspaces/{id}/history` | viewer | `200 {"history": [entries, oldest first]}` |

## Example

```bash
curl -s http://127.0.0.1:8765/api/v1/webhooks -H "Authorization: Bearer $BROFO_API_TOKEN"
```

```json
{"webhooks": [{"id": "wh_8f2c1a9e0b7d4c3a", "url": "https://example.com/brofo", "events": ["bookmark.*"], "created": "2026-11-12T18:00:00Z", "active": true}]}
```

---
[← Docs index](../../README.md#documentation)
