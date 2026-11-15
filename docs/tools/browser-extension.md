# Browser extension

One extension, built from one source for Chromium browsers (Chrome, Brave, Edge, Vivaldi, Opera, Arc), Firefox and
Safari. It connects the browser to Browser Foreman on this computer: its tabs and tab groups, its bookmarks read
straight in, and a workspace written back into its bookmarks.

## Build and install

```bash
make extension
```

That builds `web/dist/extension/chromium`, `firefox` and `safari`, each with its icons (the web interface's logo
mark, drawn at 16, 32, 48 and 128 pixels by the build). Installing is yours to do, in each browser; Browser Foreman
helps. In the Browsers menu › *Browser extensions…*, **Set it up** on a browser's card opens its extensions page and
shows the built folder in Finder, so what is left is Developer mode and *Load unpacked* (or *Load Temporary
Add-on…*). The card turns to *Connected* by itself once the extension connects. A browser cannot be made to install
an unpacked extension silently: Chrome no longer honours `--load-extension`, and forcing one in needs a managed
(MDM) policy, which Browser Foreman never sets.

| Browser | Load it |
|---|---|
| Chrome, Brave, Edge, Vivaldi, Opera, Arc | `chrome://extensions` › Developer mode › Load unpacked › `web/dist/extension/chromium` |
| Firefox | `about:debugging` › This Firefox › Load Temporary Add-on › `web/dist/extension/firefox/manifest.json`; allow it to reach `127.0.0.1` when asked |
| Safari | `make extension-safari` (needs Xcode), then build and run the project in `web/dist/extension/safari-xcode`, and turn it on in Safari › Settings › Extensions |

To see private windows, allow the extension in private browsing (Chrome: *Allow in Incognito*). Their pages stay out
of snapshots unless you choose otherwise, as with every other source.

## Connect it

In one click: press **Connect** in the extension's popup. It shows four letters, and Browser Foreman asks, wherever
you are in it, whether to connect that browser, showing the same four letters. Approve only if they match. The
request waits five minutes; at most five wait at once; only an extension can ask (a web page or another program
cannot), and nothing connects unless the owner approves.

Or make a one-time code, and type it into the popup (*Use a pairing code instead*) with the server's address:

```bash
browser-foreman pair
```

Or, in the web interface, the Browsers menu (top bar) › *Browser extensions…*, which also lists the supported
browsers, whether each is on this computer and connected, and how to install the extension in it (signed in as an owner in `accounts` mode). A code
works once, for ten minutes, and five wrong codes lock pairing for fifteen minutes. The extension gets its own API
token, named `extension:<browser>`, listed under *Browser extensions…* and in `GET /api/v1/tokens`; disconnecting
revokes it. The extension only talks to a server on this computer (`127.0.0.1` or `localhost`).

### What an extension's token can do

Only what an extension needs: its own routes (below), the workspace list, and one workspace, to write it back. With
it, nothing else in the API answers (settings, tokens, editing, history, the event stream), whatever its owner's
role. It reads bookmarks only into `<browser>-live`, `<browser>-before-write`, or a workspace already read from that
same browser by its extension. No other token can be named `extension:…`.

## Keeping it current

Each extension reports its version with its tabs. A card whose extension is older than the one built with this
Browser Foreman says so, and **Update** asks the extension to reload itself: an unpacked extension reads its folder
again, so a fresh `make extension` takes effect without visiting the browser's extensions page.

## What it does

- **Tabs and tab groups.** It sends its windows, tabs and tab groups whenever they change and once a minute. For two
  minutes after each report, that browser is not read through automation, and browsers automation cannot read
  (Firefox) appear too.
- **Commands.** The server can ask it to open links, as tabs or as one named tab group
  (`POST /api/v1/extension/commands`, owner). Where a browser has no tab groups, the links open in a new window. In
  the web interface, opening a folder's links *one window per folder* opens each folder as a tab group named after
  it in a browser whose extension is connected, and as a window of its own elsewhere.
- **Read bookmarks.** *Read this browser's bookmarks into Browser Foreman* takes the browser's whole bookmark tree
  into a workspace (`<browser>-live`), as one history entry. Each item keeps the browser's own id. The server can ask
  for the same read (`POST /api/v1/extension/commands` with `"kind": "read-bookmarks"`), which a
  [write-back](write-back.md) dry run does to see the browser as it is now.
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
| `POST /api/v1/extension/requests` | an extension's origin | ask to connect in one click: an id, a poll secret, a match code |
| `POST /api/v1/extension/requests/{id}/poll` | the asker (poll secret) | the answer; approved, the token, once |
| `GET /api/v1/extension/requests` | owner | the requests waiting, with their match codes |
| `POST /api/v1/extension/requests/{id}/approve`, `…/deny` | owner | answer one |
| `GET /api/v1/extension/guide` | owner | each installed browser's build, whether it is built, its extensions page; each extension's version |
| `POST /api/v1/extension/setup` | owner | open a browser's extensions page and show the build in Finder |
| `GET /api/v1/extension/status` | viewer | the connected extensions |
| `POST /api/v1/extension/tabs` | the extension | its windows, tabs and groups |
| `POST /api/v1/extension/bookmarks` | the extension (editor) | its bookmark tree, into a workspace |
| `GET /api/v1/extension/commands?wait=25` | the extension | commands for its browser (long poll) |
| `POST /api/v1/extension/commands/{id}/done` | the extension | what became of one |
| `POST /api/v1/extension/commands` | owner | ask a browser to open links or a tab group, or to reload its extension (`"kind": "reload"`); at most 100 wait per browser, and each is forgotten after an hour |
| `GET /api/v1/extension/commands/{id}` | viewer | a command and its state |

---
[← Docs index](../../README.md#documentation)
