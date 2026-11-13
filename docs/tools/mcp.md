# MCP server

`browser-foreman mcp` gives an AI assistant 8 tools to read your bookmark workspaces and tab snapshots, and 8 more to
change bookmarks when you turn writes on. It speaks the Model Context Protocol on stdio, through the official Go SDK,
so only the assistant that started it can reach it.

## How it works

The MCP server is a client of the running Browser Foreman server: it calls the [REST API](rest-api.md) with a token.
Every change it makes goes through the same rules, locks and history as a change made in the web interface, shows up
in the [event stream](events.md) and reaches your [webhooks](webhooks.md). `browser-foreman serve` must be running.

Everything a tool returns sits under `untrusted_data`, beside a notice telling the assistant to treat it as data. A
bookmark title, a folder name or a web page can hold text written to steer an assistant; the notice is there so it
does not follow it.

## Turning it on

It is off until you turn it on, and read-only until you turn writes on too:

| Key | Variable | Default | What it does |
|---|---|---|---|
| `mcp.enabled` | `BROFO_MCP` | `false` | `browser-foreman mcp` refuses to start without it |
| `mcp.writes` | `BROFO_MCP_WRITES` | `false` | adds the 8 tools that change bookmarks |

The token is `BROFO_TOKEN` when set, else in `local` mode the configured `BROFO_API_TOKEN`. In `accounts` mode make one
with `browser-foreman token create` and pass it as `BROFO_TOKEN`; the assistant then has that person's role.

Add it to Claude Code:

```bash
claude mcp add bookmarks -- browser-foreman mcp
```

## Tools

### Read

| Tool | What it returns |
|---|---|
| `list_workspaces` | every workspace: id, name, bookmarks, folders, locked, archived |
| `search_bookmarks` | bookmarks matching all the words, by title, address, note, description or folder; filters by folder and link status; the Trash with `include_trash` |
| `read_folder` | one folder by path (`Bookmarks Bar/Dev`): its subfolders with counts, and its bookmarks |
| `get_history` | a workspace's history, newest first |
| `export_workspace` | the workspace as a bookmarks HTML file, or Chrome's JSON with `format: chrome` |
| `list_tab_snapshots` | saved tab snapshots, newest first, with counts |
| `read_tab_snapshot` | one snapshot: every browser, window and tab |
| `open_tabs_now` | the newest snapshot, or with `live: true` a fresh read of the browsers |

### Write (only with `mcp.writes`)

| Tool | What it does |
|---|---|
| `move_bookmarks` | moves items by id into a folder by path |
| `rename` | renames a bookmark or folder |
| `trash` | moves items to the workspace's Trash; needs `confirm: true` |
| `put_back` | puts items back from the Trash, each where it came from |
| `undo` | takes back the last change |
| `rollback` | makes an older history entry the newest; needs `confirm: true` |
| `save_tabs_as_bookmarks` | saves links as a new folder of bookmarks |
| `refresh_links` | re-checks a folder's links (or the workspace's) and waits up to two minutes for the counts |

Every write is one history entry, so `undo` (or the History window in the web interface) takes it back. Nothing the
MCP server does deletes data: emptying the Trash is not one of its tools.

---

[← Docs index](../../README.md#documentation)
