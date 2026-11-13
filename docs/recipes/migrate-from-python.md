# Move from the Python tool

Bring everything the Python tool kept into Browser Foreman: every workspace with its whole history, link results,
tab snapshots and settings. The Python checkout is only read.

```bash
python3 legacy/tools/export_for_go.py --root ~/path/to/the/python/checkout --out ~/from-python.json.gz
browser-foreman restore ~/from-python.json.gz
```

```text
wrote …/from-python.json.gz: 1 workspace(s), 34 history entries, 29 trees, 6132 link results, 240 tab snapshots, settings
restored main
34 entries, 6132 newer link results, 240 tab snapshots, preferences
```

- **History.** Each saved step is rebuilt the way the web page rebuilt it, so every entry holds what the page showed
  then. Python's undo and redo steps arrive as `legacy-undo` and `legacy-redo`: every state is kept, and undo steps
  back one entry at a time.
- **Duplicates** the cleanup merged go into the Trash with the reason "duplicate of …", so they can be put back.
- **Private windows** in tab snapshots keep their pages only where the Python tool kept them.
- **Settings** apply only to a store whose preferences were never set; restoring never replaces anything, so a
  workspace whose id is taken comes back beside it.

Checked on a real store on 2026-11-13: all 34 entries held the same number of bookmarks in both tools (5,412 at the
newest), and the Trash the same 43 bookmarks, plus the 535 merged duplicates.

## Where the Python tool kept things

The Python implementation keeps everything in `storage/`, which is what the migration reads:

| Folder | Holds |
|---|---|
| `storage/workspaces/` | each bookmark set and its edit history |
| `storage/sessions/tabs.sqlite3` | open-tab snapshots, notes, link re-check results |
| `storage/thumbs/` | page snapshots |
| `storage/reports/`, `storage/exports/` | the build's reports and the cleaned import file |
| `storage/backups/` | backups made by Settings › Backup, and the original export |
| `storage/archive/` | old files moved aside (never deleted) |

---
[← Docs index](../../README.md#documentation)
