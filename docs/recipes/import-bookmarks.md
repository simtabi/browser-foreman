# Import bookmarks

Read a browser's bookmarks into a new workspace. The format is detected from the file.

| Browser | File to import |
|---|---|
| Chrome, Edge, Brave, Vivaldi, Opera, Arc | the profile's `Bookmarks` file (JSON), or an exported HTML file |
| Firefox | the profile's `places.sqlite` (read through a copy), or an exported HTML file |
| Safari | `~/Library/Safari/Bookmarks.plist` (needs Full Disk Access on macOS), or an exported HTML file |
| Browser Foreman (or the Python tool it grew from) | its downloaded import file (HTML) |

On macOS, Chrome's file is at `~/Library/Application Support/Google/Chrome/Default/Bookmarks`:

```bash
browser-foreman import ~/Library/Application\ Support/Google/Chrome/Default/Bookmarks --workspace chrome --name "Chrome"
```

```text
workspace chrome: Imported 6693 bookmarks in 85 folders from Bookmarks (chrome-json) (entry 1)
```

An exported HTML file works the same way, and keeps descriptions:

```bash
browser-foreman import ~/Downloads/bookmarks.html --workspace firefox
```

Chrome, Edge and the other Chromium browsers export their *Other bookmarks* loose at the top of the file, beside the
bookmarks bar. Browser Foreman files them in an *Other bookmarks* folder, where the browser keeps them, so write-back
puts them back in that root. Firefox's loose items are its bookmarks menu, and a file without a bookmarks bar is laid
out by whoever made it: both keep their top level as it is.

Every import creates a new workspace (`--workspace` must be new), so importing never changes one you already have.
To combine several files into one workspace without duplicates, see [merge bookmarks](merge-bookmarks.md).
See them all with `browser-foreman workspace list`; storage and history are described in [storage](../tools/storage.md).

---
[← Docs index](../../README.md#documentation)
