# Storage

Three drivers keep the same data: workspaces (a bookmark tree with every change in its history) and tab snapshots.
Pick one with `storage.driver`; move between them with `browser-foreman storage migrate`.

| Driver | Keeps | Good for |
|---|---|---|
| `sqlite` (default) | one file, `browser-foreman.sqlite3`, in WAL mode; each distinct tree is kept once, gzipped | everyday use; fast with large histories |
| `json` | plain files under `json/` | reading, diffing, syncing or backing up with ordinary tools |
| `postgres` | the tables `sqlite` keeps, in the Postgres database `BROFO_STORAGE_DSN` names | a database you already run and back up |

`sqlite` and `json` live in `storage.path` (default `~/.local/state/browser-foreman`), private to you: folders `0700`,
files `0600`. All three pass one shared test suite, so they behave the same.

## Postgres

Set `storage.driver` to `postgres` and put the connection string in `.env` (it can hold a password, so it is read
only from `.env` or the environment, never from a JSON file):

```bash
# in ~/.config/browser-foreman/.env
BROFO_STORAGE_DSN=postgres://brofo@db.example:5432/browser_foreman?sslmode=require
```

The tables are made on first start. A commit locks its workspace's row, so two servers sharing the database cannot
both build on the same head: the second is refused as a conflict, as on the other drivers. The `search_path` in the
string picks a schema, for a database shared with other things. To run the driver's own tests, `make testdb-up` starts
a disposable Postgres and prints the `BROFO_TEST_POSTGRES_DSN` to set; CI runs them against the same image.

## History

Every change is an entry: what changed, when, and the whole tree afterwards. Trees are stored once per distinct
content (keyed by SHA-256), so a long history of small changes stays small, and any past state reads back directly.

A change is made on the state it saw (`base`); if the workspace moved on meanwhile, it is refused rather than
silently overwriting the newer change.

**Undo and redo are entries too.** Undo records the state before the last change; redo puts back what undo took back,
until a new change is made. Nothing is rewritten, so an undo can itself be undone, and the history shows both.

```bash
browser-foreman workspace history main
```

```text
#1    2026-11-12 13:30  import  Imported 5486 bookmarks in 174 folders from bookmarks.html (netscape-html)
#2    2026-11-12 13:41  edit    Moved 12 bookmarks to Design/Fonts
#3    2026-11-12 13:42  undo    Undid: Moved 12 bookmarks to Design/Fonts
```

```bash
browser-foreman workspace undo main
```

```bash
browser-foreman workspace redo main
```

The import that created a workspace cannot be undone.

## The json layout

```text
json/workspaces/<id>/workspace.json   name and creation time
json/workspaces/<id>/history.jsonl    one entry per line
json/snapshots/<sha256>.json          each distinct tree, once
json/tabs/<id>.json                   one tab snapshot per file
```

Every file is written under a temporary name and renamed, so a crash never leaves half a file.

## Moving between drivers

```bash
browser-foreman storage migrate --to json
browser-foreman storage migrate --to postgres   # with BROFO_STORAGE_DSN set
```

Every workspace is copied with its whole history (times, undo and redo included) and every tab snapshot; the
source is left untouched. Then set `storage.driver` (or `BROFO_STORAGE_DRIVER`) to the new driver. A workspace that
already exists in the destination stops the copy.

---
[← Docs index](../../README.md#documentation)
