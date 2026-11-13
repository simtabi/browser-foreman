# Browser extension

One extension, built from one source for Chromium browsers (Chrome, Brave, Edge, Vivaldi, Opera, Arc), Firefox and
Safari. It connects the browser to Browser Foreman on this computer: its tabs and tab groups, its bookmarks read
straight in, and a workspace written back into its bookmarks.

## Build and install

```bash
make extension
```

That builds `web/dist/extension/chromium`, `firefox` and `safari`, each with its icons (the web interface's logo
mark, drawn at 16, 32, 48 and 128 pixels by the build). Installing is yours to do, in each browser:

| Browser | Load it |
|---|---|
| Chrome, Brave, Edge, Vivaldi, Opera, Arc | `chrome://extensions` › Developer mode › Load unpacked › `web/dist/extension/chromium` |
| Firefox | `about:debugging` › This Firefox › Load Temporary Add-on › `web/dist/extension/firefox/manifest.json`; allow it to reach `127.0.0.1` when asked |
| Safari | `make extension-safari` (needs Xcode), then build and run the project in `web/dist/extension/safari-xcode`, and turn it on in Safari › Settings › Extensions |

To see private windows, allow the extension in private browsing (Chrome: *Allow in Incognito*). Their pages stay out
of snapshots unless you choose otherwise, as with every other source.

## Connect it

Make a one-time code, then type it into the extension's popup with the server's address:

```bash
browser-foreman pair
```

Or, in the web interface, the workspace menu › *Browser extension…* (signed in as an owner in `accounts` mode). A code
works once, for ten minutes, and five wrong codes lock pairing for fifteen minutes. The extension gets its own API
token, named `extension:<browser>`, listed under *Browser extension…* and in `GET /api/v1/tokens`; disconnecting
revokes it. The extension only talks to a server on this computer (`127.0.0.1` or `localhost`).

## What it does

- **Tabs and tab groups.** It sends its windows, tabs and tab groups whenever they change and once a minute. For two
  minutes after each report, that browser is not read through automation, and browsers automation cannot read
  (Firefox) appear too.
- **Commands.** The server can ask it to open links, as tabs or as one named tab group
  (`POST /api/v1/extension/commands`, owner). Where a browser has no tab groups, the links open in a new window. In
  the web interface, opening a folder's links *one window per folder* opens each folder as a tab group named after
  it in a browser whose extension is connected, and as a window of its own elsewhere.
- **Read bookmarks.** *Read this browser's bookmarks into Browser Foreman* takes the browser's whole bookmark tree
  into a workspace (`<browser>-live`), as one history entry. Each item keeps the browser's own id.
- **Write a workspace back.** Choose a workspace read from this browser and edited, see exactly what would change
  (added, renamed, moved, removed), and confirm. The bookmarks as they are first go to Browser Foreman
  (`<browser>-before-write`), the changes are made, any order a browser placed differently is fixed, and the result is
  read back so the next write matches item for item. A workspace that was not read from this browser is refused,
  because writing it would remove everything; the browser's own top folders are never moved or removed.

## Routes

| Route | Who | Does |
|---|---|---|
| `POST /api/v1/extension/pairings` | owner | a one-time pairing code |
| `POST /api/v1/extension/pair` | anyone with a code | trade it for the extension's token |
| `GET /api/v1/extension/status` | viewer | the connected extensions |
| `POST /api/v1/extension/tabs` | the extension | its windows, tabs and groups |
| `POST /api/v1/extension/bookmarks` | the extension (editor) | its bookmark tree, into a workspace |
| `GET /api/v1/extension/commands?wait=25` | the extension | commands for its browser (long poll) |
| `POST /api/v1/extension/commands/{id}/done` | the extension | what became of one |
| `POST /api/v1/extension/commands` | owner | ask a browser to open links or a tab group |
| `GET /api/v1/extension/commands/{id}` | viewer | a command and its state |

---
[← Docs index](../../README.md#documentation)
