# Page snapshots

A picture of each bookmarked page, taken by Chrome or Chromium running headless, shown on its row, its card and in its
details.

## How a snapshot is taken

*Refresh* (`R`) on a bookmark or folder, and *Refresh all*, check the links and take snapshots of the pages that have
none (or of the chosen ones again). Each page is drawn in a fresh, throwaway browser profile, so no account, cookie or
password of yours is ever used, and only pages on the public internet are drawn: an address on this computer or the
local network is refused before the browser starts. The picture kept is a small JPEG, half the size of the window.
Site icons are kept the same way, from the bookmark file or the page.

| Route | Role | Does |
|---|---|---|
| `POST /api/v1/snapshots` | editor | body `{workspace, nodes?, force?}`: snapshots in the background; `202 {"job"}` |
| `GET /api/v1/snapshots/{id}` | viewer | the job's progress |
| `POST /api/v1/snapshots/{id}/cancel` | editor | stops it |
| `GET /thumbs/{name}`, `GET /favicons/{name}` | viewer | the pictures |

## Settings

Settings › Page snapshots (owner only; kept on the server, so every browser and the command line use the same):

| Setting | Preference | Default | Range |
|---|---|---|---|
| Window size | `snapshot_width`, `snapshot_height` | 1280 × 800 | 640–2560 by 400–4800; *Tall* (1280 × 2400) shows more of a long page |
| Wait before the picture | `snapshot_wait_seconds` | 8 | 1–30 seconds; longer suits slow or script-heavy sites |
| Folder | `snapshot_dir` | the state folder's `media/` | a full path; pictures already taken stay where they are |

A full-page capture is not offered: headless Chrome's `--screenshot` draws the window only, so the *Tall* size is
the way to see more of a page.

---
[← Docs index](../../README.md#documentation)
