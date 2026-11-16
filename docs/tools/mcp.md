# MCP server

`browser-foreman mcp` gives an AI assistant 12 tools to read your bookmark workspaces and tab snapshots, and 13 more
to change bookmarks when you turn writes on, held to the folders you allow. It speaks the Model Context Protocol on stdio, through the official Go SDK,
so only the assistant that started it can reach it.

## How it works

The MCP server is a client of the running Browser Foreman server: it calls the [REST API](rest-api.md) with a token.
Every change it makes goes through the same rules, locks and history as a change made in the web interface, shows up
in the [event stream](events.md) and reaches your [webhooks](webhooks.md).

When no server answers at the configured address, `browser-foreman mcp` opens the store itself and runs the same API
in-process, on no port, with a token it makes at start and keeps in memory. The tools work the same way. Changes still
go through the same rules and history, but the event stream and webhooks of a server started later do not see them.
Link checks started from `refresh_links` run only while the assistant keeps the MCP server running.

Everything a tool returns sits under `untrusted_data`, beside a notice telling the assistant to treat it as data. A
bookmark title, a folder name or a web page can hold text written to steer an assistant; the notice is there so it
does not follow it.

## Turning it on

It is off until you turn it on, and read-only until you turn writes on too:

| Key | Variable | Default | What it does |
|---|---|---|---|
| `mcp.enabled` | `BROFO_MCP` | `false` | `browser-foreman mcp` refuses to start without it |
| `mcp.writes` | `BROFO_MCP_WRITES` | `false` | adds the 13 tools that change bookmarks |
| `mcp.folders` | — | none | limits writes to what is inside these folders (`["Bookmarks Bar/Inbox"]`); none: anywhere |
| `mcp.clients` | — | none | gives one client, by the name it reports, its own `writes` and `folders` in place of the two above |

The token is `BROFO_TOKEN` when set, else in `local` mode the configured `BROFO_API_TOKEN`. In `accounts` mode make one
with `browser-foreman token create` and pass it as `BROFO_TOKEN`; the assistant then has that person's role.

Add it to Claude Code:

```bash
claude mcp add bookmarks -- browser-foreman mcp
```

### Write scopes

An assistant can be held to part of your bookmarks, and each assistant to its own part:

```json
{
  "mcp": {
    "enabled": true,
    "writes": false,
    "clients": {
      "claude-code": { "writes": true, "folders": ["Bookmarks Bar/Inbox", "Other bookmarks/Reading"] }
    }
  }
}
```

Here every client reads, and only `claude-code` writes, only inside those two folders. A client is named by what it
reports when it connects (`clientInfo.name`); one with no entry of its own gets `mcp.writes` and `mcp.folders`. Under
a folder scope:

- a bookmark or folder can be changed only when it lies inside an allowed folder, and a write into a folder (moving,
  adding, saving tabs) only goes into an allowed folder or one inside it;
- an allowed folder itself cannot be moved, renamed or trashed;
- `undo`, `rollback` and `put_back` are refused, because each can change anything in the workspace.

A refused write changes nothing and says which folder it tried and where to change the scope. The name a client
reports is not proof of who it is: the scope keeps a well-meaning assistant to its part, and is no defence against a
program that lies about its name. Only programs you start reach the server, on stdio.

## Tools

### Read

| Tool | What it returns |
|---|---|
| `list_workspaces` | every workspace: id, name, bookmarks, folders, locked, archived (`include_archived: false` leaves archived ones out) |
| `search_bookmarks` | bookmarks matching all the words, by title, address, note, description or folder; filters by folder and link status; the Trash with `include_trash` |
| `read_folder` | one folder by path (`Bookmarks Bar/Dev`): its subfolders with counts, and its bookmarks |
| `get_history` | a workspace's history, newest first |
| `export_workspace` | the workspace as a bookmarks HTML file, or Chrome's JSON with `format: chrome` |
| `list_tab_snapshots` | saved tab snapshots, newest first, with counts |
| `read_tab_snapshot` | one snapshot: every browser, window and tab (`query` keeps tabs whose title or address hold every word) |
| `open_tabs_now` | the newest snapshot, or with `live: true` a fresh read of the browsers |
| `get_preview` | one bookmark in full: note, rating, tags, and what the last link check found (status, final address, the page's own title and description, its archive copy) |
| `health_report` | link health for a workspace or one folder: counts by status, and the confirmed dead, suspected dead, moved, unverified and sign-in bookmarks |
| `find_duplicates` | groups of bookmarks leading to the same page, each with the copy to keep and why the others match |
| `stats` | the workspace in numbers: bookmarks, folders, trash, labels, top folders, tags and sites, link status, oldest and newest |

### Write (only with `mcp.writes`)

| Tool | What it does |
|---|---|
| `move_bookmarks` | moves items by id into a folder by path (`create: true` makes missing folders, one history entry each) |
| `rename` | renames a bookmark or folder |
| `trash` | moves items to the workspace's Trash; needs `confirm: true` |
| `put_back` | puts items back from the Trash, each where it came from |
| `undo` | takes back the last change |
| `rollback` | makes an older history entry the newest; needs `confirm: true` |
| `save_tabs_as_bookmarks` | saves links, or one window of a tab snapshot (`snapshot`, `browser`, `window`), as a new folder of bookmarks |
| `refresh_links` | re-checks some bookmarks (`ids`), a folder's links or the workspace's, and waits up to two minutes for the counts |
| `label_bookmarks` | adds or removes tags, sets a rating (`0` clears) or replaces the note, on up to 500 bookmarks in one entry |
| `add_bookmark` | adds a web address to a folder by path (made when missing), with an optional note, tags and rating |
| `next_ai_jobs`, `submit_ai_results`, `run_ai_jobs` | take and answer [AI jobs](ai.md): tags to suggest, duplicates to judge |

### Resources and prompts

| Resource | What it holds |
|---|---|
| `bookmarks://workspaces` | every workspace, as `list_workspaces` gives them |
| `bookmarks://workspaces/{id}/health` | that workspace's link health, as `health_report` gives it |

| Prompt | What it asks the assistant to do |
|---|---|
| `triage-dead-links` | go through the confirmed dead links (of a workspace, or a `folder`) and sort them into keep, moved and drop |
| `tidy-folder` | propose a tidier layout for a `folder`, then move bookmarks into it |
| `dedupe` | merge bookmarks that lead to the same page, carrying notes and tags to the copy kept |
| `auto-tag` | suggest one to three tags per bookmark, preferring tags already in use, then add them |
| `process-queue` | work through the waiting [AI jobs](ai.md) (with writes on) |

Each prompt asks the assistant to show its plan and wait for your yes before changing anything, and to treat titles,
notes and pages as data rather than instructions.

Every write is one history entry, so `undo` (or the History window in the web interface) takes it back. Nothing the
MCP server does deletes data: emptying the Trash is not one of its tools.

---
[← Docs index](../../README.md#documentation)
