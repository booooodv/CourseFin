# Stage 1: Build the React Web SPA
FROM node:22-alpine AS web-builder
WORKDIR /src/web

RUN npm install -g pnpm@12.21.0

COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile

COPY web/ ./
RUN pnpm build

# Stage 2: Build the static Go binary with embedded SPA
FROM golang:1.26-bookworm AS go-builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN rm -rf internal/server/webdist/*
COPY --from=web-builder /src/web/dist/ ./internal/server/webdist/

ARG VERSION=1.0.1
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/coursefin ./cmd/coursefin

# Stage 3: Minimal runtime image with FFmpeg
FROM debian:bookworm-slim

RUN apt-get update \
	&& apt-get install -y --no-install-recommends ffmpeg curl ca-certificates tzdata \
	&& rm -rf /var/lib/apt/lists/* \
	&& mkdir -p /data /media

COPY --from=go-builder /out/coursefin /usr/local/bin/coursefin
COPY scripts/docker-entrypoint.sh /usr/local/bin/coursefin-entrypoint.sh
RUN chmod 0755 /usr/local/bin/coursefin-entrypoint.sh

ENV COURSEFIN_PORT=8096 \
	COURSEFIN_DATA_DIR=/data

EXPOSE 8096
VOLUME ["/data"]

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["sh", "-c", "curl -fsS http://127.0.0.1:${COURSEFIN_PORT:-8096}/System/Ping || exit 1"]

ENTRYPOINT ["/usr/local/bin/coursefin-entrypoint.sh"]
