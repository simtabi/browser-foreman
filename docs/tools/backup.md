# Backup and restore

One file holds every workspace with its whole history and the link check results, so a move to another computer or a
bad day loses nothing.

## What a backup holds

| In it | Not in it |
|---|---|
| every workspace: its name, notes, lock and archive flag, and every history entry | people, password hashes, API tokens, sessions |
| every distinct bookmark tree, once (history entries point to them by hash) | webhook subscriptions and their secrets |
| every link check result | the configuration (`~/.config/browser-foreman`) |

Leaving accounts and secrets out means a backup can sit in cloud storage or be handed to someone without giving
anyone a way in. After restoring on a new computer, add people again with `browser-foreman user add` and
subscriptions with the API.

## Making one

```bash
browser-foreman backup ~/Backups/bookmarks-2026-11-12.json.gz
```

It never overwrites a file. From the web interface: the arrow beside *Download* › *Back up everything…*. Through the
API (owner role): `GET /api/v1/backup` answers the same gzipped JSON as a download.

## Restoring

```bash
browser-foreman restore ~/Backups/bookmarks-2026-11-12.json.gz
```

Restoring adds; it replaces nothing. A workspace whose id is already taken comes back beside it as
`<id>-restored-1` with its whole history, so you can compare the two and archive the one you do not want. A link
result in the backup is used only when it is newer than the one already known. From the web interface: *Restore from
a backup…*; through the API: `POST /api/v1/restore` with the file as the body (gzipped or plain JSON).

---
[← Docs index](../../README.md#documentation)
