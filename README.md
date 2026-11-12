# simtabi/browser-foreman

No badges yet: the project is unreleased and not yet on GitHub, so there is no registry version, CI run or release to
point at. They are added with the first public release.

> Clean bookmarks, safe tabs: a local server and web interface that keeps your browsers' bookmarks tidy and your open tabs safe from a crash.

Runs on macOS, Linux and Windows from one Go binary (macOS ships as one universal binary for Apple silicon and Intel); reading live tabs needs macOS today.

## Install

Releases are on hold, so build from source (Go 1.26 or newer):

```bash
git clone https://github.com/simtabi/browser-foreman.git ~/src/browser-foreman
```

```bash
cd ~/src/browser-foreman && make build
```

The binary is `build/<os>/browser-foreman`. Put it on your `PATH`; check out a tag to pin a version. See
[installation](docs/installation.md) for the universal macOS binary and every other target.

## Quick start guide and usage

### Getting started

Create the configuration once. It writes `~/.config/browser-foreman/config.json` and a private `.env` holding a new
API token (both 0600), and never overwrites either:

```bash
browser-foreman config init
```

### Usage

Bring in a browser's bookmarks as a workspace, with its full history from here on:

```bash
browser-foreman import ~/Downloads/bookmarks.html --workspace main
```

Start the server (it listens on `127.0.0.1:8765` and answers on localhost only):

```bash
browser-foreman serve
```

Watch everything that happens, as Server-Sent Events:

```bash
curl -N http://127.0.0.1:8765/api/v1/events
```

Have changes sent to your own endpoint, signed with your secret:

```bash
curl -X POST http://127.0.0.1:8765/api/v1/webhooks \
  -H "Authorization: Bearer $BROFO_API_TOKEN" -H 'Content-Type: application/json' \
  -d '{"url": "https://example.com/brofo", "events": ["bookmark.*"], "secret": "a-long-random-secret"}'
```

The full walkthrough is in [getting started](docs/getting-started.md); everything else is in the
[documentation](#documentation).

## <a name="documentation"></a>Documentation

Hosted at <https://opensource.simtabi.com/documentation/simtabi/browser-foreman/> once released.

### Guides

- [Installation](docs/installation.md) — building from source, the universal macOS binary, every target
- [Getting started](docs/getting-started.md) — first run, configuration, the API and events
- [Configuration](docs/configuration.md) — `~/.config/browser-foreman/{.env,*.json}`, every key and variable
- [Architecture](docs/architecture.md) — how the pieces fit, and why Go
- [Security](docs/security.md) — what the server protects and how
- [Roadmap](docs/roadmap.md) — what ships in v0.1.0 and in what order
- [Release](docs/release.md) — how a version is cut

### Reference

- [REST API](docs/tools/rest-api.md) — every endpoint under `/api/v1`
- [Events](docs/tools/events.md) — the event stream and every event type
- [Webhooks](docs/tools/webhooks.md) — subscriptions, signatures, retries
- [Storage](docs/tools/storage.md) — the sqlite and json drivers, history, undo and redo, migrating
- [Link checks](docs/tools/link-checks.md) — what a check finds, politeness, safety, running one
- [Backup and restore](docs/tools/backup.md) — what a backup holds and leaves out, restoring without replacing
- [Web interface](docs/tools/web-interface.md) — the bookmark and tab views

### Recipes

- [Import bookmarks](docs/recipes/import-bookmarks.md) — from Chrome, Edge, Brave, Firefox, Safari or an export
- [Receive a webhook](docs/recipes/receive-a-webhook.md) — verify the signature in your endpoint
- [Stream events](docs/recipes/stream-events.md) — follow changes live, and catch up after a reconnect

## Contributing & security

See [CONTRIBUTING.md](CONTRIBUTING.md). Report vulnerabilities privately: [SECURITY.md](SECURITY.md).

## License

MIT, Copyright (c) 2026 Simtabi LLC. See [LICENSE](LICENSE).
