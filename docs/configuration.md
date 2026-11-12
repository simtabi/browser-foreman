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
writes a starting `config.json` and a `.env` with a fresh token, both `0600`, and never overwrites.

## Keys

| JSON key | Variable | Default | Meaning |
|---|---|---|---|
| `server.addr` | `BROFO_SERVER_ADDR` | `127.0.0.1:8765` | where the server listens |
| `server.mode` | `BROFO_SERVER_MODE` | `local` | `local`: one owner, loopback only, no sign-in; `accounts`: sign-in required (phase 3) |
| `storage.driver` | `BROFO_STORAGE_DRIVER` | `sqlite` | `sqlite` or `json` (phase 2) |
| `storage.path` | `BROFO_STORAGE_PATH` | the state folder | where data is kept |
| `webhooks.timeout` | `BROFO_WEBHOOK_TIMEOUT` | `10s` | per delivery attempt, `1s`–`2m` |
| `webhooks.attempts` | `BROFO_WEBHOOK_ATTEMPTS` | `5` | tries before a delivery is marked failed, 1–20 |
| — | `BROFO_API_TOKEN` | none | the token API writes need; `.env` or the environment only, never JSON |

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
