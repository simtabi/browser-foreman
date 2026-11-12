# Events

Every change Browser Foreman makes is published as an event, streamed live at `/api/v1/events` and delivered to
[webhooks](webhooks.md).

## An event

```json
{"id": 42, "type": "webhook.created", "at": "2026-11-12T18:00:00Z", "data": {"id": "wh_8f2c1a9e0b7d4c3a", "url": "https://example.com/brofo"}}
```

`id` increases by one per event for the life of the server process.

## The stream

`GET /api/v1/events` answers `text/event-stream`. Each event is one message:

```text
id: 42
event: webhook.created
data: {"id":42,"type":"webhook.created","at":"…","data":{…}}
```

A comment line (`: ping`) every 15 seconds keeps the connection open through proxies. Reconnect with
`Last-Event-ID: <last id you saw>` to receive what you missed; the server keeps the last 1,000 events. If some fell out
of that buffer, the stream starts with `: some events were missed; reload the state`.

## Types

| Type | When |
|---|---|
| `server.started` | the server began listening |
| `webhook.created` | a subscription was added |
| `webhook.deleted` | a subscription was removed |
| `webhook.test` | sent by the test endpoint, to one subscription only |

Phase 4 adds a type for every change: `bookmark.created`, `bookmark.updated`, `bookmark.moved`, `bookmark.trashed`,
`bookmark.restored`, `bookmark.deleted`, `folder.*`, `workspace.committed`, `workspace.undone`, `trash.emptied`,
`link.checked`, `tabs.snapshot.saved`, `tabs.closed`.

---
[← Docs index](../../README.md#documentation)
