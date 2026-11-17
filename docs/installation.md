# Installation

Browser Foreman is one binary: download it from a GitHub release, or build it from source.

## Requirements

- To build from source: Go 1.26 or newer, and `make`. A downloaded binary needs neither.
- macOS for reading live tabs; every other feature runs on macOS, Linux, Windows and FreeBSD.
- Node.js 22 or newer only to rebuild the web interface (`make web`).

## Download

Each release on [GitHub](https://github.com/simtabi/browser-foreman/releases) carries one binary per platform,
`browser-foreman_<os>_<arch>` (`.exe` on Windows), the universal macOS binary `browser-foreman_macos_universal`, and
`checksums.txt`; the first was v0.1.0. Check a download before running it:

```bash
cd ~/Downloads && shasum -a 256 -c checksums.txt --ignore-missing
```

## Build

```bash
git clone https://github.com/simtabi/browser-foreman.git ~/src/browser-foreman
```

```bash
cd ~/src/browser-foreman && make build
```

The binary lands in `build/<os>/browser-foreman` (`build/macos/` on a Mac). Copy it onto your `PATH`:

```bash
cp ~/src/browser-foreman/build/macos/browser-foreman ~/.local/bin/
```

## The universal macOS binary

One file that runs natively on Apple silicon and Intel Macs:

```bash
cd ~/src/browser-foreman && make universal
```

It writes `build/macos/browser-foreman_macos_universal`, plus the two single-architecture builds it is made from.

## Every other platform

```bash
cd ~/src/browser-foreman && make cross
```

This builds every target in `build/targets.txt` into `build/<os>/browser-foreman_<os>_<arch>` and lists their
SHA-256 sums in `build/checksums.txt`. Verify them with `cd build && shasum -a 256 -c checksums.txt`.

## Pin a version

Check out a tag before building: `git -C ~/src/browser-foreman checkout v0.2.0`, or the newest tag on the
[releases page](https://github.com/simtabi/browser-foreman/releases).

## Releases

Binaries are published on the GitHub release and nowhere else: no package manager, image or `go install` path.
See [release](release.md).

---
[← Docs index](../README.md#documentation)
