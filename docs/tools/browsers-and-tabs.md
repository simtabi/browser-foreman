# Browsers and tabs

Browser Foreman knows the browsers on this computer, opens links in the ones you choose, and keeps snapshots of your
open tabs so a crash never loses a session. macOS first; on other systems the server runs and these routes answer
that the feature is not supported there yet.

## Browsers

`GET /api/v1/browsers` lists the installed browsers it knows (Chrome, Chrome Canary, Chromium, Brave, Edge, Vivaldi,
Opera, Arc, Safari, Firefox, Zen, Orion), the system's default browser, and the ones you chose to open links in.
Each browser's own app icon is at `GET /api/v1/browsers/icon?name=Safari`.

## Opening links

`POST /api/v1/open` (owner role, since it starts programs on this computer):

```bash
curl -s -X POST http://127.0.0.1:8765/api/v1/open -H "Authorization: Bearer $BROFO_API_TOKEN" \
  -d '{"urls": ["https://go.dev/"], "how": "window", "browsers": ["Safari", "Google Chrome"]}'
```

| `how` | Opens |
|---|---|
| `tabs` | as tabs in a window the browser already has |
| `window` | in a new window (Chromium and Firefox; Safari opens tabs) |
| `incognito` | in a new private window, with the browser's own launch option; a browser with none (Safari, Arc, Orion) is refused rather than opening the link in a normal window |

With no `browsers`, links go where the preferences say (`open_with`, the system default unless you chose
otherwise). Only `http` and `https` links open, at most 200 at once, and each browser answers on its own. Events name
how many links opened, never which.

## Open tabs and snapshots

| Route | Role | Does |
|---|---|---|
| `GET /api/v1/tabs` | viewer | every tracked browser's windows and tabs now |
| `POST /api/v1/tabs/snapshots` | editor | store a snapshot now; an unchanged session is not stored again unless `force` |
| `GET /api/v1/tabs/snapshots` | viewer | snapshots newest first, with browsers and counts, no pages; older than `keep_days` only with `?all=1` |
| `GET /api/v1/tabs/snapshots/{id}` | viewer | one snapshot with its pages |
| `POST /api/v1/tabs/snapshots/{id}/restore` | owner | reopen its windows, each in a new window of its browser (`{"windows": [0, 2]}` for some) |
| `POST /api/v1/tabs/snapshots/delete` | owner | delete snapshots for good: `{"ids": [...]}`, `{"before": "2026-11-01"}` or `{"everything": true, "confirm": "DELETE ALL"}`; notes stay |
| `POST /api/v1/tabs/purge` | owner | forget private windows: `{"level": "pages"}` keeps their counts, `"windows"` removes them; `older_than_hours` limits it to older snapshots |
| `GET /api/v1/tabs/notes` | viewer | every note and tag on tabs (by address), windows and snapshots |
| `PUT /api/v1/tabs/notes` | editor | `{"kind": "tab", "key": "https://…", "note": "…", "tags": ["work"]}`; an empty note with no tags removes it |
| `POST /api/v1/tabs/close` | owner | close tabs, after a snapshot that brings them back; a tab that changed since it was read stays open |
| `GET /api/v1/tabs/timeline` | viewer | the last `?days` of snapshots with what opened and closed at each |
| `POST /api/v1/tabs/ages` | viewer | when each address was first seen open |

- **Reading never starts a browser.** A browser that is not running answers `running: false`.
- **Permission.** The first read of each browser makes macOS ask whether Browser Foreman may control it; until you
  allow it (System Settings › Privacy & Security › Automation) that browser answers `needs-permission`.
- **Private windows stay private.** Their pages are not read into snapshots, the timeline or the event stream: only
  how many tabs they had. Turn on `include_incognito` to keep them.
- **Autosave** stores a snapshot whenever the session changes (checked every `watch_seconds`) or every
  `interval_seconds`, as the preferences say. Snapshots older than `keep_days` leave the lists and are kept.
- Firefox, Zen and Orion do not let other programs read their tabs, so they can open links but are not tracked.

## Preferences

`GET /api/v1/preferences` and `PUT /api/v1/preferences` (owner):

| Key | Default | Meaning |
|---|---|---|
| `open_with` | `["System default"]` | the browsers links open in |
| `tracked` | every readable one | the browsers whose tabs are read |
| `autosave`, `save_mode` | `true`, `change` | snapshots on change, or every `interval_seconds` (`interval`) |
| `interval_seconds`, `watch_seconds` | 300, 15 | 60–86400; 5–3600 |
| `include_incognito` | `false` | keep private windows' pages |
| `incognito_purge_hours` | `0` | after each save, remove the pages of private windows older than this (counts stay); `0` never |
| `keep_days` | 14 | 1–3650 |
| `recheck_days` | 7 | re-check links whose last check is older than this; 0 is off |

Scheduled re-checks only visit links that were checked before: a link never checked waits for a check you start, so
the first run of the server never starts thousands of requests on its own.

---
[← Docs index](../../README.md#documentation)
