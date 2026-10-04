GO ?= go
GOFMT ?= gofmt
DEMO_BIN ?= psgdemo
DEMO_PKG := ./cmd/psgdemo
PLAYER_BIN ?= sndplayer
PLAYER_PKG := ./cmd/sndplayer
GO_FILES := $(shell find emulation renderer format internal cmd -name '*.go' -type f | sort)

.PHONY: help fmt fmt-check test bench build build-demo build-sndplayer run-demo run-demo-interactive tidy release-check ci clean

help:
	@printf '%s\n' \
		'Available targets:' \
		'  make fmt                   Format Go source files' \
		'  make fmt-check             Verify Go source formatting' \
		'  make test                  Run the Go test suite' \
		'  make bench                 Run benchmark suite' \
		'  make build                 Build the demo binary' \
		'  make build-demo            Build the demo binary' \
		'  make build-sndplayer       Build the sndplayer terminal player' \
		'  make run-demo              Run the scripted demo' \
		'  make run-demo-interactive  Run the interactive demo' \
		'  make tidy                  Tidy Go modules' \
		'  make release-check         Run release sanity checks' \
		'  make clean                 Remove build artifacts'

fmt:
	$(GOFMT) -w $(GO_FILES)

fmt-check:
	test -z "$$($(GOFMT) -l $(GO_FILES))"

test:
	$(GO) test ./...

bench:
	$(GO) test ./... -run '^$$' -bench .

build: build-demo build-sndplayer

build-demo:
	$(GO) build -o $(DEMO_BIN) $(DEMO_PKG)

build-sndplayer:
	$(GO) build -o $(PLAYER_BIN) $(PLAYER_PKG)

run-demo:
	$(GO) run $(DEMO_PKG) -mode script

run-demo-interactive:
	$(GO) run $(DEMO_PKG) -mode interactive

tidy:
	$(GO) mod tidy

release-check: test build-demo

ci: fmt-check test
	$(GO) vet ./...
	$(GO) build $(DEMO_PKG)
	$(GO) build -o /dev/null $(PLAYER_PKG)

clean:
	rm -f $(DEMO_BIN) $(PLAYER_BIN)
