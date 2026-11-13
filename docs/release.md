# Release

How a Browser Foreman version is cut. Binaries are published as GitHub release assets and nowhere else: no
Homebrew, Scoop, Winget, image or `go install` path.

## Versions

Semantic versioning. Tags are `vX.Y.Z` and are final: never moved, deleted or reused. Testing uses `vX.Y.Z-rc.N`.
A bad version is retracted and the fix ships as the next number.

## Steps

1. Move the `[Unreleased]` section of `CHANGELOG.md` under a new `## [X.Y.Z]` heading, through a pull request.
2. After it merges, tag `vX.Y.Z` on `main` and push the tag. **Pushing the tag builds and publishes the release**,
   so push a version tag only for final code:

```bash
git -C ~/src/browser-foreman tag -a vX.Y.Z -m vX.Y.Z && git -C ~/src/browser-foreman push origin vX.Y.Z
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
