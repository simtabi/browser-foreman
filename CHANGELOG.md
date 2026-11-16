# Changelog

All notable changes are recorded here, following [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
[Semantic Versioning](https://semver.org/).

## [Unreleased]

Everything planned for v0.1.0 is listed in [docs/roadmap.md](docs/roadmap.md); v0.1.0 is released when all of it
ships.

### Added

- `ai.provider: openai` answers AI jobs through OpenAI's Chat Completions API (`OPENAI_API_KEY`, `ai.model`
  required, `ai.openai_url` for another server speaking it), with answers held to the job's schema in strict mode.
- AI jobs are kept in `ai-jobs.json` in the state folder, so a restart loses none; jobs still waiting are answered
  again when the server starts with a provider.
- AI jobs, off by default: `browser-foreman ai tag` suggests one to three tags per untagged bookmark, and
  `ai dedupe` asks whether look-alike bookmarks are the same page (only the groups Find duplicates is unsure of).
  `ai.provider` chooses who answers: Anthropic's API (structured JSON answers, refusal fallbacks), a local Ollama, or
  an MCP assistant (`next_ai_jobs`, `submit_ai_results`, `run_ai_jobs` through sampling, the `process-queue` prompt).
  Every answer is checked against its job before anything is written, and `--apply` writes it as one history entry.
- MCP: `get_preview`, `health_report`, `find_duplicates` and `stats`; `label_bookmarks` (tags, rating, note) and
  `add_bookmark` with writes on; the resources `bookmarks://workspaces` and `bookmarks://workspaces/{id}/health`; and
  the prompts `triage-dead-links`, `tidy-folder`, `dedupe` and `auto-tag`, each of which asks before changing anything.
- MCP write scopes: `mcp.folders` holds every write to what is inside some folders, and `mcp.clients` gives one client,
  by the name it reports, its own writes and folders. Under a folder scope, undo, rollback and put back are refused.
- `browser-foreman service install`, `uninstall` and `status` run `serve --no-open` at login (a launchd agent, a
  systemd user unit or a Windows logon task), so tab autosave and link re-checks keep going with no terminal open.
- Desktop notifications through go-notifykit: a scheduled re-check that confirms links dead, or finds dead ones working
  again, shows one banner, which opens the page when clicked (`notify.desktop`, on by default). `browser-foreman
  notify send`, `ping` and `doctor`, and `serve` listens for pings.
- *Browsers › Write back to a browser…* previews and applies a write-back from the page, through a connected
  browser's extension, with the same plan, fingerprint check, backup and read-back verification as `apply`. The
  command line and the page now share one implementation.
- `apply --route extension --yes` writes through the extension by itself while Browser Foreman runs, refusing a
  browser that changed since the plan (a fingerprint both sides compute alike), then reads the browser back and
  plans again to verify. Extension 0.1.4 takes waiting commands as soon as it reloads.
- Settings › Browser extensions: strict approvals (the default; a browser is approved only from a page open in that
  browser) or loose (from any). The connect prompt has Not now, which leaves the request waiting.
- `serve` prints the page's link on a line of its own and, started from a terminal, opens it in the default browser,
  also when Browser Foreman is already running (`server.open`: auto, always or never; `--open`, `--no-open`).
- The extension (0.1.2) asks to connect by itself once it is loaded and finds Browser Foreman, showing its four
  letters on its toolbar icon; it stops after a deny, a disconnect or three unanswered requests, until Connect is
  pressed. Until now nothing happened after installing it unless its popup's Connect was pressed, and no step said so.
- *Browser extensions…* is redesigned as a setup assistant: a waiting request on top with its letters, one row per
  browser with where it stands and its next action, and a setup guide that checks each step live (built, loaded,
  asking, connected, sending tabs, up to date) and says what is most likely wrong when one stalls.
- An extension asking to connect shows in the terminal running `serve`, with its four letters, and
  `browser-foreman pair --approve` approves it there once those letters are typed (or given with `--match`). Until
  now a request was seen only in an open Browser Foreman page, so with none open it expired unseen.
- `serve` handles a port already in use. If the same Browser Foreman holds it, `serve` says so and exits without an
  error; another build can be replaced with `--replace`. If another program holds it, `serve` names that program and
  uses the next free port, recorded in `server.json` so `mcp`, `pair`, `apply` and `stop` follow it.
  `browser-foreman stop` stops the running server. In local mode, health names the service, its version and its
  process.
- The write-back's dry run reads the profile through the extension (the server asks it to read now, or uses its last
  read when Browser Foreman is not running) or from the profile's file, as `--read` or `writeback.read` says.
  `writeback.mode`, `writeback.route` and `writeback.read` set the choices, each `ask` by default. The extension
  (0.1.1) carries out the server's new `read-bookmarks` command.
- `browser-foreman apply` writes a cleaned workspace back into a Chromium profile as a three-way merge of the export,
  the cleanup and the profile now: merge keeps what the profile changed after the export, and replace drops what it
  gained once you confirm the count. It is a dry run unless `--yes`. The file route writes the profile's `Bookmarks`
  file with Chromium's checksum, refuses a running browser, a syncing profile and a profile that changed since the
  dry run, and saves the profile first. The extension route saves the result for the extension to write.
- `browser-foreman mcp` works without a running server: when none answers at the configured address, it opens the
  store and runs the same API in-process, on no port, as the Python tool's MCP server read its files directly.
- The MCP tools take the arguments the Python server's did: `list_workspaces` `include_archived`,
  `read_tab_snapshot` `query`, `move_bookmarks` `create`, `save_tabs_as_bookmarks` from one window of a tab snapshot,
  and `refresh_links` `ids`.
- Stack Overflow and other Stack Exchange question links a check could not settle are looked up through the public
  Stack Exchange API, a hundred to a request: deleted questions are dead, closed ones say why. On the 2026-11-10
  export, 224 of the 390 links still blocked were Stack Exchange questions.
- Links a plain check could not settle (bot checks, `403`s, timeouts) are opened in headless Chrome after each run and
  judged from what Chrome shows: working only for a real page with content and no error title, dead for a "not
  found" page. It is the Python tool's render evidence, ported, and can be turned off (*Confirm in Chrome*).
- A loading screen covers the whole page from its first paint until everything is ready (bookmarks drawn, menus and
  filters filled, fonts and styles loaded), so the page is never seen half built. Settings › Startup › *Loading
  screen* turns it off. It lifts by itself after 15 seconds if the page cannot finish.
- Failing GitHub links are checked again through GitHub's API, with `gh` signed in as you, after each link-check run.
  A deleted repository is told apart from a private, renamed or archived one, and a file or branch gone from a
  repository that still exists is caught. It is the Python tool's GitHub check, ported.
- *Sort into folders…* (workspace menu, command palette, `browser-foreman sort`, `POST /api/v1/workspaces/{id}/sort`)
  files bookmarks by your own rules in `sort-rules.json`. Protected folders stay as they are and pull matching links
  in; dead links, local addresses and sign-out pages go to review folders; loose links are filed by site and keyword
  topics; big folders get type and topic subfolders. The plan shows first, and applying is one history entry. It is
  the Python tool's category build, with the rules as data.
- *Find duplicates…* (workspace menu and command palette) lists bookmarks already in a workspace that lead to the same
  page: the same address written differently, the same page after redirects, the same canonical address, or the same
  page with another query and the same title. Ticked groups send their extra copies to the Trash as duplicates of the
  one kept, in one history entry. The same list is `GET /api/v1/workspaces/{id}/duplicates`.
- Link checks now confirm a dead link before calling it confirmed: the first failed check marks it *page gone?* (not
  confirmed yet), and a check at least 24 hours later that finds it dead again confirms it.
- Link checks record whether a page forbids being shown in a frame. *Load the live page here* then keeps the snapshot
  and says why, instead of showing a blank frame.
- Link checks read what each page says about itself (OpenGraph title, description, image and canonical address).
  Details shows the description and the canonical address, and the image stands in where there is no snapshot.
- *Archive copies* (off by default): each confirmed-dead link is looked up in the Internet Archive, and its panel
  offers *Open the archived copy*.
- One-click connecting for the browser extension: press Connect in its popup, and Browser Foreman asks (wherever you
  are in it) whether to connect that browser, showing the same four letters as the popup. The pairing code still
  works. Only an extension can ask; a request waits five minutes, five at most at once.
- Browser extensions › **Set it up** opens a browser's extensions page and shows the built extension in Finder; the
  card turns to Connected by itself. Each extension reports its version; one older than the build here says so, and
  **Update** has it reload itself from the new build.
- Loading feedback (`ts/busy.ts`): slow actions (expanding or collapsing everything, filters, Group by and Sort, the
  date range, list and grid, switching workspace) show a spinner with what is happening and a thin progress bar, only
  when they take longer than a blink; the tree, the details, the Timeline and every list dialog (Workspaces, History,
  Browser history, Browser extensions) open at once with skeleton rows and a message until their data arrives.
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

- A page whose server stopped, or moved to another address, says so in words and names what to do, in place of the
  browser's "Failed to fetch"; *Browser extensions* shows one notice and recovers by itself when the server is back,
  instead of a new error every three seconds, and the same message never stacks as several toasts.
- The extension setup guide names the likeliest reason a browser never asks to connect: its extension is already
  connected to another Browser Foreman, and an extension talks to one at a time.
- An address with a default port (`https://site:443/`) matched no copy of itself without the port, so a write-back
  planned it as new every time (duplicating it) and the duplicate finder missed it. The port is now ignored when
  comparing and left out when writing.
- After writing, the extension could not read the browser back into the plan it had written (403).
- Disconnect… in *Browser extensions…* did nothing visible: its confirmation opened behind the dialog. Dialogs now
  stack, each above the one it was opened from.
- The folder list's count read as a number of folders; it now says "6,158 bookmarks". The footer and About called
  the workspace's last change "Built"; they now say "Changed". An empty folder's tile says Empty.
- One look across the page: one button size per context, one icon per action, one colour per status, title icons
  and labelled close buttons on every dialog with the dismiss button on the left, one spinner colour and loading
  placeholder, the theme's corner radii and a four-step type scale. Every notification says its kind. A web test
  (ui-rules) enforces it.
- In *Browser extensions…*, an opened *How to install* no longer closes by itself when the dialog refreshes its
  status every three seconds.
- A site that kept rate-limiting made each of its deferred links wait out the pause again at the end of a run, a few at
  a time. One proof-of-concept pass took 4.4 hours because of it. Now the site's other links stop waiting once one has
  waited in vain.
- A link check no longer leaves a rate-limited site's links as "could not verify". They are checked again at the end
  of the same run, once the site's pause is over. A timeout gets one more try, and each name on a hosting platform
  (`alice.github.io`, `my-app.vercel.app`…) is its own site rather than sharing one pause. On the 2026-11-10 export,
  the first pass left 1,998 of 6,151 links unverified, 525 of them GitHub links skipped during a pause.
- The MCP dialog no longer names the old Python folder (`~/Artisan/bookmarks-cleanup`) as where Claude Code finds the
  server; it names the folder the server runs from.
- The search placeholder is no longer cut when the header search is narrow: the magnifier gives way first.
- Badges ("active", "now") and table sort headings keep their whole word instead of being squeezed.
- On a phone, tab titles take two lines, names in the Workspaces and Extensions dialogs wrap, and the About dialog's tabs wrap instead of running off the edge; on a desktop a cut tab title or name shows in full on hover.
- The snapshot folder's placeholder fits the field on a phone.

- A search that finds nothing says so, with what was searched for: Bookmarks shows *Nothing matches “…”* with a
  button to clear the search and filters (the empty Trash row is not a result); Open tabs shows *No tab matches “…”*
  instead of every window saying no tab here matches, and counts the matching tabs when there are some; the Timeline
  names the search rather than blaming the date range.
- In dark mode, the Local, Skipped and Unchecked colours of link health show (they were dark on dark): Local is cyan,
  Skipped grey, and Unchecked a hollow ring.
- On a phone, Open tabs no longer puts every saved snapshot (hundreds of rows) before the open windows: the snapshots
  side scrolls within itself. Timeline events keep their text on full lines, with their link underneath when there is
  no room beside them; their Opened and Closed lists use the app's chevron.
- The top bar's search shows its whole placeholder (*Search open tabs…*, *Search timeline…*); the view tabs' counts
  give way below 1600 pixels, as they show inside each view too.
- Security: a paired extension's token carried its owner's whole role, so it could change settings, manage tokens or
  edit any workspace. It now works only for the extension's own routes, the workspace list and reading one workspace;
  it reads bookmarks only into its own workspaces or one already read from its browser; and no other token can be
  named like an extension's (a viewer could have pushed tabs as a browser).
- Commands queued for a browser whose extension never collects them are forgotten after an hour, at most 100 waiting.
- Open tabs: a window's buttons (Open all, Reopen missing) keep their labels whole on a phone, moving under the
  window's title instead of shrinking.
- Dialogs never scroll sideways: a table wider than its dialog scrolls within itself. The Browser extensions cards keep
  their status on readable lines at any width.
- The Timeline compares a workspace's tab snapshots with that workspace's own earlier snapshot; the one before the
  range could come from another workspace's pool, so its first step showed every page as opened.
- *Last opened* credits every bookmark of a page: two bookmarks of the same page (http and https, a trailing slash)
  both show its visits, where one of them showed none.
- Reordering inside a folder of more than 2,000 bookmarks records just what moved; before, every child was recorded
  as moved. The diff now finds the kept order in O(n log n) time, at any folder size.
- A custom range ending at a minute (*to 09:30*) includes that whole minute.
- Menus no longer close by themselves: a background refresh that rebuilt a menu's button (Browsers, Download and
  others, about half a second after opening) closed it; the rebuilt menu now stays open. Scrolling or resizing moves
  an open menu with its button instead of closing it; it closes once the button leaves the screen, on Escape, or on a
  click elsewhere.
- Bookmark cards draw their border at once instead of fading it in from dark.
- Two preference changes made at once (switching workspace while changing a setting) no longer lose one of them.
- A tag's 40-character limit counts characters, not bytes, so tags in accented or non-Latin scripts get the full
  length; a whole-tree save refuses a rating or tags on a folder.
- The page adapts to the window without clipping or covering anything, checked from 1920 to 390 pixels wide: the
  search keeps a usable width (the product name, then the view tabs' labels, give way first, and the Commands label
  and shortcut hints go by the search's own width); the Show filters wrap instead of hiding off the edge; on a phone
  the Browsers menu is its icon and the panes line up with the bar above them.
- Bookmark cards without a page snapshot show a short tinted band with the site's initial, not an empty box (the
  initial was white on white).
- Confirm dialogs' buttons are as wide as their words need, with room either side, and stack when they do not fit.
- Deleting tab history and purging private windows act on the open workspace's tab snapshots only (what its panel
  counts), not every workspace's; the automatic purge still covers all of them.
- Merging no longer stops with an error when a Trash entry's reason says *duplicate of* with no bookmark after it.
- A table's column header (History, Workspaces and the other lists in dialogs and panes) stays in view under its
  search bar while the rows scroll, instead of scrolling away with them; it is solid, so rows no longer show through.
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

- The SQLite store keeps each tree snapshot gzipped, about a tenth of the size: a 5,400-bookmark history of 30 trees
  takes 14 MB rather than about 45 MB. Stores written before still read; their old rows stay as they are.
- Each tab snapshot lists its pool, so Open tabs, the Timeline, counts, deletes and purges skip other workspaces'
  snapshots without opening them; snapshots saved before this get their pool once, when the data is first opened.
- Moving or trashing a big selection no longer slows down with its size (one pass per folder, not one per item).
- Page snapshots pin Chrome to the public address that was checked, so a name server cannot point the page at a
  private address between the check and the snapshot.
- The page loads faster with many bookmarks: site icons and page snapshots are found with one read of each folder,
  not one per bookmark; history searches with an end date let the database skip what is newer.
- Work that blocks the page dims it behind a centred card with a large spinner and what is happening, and turns every
  button off until it ends, so nothing can be pressed twice. A save shows a spinner in the button that started it,
  which stays off until the answer arrives; a slow one also shows a note at the top.
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
