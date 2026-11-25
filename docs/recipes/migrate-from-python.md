# Move from the Python tool

Bring everything the Python tool kept into Browser Foreman: every workspace with its whole history, link results,
tab snapshots and settings. The Python checkout is only read.

The exporter is one Python file with no dependencies. It shipped with the Python implementation, which left this
repository before 0.1.0; take it from the last commit that has it:

```bash
curl -fsSLO https://raw.githubusercontent.com/simtabi/browser-foreman/03a2fd4/legacy/tools/export_for_go.py
python3 export_for_go.py --root ~/path/to/the/python/checkout --out ~/from-python.json.gz
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
- **Page details.** The Python renderer kept each page's title, description and final address in
  `storage/thumbs/_meta.jsonl`. Add them to the link results that have none (a result is never made, a status never
  changed, and a value already there is kept):

  ```bash
  browser-foreman page-details ~/path/to/the/python/checkout/storage/thumbs/_meta.jsonl
  ```

  Its `_failed.json` lists the pages the renderer could not draw; it says nothing about the links, so it is not
  brought over: *Refresh* takes those snapshots again.

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
