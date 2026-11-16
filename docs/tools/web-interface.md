# Web interface

The bookmark tree, Open tabs, Timeline, the Trash and every menu. The binary embeds the interface (`make build` runs
`make web` first) and serves it at `/`.

## Start the server

```bash
browser-foreman serve
```

Open <http://localhost:8765/>. In `local` mode the page opens straight away; in `accounts` mode it asks you to sign in
first. The first visit shows a welcome and a short tour; Help › Tour replays it. `?ws=<id>` opens another workspace.

## What it covers

Everything on the page works against the server: Bookmarks (the tree, link health from [link checks](link-checks.md),
details, your labels, the Trash, history with *Go back here*), Open tabs and the Timeline
([browsers and tabs](browsers-and-tabs.md)), workspaces, import and export, and backup and restore
([backup](backup.md)). Every bookmark edit is saved as one history entry through `PUT /api/v1/workspaces/{id}/tree`,
and the server publishes what changed as events. If the bookmarks changed somewhere else since the page loaded, the
save is refused and the page offers *Reload* or *Keep my change*, so nothing is overwritten. A locked workspace opens
read-only.

## The layout

One header holds everything that applies everywhere: the workspace switcher, the three views (with counts), one
search box that searches the view you are in (each view keeps its own query; press `/` to jump to it), Commands
(`⌘K`), the Browsers menu (which browsers' tabs to show, *Browsers and access…* and *Browser extensions…*), Undo and
Redo for the view you are in, *Expand / Collapse all* for that view (folders in Bookmarks, windows in Open tabs), the
theme toggle (a moon in light mode, a sun in dark), and the ⋮ menu (workspace, help, settings).
Each view then has its own bar. Bookmarks has *Update moved*, *Refresh all*, *Trash* and *Download*, then two rows:
**Show** (one segmented control: All, Untouched, Dead, Moved, Redirects, Unverified, Login, Duplicates, each with its
count), **Saved** (your smart folders; see below) and **View** (Group by, Sort, Folders, and *Added*, a period picker
with quick ranges and a custom date and time range; *Clear filters* at the end). The footer shows whether the server is running and when tabs were last saved.

## The three views

| View | What it shows |
|---|---|
| Bookmarks | your folders with the cleanup merged in; link health, snapshots, details, edits with full history |
| Open tabs | every window per browser, normal and incognito, live or as saved; reopen what a crash closed |
| Timeline | everything that happened, by day: tab snapshots, bookmark edits, link checks |

## Workspaces

The workspace switcher sits in the top bar beside the logo, so it is there in every view: a workspace scopes
Bookmarks, Open tabs and Timeline (its edits, its history and, when it keeps its own, its tab snapshots). The ⋮ menu
has *All workspaces…*, *Rename this workspace…* and *Notes…*; *All workspaces…* renames, archives, locks, deletes and
restores any of them, and chooses for each whether it keeps its **own tab snapshots** or uses the **shared** ones.
Shared is the default: every workspace that does not keep its own reads and saves one central set. Switching a
workspace to its own starts it empty; snapshots already taken stay where they were saved.

## A loading screen until it is ready

The page opens behind a loading screen that covers it until everything is ready: the bookmarks drawn, the menus and
filters filled, and the fonts and styles loaded. You never see it half built. Turn it off in Settings › Startup ›
*Loading screen* to see each panel fill in behind its own placeholder instead; the change applies from the next load.
If something stops the page from finishing, the screen lifts by itself after 15 seconds.

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

## Smart folders

A smart folder is a saved search: a name over a Show filter and search text, such as *Dead links to triage* (Show:
Dead) or *Later* (search `#later`, which matches that tag exactly). It holds whatever matches now, so it changes as
your bookmarks do; nothing is copied or moved.

- **Save one:** pick a Show filter or type a search, then *Saved › Save this view…* and name it. It belongs to the
  workspace you are in.
- **Open one:** click it on the Saved row. Its button shows how many bookmarks it holds, and lights up while the view
  is that smart folder. Back and forward step through it like any other view.
- **Forget one:** open it, then *Forget it…*. Only the saved search goes.

Smart folders are kept in the server's preferences (`smart_folders`), so they appear in every browser, and an
assistant can list and open them over [MCP](mcp.md). Saving them is the owner's: in accounts mode, other roles see
them but cannot change them. At most 50.

## List and grid

One toggle in the Folders pane's header switches between a **list** (rows under their folders) and a **grid** (each
folder's bookmarks as cards with their page snapshot, stars and tags); its icon shows the layout it switches to. The choice is kept in this browser.
A long list (a broad search, or every folder expanded) shows its first rows at once and fills in the rest a few
hundred at a time, so the page stays usable with tens of thousands of bookmarks open; moving the selection repaints
only the rows it leaves and reaches. In the grid the
details pane is narrower and its folder summary stacks its columns.

## Your labels: rating, tags and last opened

A bookmark's Details hold **Your rating** (1 to 5 stars; click the same star again to clear) and **Tags** (type and
press Enter or a comma; tags used elsewhere are suggested). Rows and cards show both. Search finds tags, and `#work`
matches that tag exactly; Sort has *Highest rated* and *Recently opened*, Group by has *Rating*, *Tag* and *Last
opened*. **Last opened** comes from your browsers' own history (read when the page opens, never stored); when macOS
blocks reading it, the details say so. With several bookmarks selected, *Actions › Label them* rates them, adds a tag
to all or removes one from all. Every change is a history entry ("Rated \"Go\" 4 stars; tagged work").

## Keyboard

`?` lists every shortcut. The ones used most: `/` search, `⌘K` commands, ↑ ↓ → ← move and open, Space select, Enter
open, `.` the context menu, `1`–`5` rate the selected bookmark and `0` clear it, `T` its tags, `R` refresh, `M` move,
`G` group, `F2` rename, `⌫` to the Trash, `-` collapse all, ⌘Z and ⇧⌘Z undo and redo.

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

- **The workspace (top bar, beside the logo):** switch workspace, *All workspaces…*, and this workspace's history,
  update from source, rename, notes, lock, reset, archive and delete.
- **⋮ (top right):** Workspace (*All workspaces…*, *Rename this workspace…*, *Notes…*), Help (help, shortcuts, tour,
  About), and Settings.
- **Browsers (top bar):** which browsers' tabs and history to show, *Browsers and access…* (tracking, opening links,
  reading bookmarks and history) and *Browser extensions…* (the supported browsers, how to install the extension in
  each, pairing, connected browsers).
- **The arrow beside Download:** import, export, merged duplicates, history and workspaces, backup and restore.
- **Settings:** preferences, Browser history access, Page snapshots (window size, wait, folder), MCP access for Claude,
  and a **Danger zone** with every action that throws work away: Reset edits, Reset this workspace, Delete this
  workspace, Factory reset. Each asks first.

## Where things are kept

Everything lives in the state folder (`~/.local/state/browser-foreman/` by default; see
[configuration](../configuration.md)):

| Path | Holds |
|---|---|
| `browser-foreman.sqlite3` | every workspace and its history, link check results, tab snapshots and notes, preferences (the `sqlite` driver; the `json` driver keeps the same in JSON files, see [storage](storage.md)) |
| `media/icons/`, `media/thumbs/` | site icons and page snapshots, unless Settings › Page snapshots names another folder ([page snapshots](page-snapshots.md)) |

Backups are written where you choose ([backup](backup.md)). The Python tool's `storage/` folders are listed in
[moving from the Python tool](../recipes/migrate-from-python.md).

---
[← Docs index](../../README.md#documentation)
