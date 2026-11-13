# Labels

Three things you add to a bookmark beyond its title and address: a rating, tags, and when you last opened it.

## Rating and tags

| Label | Values | Kept |
|---|---|---|
| `rating` | 1 to 5 stars; 0 or left out is not rated | on the bookmark, in the workspace and its history |
| `tags` | up to 20, each up to 40 characters, no commas; trimmed, once each ignoring case, sorted | on the bookmark, in the workspace and its history |

Both are set in a bookmark's Details (*Your rating*, *Tags*), with the keyboard (`1`–`5`, `0` clears, `T` goes to the
tags), or for many at once (select several, then *Actions › Label them*: rate, add a tag, remove a tag). Each change is
one history entry, worded for what it did: `Rated "Go" 4 stars; tagged work`.

Finding them again:

- **Search:** words also match tags; `#work` matches the tag exactly.
- **Sort:** *Highest rated*. **Group by:** *Rating* and *Tag* (a bookmark with several tags sits under its first).
- **Rows and cards** show the stars and up to four tags.

Through the API, an `update` change sets them:

```bash
curl -X POST http://127.0.0.1:8765/api/v1/workspaces/main/changes \
  -H "Authorization: Bearer $BROFO_API_TOKEN" -H 'Content-Type: application/json' \
  -d '{"ops": [{"op": "update", "id": "b-42", "rating": 4, "tags": ["work", "read later"]}]}'
```

`bookmark.updated` events carry the changed `rating` and `tags` (see [events](events.md)).

## Last opened

When you last opened each bookmark, and how many visits, comes from the browsers' own history: every profile this
computer lets Browser Foreman read, matched the way merging matches addresses (http and https, `www.` and a trailing
slash do not hide a visit). It is read when the page opens, through copies, and never stored.

| Route | Role | Answers |
|---|---|---|
| `GET /api/v1/workspaces/{id}/opened` | owner | `{opened: {url: {last, visits}}, read, skipped}`; `Cache-Control: no-store` |

The Details show it, Sort has *Recently opened* and Group by has *Last opened*. On macOS, reading browser history needs
Full Disk Access for the app that runs the server; until then the Details say the history cannot be read
([browser history](browser-history.md) explains granting it).

---
[← Docs index](../../README.md#documentation)
