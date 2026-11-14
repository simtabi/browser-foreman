# Workspaces

A workspace is one bookmark set with its full history; it scopes every view: Bookmarks, Open tabs and the Timeline.

## What a workspace holds

| Part | Notes |
|---|---|
| Bookmarks | the tree, the Trash, your labels (rating, tags) and descriptions |
| History | every change as an entry; *Go back here* makes an older state the newest, as a new entry you can undo |
| Sources | where its bookmarks came from (files, other workspaces, browser profiles), so *Update from source* can read them again |
| Name and notes | renamed and noted from the top bar's workspace menu, the ⋮ menu, or *All workspaces…* |
| Colour and icon | *Appearance…* (the workspace menu, or a row in *All workspaces…*): one of 12 colours and an icon, shown on its badge in the switcher, the menu and the list; the top bar takes the colour of the workspace you are in. They can be set on a locked workspace too |
| State | locked (read-only), archived (out of the list, kept), or in the workspace Trash (restorable until deleted for good) |
| Tab snapshots | shared with every other workspace (the default), or its own; see below |

## Switching and managing

The workspace switcher sits in the top bar beside the logo, in every view. *All workspaces…* (in that menu and in the
⋮ menu) lists them with their bookmark count, changes and status, and opens, renames, notes, archives, locks, deletes
and restores each one. `?ws=<id>` in the address opens a workspace directly.

## Finding duplicates

A merge keeps one copy of each page it brings in. *Find duplicates…*, in the workspace menu and the command palette,
finds copies already in a workspace, by four rules from surest to weakest:

| Rule | Two bookmarks match when |
|---|---|
| Same page | their addresses agree once tracking parameters (`utm_…`, `fbclid`, `ref`…), `www.` and a trailing slash are set aside |
| Redirects to the same page | a link check followed one to the other's address, or both to the same place |
| Same canonical page | both pages name the same address as their own (`<link rel="canonical">`, read by link checks) |
| Same page, other query | the same site and path with another query or fragment, and the same title |

Each group names the bookmark to keep: the one in the most specific folder, then the earliest added. You can pick
another. The groups you tick send their other copies to the Trash as duplicates of the kept one, in one history entry,
so they join *Duplicates* and *Put back* or *Undo* brings them back. Groups found only by the last rule start
unticked, because a search page and its results can share a title.

The same list is `GET /api/v1/workspaces/{id}/duplicates` (viewer). It changes nothing. To act on it, send a `trash`
change with `"reason": "duplicate of <kept id>"` to `POST /api/v1/workspaces/{id}/changes`.

## Tab snapshots: own or shared

Open-tab snapshots live in **pools**. By default every workspace uses the one **shared** pool. A workspace set to
keep its **own** tab snapshots (*All workspaces…* › its ⋯ menu › *Keep its own tab snapshots*) has a pool of its own:

- Open tabs, the Timeline and the snapshot counts show only that workspace's pool.
- New snapshots, automatic or *Save now*, go into the pool of the workspace you last opened.
- Switching a workspace to its own starts it empty; snapshots already taken stay in the pool they were saved in.
  Switching back uses the shared pool again; its own snapshots are kept.

| Route | Role | Does |
|---|---|---|
| `PUT /api/v1/workspaces/{id}/tabs` | owner | `{"own": true}` or `{"own": false}` |
| `POST /api/v1/workspaces/{id}/open` | editor | records the workspace in use (the page sends it as it loads) |
| `GET /api/v1/tabs/snapshots?workspace=` | viewer | that workspace's pool; without `?workspace=`, the workspace last opened |

The choices are kept as the `tabs_own` and `active_workspace` preferences, and travel with a [backup](backup.md).

## From the command line

```bash
browser-foreman workspace list
```

```bash
browser-foreman import ~/Downloads/bookmarks.html --workspace work
```

---
[← Docs index](../../README.md#documentation)
