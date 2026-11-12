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
  check, security headers; writes need `Authorization: Bearer <token>`.
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
