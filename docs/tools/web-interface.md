# Web interface

The bookmark tree, Open tabs, Timeline, the Trash and every menu. The binary embeds the interface (`make build` runs
`make web` first) and serves it at `/`.

## Start the server

```bash
browser-foreman serve
```

Open <http://localhost:8765/>. In `local` mode the page opens straight away; in `accounts` mode it asks you to sign in
first. The first visit shows a welcome and a short tour; Help › Tour replays it. `?ws=<id>` opens another workspace.

## What the binary serves today

The Bookmarks view works in full against the server, link health included ([link checks](link-checks.md)), with the
workspace menu (switch, rename, notes, lock, archive, History with *Go back to this*), import into a new workspace,
export, and backup and restore ([backup](backup.md)). A locked workspace opens read-only: the tree, search and filters, editing titles, addresses and
notes, new folders, drag and drop, the Trash (put back, delete permanently), Undo and Redo, and Download. Every edit is
saved as one history entry through `PUT /api/v1/workspaces/{id}/tree`, and the server publishes what changed as events.
If the bookmarks changed somewhere else since the page loaded, the save is refused and the page offers to reload, so
nothing is overwritten.

Still to come, so their parts of the page stay empty or say they are not available yet:

| Part | Arrives in |
|---|---|

Open tabs, the Timeline and the Browsers menu work through the server too ([browsers and tabs](browsers-and-tabs.md)).
Notes on tabs, deleting snapshots and purging private pages answer that they are not available yet.

The sections below describe the whole interface, as the Python implementation in `legacy/` serves it.

## The layout

One header holds everything that applies everywhere: the three views (with counts), one search box that searches
the view you are in (each view keeps its own query; press `/` to jump to it), Commands (`⌘K`), the Browsers menu
(which browsers' tabs to show, and *Browsers and access…*), Undo and Redo for the view you are in, the theme toggle
(a moon in light mode, a sun in dark), and the ⋮ menu (settings, help).
Each view then has its own bar: Bookmarks shows the workspace, *Update moved*, *Refresh all* and *Download*, and the
Show chips with a count on each. The footer shows whether the server is running and when tabs were last saved.

## The three views

| View | What it shows |
|---|---|
| Bookmarks | your folders with the cleanup merged in; link health, snapshots, details, edits with full history |
| Open tabs | every window per browser, normal and incognito, live or as saved; reopen what a crash closed |
| Timeline | everything that happened, by day: tab snapshots, bookmark edits, link checks |

## It remembers where you were

The address carries the view, filter, search, selected folder or bookmark and tab snapshot, for example
`#/tabs?snapshot=2026-11-12_084652`. Refresh, Back and Forward land in the same place, and opening the bare address
later resumes where you left off. Turn it off in Settings › Startup › *Remember where I was*; a factory reset forgets it.

## Working with open tabs

- **Pages:** each window lists 25 tabs per page, the Timeline 25 events, and a folder's card overview 120 cards. A size picked in one list's footer applies to that list only; Settings ›
  Rows per page sets the default for the rest.
- **Incognito ⇄ normal:** a window's ⋯ menu, a tab's right-click menu and the selection bar offer *Move to a normal
  window* or *Move to an incognito window*. The tabs open in the other mode in the same browser, then close where
  they were; a snapshot is saved first. Saved snapshots can only be opened, not moved.
- **Tabs ⇄ bookmarks:** *Move to bookmarks* saves tabs as a bookmark folder and closes them; a bookmark folder's
  *Move to tabs* opens its links (normal or incognito) and puts them in the Trash, where Undo brings them back.
- **All, some or no browsers:** the header's Browsers menu (*Show tabs and history from*) filters everything that
  belongs to a browser, and updates it as you tick: Open tabs and its counts, the Snapshots list, the Timeline's tab
  snapshots and chart, Clean up and Restore. With none ticked, those views say so and offer *Show all browsers*. The Browsers dialog lists, for each browser,
  whether its tabs are tracked, whether it opens links, and whether its bookmarks and history can be read; *Import
  bookmarks…* brings in the ticked browsers' bookmark files, and *Allow reading (Full Disk Access)…* explains the
  macOS setting both need.
- **Bookmarks → tabs:** the Open menu of a bookmark or folder also opens the current window, one window per folder,
  or moves them to tabs (then to the Trash) in any of those ways.
- **Tabs → a folder of your choice:** *Save as a bookmark folder* and *Move to bookmarks* ask where: a new folder in
  "Saved tabs", or a new folder inside any of yours. A whole window can be moved too (its ⋯ menu).
- **Which browser opens links:** the system's default browser (System Settings › Desktop & Dock › Default web
  browser), unless you choose otherwise in Browsers. *Use the system default browser* follows whatever macOS uses.
- **Several browsers at once:** in Browsers, tick *opens links* on as many browsers as you like (or *every browser
  at once*); links then open in all of them together. Each browser that is not running has to start first.
- **Just this once:** *Open in* (right-click, the details panel's Open menu, or the command palette) lists the
  system default and every browser on this Mac; the links open as new tabs there, and your usual choice stays.

## Undo and redo, where it happened

Each view has its own Undo and Redo (the arrows in the top bar, ⌘Z and ⇧⌘Z). In Bookmarks they walk the
workspace history: every undo and redo is a new history entry, so nothing is lost. In Open tabs they reopen what you
closed or moved, and close it again. The Timeline has nothing to undo. A button is enabled only when there is
something to undo or redo there, and its tooltip says what.

## Staying current

While the page is open it reads your browsers again every 5 seconds (Settings › Open tabs › *Refresh from the
browsers every*, from 2 seconds to off) and updates the open-tabs count and list when anything changed. Edits made
in another window or through Claude appear on their own too.

## Right-click menus

The menu fits what you clicked: a bookmark, a folder, a browser root such as Bookmarks Bar, the Trash, an item in the
Trash, several selected items, or empty space. The common actions sit at the top level (open, live page, copy, move,
rename, trash, undo); the rest are grouped in submenus: *Open in…* (tabs, a new window, one window per folder,
incognito, and *Another browser* with the system default and each browser on this Mac), *Move to tabs, then Trash*,
*Organise* and *View*. A group appears only when something in it applies here; a group with a single action shows
that action directly. Keyboard: ↑ ↓ move, → opens a submenu, ← or Esc goes back, Esc closes, Enter runs.

## Moving things by dragging

Drag any bookmark or folder (or several selected ones) anywhere in the Bookmarks tree. Over a folder, its top
quarter drops above it, the middle drops into it, and the bottom quarter drops below it (when it is closed). Over a
bookmark, the top half drops above it and the bottom half below it. To move something out of a folder, drop it
below the folder's last row with the pointer at that row's left edge: the line extends to the left and the items
land after the folder, one level up. A blue line or outline shows where the drop will land. Every drop is one history
entry, so Undo puts it back.

## The Trash

Everything you trash stays in the workspace and is left out of the import file. The **Trash** button in the
Bookmarks bar (its badge counts the bookmarks inside) opens one list of everything in it, with where each item came
from. Each row says why it is there (a duplicate of a bookmark you still have, a dead link, a redirect to another
site, one of the review groups such as Google searches, or put there by you), and the counts across the top filter
the list to one reason. Duplicates are shown as a small tree: the copy you kept, with the trashed copies under it.
Each link has a visit button (it opens the page in a new tab and leaves it in the Trash), so you can check
a link before deciding. Filter it, tick items, then **Put back** (each returns to the folder it came from, or to that folder's new home
if it has moved since) or **Delete permanently**; **Put back all** and **Empty Trash…** act on everything. Every
action is one history entry, so Undo or History can still bring it back. *Open the Trash* is also in the command
palette.

## Menus

- **⋮ (top right):** View (expand or collapse folders in Bookmarks), Help (help, shortcuts, tour, About), and Settings.
  Undo, Redo and the theme toggle sit beside it in the top bar.
- **The workspace name (Bookmarks bar):** switch workspace, all workspaces, and this workspace's history, rename,
  notes, lock, update from source and archive.
- **The arrow beside Download:** import, export, merged duplicates, history and workspaces, backup and restore.
- **Settings:** preferences, Browser history access, MCP access for Claude, and a **Danger zone** with every action
  that throws work away: Reset edits, Reset this workspace, Delete this workspace, Factory reset. Each asks first.

## Where things are kept

The binary keeps everything in its state folder (see [configuration](../configuration.md)). The Python implementation
keeps it in `storage/`:

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
