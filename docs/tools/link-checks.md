# Link checks

A link check visits each bookmarked web address once and records what it found, so dead and moved links show up in
the web interface and reach webhooks.

## What a check finds

| Status | Meaning |
|---|---|
| `ok` | the page answers at the same address |
| `moved` | it redirects; the new address is offered as *Update moved* |
| `needs-login` | it sends visitors to a sign-in page |
| `dead` | gone: `kind` is `page` (404, 410, or a page that says it is not found) or `host` (the domain does not exist, refuses connections, or is parked or switched off) |
| `unverified` | could not tell: timeouts, bot checks, rate limiting, TLS trouble |
| `local` | on this computer, the local network or a staging name: not visited |
| `skipped` | looks like an action link (sign-out, unsubscribe, password reset, a token in the address): not visited |

The check reads the first 64 KiB of an HTML page, so a page that answers `200` but says "not found", or a parked
domain, counts as dead, and a bot-check page counts as unverified rather than working.

## Being polite, and safe

- **A few at a time per site.** At most two requests at once per site (one for sites known to rate-limit, such as
  GitHub and Stack Overflow), with their starts spaced out; `github.com` and `gist.github.com` share one budget.
  Addresses are interleaved by site, so no site gets a burst.
- **Backing off.** A `429` or `503` pauses that site for 30 seconds, doubling up to 15 minutes and honouring
  `Retry-After`; three network failures in a row pause it for 5 minutes. While a site is paused its links come back
  `unverified`, never `dead`.
- **Nothing local.** Connections to loopback, private, link-local (cloud metadata), multicast and similar addresses
  are refused when the connection is made, on the address actually dialled. A redirect, or a public name that
  resolves to a private address, cannot get round it.
- **No credentials.** Checks send no cookies and no tokens, and never follow a redirect to an action link or to a
  non-web address.
- **Earlier results are kept.** A check that cannot reach a site does not replace what an earlier check found.

## Running one

From the web interface: right-click a folder or bookmark › *Re-check*, or *Refresh all*. The activity panel shows
progress and has Pause and Cancel. Through the API:

```bash
curl -s -X POST http://127.0.0.1:8765/api/v1/linkchecks -H "Authorization: Bearer $BROFO_API_TOKEN" \
  -d '{"workspace": "main", "nodes": ["b3f2a91c04de"]}'
```

```json
{"job": {"id": "lc_5e1c0a2b9f33", "state": "running", "total": 9, "checked": 0, "started": "2026-11-12T21:30:00Z", "done": false}}
```

Then `GET /api/v1/linkchecks/{id}` for progress (`?results=1` adds the results), and `POST …/pause`, `…/resume` or
`…/cancel`. One check runs at a time. Results are kept per address, so every workspace holding an address shares
them, and the web interface shows them as soon as it loads.

## Events

`linkcheck.started`, then `link.checked` for each address whose status changed (`{url, status, previous, final,
note}`; unchanged results publish nothing), then `linkcheck.finished` with the counts.

---
[← Docs index](../../README.md#documentation)
