# Contributing to Browser Foreman

Thank you for helping. This file covers how the repository is laid out, how to run the checks, and what a pull request
needs.

## Layout

| Path | What it holds |
|---|---|
| `src/` | the Go module (`go.mod` is here, not at the root). `cmd/browser-foreman` is the entrypoint; everything else is under `internal/` |
| `web/` | the web interface: TypeScript + Alpine sources in `web/src`, built with esbuild into `web/dist` |
| `site/` | the landing page |
| `docs/` | the documentation (the README's Documentation section is the index) |
| `build/` | build output, one folder per OS; only `targets.txt` is tracked |

## Checks

```bash
make check
```

That is gofmt, `go vet` and the Go tests. For the web interface:

```bash
cd web && npm install && npm run check:web && npm run test:web
```

Run every Go command through the Makefile or with `go -C src`: a bare `go test ./...` at the repository root finds no
packages and passes.

## Conventions

- **Commits:** imperative subject, 72 characters at most, no `feat:`/`fix:` prefix; the body says why.
- **Pull requests:** one concern per pull request, opened as a draft while commits are still coming. CI runs on pull
  requests; `main` only changes through one.
- **Tests:** a change in behaviour comes with a test that fails without it. A rule that matters gets a guard in the same
  change (see `src/internal/version/release_workflow_test.go` for the pattern).
- **Security:** never log or return a secret; never weaken the Host, Origin or token checks without a reviewed reason.
- **Test credentials** are generated when the test runs, never written in the source.
- **Docs:** user-facing changes update `docs/` and the `[Unreleased]` section of `CHANGELOG.md`.

## Conduct

This project follows the [code of conduct](CODE_OF_CONDUCT.md).
