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
```

Check it answers:

```bash
curl -s http://127.0.0.1:8765/api/v1/health
```

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
