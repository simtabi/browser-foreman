# Roadmap

Everything below shipped in **v0.1.0** (released 2026-11-10); later releases are under *After v0.1.0*. Each phase lands as its own
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

## Phase 3 — Accounts and sign-in (done)

- `server.mode: accounts`: users with argon2id-hashed passwords, sign-in with sessions (HttpOnly, SameSite=Strict
  cookies), CSRF tokens, sign-in rate limits and lockout, per-user API tokens (stored hashed, shown once).
- Roles: owner, editor, viewer. `browser-foreman user {add,list,passwd,remove}` and `token {create,list,revoke}`.
- `local` mode keeps today's behaviour: one owner on this computer, loopback only, no sign-in.
- `/api/v1/events` needs a viewer, the same rule as reading bookmarks, so the phase 4 bookmark events are never public.
- Not in this phase: a sign-in page in the web interface (phase 4, with the rest of the interface), and ending a
  person's other sessions when their password changes (phase 4).

## Phase 4 — Web interface and REST API parity (done)

- Done: the web interface embedded and served at `/`, with a sign-in page; bookmarks, folders, the Trash, history
  (undo, redo, rollback), import and export, all as `/api/v1` routes with an event for every change; the OpenAPI
  document.
- Done: link checks (`/api/v1/linkchecks`), shown in the web interface.
- Done: the webhook delivery log, read-only settings, backup and restore, the workspace menu (rename, notes, lock,
  archive, History), and a new password ending the person's other sessions.
- Left for later phases: the Open tabs view and the Browsers menu (phase 5); saving tabs as bookmarks (phase 5).
- The web interface (in `web/`) embedded in the binary and served at `/`.
- Every capability under `/api/v1`: bookmarks and folders, workspaces and history (undo, redo, rollback), link checks
  and refreshes, the Trash, import and export (Netscape HTML, Chrome JSON), backups and restore, settings.
- Every change published as an event (`bookmark.created`, `bookmark.moved`, `workspace.committed`, `trash.emptied`,
  `tabs.snapshot.saved`, …) and therefore available to webhooks.
- An OpenAPI document at `/api/v1/openapi.json`.

## Phase 5 — Browsers (done)

- Installed browsers and the system default, opening links in one, some or all of them, real browser icons.
- Reading open tabs (macOS first), snapshots on change or on a schedule, restoring a session after a crash,
  incognito handled privately.
- Scheduled link re-checks.
- Done on macOS, Linux and Windows (Linux and Windows read tabs from session files; live tabs come from the
  extension everywhere). Ported from the Python tool: notes and tags on tabs and windows, deleting snapshots, purging
  private pages, reading browser history (with Full Disk Access on macOS) and every profile's bookmarks file.

## Phase 6 — Browser plugins (done)

- One WebExtension source, built for Chromium browsers (Manifest V3), Firefox and Safari.
- Live tabs and tab groups, bookmark editing in place (no import or export round trip), all through the local API with
  a token the extension is paired with.

- Done: pairing, tabs and tab groups, commands, reading and writing bookmarks in place, icons, and opening a folder
  as a tab group from the web interface. Not yet: store listings (Chrome Web Store, Firefox Add-ons, App Store),
  which need the owner's developer accounts.

## Beyond the plan (done)

Added while the phases above were built, each with tests and docs:

- Merging several browsers into one workspace, finding look-alike duplicates, filing bookmarks by rules, and
  [write-back](tools/write-back.md) into a Chromium profile, from the command line or the page, previewed first.
- [Labels](tools/labels.md): ratings, tags and when each bookmark was last opened; [page snapshots](tools/page-snapshots.md).
- [Running at login](tools/service.md) (launchd, systemd, a Windows logon task) and a notification when a scheduled
  re-check finds links newly dead or fixed.
- The [MCP server](tools/mcp.md): health, duplicates and statistics to read; labelling and adding bookmarks behind
  write scopes.
- [AI jobs](tools/ai.md): tag suggestions and duplicate verdicts by Anthropic's or OpenAI's API, a local Ollama or an
  assistant's own model, queued so they survive a restart.
- Smart folders (saved searches) and a page that stays responsive with 50,000 bookmarks.
- The browser extension found and connected from the page in one click, one connected copy per browser, and
  *Set it up* opening in the very browser copy the page is in.

## Phase 7 — Landing page and release (done, except publishing the site)

- Done: the landing page in `site/` with real screenshots (`make screenshots`, [screenshots](screenshots.md)); a full
  documentation pass, kept in step with the code by tests that fail on an undocumented route or event; lint clean,
  and linted in CI.
- Done: the public repository `simtabi/browser-foreman`, with CI on every pull request, `main` protected (a pull
  request with the `go` and `web` checks green) and release tags locked; v0.1.0-rc.1, then v0.1.0, published on the
  GitHub release with notes, checksums and build provenance.
- Left: publishing the landing page and the hosted docs at the product URL; the extensions' store listings (phase 6).

## After v0.1.0

- v0.2.0: Settings › *Server settings* redesigned, with AI as one nested choice and the model picked from the
  provider's own list (`GET /api/v1/ai/models`); `make check` lints at CI's version.
- Next release (in `CHANGELOG.md` under *Unreleased*): a security and reliability audit; two-way sync between a
  browser and its workspace; *Save this page* and a dead-page badge in the extension; a `postgres` storage driver;
  link results shown while a check runs; `webhook` and `doctor` commands; blank snapshots taken again.
- Left after it: true windowing of the bookmark list for libraries past about 50,000 bookmarks (measured: a click deep
  in a fully expanded list can take 0.6 to 1.8 s); the extensions' store listings; the hosted docs.

---
[← Docs index](../README.md#documentation)
