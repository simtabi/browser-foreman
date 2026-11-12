# Installation

Browser Foreman is one binary, built from source while releases are on hold.

## Requirements

- Go 1.26 or newer, and `make`.
- macOS for reading live tabs; every other feature runs on macOS, Linux, Windows and FreeBSD.
- Node.js 22 or newer only to rebuild the web interface (`make web`).

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

Check out a tag before building: `git -C ~/src/browser-foreman checkout v0.1.0` (once it exists).

## Releases

No downloads, package managers or `go install` path are offered until binary releases begin; see
[release](release.md).

---
[← Docs index](../README.md#documentation)
