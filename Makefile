# Everyday tasks. Building needs Go and Node: the page in frontend/ is
# built first (into backend/views/web/page/dist) and compiled into houridle.
#
#   make            build houridle for this computer, into bin/
#   make install    build it, install it as a service and start it (Linux)
#   make test       vet and test
#   make page       build just the page (make and make all-os do it first)
#   make all-os     build for Windows and Linux (like a release)

VERSION ?= $(shell git describe --tags --always 2>/dev/null | sed 's/^v//')
LDFLAGS := -s -w -X main.version=$(or $(VERSION),dev)
GO      := go build -trimpath -ldflags "$(LDFLAGS)"

.PHONY: build install test page all-os clean

build: page
	$(GO) -o bin/houridle$(shell go env GOEXE) ./backend/cmd/houridle

install:
	./install.sh

test:
	go vet ./...
	go test ./...

page:
	cd frontend && { [ -d node_modules ] || npm ci; } && npm run build

all-os: page
	mkdir -p dist
	GOOS=windows GOARCH=amd64 $(GO) -o dist/houridle.exe ./backend/cmd/houridle
	GOOS=linux GOARCH=amd64 $(GO) -o dist/houridle-linux-amd64 ./backend/cmd/houridle
	GOOS=linux GOARCH=arm64 $(GO) -o dist/houridle-linux-arm64 ./backend/cmd/houridle
	cd dist && sha256sum * > SHA256SUMS

clean:
	rm -rf dist bin/houridle bin/houridle.exe backend/views/web/page/dist
