GO ?= go
PNPM ?= pnpm

.PHONY: build-web sync-web build test docker-build

build-web:
	$(PNPM) --dir web build

sync-web: build-web
	rm -rf internal/server/webdist/*
	cp -R web/dist/* internal/server/webdist/

build: sync-web
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags="-s -w" -o coursefin ./cmd/coursefin

test:
	$(GO) test -race -count=1 ./...
	$(PNPM) --dir web test -- --run

docker-build:
	docker build -t coursefin:local .
