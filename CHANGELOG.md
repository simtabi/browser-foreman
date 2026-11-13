# Changelog

All notable changes are recorded here, following [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
[Semantic Versioning](https://semver.org/).

## [Unreleased]

Everything planned for v0.1.0 is listed in [docs/roadmap.md](docs/roadmap.md); v0.1.0 is released when all of it
ships.

### Added

- Workspace colour and icon: *Appearance…* picks one of 12 colours and an icon for each workspace, shown on its badge
  in the switcher, its menu and All workspaces, with the top bar tinted in the current workspace's colour
  (`color` and `icon` on `PATCH /api/v1/workspaces/{id}`, kept by both storage drivers and in backups).
- Labels by keyboard and in bulk: 1 to 5 rate the selected bookmark, 0 clears it, T goes to its tags; with several
  selected, Actions › Label them rates them, adds a tag to all or removes one from all (also in the command palette).
  The history names these changes ("Rated \"Go\" 4 stars; tagged work"), and `bookmark.updated` carries
  `renamed: true` when the title changed.
- Workspaces across every view: the switcher moved to the top bar, the ⋮ menu has All workspaces, Rename and Notes,
  and All workspaces renames, notes and chooses per workspace whether it keeps its own tab snapshots or uses the
  shared ones (`PUT /api/v1/workspaces/{id}/tabs`, `POST /api/v1/workspaces/{id}/open`, `?workspace=` on the tab
  routes, `tabs_own` and `active_workspace` preferences). Open tabs, Timeline and the counts follow the workspace.
- Your own labels on bookmarks: a 1 to 5 star rating and tags (`rating` and `tags` on every bookmark, on the API, in
  edits and whole-tree saves, validated the same way). Set them in the bookmark's details; they show on its row and
  card, search finds tags (`#work` matches one exactly), Sort has Highest rated and Group by has Rating and Tag.
- Last opened: `GET /api/v1/workspaces/{id}/opened` reads when each bookmark was last visited, and how often, from
  the browsers' own history (owner only, through copies, never stored). The details show it; Sort has Recently opened
  and Group by has Last opened. When macOS blocks reading history, the details say so.
- Grid layout: the Folders pane's List / Grid switch shows each folder's bookmarks as cards with their snapshot,
  stars and tags.
- Snapshot settings (Settings › Page snapshots): the window size (up to a tall 1280 × 2400 that shows more of a page),
  how long a page may run first, and the folder snapshots and site icons are kept in (`snapshot_width`,
  `snapshot_height`, `snapshot_wait_seconds`, `snapshot_dir` preferences).
- Browser extensions dialog, now in the Browsers menu: the five major browsers (Chrome, Edge, Firefox, Safari, Brave)
  and any other supported one on this computer, each with whether it is installed and connected and the steps to
  install the extension there; then the pairing code and the connected list.
- `browser-foreman` Go binary (module `github.com/simtabi/browser-foreman/src`) with `serve`, `version` and
  `config {path,show,init}`.
- Configuration from `~/.config/browser-foreman/`: every `*.json` merged in name order, then `.env`, then `BROFO_*`
  environment variables; validated on load; a `.env` other users can read is refused.
- HTTP server on `127.0.0.1:8765`: `GET /api/v1/health`, `GET /api/v1/version`; localhost-only Host check, same-origin
  check, security headers.
- Event bus and `GET /api/v1/events` (Server-Sent Events) with replay from `Last-Event-ID`.
- Webhooks: `GET/POST /api/v1/webhooks`, `DELETE /api/v1/webhooks/{id}`, `POST /api/v1/webhooks/{id}/test`;
  HMAC-SHA256 signed deliveries (`X-Brofo-Signature`), retries with exponential backoff, link-local and cloud metadata
  targets refused, secrets never returned.
- `make universal`: one macOS binary for Apple silicon and Intel; `make cross` for every target in `build/targets.txt`.
- The web interface (TypeScript + Alpine) in `web/`, and the Python implementation it was built with in `legacy/`, kept
  as the reference until the Go binary reaches parity.
- Landing page in `site/`.
- Storage: `sqlite` (default, pure Go) and `json` drivers behind one interface and one test suite; workspaces with
  append-only history (trees stored once per content hash), conflict detection, undo and redo as entries, tab
  snapshots; `storage migrate --to json|sqlite` copies everything, history included.
- `browser-foreman import FILE` for Chrome's Bookmarks JSON and any exported bookmarks HTML (Firefox, Safari, Chrome,
  Browser Foreman), and `workspace list|history|undo|redo`.
- `GET /api/v1/workspaces`, `/api/v1/workspaces/{id}` (`?at=N`) and `/api/v1/workspaces/{id}/history`, token
  required.
- Accounts (`server.mode: accounts`): argon2id passwords, sign-in sessions in HttpOnly SameSite=Strict cookies with
  CSRF tokens, sign-in rate limits and lockout, roles (viewer, editor, owner), per-person API tokens stored hashed and
  shown once, and a guard that keeps the last owner. `/api/v1/auth/{signin,signout,me}`, `/api/v1/tokens`,
  `/api/v1/users`; `browser-foreman user {add,list,passwd,remove}` and `token {create,list,revoke}`;
  `auth.session_ttl` / `BROFO_SESSION_TTL`.

- Changing bookmarks through the API: add, edit, move, Trash, restore and delete, single or batched as one entry;
  undo, redo and rollback; import a file into a workspace and export one as bookmarks HTML or Chrome JSON. Every change
  is an event (`bookmark.moved`, `trash.emptied`, `workspace.committed`…) and so reaches webhooks. `If-Match` refuses
  a change made on a stale head with `412`. `javascript:` and `data:` URLs are refused.
- The web interface is embedded in the binary and served at `/`: the Bookmarks view reads and saves the server's
  tree (`PUT /api/v1/workspaces/{id}/tree`, which publishes what changed), with undo and redo through the history and a
  refused save when the bookmarks changed elsewhere. A sign-in page in `accounts` mode. The page loads nothing from
  other hosts and runs only its own scripts (a per-response CSP nonce).
- Link checks: `POST /api/v1/linkchecks` checks a workspace's links (or a folder's) in the background, politely per
  site, with pause, resume and cancel; results are kept per address and shown by the web interface (dead, moved,
  needs login…), and a changed status publishes `link.checked`. Pages that say "not found", parked domains and bot
  checks are recognised. Connections to private addresses are refused at dial time, and action links are never
  visited.
- Workspaces can be renamed, given notes, locked (read-only, refused by the store itself: `423`) and archived;
  the web interface has the workspace menu, a History window with *Go back to this*, import into a new workspace,
  and downloads through the API.
- Backup and restore: `browser-foreman backup FILE` / `restore FILE` and `GET /api/v1/backup` / `POST
  /api/v1/restore`. A backup holds every workspace with its history and the link results, and no accounts or secrets;
  restoring adds and replaces nothing.
- The webhook delivery log: the latest 1,000 deliveries, `GET /api/v1/webhooks/deliveries`.
- A new password signs the person out of their other sessions.
- `GET /api/v1/settings`: the configuration in force, read-only and without secrets.
- Browsers (macOS): the installed browsers, the system default and their icons; opening links as tabs, in a new
  window or a private one, in one, some or all browsers (`POST /api/v1/open`, owner).
- Open tabs (macOS): reading every tracked browser's windows and tabs, snapshots on change or on a schedule,
  restoring a session, closing tabs after a snapshot that brings them back, a timeline of what opened and closed.
  Private windows keep only their tab count unless the owner chooses otherwise. Preferences at
  `/api/v1/preferences`.
- Scheduled link re-checks of links whose last check is older than `recheck_days`.
- The web interface's Open tabs view, Timeline and Browsers menu work with the Go server.
- Requests another site's page makes from a browser are refused even without an Origin header.
- A browser extension, one source built for Chromium browsers (Manifest V3), Firefox and Safari (`make extension`,
  `make extension-safari`): it pairs with a one-time code (`browser-foreman pair`), reports tabs and tab groups,
  opens links or a named tab group when the server asks, reads the browser's bookmarks into a workspace, and writes a
  workspace back into the browser's bookmarks after showing the changes, saving the current ones first.
- Moving from the Python tool: `legacy/tools/export_for_go.py` writes everything it kept (workspaces with their
  whole history, link results, tab snapshots, settings) as a backup that `browser-foreman restore` takes in. Backups
  now carry tab snapshots and preferences; bookmarks can carry the page's own description.
- `browser-foreman mcp`: an MCP server on stdio, through the official Go SDK, with 8 read tools and, behind
  `mcp.writes`, 8 that change bookmarks through the API (trash and rollback also need `confirm: true`). Off unless
  `mcp.enabled`; everything it returns is marked as untrusted data.
- Merging: `POST /api/v1/workspaces/{id}/sources` and `browser-foreman merge` add several files (and other
  workspaces) to a workspace, keeping one copy of each page by the Python tool's rule; every other copy goes to the
  Trash as a duplicate. `GET /api/v1/workspaces/{id}/duplicates.csv` lists them, and the page's Import, Add to this
  workspace, Merged duplicates (CSV) and Removed duplicates view use them.
- Update from source: a workspace remembers the files and workspaces merged into it (kept in backups and
  `storage migrate`), and `GET/POST /api/v1/workspaces/{id}/update` (and Update from source… in the page) brings in
  what is new upstream and titles changed upstream that were not renamed here, without removing anything.
- Saving tabs as bookmarks: `POST /api/v1/workspaces/{id}/save-links` makes a new folder of them in one entry; the
  page's Save and Move to bookmarks and the MCP tool use it.
- Firefox and Safari: the importer reads Firefox's `places.sqlite` (through a copy, with its write-ahead log) and
  Safari's `Bookmarks.plist`. `GET /api/v1/profiles` finds every browser profile's bookmarks file on macOS, Linux
  and Windows (Chromium profiles by the names the browser shows), and says why one cannot be read (Full Disk Access
  on macOS). Owners can merge one in with `?path=`, which Update then reads again; only bookmarks file names open.
- Tab notes, deleting snapshots and purging private pages: `GET/PUT /api/v1/tabs/notes`,
  `POST /api/v1/tabs/snapshots/delete` and `POST /api/v1/tabs/purge` (owners), with `incognito_purge_hours` applied
  after each automatic save; notes travel in backups, `storage migrate` and the Python migration. The page's notes,
  delete and purge controls now work instead of answering "not available yet".
- Browser history: `GET /api/v1/history` (owners) reads every Chromium, Firefox and Safari profile through copies,
  one row per address, marking what a workspace already has; `GET /api/v1/history/access` and
  `POST …/open-settings` check and open Full Disk Access on macOS. Nothing read is stored or published. The page has
  a Browser history window that saves chosen pages as bookmarks.
- Workspace Trash and reset: `POST /api/v1/workspaces/{id}/delete` moves a workspace to the workspace Trash,
  history and all (`GET /api/v1/workspaces?trash=1` lists it), `…/restore` brings it back, `…/purge` removes it for
  good with its id typed, and `…/reset` goes back to how it started as one undoable entry. The page's Workspaces
  window has In use and Trash tabs, and the workspace menu has Reset and Delete.
- Site icons and page snapshots: icons are kept from the `ICON` data in imported bookmark files (no site is asked),
  and `POST /api/v1/snapshots` takes snapshots with the installed Chrome, headless, in a throwaway profile, public
  pages only. The page shows both and Refresh takes snapshots again. File names match the Python tool's, so its
  icons and snapshots can be copied into `<storage>/media/{icons,thumbs}`.
- Linux and Windows: browsers are found (on `PATH`, or where they install) with the system's default, links open
  in them, and open tabs are read from each browser's session file (Firefox's `recovery.jsonlz4`, Chromium's
  `Sessions/Session_*`), which never holds private windows. Firefox and Zen are read the same way on macOS.
- `GET /api/v1/openapi.json`: the API as OpenAPI 3.1, kept in step with the routes by a test.

### Fixed

- Buttons and menus show a focus ring for the keyboard only, so a click (or a dialog focusing its safe choice, as the
  welcome's *Not now*) leaves no outline or grey block; workspace badges keep their size in menus and buttons; the
  welcome counts the workspace's own bookmarks and those in the Trash (it read 5,996 of 5,416).
- The welcome text no longer calls the browser extension planned.
- Merging the same file again no longer refills the Trash: copies it already holds as duplicates of the same kept
  bookmark are skipped and counted (`already_in_trash`). A test workspace merged ~60 times had 617,515 such copies.
- The folder summary stacks its columns when the details pane is narrow (grid layout), instead of wrapping About's
  numbers one character per line; the top bar no longer overlaps at narrower widths; the Bookmarks toolbar says
  edits are saved in the workspace when there is a server.
- Open tabs said 0 snapshots kept and 0 private pages stored: `GET /api/v1/tabs/stored` now counts both.
- Expand / Collapse all now covers the Trash and works in filtered views; it used to leave the Trash open.
- A web test now fails when `page.ts` (still unchecked by the compiler) uses a name that does not exist, and the
  header counts log their errors instead of hiding them.
- The Timeline's All range showed no tab snapshots: `days=0` now means every snapshot kept.

### Changed

- The workspace switcher keeps its ordinary button, with the workspace's icon in its colour, and the Browsers menu shows
  each browser's state as a pill: green with windows, grey when not running, amber when it needs attention.
- List and grid switch instantly and keep the panes' widths: the grid is CSS on the same rows (nothing is rebuilt),
  and long lists lay out only what is near the screen (`content-visibility`).
- Docs to the Simtabi and laranail standard: new reference pages for workspaces, labels and page snapshots, new
  recipes for rating and tagging and for keeping tabs per workspace, the web interface page brought up to date (the
  layout, list and grid, labels, keyboard, menus, where things are kept), the REST API page listing the new routes
  and fields, and a Go test (`internal/docscheck`) that fails when a page lacks its title or footer, the README index
  misses a page or lists one that does not exist, or the README's sections are out of order.
- List / grid is one toggle that shows the layout it switches to; on phones the header buttons wrap and bookmark
  rows drop their date so titles have room.
- Interface pass: your rating and tags are labelled rows of a bookmark's Details, and on its row they sit under the
  address instead of taking room from the title; letter tiles use a dark tint in dark mode; the Timeline's kinds are
  Tabler selectgroup pills; Open tabs shows placeholders while it loads; actions that delete or close are solid danger
  buttons (purge, clear tab history, empty and delete from the Trash, the Settings danger zone, Stop, Cancel), and the
  Trash pager says groups when copies of one page are grouped.
- Custom date and time ranges: every period picker has From / To fields beside its quick ranges (To before From is
  refused). `GET /api/v1/tabs/timeline` and `GET /api/v1/history` take `?from=` and `?to=` (RFC 3339 or
  `2006-01-02T15:04`); the Timeline's bookmark chart follows the range and counts by day for two months or less.
  Icons in buttons, tabs and menus are larger and sized per control; the Expand / Collapse all button stays visible
  (disabled) when a filter leaves nothing to fold; the Download divider is softer.
- One period picker everywhere (`ts/rangepick.ts`): the Timeline's range buttons, Browser history's select and the
  Added filter are the same compact dropdown. One Expand / Collapse all button (`ts/fold.ts`) sits beside Undo and
  Redo and acts on the view you are in: folders in Bookmarks, windows in Open tabs. The Browsers dialog is borderless
  rows with switches and status badges, a warning only when macOS blocks reading, and Import in the footer.
- The View row is one line: Group by, Sort and Folders are compact pickers showing their value (outlined when not the
  default), and the two date-time fields and Presets became one Added dropdown with presets and a custom range. Clear
  filters sits at the end of that row and is disabled when nothing is filtered. Tree rows no longer show their letter
  twice, and Link health's labels are short enough not to be cut.
- The toolbar: Show is one Tabler segmented control on a single row with short labels (full names in the tooltips),
  the Download button and its menu arrow follow Bootstrap's nested button group (both outer corners rounded, a
  bright divider, no outline after a click), and Expand / Collapse all folders moved from the app menu to one toggle
  in the Folders header.
- The web interface fits a 375px phone: no sideways scrolling, secondary table columns hidden on small screens, and
  long names cut with an ellipsis. Scripts and stylesheets carry a content hash (`?v=`), so a new build is never
  served from the browser's cache, and every History entry migrated from the Python version has a readable label.
- Releases: a pushed `vX.Y.Z` tag, or `gh workflow run release.yml -f tag=vX.Y.Z`, now builds and publishes the
  binaries on the GitHub release, and nowhere else. `release.yml` calls simtabi/.github's shared `go-release.yml`
  (no `confirm=release-go` dispatch any more), and the new `src/.goreleaser.yaml` publishes the same assets the old
  workflow did: one binary per target in `build/targets.txt`, `browser-foreman_macos_universal` and `checksums.txt`,
  which now lists bare file names.
- The web interface: counts agree everywhere (the Trash is counted on its own), the tree header no longer wraps,
  a save refused because the bookmarks changed elsewhere shows a Tabler alert with Reload and Keep my change, Open
  tabs shows a loading state, the Timeline drops "−0 closed" and names months, link health says Skipped for links
  it does not visit, same-named folders show their parent, the theme is set before the page paints, and texts that
  still named the Python server now name `browser-foreman serve`.
- Dialogs and lists: a Tabler data table (search, sortable columns, tabs, row actions in a menu, pages) for
  Workspaces, History, Browser history and the extension's connected browsers; every dialog opens from the top so it
  grows downward instead of jumping, its pager sits in a fixed strip above the buttons, panes keep their pager in
  view, footers follow one order (Close or Cancel left, actions right) and empty ones are hidden. The licence text
  shows, and the credits list what the Go version is built with.
- `GET /api/v1/events` now needs a viewer (a token or a session), like reading bookmarks.
