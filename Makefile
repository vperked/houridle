# Everyday tasks. houridle needs only Go to build: the status page in
# backend/web/dist is committed. Node is only needed to change the page.
#
#   make            build houridle for this computer
#   make install    build it, install it as a service and start it (Linux)
#   make test       vet and test
#   make page       rebuild the status page after changing frontend/
#   make all-os     build for Windows and Linux (like a release)

VERSION ?= $(shell git describe --tags --always 2>/dev/null | sed 's/^v//')
LDFLAGS := -s -w -X main.version=$(or $(VERSION),dev)
GO      := go build -trimpath -ldflags "$(LDFLAGS)"

.PHONY: build install test page all-os clean

build:
	$(GO) -o houridle$(shell go env GOEXE) ./backend

install:
	./install.sh

test:
	go vet ./...
	go test ./...

page:
	cd frontend && npm ci && npm run build

all-os:
	mkdir -p dist
	GOOS=windows GOARCH=amd64 $(GO) -o dist/houridle.exe ./backend
	GOOS=linux GOARCH=amd64 $(GO) -o dist/houridle-linux-amd64 ./backend
	GOOS=linux GOARCH=arm64 $(GO) -o dist/houridle-linux-arm64 ./backend
	cd dist && sha256sum * > SHA256SUMS

clean:
	rm -rf dist houridle houridle.exe
