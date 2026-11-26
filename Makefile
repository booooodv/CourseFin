GO ?= go
PNPM ?= pnpm
VERSION ?= 1.5.0

.PHONY: build-web sync-web build test docker-build

build-web:
	$(PNPM) --dir web build

sync-web: build-web
	rm -rf internal/server/webdist/*
	cp -R web/dist/* internal/server/webdist/

build: sync-web
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags="-s -w -X main.version=$(VERSION)" -o coursefin ./cmd/coursefin

test:
	$(GO) test -race -count=1 ./...
	$(PNPM) --dir web test

docker-build:
	docker build --build-arg VERSION=$(VERSION) -t coursefin:$(VERSION) -t coursefin:latest .
