# Security

What Browser Foreman protects, and how. Report a weakness privately: see [SECURITY.md](../SECURITY.md).

## The server

- **Local by default.** `local` mode listens on a loopback address and refuses to start on any other; the loader
  enforces it. Every request whose Host header is not localhost is refused, so a web page cannot reach the server
  through DNS rebinding.
- **No cross-site requests.** A browser request whose Origin is another site is refused.
- **Writes need a token.** Anything that changes state needs `Authorization: Bearer <token>`, compared in constant
  time. With no token configured, writes are refused rather than left open.
- **Headers.** Every response carries `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`,
  `Referrer-Policy: no-referrer` and a Content Security Policy limited to the server itself.

## Secrets

- The API token is read only from `.env` or the environment, never from JSON config files.
- `.env` must be `0600`; a file other users can read is refused. `config init` creates it `0600`.
- Tokens and webhook secrets are never printed, logged or returned by the API.

## Webhooks

- Targets must be `http` or `https`, with no credentials in the URL. Link-local, multicast, unspecified and cloud
  metadata addresses are refused. Loopback is allowed, for local callbacks.
- Every delivery is signed: `X-Brofo-Signature: t=<unix>,v1=<HMAC-SHA256 of "t.body">`. Receivers should compare in
  constant time and refuse a timestamp more than five minutes old: [receive a webhook](recipes/receive-a-webhook.md).
- Secrets are at least 16 characters and live in `webhooks.json` (`0600`) in the state folder.

## Accounts (phase 3)

Passwords hashed with argon2id, sessions in HttpOnly SameSite=Strict cookies, CSRF tokens on every form, sign-in rate
limits and lockout, per-user API tokens stored hashed. See the [roadmap](roadmap.md).

## Releases

Binary releases only run when dispatched by hand with a confirmation; a test fails if the workflow gains any other
trigger.

---
[← Docs index](../README.md#documentation)
