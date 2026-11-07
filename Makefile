# Everyday tasks. Building needs Go and Node: the page in frontend/ is
# built first (into backend/views/web/page/dist) and compiled into houridle.
#
#   make            build houridle for this computer, into bin/
#   make install    build it, install it as a service and start it (Linux)
#   make test       vet and test
#   make page       build just the page (make and make all-os do it first,
#                   and only when something in frontend/ changed)
#   make all-os     build for Windows and Linux (like a release)

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//')
LDFLAGS := -s -w -X main.version=$(or $(VERSION),dev)
GO      := go build -trimpath -ldflags "$(LDFLAGS)"

# The built page, and everything it's built from: it's rebuilt only when
# one of those is newer.
PAGE     := backend/views/web/page/dist/index.html
PAGE_SRC := $(shell find frontend/src frontend/public -type f 2>/dev/null) $(wildcard frontend/index.html frontend/package.json frontend/package-lock.json frontend/vite.config.ts frontend/tsconfig*.json)

.PHONY: build install test page all-os clean

build: $(PAGE)
	$(GO) -o bin/houridle$(shell go env GOEXE) ./backend/cmd/houridle

install:
	./install.sh

test:
	go vet ./...
	go test ./...

page: $(PAGE)

# The page's packages are installed again only when package-lock.json
# changed since the last time (a copy in node_modules marks it).
$(PAGE): $(PAGE_SRC)
	cd frontend && { cmp -s package-lock.json node_modules/.houridle-lock || { npm ci --prefer-offline --no-audit --no-fund && cp package-lock.json node_modules/.houridle-lock; }; } && npm run build

all-os: $(PAGE)
	mkdir -p dist
	GOOS=windows GOARCH=amd64 $(GO) -o dist/houridle.exe ./backend/cmd/houridle
	GOOS=linux GOARCH=amd64 $(GO) -o dist/houridle-linux-amd64 ./backend/cmd/houridle
	GOOS=linux GOARCH=arm64 $(GO) -o dist/houridle-linux-arm64 ./backend/cmd/houridle
	cd dist && sha256sum * > SHA256SUMS

clean:
	rm -rf dist bin/houridle bin/houridle.exe backend/views/web/page/dist
