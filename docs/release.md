# Release

How a Browser Foreman version is cut. Binaries are published as GitHub release assets and nowhere else: no
Homebrew, Scoop, Winget, image or `go install` path.

## Versions

Semantic versioning. Tags are `vX.Y.Z` and are final: never moved, deleted or reused. Testing uses `vX.Y.Z-rc.N`.
A bad version is retracted and the fix ships as the next number.

## The first push of a new repository

Every change reaches `main` through a pull request, and the pre-push guard refuses any push that creates
`main`. A brand-new, empty repository has no `main` for a pull request to target, so its first push is the one
exception, made once by the owner with the guard's own override:

```bash
SIMTABI_ALLOW_MAIN_PUSH=1 git -C ~/Artisan/projects/opensource/simtabi/packages/browser-foreman push -u origin main
```

- Only a person sets `SIMTABI_ALLOW_MAIN_PUSH=1`, for that one push; an agent is refused it.
- If a feature branch was pushed first, GitHub made it the default branch: set it back with
  `gh api -X PATCH repos/simtabi/browser-foreman -f default_branch=main`.
- sange scans the whole history on that first push. A value it flags that is not a secret needs an exception,
  which only a person at an interactive terminal can confirm (`sange compliance exceptions add …`, then type
  `add exception`). Browser Foreman has two, added 2026-11-17: the ID character set in an early `store.go`, and a
  placeholder password in early test URLs.

## Before tagging

Every one of these holds, read back rather than assumed:

- `make check lint` passes, and so does `npm run check:web && npm run test:web` in `web/`.
- The pull request's CI is green on the commit to be tagged, and local `main` equals `origin/main`.
- Every Go dependency of our own is on a tagged version, not a pseudo-version (`grep simtabi src/go.mod`).
- `goreleaser check --config src/.goreleaser.yaml` passes, and the snapshot below builds every target.
- `CHANGELOG.md` has the `## [X.Y.Z]` section, and the roadmap says what shipped.
- The browser extension's version, in `web/package.json`, was raised if the extension changed since the last
  release. The manifests and the page read it from there; it moves independently of the binary's version.

## Steps

1. Move the `[Unreleased]` section of `CHANGELOG.md` under a new `## [X.Y.Z]` heading, through a pull request.
2. After it merges, tag `vX.Y.Z` on `main` and push the tag. **Pushing the tag builds and publishes the release**,
   so push a version tag only for final code:

```bash
git -C ~/Artisan/projects/opensource/simtabi/packages/browser-foreman tag -a vX.Y.Z -m vX.Y.Z
git -C ~/Artisan/projects/opensource/simtabi/packages/browser-foreman push origin vX.Y.Z
```

3. To build the release for a tag already on GitHub (a first release of an older tag, or a re-run), request it:

```bash
gh workflow run release.yml --repo simtabi/browser-foreman -f tag=vX.Y.Z
```

`.github/workflows/release.yml` calls simtabi/.github's shared `go-release.yml`, which checks the tag, takes the
`## [X.Y.Z]` CHANGELOG section as the notes (a final version without one is refused), pushes the `src/vX.Y.Z` module
tag, and runs GoReleaser with `src/.goreleaser.yaml`. That stages the web interface, runs the tests, builds every
target in `build/targets.txt` and the universal macOS binary, and publishes them with `checksums.txt` on the GitHub
release, then attests their build provenance (public repositories only). This publishes for real.

A local dry run builds the same assets into the ignored `.cache/goreleaser/`:

```bash
goreleaser release --snapshot --clean --config src/.goreleaser.yaml --skip=sign,publish,sbom
```

---
[← Docs index](../README.md#documentation)
