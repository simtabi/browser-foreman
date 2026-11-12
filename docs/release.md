# Release

How a Browser Foreman version is cut. Binary releases are on hold: users build from source until the maintainer
starts them.

## Versions

Semantic versioning. Tags are `vX.Y.Z` and are final: never moved, deleted or reused. Testing uses `vX.Y.Z-rc.N`.
A bad version is retracted and the fix ships as the next number.

## Steps

1. Move the `[Unreleased]` section of `CHANGELOG.md` under a new `## [X.Y.Z]` heading, through a pull request.
2. After it merges, tag `vX.Y.Z` on `main` and push the tag. **Pushing the tag publishes nothing.**
3. To publish binaries, which happens only when the maintainer approves that version:

```bash
gh workflow run release.yml --repo simtabi/browser-foreman --ref vX.Y.Z -f confirm=release-go
```

`vX.Y.Z` must be a tag already on GitHub. The workflow refuses to run without `confirm=release-go` and a version tag,
runs the tests, builds every target and the universal macOS binary, and creates the GitHub release with the
CHANGELOG section as its notes and `checksums.txt` beside the binaries. This publishes for real.

---
[← Docs index](../README.md#documentation)
