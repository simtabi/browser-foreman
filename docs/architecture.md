# Architecture

Browser Foreman is one Go program that keeps a local copy of your browsers' bookmarks and open tabs, serves a web
interface and an API over them, and tells other programs what changed.

## The pieces

| Package (`src/internal/…`) | Owns |
|---|---|
| `cli` | the command tree; commands parse flags, call a package and print |
| `config` | where files live, reading `*.json`, `.env` and the environment, validation |
| `api` | the HTTP server: `/api/v1`, the event stream, the Host/Origin/token checks |
| `events` | the event bus and its replay buffer |
| `webhooks` | subscriptions, signing, delivery with retries |
| `store` | the storage interface, the sqlite and json drivers, history (undo, redo) and migration |
| `importer` | reading Chrome's JSON and exported HTML bookmark files into a tree |
| `version` | build metadata stamped by the Makefile |

Planned packages (see the [roadmap](roadmap.md)): `auth` (users, sessions,
tokens), `browsers` (installed browsers, opening links, reading tabs), `web` (the embedded interface).

Outside `src/`: `web/` holds the web interface's TypeScript sources; `legacy/` holds the Python implementation the
project started as, the reference for behaviour while it is ported; `site/` is the landing page.

## How a change travels

1. A request reaches `api` and passes the checks: the Host is local (local mode), the Origin is this server's own,
   and a write carries the token.
2. The change is made, then published on the event bus as an `Event` with a type such as `webhook.created`.
3. The bus hands it to every live `/api/v1/events` stream and to the webhook dispatcher, which delivers it, signed,
   to each matching subscription, retrying with backoff.

Publishing never waits: a stream that falls behind drops events and catches up from the replay buffer by ID.

## Why Go?

The project began as a Python server. A single static binary installs anywhere with nothing else to set up, builds
for every platform from one machine (and as one universal binary for both kinds of Mac), and keeps the long-running
server small. The web interface stays TypeScript and is embedded in the binary.

## Why configuration in files and the environment?

Settings you might share (`*.json`) and secrets (`.env`, the environment) are kept apart, so a configuration can be
copied or committed without leaking the token. Every key has an environment variable for containers and services.

---
[← Docs index](../README.md#documentation)
