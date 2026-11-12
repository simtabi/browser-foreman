# Browser Foreman. The Go module lives in src/; every go command runs there (go -C src).
# Binaries land in build/<os>/ with self-describing names; nothing else is written to build/.
BINARY  := browser-foreman
PKG     := github.com/simtabi/browser-foreman/src
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.1.0-dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X $(PKG)/internal/version.Version=$(VERSION) -X $(PKG)/internal/version.Commit=$(COMMIT) -X $(PKG)/internal/version.Date=$(DATE)
HOST_OS := $(shell go env GOOS | sed 's/darwin/macos/')

.PHONY: help build test vet fmt lint universal cross web check
help:  ## list targets
	@grep -E '^[a-z-]+:.*##' Makefile | sed 's/:.*## /\t/'

build:  ## this machine: build/<os>/browser-foreman
	mkdir -p build/$(HOST_OS)
	CGO_ENABLED=0 go -C src build -trimpath -ldflags '$(LDFLAGS)' -o ../build/$(HOST_OS)/$(BINARY) ./cmd/$(BINARY)

test:  ## unit tests
	go -C src test -count=1 ./...

vet:  ## go vet
	go -C src vet ./...

fmt:  ## gofmt check (fails when a file needs formatting)
	@test -z "$$(gofmt -l src)" || (gofmt -l src; exit 1)

lint:  ## golangci-lint, when installed
	cd src && golangci-lint run ./...

universal:  ## one macOS binary for Apple silicon and Intel: build/macos/browser-foreman_macos_universal
	mkdir -p build/macos
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go -C src build -trimpath -ldflags '$(LDFLAGS)' -o ../build/macos/$(BINARY)_macos_arm64 ./cmd/$(BINARY)
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go -C src build -trimpath -ldflags '$(LDFLAGS)' -o ../build/macos/$(BINARY)_macos_amd64 ./cmd/$(BINARY)
	lipo -create -output build/macos/$(BINARY)_macos_universal build/macos/$(BINARY)_macos_arm64 build/macos/$(BINARY)_macos_amd64

cross:  ## every target in build/targets.txt, then build/checksums.txt
	@while read os arch; do \
	  case "$$os" in ''|\#*) continue;; esac; \
	  dir=$$(echo $$os | sed 's/darwin/macos/'); ext=; [ "$$os" = windows ] && ext=.exe; \
	  mkdir -p build/$$dir; \
	  CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go -C src build -trimpath -ldflags '$(LDFLAGS)' -o ../build/$$dir/$(BINARY)_$${dir}_$$arch$$ext ./cmd/$(BINARY) || exit 1; \
	done < build/targets.txt
	cd build && find . -mindepth 2 -type f | sed 's|^\./||' | sort | xargs shasum -a 256 > checksums.txt

web:  ## build the web interface (TypeScript) into web/dist
	cd web && npm install --no-audit --no-fund && npm run build:web

check: fmt vet test  ## what CI runs
