# Webhooks

A webhook (a callback) is a URL that receives the [events](events.md) it subscribed to, as signed JSON POSTs.

## Subscribe

```bash
curl -X POST http://127.0.0.1:8765/api/v1/webhooks \
  -H "Authorization: Bearer $BROFO_API_TOKEN" -H 'Content-Type: application/json' \
  -d '{"url": "https://example.com/brofo", "events": ["bookmark.*", "tabs.snapshot.saved"], "secret": "a-long-random-secret-value"}'
```

- `events`: exact types, a family (`bookmark.*`) or everything (`*`).
- `secret`: at least 16 characters; only you and Browser Foreman know it. It is never returned.
- `url`: `http` or `https`, no credentials in it. Link-local, multicast, unspecified and cloud metadata addresses are
  refused; loopback is fine.

## A delivery

```http
POST /brofo HTTP/1.1
Content-Type: application/json
User-Agent: browser-foreman-webhooks
X-Brofo-Event: bookmark.created
X-Brofo-Delivery: 42-1
X-Brofo-Signature: t=1731434400,v1=5f2b…

{"id":42,"type":"bookmark.created","at":"2026-11-12T18:00:00Z","data":{…}}
```

`X-Brofo-Delivery` is `<event id>-<attempt>`. Check the signature before trusting the body:
[receive a webhook](../recipes/receive-a-webhook.md).

## Retries

A `2xx` answer is success. A network error, a `5xx` or a `429` is retried with exponential backoff (2 s, 4 s, 8 s, …)
up to `webhooks.attempts` tries (default 5), each limited to `webhooks.timeout` (default 10 s). Any other `4xx` means the
receiver refused it, so it is not retried. Failed deliveries are reported in the server's log.

## The delivery log

The latest 1,000 deliveries are kept in `deliveries.jsonl` in the state folder (`0600`), each with its event type,
attempts, the receiver's last status and any error. `GET /api/v1/webhooks/deliveries` (owner role) lists them newest
first; `?subscription=wh_…` narrows it to one subscription and `?limit=` takes 1 to 1,000 (100 by default). Secrets
are never written there.

## Test

`POST /api/v1/webhooks/{id}/test` sends a `webhook.test` event to that one subscription now and answers with the
delivery record (`attempts`, `status`, `ok`, `error`).

---
[← Docs index](../../README.md#documentation)
