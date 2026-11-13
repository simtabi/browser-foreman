# Changelog

All notable changes are recorded here, following [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
[Semantic Versioning](https://semver.org/).

## [Unreleased]

Everything planned for v0.1.0 is listed in [docs/roadmap.md](docs/roadmap.md); v0.1.0 is released when all of it
ships.

### Added

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
- `GET /api/v1/openapi.json`: the API as OpenAPI 3.1, kept in step with the routes by a test.

### Changed

- `GET /api/v1/events` now needs a viewer (a token or a session), like reading bookmarks.
