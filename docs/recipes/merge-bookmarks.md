# Merge bookmarks from several browsers

Combine bookmarks files (and other workspaces) into one workspace, keeping one copy of each page. Every other copy goes
to the workspace's Trash, where it can be put back, so nothing is lost.

## Which copy is kept

Two links are copies when they open the same page: the host is compared without `www.` and in lower case, a trailing
slash is ignored, and tracking parameters (`utm_*`, `fbclid`, `gclid`, `mc_*`, `ref_src`, `igshid`, `ref`) and
text-highlight fragments (`#:~:text=`) are dropped. Any other `#…` counts, since app routes like `#/devices` are other
pages.

- A bookmark already in the workspace always wins: adding a source never moves what you are cleaning.
- Among new copies, the one in the deepest folder wins (a filed link beats a loose one); then the earliest added.
- The kept copy takes the earliest date added of all its copies, and a real title when it had only its address.

## From the command line

```bash
browser-foreman merge ~/Downloads/chrome.html ~/Downloads/firefox.html --workspace all --name "All browsers"
browser-foreman merge ~/Downloads/firefox.html --workspace all
```

```text
workspace all: Added 5414 bookmarks from chrome.html, firefox.html; 2 duplicate copies went to the Trash (entry 1)
workspace all: Added 0 bookmarks from firefox.html; 2 duplicate copies went to the Trash (entry 2)
```

The workspace is made when it does not exist. `--separate` puts each file's folders under a folder named after it,
instead of joining folders with the same path.

## In the web interface

The arrow beside Download holds **Import bookmarks…** (into a new workspace) and **Add to this workspace…**. Both take
several files at once, can add another workspace's bookmarks too, and offer the same "keep each file's folders apart"
choice. **Merged duplicates (CSV)** in the same menu lists every copy and the one kept for it; **Show › Removed
duplicates** lists them in the page.

## Through the API

```bash
curl -X POST "http://127.0.0.1:8765/api/v1/workspaces/all/sources?from=chrome" \
  -H "Authorization: Bearer $BROFO_API_TOKEN" -F file=@firefox.html -F file=@Bookmarks
curl "http://127.0.0.1:8765/api/v1/workspaces/all/duplicates.csv" -H "Authorization: Bearer $BROFO_API_TOKEN"
```

Each merge is one history entry, so undo takes the whole merge back.

## Update from the sources

A workspace remembers where its bookmarks came from: each file merged in (a copy is kept) and each workspace merged
in. **Update from source…** in the workspace menu (or `POST /api/v1/workspaces/{id}/update`) reads them again and
shows what changed before applying it:

- bookmarks new upstream are added, by the same rule as a merge;
- a title changed upstream is taken, unless you renamed that bookmark here;
- bookmarks no longer upstream are listed, and stay until you trash them.

A kept file never changes, so Update matters for workspaces merged in, which keep changing.

---
[← Docs index](../../README.md#documentation)
