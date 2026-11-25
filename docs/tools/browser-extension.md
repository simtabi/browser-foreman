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

By itself: once loaded, the extension finds Browser Foreman on `127.0.0.1:8765` and asks to connect within a
minute, with four letters on its toolbar icon. Browser Foreman asks you, wherever you are in it, showing the same four
letters; approve only if they match. It stops asking by itself after you deny it or disconnect it, or after three
requests nobody answered; **Connect** in its popup asks again at any time (and shows the letters there too). The
request waits five minutes; at most five wait at once; only an extension can ask (a web page or another program
cannot), and nothing connects unless the owner approves.

**From the page, in one click** (0.1.6 and later): open Browser Foreman in the browser the extension is in, then
*Browsers › Browser extensions…*. The page asks that browser's extension directly whether it is installed and
connected, every few seconds while the dialog is open, so the row says *Extension 0.1.6 installed · ready to
connect* as soon as it is. **Connect** has the extension ask this server, and the page approves that very request:
no letters to compare, because both ends are in the same browser. This also reconnects an extension that was
disconnected or denied, which does not ask again by itself. Installed or reloaded the extension after opening the
page? Reload the page: they find each other when the page loads.

An unconnected extension (0.1.9 and later) connects this way at once only to its usual server: the one it last used,
or `127.0.0.1:8765`. A page at any other address on this computer, such as Browser Foreman moved to the next free
port, or some other program, puts its address in the extension's popup and shows `?` on the icon instead; press
**Connect** there if it is yours.

How the page reaches the extension: a small content script on pages from this computer (`127.0.0.1`, `localhost`,
already in the extension's permissions) passes two questions, *are you here?* and *connect to me*, to the
extension. The extension checks the page's origin as the browser reports it, answers only pages on this computer,
and connects only to the server it already uses, or, while it is not connected, to the asking page's server. So a
page on another port cannot take a connected extension away, and no page can reach the popup's own actions
(pairing codes, writes, disconnecting).

**One connected copy per browser** (0.1.7 and later). Two copies can call themselves the same browser on one
computer: your own Chrome, and another Chrome (one an assistant drives, say). Each copy makes an id of its own when
it is installed, and the server connects one copy per browser name:

- approving a copy (or pairing it with a code) pins it, saved with the server's settings, and revokes the other copies'
  keys; a revoked copy is told so and stops asking by itself, so the two never race for one identity;
- a request from a second copy says so, *Google Chrome (copy b2c4) asks to connect*, with a warning that approving
  it disconnects the copy connected now;
- *Browser extensions…* shows which copy is connected and, when the page runs in another copy of that browser,
  **Use this browser** moves it to the copy the page is open in;
- while one copy is connected, another copy does not ask by itself (the server refuses it, and it stops); pressing
  Connect in that copy's popup, or **Use this browser**, still asks, on purpose;
- with strict approvals, a request prompts only in Browser Foreman open in the very copy that asks, not in every
  page of a browser with the same name.

**Which running copy** (macOS). Two copies of Chrome both answer to *Google Chrome*, so asking the system to open
Chrome picks one of them, often not the one you are looking at. The server instead finds the copy behind a page's
connection (the process holding it, then its parents up to the browser's main process) and:

- **Set it up** opens the extensions page as a new tab in the window of the copy this page is open in, by handing
  the address to that copy (its own executable, with its own profile folder); a page in another browser opens it in
  the one copy of that browser you started, and only when that cannot be told does the system pick;
- the setup guide lists every running copy (app folder, automated or not, its own profile or not) when there are
  several, and says which one the page is in: each copy has extensions of its own, so the extension goes in that one;
- a request to connect says which copy it came from, and whether that is the copy this page is open in.

Many windows of one copy are one browser: the extension and the page bridge work the same in each.

Reloading or updating the extension cuts it off from pages already open; *Browser extensions…* then says to reload
the page, with a button.

The prompt offers **Connect**, **Deny** and **Not now**. Not now (or Escape) decides nothing: the request keeps
waiting, the prompt does not come back for it, and you can still approve it in *Browser extensions…* or from a
terminal until it expires.

Which page may approve is a setting, Settings › Browser extensions › *Approve a connecting browser*:

- **Strict** (the default): a browser is approved only from Browser Foreman open in that same browser, so Chrome
  never asks you about Brave. In other browsers the request still shows in *Browser extensions…*, where it can be
  denied but not approved.
- **Loose**: from Browser Foreman open in any browser.

The terminal (`browser-foreman pair --approve`) works in either mode.

**Disconnect…** in a browser's row asks first, then revokes the extension's key at once. The extension stops
sending its tabs, cannot read or write bookmarks, and does not ask to connect again until Connect is pressed in
its popup.

The request also shows in the terminal running `browser-foreman serve`, with its letters, so it is seen when no
Browser Foreman page is open. Approve it from a terminal by typing the letters the popup shows:

```bash
browser-foreman pair --approve
```

`--match KXQT` gives the letters instead of typing them. A request is approved only when the letters match it.

Or make a one-time code, and type it into the popup (*Use a pairing code instead*) with the server's address:

```bash
browser-foreman pair
```

Or, in the web interface, the Browsers menu (top bar) › *Browser extensions…* (signed in as an owner in `accounts`
mode). It lists each browser on this computer with where it stands, and a setup guide that follows it live: built,
loaded, asking to connect (with the letters and Connect and Deny right there), connected, sending its tabs, and up to
date. When a step stalls it says what is most likely wrong, such as Developer mode being off, the wrong folder chosen,
an *Errors* button on the extension's card, Browser Foreman on another port, or a browser that has gone quiet. A code
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
- **Save this page.** The popup's *Save this page* adds the page the tab shows to a workspace you choose, in a top
  folder named *Saved from <browser>* (made the first time), with a picture of the tab as its snapshot. It only adds:
  an address the workspace already holds is left where it is, and nothing else in the workspace changes. The
  browser's own pages cannot be pictured, and are saved without one. Pressing the toolbar button is what lets the
  extension picture the tab (the `activeTab` permission, from 0.1.10).
- **A badge on dead pages.** On a page Browser Foreman has confirmed dead (two checks a day apart), the toolbar
  button shows a red `!`. The extension asks the server about the page each tab shows; the server already receives
  those addresses with the tab report.

## Routes

| Route | Who | Does |
|---|---|---|
| `POST /api/v1/extension/pairings` | owner | a one-time pairing code |
| `POST /api/v1/extension/pair` | anyone with a code | trade it for the extension's token |
| `POST /api/v1/extension/requests` | an extension's origin, from this computer | ask to connect in one click: an id, a poll secret, a match code (an extension on another computer pairs with a code instead) |
| `POST /api/v1/extension/requests/{id}/poll` | the asker (poll secret) | the answer; approved, the token, once |
| `GET /api/v1/extension/requests` | owner | the requests waiting, with their match codes |
| `POST /api/v1/extension/requests/{id}/approve`, `POST /api/v1/extension/requests/{id}/deny` | owner | answer one |
| `GET /api/v1/extension/guide` | owner | each installed browser's build, whether it is built, its extensions page; each extension's version |
| `POST /api/v1/extension/setup` | owner | open a browser's extensions page and show the build in Finder |
| `GET /api/v1/extension/status` | viewer | the connected extensions |
| `POST /api/v1/extension/tabs` | the extension | its windows, tabs and groups |
| `POST /api/v1/extension/bookmarks` | the extension (editor) | its bookmark tree, into a workspace |
| `GET /api/v1/extension/commands?wait=25` | the extension | commands for its browser (long poll) |
| `POST /api/v1/extension/commands/{id}/done` | the extension | what became of one |
| `POST /api/v1/extension/pages` | the extension | body `{workspace, url, title, picture?}` (a JPEG or PNG data URL up to 8 MB): add the page to *Saved from <browser>*; `201` with the entry, or `200 {"already": true, "node"}` |
| `GET /api/v1/extension/link?url=` | the extension | `{status, confirmed, archive, checked}` for one address, `{"status": "unchecked"}` when never checked |
| `POST /api/v1/extension/commands` | owner | ask a browser to open links or a tab group, or to reload its extension (`"kind": "reload"`); at most 100 wait per browser, and each is forgotten after an hour |
| `GET /api/v1/extension/commands/{id}` | viewer | a command and its state |

---
[← Docs index](../../README.md#documentation)
