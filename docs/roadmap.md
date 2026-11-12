# Roadmap

Everything below ships in **v0.1.0**; the version is released when the last item is done. Each phase lands as its own
pull request, with tests and docs.

| Status | Meaning |
|---|---|
| done | in `main`, tested |
| next | being built now |
| planned | designed, not started |

## Phase 1 — Foundation (done)

- The `browser-foreman` Go binary: `serve`, `version`, `config {path,show,init}`.
- Configuration in `~/.config/browser-foreman/`: `*.json` merged in name order, `.env` for secrets, `BROFO_*`
  environment variables over both. State in `~/.local/state/browser-foreman/`.
- HTTP server with Host, Origin and token checks and security headers.
- Events: an in-process bus, `GET /api/v1/events` as Server-Sent Events with replay.
- Webhooks: subscriptions, signed deliveries, retries, test deliveries.
- Universal macOS binary (`make universal`), every other target (`make cross`), CI on pull requests, a release workflow
  that needs a person.

## Phase 2 — Storage options (done)

- A storage interface with two drivers: **sqlite** (default, pure Go, no C toolchain) and **json** (one folder of
  plain files, easy to inspect and sync). `storage.driver` picks one; `browser-foreman storage migrate --to json`
  moves data between them.
- Workspaces with full history (every change undoable, undo and redo as entries), bookmark trees and tab
  snapshots, in both drivers, with one shared test suite.
- Import from Chrome, Edge, Brave, Vivaldi, Opera and Arc (`Bookmarks` JSON) and from any exported bookmarks HTML
  (Firefox, Safari, and the Python tool's download), plus `workspace list|history|undo|redo` and read-only
  `/api/v1/workspaces`.
- Moved to phase 4, where the server starts changing workspaces: link check results and the webhook delivery log.

## Phase 3 — Accounts and sign-in (next)

- `server.mode: accounts`: users with argon2id-hashed passwords, sign-in with sessions (HttpOnly, SameSite=Strict
  cookies), CSRF tokens, sign-in rate limits and lockout, per-user API tokens (stored hashed, shown once).
- Roles: owner, editor, viewer. `browser-foreman user {add,list,passwd,remove}` and `token {create,list,revoke}`.
- `local` mode keeps today's behaviour: one owner on this computer, loopback only, no sign-in.
- `/api/v1/events` gets the same access rule as reading bookmarks (a session or a token) before any event carries
  bookmark data in phase 4. Today's events describe only the server and its webhooks.

## Phase 4 — Web interface and REST API parity

- The web interface (today in `web/`, served by the Python tool) embedded in the binary and served at `/`.
- Every capability under `/api/v1`: bookmarks and folders, workspaces and history (undo, redo, rollback), link checks
  and refreshes, the Trash, import and export (Netscape HTML, Chrome JSON), backups and restore, settings.
- Every change published as an event (`bookmark.created`, `bookmark.moved`, `workspace.committed`, `trash.emptied`,
  `tabs.snapshot.saved`, …) and therefore available to webhooks.
- An OpenAPI document at `/api/v1/openapi.json`.

## Phase 5 — Browsers

- Installed browsers and the system default, opening links in one, some or all of them, real browser icons.
- Reading open tabs (macOS first), snapshots on change or on a schedule, restoring a session after a crash,
  incognito handled privately.
- Scheduled link re-checks.

## Phase 6 — Browser plugins

- One WebExtension source, built for Chromium browsers (Manifest V3), Firefox and Safari.
- Live tabs and tab groups, bookmark editing in place (no import or export round trip), all through the local API with
  a token the extension is paired with.

## Phase 7 — Landing page and release

- The landing page in `site/`, with screenshots, published at the product URL.
- A full documentation pass; the hosted docs.
- The owner decides on the public repository and the v0.1.0 tag.

---
[← Docs index](../README.md#documentation)
