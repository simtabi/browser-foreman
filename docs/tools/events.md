# Events

Every change Browser Foreman makes is published as an event, streamed live at `/api/v1/events` and delivered to
[webhooks](webhooks.md).

## An event

```json
{"id": 42, "type": "webhook.created", "at": "2026-11-12T18:00:00Z", "data": {"id": "wh_8f2c1a9e0b7d4c3a", "url": "https://example.com/brofo"}}
```

`id` increases by one per event for the life of the server process.

## The stream

`GET /api/v1/events` needs a viewer (a token or a signed-in session) and answers `text/event-stream`. Each event is one message:

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
| `auth.signed_in` | someone signed in (`{"user"}`) |
| `user.created` | a person was added (`{"user", "role"}`) |
| `user.deleted` | a person was removed (`{"user"}`) |
| `bookmark.created`, `folder.created` | added (`{workspace, seq, id, parent, title, url}`) |
| `bookmark.updated`, `folder.updated` | title, URL or note changed (the changed fields) |
| `bookmark.moved`, `folder.moved` | `{from, to, index}` |
| `bookmark.trashed`, `folder.trashed` | `{from, reason}` |
| `bookmark.restored`, `folder.restored` | `{to}` |
| `bookmark.deleted`, `folder.deleted` | deleted from the Trash for good |
| `trash.emptied` | `{count}` |
| `workspace.created` | `{workspace, name}` |
| `workspace.deleted` | `{workspace, name}`: moved to the workspace Trash |
| `workspace.restored` | `{workspace, name}` |
| `workspace.purged` | `{workspace, name}`: removed for good |
| `workspace.committed` | after any change above: `{workspace, seq, op, summary}` |
| `workspace.imported`, `workspace.undone`, `workspace.redone`, `workspace.rolled_back` | the same fields |
| `workspace.exported` | `{workspace, format, at}` |
| `workspace.updated` | renamed, notes changed, locked or archived: `{workspace, name, locked, archived}` |
| `backup.created`, `backup.restored` | counts; a restore names each workspace's new id |

| `linkcheck.started` | `{id, workspace, total}` |
| `link.checked` | an address's status changed: `{url, status, previous, final, note}` |
| `linkcheck.finished` | `{id, workspace, state, checked, total, changed, dead}` |

| `tabs.snapshot.saved` | `{id, reason, windows, tabs, incognito_windows, incognito_tabs}`; never pages |
| `tabs.closed` | `{closed, skipped, undo_snapshot}` |
| `tabs.restored` | `{id, windows}` |
| `tabs.noted` | `{kind, key, removed}`; never the note itself |
| `tabs.deleted` | `{deleted}` |
| `tabs.purged` | `{level, snapshots, windows}` |
| `links.opened` | `{count, how, browsers, ok}`; never the links |
| `preferences.updated` | `{open_with, autosave}` |
| `extension.paired` | `{browser, token}` (the token's id, never the secret) |
| `linkcheck.scheduled` | a scheduled re-check started: `{workspace, due}` |

---
[← Docs index](../../README.md#documentation)
