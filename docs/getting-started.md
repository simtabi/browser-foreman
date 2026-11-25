# Getting started

From a fresh build to a running server, a live event stream and your first webhook.

## Set up

Create the configuration and a token (once):

```bash
browser-foreman config init
```

```text
wrote /Users/you/.config/browser-foreman/config.json
wrote /Users/you/.config/browser-foreman/.env
```

Load the token into your shell for the examples below:

```bash
export $(grep BROFO_API_TOKEN ~/.config/browser-foreman/.env)
```

## Run the server

```bash
browser-foreman serve
```

```text
Browser Foreman 0.1.0-dev on http://127.0.0.1:8765 (config …/.config/browser-foreman, state …/.local/state/browser-foreman)
Open Browser Foreman: http://127.0.0.1:8765/open/…
```

Started from a terminal, it also opens that page in your default browser (`server.open`; `--no-open` skips it). If
it is already running, `serve` says so, gives a new link, and opens it. The link works once, within two minutes, and
signs in the browser that opens it, for 30 days: the page never hands its token to anything else on this computer.
To open it again later, or in another browser:

```bash
browser-foreman open
```

Check it answers:

```bash
curl -s http://127.0.0.1:8765/api/v1/health
```

In local mode the answer also names the service, its version and its process.

### When the port is already in use

`serve` checks what holds its address before giving up:

| What holds it | What `serve` does |
|---|---|
| This same Browser Foreman | Says it is already running there, with its process, and exits without an error. |
| Another build of Browser Foreman, such as one started before a rebuild | Stops with how to replace it. `browser-foreman serve --replace` stops it and takes its place. |
| Another program | Names the program and uses the next free port, up to 20 higher. |

`browser-foreman stop` stops the running server, as Ctrl-C in its terminal would. Both `stop` and `--replace` signal
only a process that answers as Browser Foreman and whose program is `browser-foreman`.

A server on a fallback port records its address in `server.json` in the state folder. `mcp`, `pair`, `apply` and
`stop` read it, so they find the server there. A browser extension paired with the usual address needs pairing again.
To keep one port, set `server.addr`.

## Watch events

Every change is an event. Follow them live:

```bash
curl -N http://127.0.0.1:8765/api/v1/events
```

A client that reconnects sends `Last-Event-ID` and receives what it missed. See [events](tools/events.md).

## Add a webhook

```bash
curl -X POST http://127.0.0.1:8765/api/v1/webhooks \
  -H "Authorization: Bearer $BROFO_API_TOKEN" -H 'Content-Type: application/json' \
  -d '{"url": "https://example.com/brofo", "events": ["*"], "secret": "a-long-random-secret-value"}'
```

Send it a test event, using the `id` the call returned:

```bash
curl -X POST http://127.0.0.1:8765/api/v1/webhooks/wh_0123456789abcdef/test -H "Authorization: Bearer $BROFO_API_TOKEN"
```

Your endpoint should check the signature: [receive a webhook](recipes/receive-a-webhook.md).

## What is next

The web interface, storage, accounts and browser plugins arrive in the phases of the [roadmap](roadmap.md).

---
[← Docs index](../README.md#documentation)
