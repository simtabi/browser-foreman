# Architecture

Browser Foreman is one Go program that keeps a local copy of your browsers' bookmarks and open tabs, serves a web
interface and an API over them, and tells other programs what changed.

## The pieces

| Package (`src/internal/…`) | Owns |
|---|---|
| `cli` | the command tree; commands parse flags, call a package and print |
| `config` | where files live, reading `*.json`, `.env` and the environment, validation |
| `api` | the HTTP server: `/api/v1`, the event stream, the web page, the Host/Origin/token checks, the AI job queue, extension pairing |
| `webui` | the web interface's files, embedded at build time |
| `auth` | argon2id password hashes, random secrets for tokens and sessions, the sign-in rate limiter |
| `events` | the event bus and its replay buffer |
| `webhooks` | subscriptions, signing, delivery with retries |
| `store` | the storage interface, the sqlite and json drivers, history (undo, redo), preferences and migration |
| `edit` | changes to a bookmark tree, as a pure function returning the new tree and its events |
| `importer`, `exporter` | Chrome's JSON and the bookmarks HTML every browser reads, in and out |
| `merge`, `dupes`, `sorting` | folding copies of a page into one, finding look-alikes, filing bookmarks by the owner's rules |
| `chromefile`, `writeback` | a Chromium profile's Bookmarks file, and the three-way plan that writes a workspace back into it |
| `linkcheck` | checking links the way a careful visitor would |
| `media` | site icons from the bookmarks files, and page snapshots taken by headless Chrome |
| `browsers` | installed browsers, the default, icons, opening links, and (macOS) which running copy of a browser a page is in |
| `tabs` | reading open windows and tabs, snapshots, restoring a session |
| `history` | reading browsers' visit history, and whether this program may |
| `ai` | AI jobs (tag suggestions, duplicate verdicts), their prompts and checks, and the Anthropic, OpenAI and Ollama providers |
| `mcpserver` | the Model Context Protocol server on stdio, read-only unless writes are on |
| `notify` | desktop notifications through go-notifykit |
| `service` | running at login: a launchd agent, a systemd user unit or a Windows logon task |
| `docscheck` | tests that keep the README and `docs/` to the documentation standard and in step with the code |
| `version` | build metadata stamped by the Makefile |

Outside `src/`: `web/` holds the web interface's TypeScript sources and the browser extension's (`web/src/extension`,
one source built for Chromium, Firefox and Safari); `legacy/` holds the Python implementation the
project started as, kept for reference and never built; `site/` is the landing page.

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
