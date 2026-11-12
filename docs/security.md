# Security

What Browser Foreman protects, and how. Report a weakness privately: see [SECURITY.md](../SECURITY.md).

## The server

- **Local by default.** `local` mode listens on a loopback address and refuses to start on any other; the loader
  enforces it. Every request whose Host header is not localhost is refused, so a web page cannot reach the server
  through DNS rebinding.
- **No cross-site requests.** A browser request whose Origin is another site is refused.
- **Every private request needs a caller.** Bookmarks, events, tokens, users and webhooks need a token or a signed-in
  session; only health, version and sign-in are open. Tokens are compared in constant time (the configured one) or by
  their SHA-256 (a person's). In `local` mode with no token configured, the API is refused rather than left open.
- **Roles.** viewer reads, editor will change bookmarks (phase 4), owner manages people, tokens and webhooks.
- **The web page.** Its Content Security Policy allows scripts from the server and inline scripts carrying that
  response's nonce, nothing from other hosts. It needs `'unsafe-eval'` for Alpine, which compiles the page's own
  `x-*` attributes. Bookmark data is embedded as JSON with `<`, `>`, `&`, U+2028 and U+2029 escaped, so a title cannot
  end the script it sits in. In `local` mode the page carries the API token, readable only from this computer (the
  Host and Origin checks keep other sites out); in `accounts` mode it needs a session and carries only its CSRF token.
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

## Accounts

`server.mode: accounts` turns on sign-in. It is the mode to use when the server listens beyond loopback.

- **Passwords** are hashed with argon2id (64 MiB, 3 passes, 2 lanes, 16-byte random salt) and stored as a PHC string;
  at least 12 characters. The CLI reads them without echo, or from `--password-stdin`, never from an argument.
- **Sign-in** costs the same time for an unknown name as for a wrong password. Five failures in 15 minutes lock that
  name and that address out for 15 minutes (`429` with `Retry-After`).
- **Sessions** are 256-bit random ids in an `HttpOnly`, `SameSite=Strict` cookie (`Secure` over TLS); only their
  SHA-256 is stored. They last `auth.session_ttl` (24 hours by default) and end at sign-out.
- **CSRF.** A change made with a session must send the session's `X-CSRF-Token`.
- **API tokens** are `brofo_` plus 256 random bits, shown once and stored as SHA-256. Disabling a person stops their
  tokens and sessions at once; removing them deletes both.
- **The last owner stays.** The API and the CLI refuse to demote, disable or remove the last owner who can sign in.
- In `accounts` mode the configured `BROFO_API_TOKEN` grants nothing; only people's own tokens count.

## Releases

Binary releases only run when dispatched by hand with a confirmation; a test fails if the workflow gains any other
trigger.

---
[← Docs index](../README.md#documentation)
