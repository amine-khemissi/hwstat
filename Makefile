BINARY  := hwstat
PKG     := github.com/amine-khemissi/hwstat/version
PREFIX  ?= $(HOME)/.local

GIT_COMMIT := $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
GIT_TAG    := $(shell git describe --tags --exact-match 2>/dev/null)
GIT_DIRTY  := $(shell test -n "$$(git status --porcelain 2>/dev/null)" && echo dirty)

LDFLAGS := -s -w \
	-X '$(PKG).GitCommit=$(GIT_COMMIT)' \
	-X '$(PKG).GitTag=$(GIT_TAG)' \
	-X '$(PKG).GitDirty=$(GIT_DIRTY)'

.PHONY: build install vet test clean dist

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) .

install: build
	install -Dm755 $(BINARY) $(PREFIX)/bin/$(BINARY)

vet:
	go vet ./...

test:
	go test ./...

clean:
	rm -f $(BINARY)

# Cross-compiled release archives.
dist: clean
	@mkdir -p dist
	GOOS=linux  GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-amd64 .
	GOOS=linux  GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-arm64 .
