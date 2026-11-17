# Configuration

Browser Foreman reads its settings from one folder, `~/.config/browser-foreman/`, and keeps what it writes in
`~/.local/state/browser-foreman/`. Both follow `XDG_CONFIG_HOME` and `XDG_STATE_HOME` when set, on macOS too.

## Where settings come from

Later layers override earlier ones:

1. Built-in defaults.
2. Every `*.json` file in the config folder, merged in name order: `00-base.json`, then `10-team.json`, then
   `config.json`. Unknown keys are refused, so a typo fails loudly instead of being ignored.
3. `.env` in the config folder: `KEY=VALUE` lines, `#` comments, optional quotes and `export`. It must not be readable
   by other users (`chmod 600`); a looser file is refused, because it holds the API token.
4. Environment variables.

`browser-foreman config show` prints the result and the files it read (never the token). `browser-foreman config init`
writes a starting `config.json`, and a `.env` made from the `env.example` the app ships (every variable it reads,
empty, with what each is for) with a fresh token. Both are `0600`, and neither is ever overwritten.

## Editing from the page

Settings › *Server settings* edits these same files: owner only, and saved only from the computer the server runs
on. Each setting shows where its value comes from (the default, a config file, `.env` or the environment).

- **Secrets are write-only.** API keys and the token go to `.env`; the page shows whether one is set, never its value,
  and can replace or remove it.
- **Where a change lands.** A value already in `.env` stays there; any other goes to the last config file that sets
  it, else `config.json`. Comments and other lines in `.env` are kept. The whole change is checked by the same
  loader the server starts with, and nothing is written unless all of it passes.
- **The environment wins.** A setting a `BROFO_*` variable sets in the shell is shown, not editable.
- **What the page may change** is `settings.edit`, set in the files only. `safe` (the default) leaves the server's
  address and mode, storage, and the API token to the files. `all` lets the page change those too, each after a
  confirmation.
- **AI is one choice, not five fields.** *Who answers AI jobs* picks the provider (your assistant over MCP,
  Anthropic, OpenAI or a local Ollama), and only that provider's settings show beneath it: its key, a model chosen
  from the models the provider itself lists (or *Other…* for one it does not), effort for Anthropic, and the address
  under *Advanced*. One button saves provider and model together, so a provider is never saved without the model it
  needs. Every other group is folded, with its current values in the summary line.
- **When it applies.** AI settings and keys take effect at once. The rest take effect when the server restarts.

## Keys

| JSON key | Variable | Default | Meaning |
|---|---|---|---|
| `server.addr` | `BROFO_SERVER_ADDR` | `127.0.0.1:8765` | where the server listens |
| `server.open` | `BROFO_SERVER_OPEN` | `auto` | open the page in the default browser when `serve` starts: `auto` (when started from a terminal), `always` or `never`; `--open` and `--no-open` override it |
| `server.mode` | `BROFO_SERVER_MODE` | `local` | `local`: one owner, loopback only, no sign-in; `accounts`: people sign in; see [security](security.md#accounts) |
| `storage.driver` | `BROFO_STORAGE_DRIVER` | `sqlite` | `sqlite` or `json`; see [storage](tools/storage.md) |
| `storage.path` | `BROFO_STORAGE_PATH` | the state folder | where data is kept |
| `webhooks.timeout` | `BROFO_WEBHOOK_TIMEOUT` | `10s` | per delivery attempt, `1s`–`2m` |
| `webhooks.attempts` | `BROFO_WEBHOOK_ATTEMPTS` | `5` | tries before a delivery is marked failed, 1–20 |
| `auth.session_ttl` | `BROFO_SESSION_TTL` | `24h` | how long a sign-in lasts, `5m`–`720h` |
| `mcp.enabled` | `BROFO_MCP` | `false` | lets `browser-foreman mcp` start; see [MCP server](tools/mcp.md) |
| `mcp.writes` | `BROFO_MCP_WRITES` | `false` | adds the MCP tools that change bookmarks |
| `mcp.folders` | — | none | limits MCP writes to these folders; see [write scopes](tools/mcp.md#write-scopes) |
| `mcp.clients` | — | none | per-client `writes` and `folders`, by the name the client reports |
| `writeback.mode` | — | `ask` | `merge`, `replace`, or `ask` each time; see [write-back](tools/write-back.md) |
| `writeback.route` | — | `ask` | `file`, `extension`, or `ask` each time |
| `writeback.read` | — | `ask` | how the extension route reads the profile now: `extension`, `file`, or `ask` |
| `notify.desktop` | `BROFO_NOTIFY_DESKTOP` | `true` | a desktop notification when a scheduled re-check finds links that stopped working or work again; see [running at login and notifications](tools/service.md) |
| `ai.provider` | `BROFO_AI_PROVIDER` | `none` | who answers AI jobs: `none` (an MCP assistant), `anthropic`, `openai` or `ollama`; see [AI](tools/ai.md) |
| `ai.model` | `BROFO_AI_MODEL` | `claude-opus-5-5` for `anthropic` | the model; required for `openai` and `ollama` |
| `ai.effort` | — | `low` | with `anthropic`: `low`, `medium` or `high` |
| `ai.ollama_url` | — | `http://127.0.0.1:11434` | where Ollama listens |
| `ai.openai_url` | — | `https://api.openai.com/v1` | where the OpenAI API is |
| — | `OPENAI_API_KEY` | none | OpenAI's key, `.env` or the environment only |
| — | `ANTHROPIC_API_KEY` | none | Anthropic's key, `.env` or the environment only; without it, an `ant auth login` profile |
| `settings.edit` | `BROFO_SETTINGS_EDIT` | `safe` | what the page's Settings may change: `safe`, or `all` (address, mode, storage and token too, after a confirmation); files and environment only |
| — | `BROFO_API_TOKEN` | none | the owner's token in `local` mode; `.env` or the environment only, never JSON |

## Example

`~/.config/browser-foreman/config.json`:

```json
{
  "server": {"addr": "127.0.0.1:8765", "mode": "local"},
  "storage": {"driver": "sqlite"},
  "webhooks": {"timeout": "10s", "attempts": 5}
}
```

`~/.config/browser-foreman/.env`:

```bash
BROFO_API_TOKEN=paste-the-token-config-init-wrote
```

## Rules the loader enforces

- `local` mode must listen on a loopback address; to listen more widely, use `accounts` mode.
- Ports are 1–65535; drivers, attempts and timeouts must be in range.

---
[← Docs index](../README.md#documentation)
