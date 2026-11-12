# Security Policy

Where this file is silent, the [Simtabi security policy](https://github.com/simtabi/.github/blob/HEAD/SECURITY.md)
applies.

## Reporting a vulnerability

Once the repository is public, the preferred channel is GitHub private vulnerability reporting: open a private report
at <https://github.com/simtabi/browser-foreman/security/advisories/new>. Email **security@simtabi.com** if you do not
use GitHub, or before then.

Do **not** open a public issue for a security report.

Include, where possible: a description of the issue, the affected version, reproduction steps, and any suggested fix.
We aim to acknowledge reports within a few business days and keep you updated until it is resolved.

## Scope notes for this project

Browser Foreman holds a list of everything you bookmark and every tab you open. These invariants are load-bearing;
please report any regression in them:

- **Local by default.** In `local` mode the server binds a loopback address and refuses any request whose Host is not
  localhost (DNS rebinding) or whose Origin is another site.
- **Secrets stay out of files you share.** The API token is read only from `.env` or the environment, never from a
  JSON config file; a `.env` that other users can read is refused; tokens and webhook secrets are never printed or
  returned by the API.
- **Webhooks cannot reach what they should not.** Targets must be http(s); link-local, multicast, unspecified and
  cloud metadata addresses are refused. Every delivery is signed (HMAC-SHA256 over the timestamp and body).
- **Releases need a person.** The release workflow only runs when dispatched by hand with a confirmation.
