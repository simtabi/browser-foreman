# Browser history

Browser Foreman reads your browsers' visit history so you can find pages you visited but never bookmarked, and save
them. It reads Chromium browsers (Chrome, Edge, Brave, Arc, Vivaldi, Chromium), Firefox and Safari, every profile, on
macOS, Linux and Windows.

## What it reads, and what it keeps

History is read only when you ask (Browser history… in the web interface, or `GET /api/v1/history`), each database
through a private copy, so a running browser is never disturbed. Nothing read is stored, cached or sent as an event
or webhook: it stays your browser's. Only owners can read it.

## Full Disk Access on macOS

macOS keeps every browser's history behind Full Disk Access. Until the app that runs Browser Foreman has it, those
browsers are listed as blocked and left out. Settings › Browser history, and the Browser history window, show which
browsers can be read and open System Settings at the right place. Add the app there (the terminal you start
`browser-foreman serve` from, or the app that starts it), quit and reopen it, then check again.

## Routes

| Route | Role | What it does |
|---|---|---|
| `GET /api/v1/history/access` | owner | each profile's history file and whether it can be read; reads one byte of each |
| `POST /api/v1/history/access/open-settings` | owner | open System Settings at Full Disk Access (macOS) |
| `GET /api/v1/history` | owner | pages visited, newest first, one row per address across browsers: `q`, `days` (30), `browser`, `limit` (300); `workspace` marks each `bookmarked` or not |

---

[← Docs index](../../README.md#documentation)
