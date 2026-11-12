# Import bookmarks

Read a browser's bookmarks into a new workspace. The format is detected from the file.

| Browser | File to import |
|---|---|
| Chrome, Edge, Brave, Vivaldi, Opera, Arc | the profile's `Bookmarks` file (JSON), or an exported HTML file |
| Firefox, Safari | an exported bookmarks HTML file (Bookmarks › Export) |
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

Every import creates a new workspace (`--workspace` must be new), so importing never changes one you already have.
See them all with `browser-foreman workspace list`; storage and history are described in [storage](../tools/storage.md).

---
[← Docs index](../../README.md#documentation)
