# Write-back

`browser-foreman apply` writes a cleaned workspace back into a Chromium profile (Chrome, Brave, Edge, Vivaldi,
Chromium) that kept changing after you exported it. It is a dry run unless you add `--yes`.

## How the plan is made

The write is a three-way merge of three trees:

- **base**: the export the cleanup started from (`--base`, a workspace);
- **ours**: the cleaned workspace (`--workspace`, `main` by default);
- **theirs**: the profile as it is now.

Bookmarks match by address (ignoring `www.`, a trailing slash and tracking parameters). A bookmark whose address
the cleanup changed, such as a link that redirected, matches by its title. Folders match by path, or by where most
of their bookmarks are, which finds a folder the cleanup moved. The roots match by role, and anything loose at the
top of the export belongs to *Other bookmarks*. The result keeps the profile's own ids, so the write moves and renames
bookmarks instead of deleting and recreating them. Titles that differ only in spacing keep the profile's spacing.

| Mode | The cleanup | What the profile changed after the export |
|---|---|---|
| `merge` | applied | Bookmarks it added stay, in their folders. Ones it deleted stay deleted. Its new titles stay unless the cleanup renamed the same bookmark. Its moves stay unless the cleanup moved the same bookmark. |
| `replace` | applied | Bookmarks it added are dropped, once you confirm their number with `--drop-new`. Ones it deleted stay deleted. |

*Mobile bookmarks* is never changed. Notes, descriptions, ratings and tags have no place in a browser and stay in
Browser Foreman; the report counts them.

## Routes

| Route | When | What it does |
|---|---|---|
| `file` | the browser is quit | Writes the profile's `Bookmarks` file with Chromium's checksum. It refuses while the browser runs (its `SingletonLock`), and for a profile that syncs its bookmarks unless you add `--sync-risk-ok`: Sync can overwrite the file, or pass it on to every device. |
| `extension` | the browser runs, or the profile syncs | Plans against `<browser>-live`, the workspace the [extension](browser-extension.md) read. With `--yes` it saves the result as `<browser>-target` and, while Browser Foreman runs, has the extension write it: the extension first checks the browser still matches the plan (a fingerprint of its bookmarks) and refuses if not, saves the bookmarks as they are to `<browser>-before-write`, writes, and reads the result back. `apply` then reads the browser again and plans against it: an empty plan is the verification. If the profile syncs, the change reaches every device signed in to it. Without a running server, write `<browser>-target` from the extension's popup. |

The route that is safe for the profile is offered first, and the file route is not offered when the profile's file
cannot be read.

## Reading the profile now

The file route always reads the file it is about to write. The extension route can read the profile either way
(`--read`):

| Read | How | Needs |
|---|---|---|
| `extension` | The server asks the browser's extension to read its bookmarks into `<browser>-live` now, and waits up to two minutes. If Browser Foreman is not running, the bookmarks the extension read last are used instead, and the dry run says when they were read. | The extension installed and connected. |
| `file` | The profile's `Bookmarks` file is read, with the ids the extension sees. | Access to the profile's folder (see below). |

## Choosing

Each choice comes from its flag, else from the configuration (`writeback.mode`, `writeback.route` and
`writeback.read`), unless the setting is `ask`, which is the default. A choice still open is asked on a terminal,
and is an error everywhere else. The profile is asked for unless there is only one, and `Default` is listed first.

## Writing it

```bash
browser-foreman apply --base original-2026-11-10 --mode merge --route file
browser-foreman apply --base original-2026-11-10 --mode merge --route file --yes --fingerprint 3f1c…
```

The dry run prints:

- what the write changes: bookmarks added, removed, moved, and renamed or given a new address;
- what the profile changed after the export;
- any conflicts, which the cleanup wins;
- for `replace`, how many bookmarks it drops;
- the profile's fingerprint.

On a terminal it then asks whether to write. Elsewhere, pass `--yes` with that fingerprint. The write refuses a
profile that changed since the dry run.

Before the file is written, the profile's bookmarks are saved to the workspace `<browser>-before-write`, and the old
file is kept beside the new one as `Bookmarks.before-browser-foreman-<time>`. Afterwards the file is read back and
planned against again. The plan must come out empty, and anything left over is reported as drift. Writing the same
plan twice changes nothing the second time.

Addresses are written the way browsers store them: a default port (`https://site:443/`) is left out, so a
write never sets an address the browser then changes back.

## From the page

*Browsers › Write back to a browser…* writes the open workspace through a connected browser's extension, with the
same plan and checks as `apply --route extension --read extension`:

1. Choose the browser (only those whose extension is connected), the workspace holding the export the cleanup
   started from, and the mode. The page remembers the export per workspace.
2. **Preview** reads the browser now and shows the plan: what is added, removed, moved and renamed, what the browser
   changed after the export, any conflicts, and what stays in Browser Foreman. Nothing is written.
3. **Apply…** asks first, naming the number of changes and, for `replace`, the bookmarks it drops. It reads the
   browser again and refuses if it changed since the preview. Then the extension saves the browser's bookmarks to
   `<browser>-before-write`, writes, and reads them back, and the page plans again: it reports success only when that
   plan is empty.

The file route stays on the command line, because it needs the browser quit and access to its profile folder.

The page calls two owner-only routes, which take `workspace`, `base`, `browser` and `mode`:

| Route | Does |
|---|---|
| `POST /api/v1/writeback/preview` | Reads the browser and returns the plan as `report`, with `expect`, the browser's fingerprint. |
| `POST /api/v1/writeback/apply` | Takes `expect` (and `drop_new` for `replace`), answers 409 if the browser changed since, writes, and returns `written`. A write that does not verify answers 502 with `written.drift`. |

## On macOS

macOS keeps other apps' data private. If reading a profile fails with *Operation not permitted*, allow the terminal
or app running Browser Foreman to read it: System Settings › Privacy & Security › Full Disk Access. Otherwise use
`--route extension --read extension`, which needs no such access.

---
[← Docs index](../../README.md#documentation)
