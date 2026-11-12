# REST API

Seven endpoints today, all under `/api/v1`, all JSON. Phase 4 of the [roadmap](../roadmap.md) adds bookmarks,
folders, workspaces, history, tabs and settings.

## Rules for every request

- Send it to a local host (`127.0.0.1`, `::1` or `localhost`) in `local` mode; any other Host is refused with `403`.
- From a browser, the Origin must be this server's own; another site gets `403`.
- Writes need `Authorization: Bearer <token>` (`BROFO_API_TOKEN`). Missing or wrong: `401`. No token configured on
  the server: `503`.
- Errors are `{"error": "message"}`.

## Endpoints

| Method | Path | Token | Answers |
|---|---|---|---|
| `GET` | `/api/v1/health` | no | `200 {"ok": true}` |
| `GET` | `/api/v1/version` | no | `200 {"version", "commit", "date"}` |
| `GET` | `/api/v1/events` | no | Server-Sent Events; see [events](events.md) |
| `GET` | `/api/v1/webhooks` | yes | `200 {"webhooks": [{id, url, events, created, active}]}`; secrets are never returned |
| `POST` | `/api/v1/webhooks` | yes | body `{url, events, secret}`; `201` with the subscription, `400` with the reason |
| `DELETE` | `/api/v1/webhooks/{id}` | yes | `204`, or `404` |
| `POST` | `/api/v1/webhooks/{id}/test` | yes | sends a `webhook.test` event now; `200` with the delivery record |

## Example

```bash
curl -s http://127.0.0.1:8765/api/v1/webhooks -H "Authorization: Bearer $BROFO_API_TOKEN"
```

```json
{"webhooks": [{"id": "wh_8f2c1a9e0b7d4c3a", "url": "https://example.com/brofo", "events": ["bookmark.*"], "created": "2026-11-12T18:00:00Z", "active": true}]}
```

---
[← Docs index](../../README.md#documentation)
