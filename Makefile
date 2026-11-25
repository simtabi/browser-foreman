# Browser Foreman. The Go module lives in src/; every go command runs there (go -C src).
# Binaries land in build/<os>/ with self-describing names; nothing else is written to build/.
BINARY  := browser-foreman
PKG     := github.com/simtabi/browser-foreman/src
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.1.0-dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X $(PKG)/internal/version.Version=$(VERSION) -X $(PKG)/internal/version.Commit=$(COMMIT) -X $(PKG)/internal/version.Date=$(DATE)
HOST_OS := $(shell go env GOOS | sed 's/darwin/macos/')
# The golangci-lint CI runs (.github/workflows/ci.yml); a test keeps the two equal.
GOLANGCI_VERSION := v2.15.0
# The govulncheck that make vuln and CI run.
GOVULNCHECK_VERSION := v1.10.0
# vet compiles for each of these too: the build-tagged files for other systems are otherwise first compiled when a
# release tag is built, where a mistake burns the tag.
VET_OSES := darwin linux windows freebsd

.PHONY: help build test vet fmt lint tidy vuln notices universal cross web check screenshots
help:  ## list targets
	@grep -E '^[a-z-]+:.*##' Makefile | sed 's/:.*## /\t/'

build: web  ## this machine: build/<os>/browser-foreman
	mkdir -p build/$(HOST_OS)
	CGO_ENABLED=0 go -C src build -trimpath -ldflags '$(LDFLAGS)' -o ../build/$(HOST_OS)/$(BINARY) ./cmd/$(BINARY)

test:  ## unit tests, under the race detector (CI runs this too)
	go -C src test -race -count=1 ./...

vet:  ## go vet, for this system and for every system in VET_OSES
	go -C src vet ./...
	@for os in $(VET_OSES); do echo "go vet (GOOS=$$os)"; GOOS=$$os go -C src vet ./... || exit 1; done

tidy:  ## go.mod and go.sum hold exactly what the code needs (fails when go mod tidy would change them)
	go -C src mod tidy -diff

vuln:  ## known vulnerabilities in code the binary calls (govulncheck at GOVULNCHECK_VERSION)
	cd src && go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

notices:  ## rewrite THIRD_PARTY_NOTICES from the linked Go modules and bundled npm packages (needs web/node_modules)
	go -C src test -count=1 -run TestThirdPartyNoticesAreCurrent ./internal/version -update-notices

fmt:  ## gofmt check (fails when a file needs formatting)
	@test -z "$$(gofmt -l src)" || (gofmt -l src; exit 1)

lint:  ## golangci-lint at the version CI runs, fetched by go run when not installed
	cd src && go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION) run ./...

universal: web  ## one macOS binary for Apple silicon and Intel: build/macos/browser-foreman_macos_universal
	mkdir -p build/macos
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go -C src build -trimpath -ldflags '$(LDFLAGS)' -o ../build/macos/$(BINARY)_macos_arm64 ./cmd/$(BINARY)
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go -C src build -trimpath -ldflags '$(LDFLAGS)' -o ../build/macos/$(BINARY)_macos_amd64 ./cmd/$(BINARY)
	lipo -create -output build/macos/$(BINARY)_macos_universal build/macos/$(BINARY)_macos_arm64 build/macos/$(BINARY)_macos_amd64

cross: web  ## every target in build/targets.txt, then build/checksums.txt
	@while read os arch; do \
	  case "$$os" in ''|\#*) continue;; esac; \
	  dir=$$(echo $$os | sed 's/darwin/macos/'); ext=; [ "$$os" = windows ] && ext=.exe; \
	  mkdir -p build/$$dir; \
	  CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go -C src build -trimpath -ldflags '$(LDFLAGS)' -o ../build/$$dir/$(BINARY)_$${dir}_$$arch$$ext ./cmd/$(BINARY) || exit 1; \
	done < build/targets.txt
	cd build && find . -mindepth 2 -type f | sed 's|^\./||' | sort | xargs shasum -a 256 > checksums.txt

web:  ## build the web interface (TypeScript) and stage it for embedding in the binary
	cd web && npm install --no-audit --no-fund && npm run build:web
	rm -rf src/internal/webui/dist/assets src/internal/webui/dist/index.html
	mkdir -p src/internal/webui/dist/assets/js
	cp web/src/template.html src/internal/webui/dist/index.html
	cp -R web/assets/css web/assets/fonts src/internal/webui/dist/assets/
	cp web/dist/assets/js/app.js web/dist/assets/js/v1.js web/dist/assets/js/theme.js src/internal/webui/dist/assets/js/

extension:  ## build the browser extension into web/dist/extension/{chromium,firefox,safari}
	cd web && npm install --no-audit --no-fund && npm run build:extension

screenshots: build  ## every feature, grouped and numbered, into docs/screenshots (a throwaway demo store; CHECK=1 checks its links first)
	cd web && npm install --no-audit --no-fund && node src/screenshots/capture.mjs $(if $(CHECK),--check)

extension-safari: extension  ## wrap the Safari build in an Xcode project (needs Xcode; you sign and enable it)
	xcrun safari-web-extension-converter web/dist/extension/safari --project-location web/dist/extension/safari-xcode \
	  --app-name "Browser Foreman" --bundle-identifier com.simtabi.browser-foreman --no-open --force

check: fmt vet tidy lint vuln test  ## what CI runs for Go; then cd web && npm run check:web && npm run test:web
