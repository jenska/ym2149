GO ?= go
GOFMT ?= gofmt
DEMO_BIN ?= psgdemo
DEMO_PKG := ./cmd/psgdemo
GO_FILES := $(shell find emulation renderer internal cmd -name '*.go' -type f | sort)
EBITEN_AUDIO_DIR := ./renderer/ebitenaudio
AUDIOSTREAM_DIR := ./renderer/audiostream

.PHONY: help fmt test bench build build-demo run-demo run-demo-interactive tidy clean

help:
	@printf '%s\n' \
		'Available targets:' \
		'  make fmt                   Format Go source files' \
		'  make test                  Run the Go test suite' \
		'  make bench                 Run benchmark suite' \
		'  make build                 Build the demo binary' \
		'  make build-demo            Build the demo binary' \
		'  make run-demo              Run the scripted demo' \
		'  make run-demo-interactive  Run the interactive demo' \
		'  make tidy                  Tidy Go modules' \
		'  make clean                 Remove build artifacts'

fmt:
	$(GOFMT) -w $(GO_FILES)

test:
	$(GO) test ./...
	cd $(AUDIOSTREAM_DIR) && $(GO) test ./...
	cd $(EBITEN_AUDIO_DIR) && $(GO) test ./...

bench:
	$(GO) test ./... -run '^$$' -bench .

build: build-demo

build-demo:
	$(GO) build -o $(DEMO_BIN) $(DEMO_PKG)

run-demo:
	$(GO) run $(DEMO_PKG) -mode script

run-demo-interactive:
	$(GO) run $(DEMO_PKG) -mode interactive

tidy:
	$(GO) mod tidy
	cd $(AUDIOSTREAM_DIR) && $(GO) mod tidy
	cd $(EBITEN_AUDIO_DIR) && $(GO) mod tidy

clean:
	rm -f $(DEMO_BIN)
