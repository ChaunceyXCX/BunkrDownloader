# syntax=docker/dockerfile:1.7
# =============================================================================
#  BunkrDownloader · Go + Gin
#
#  Multi-stage build:
#    1. web     – builds the Vue 3 SPA with Node
#    2. builder – compiles the static Go binary with the SPA embedded
#    3. runtime – distroless-style slim image with aria2c baked in
#
#  The image ships aria2c, so no network fetch happens on first boot, and the
#  process runs as a non-root user with a read-only-friendly filesystem layout.
# =============================================================================

ARG NODE_IMAGE=node:22-alpine
ARG GO_IMAGE=golang:1.24-alpine
ARG ALPINE_IMAGE=alpine:3.20

# ---------- 1. Frontend --------------------------------------------------------
FROM ${NODE_IMAGE} AS web

WORKDIR /build

# Dependencies are installed from the lockfile first so this layer is cached
# until the dependency set actually changes.
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm ci --no-audit --no-fund || npm install --no-audit --no-fund

COPY frontend/ ./
RUN npm run build \
    && test -f dist/index.html \
    && grep -q './assets/' dist/index.html \
    && echo "✓ SPA built"


# ---------- 2. Go build --------------------------------------------------------
FROM ${GO_IMAGE} AS builder

ARG VERSION=1.0.0
ENV CGO_ENABLED=0 GOOS=linux GOTOOLCHAIN=local

WORKDIR /src

# Module cache first.
COPY go.mod go.sum ./
RUN go mod download

COPY cmd/      ./cmd/
COPY internal/ ./internal/

# The SPA is embedded from internal/web/dist, which the web stage produced.
COPY --from=web /build/dist/ ./internal/web/dist/
RUN test -f internal/web/dist/index.html || (echo "SPA assets missing" && exit 1)

# -trimpath keeps the build reproducible; -s -w strip the symbol table.
RUN go vet ./... \
    && CGO_ENABLED=0 go build \
        -trimpath \
        -ldflags "-s -w -X main.version=${VERSION}" \
        -o /out/bunkr-web ./cmd/bunkr-web

# Smoke-test the binary in the build environment.
RUN /out/bunkr-web -version


# ---------- 3. Runtime ---------------------------------------------------------
FROM ${ALPINE_IMAGE} AS runtime

LABEL org.opencontainers.image.title="BunkrDownloader" \
      org.opencontainers.image.description="Bunkr album downloader with accounts, memberships and an aria2 engine" \
      org.opencontainers.image.source="https://github.com/chaunceyxie1/BunkrDownloader" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="1.0.0"

ENV TZ=UTC \
    BUNKR_HOST=0.0.0.0 \
    BUNKR_PORT=8765 \
    BUNKR_DATA_DIR=/data \
    BUNKR_DOWNLOAD_DIR=/downloads \
    BUNKR_DB=/data/bunkr.db \
    BUNKR_ARIA2_ENABLED=true \
    BUNKR_ARIA2_HOST=127.0.0.1 \
    BUNKR_ARIA2_PORT=6800 \
    BUNKR_ARIA2_SECRET=bunkr \
    BUNKR_LOG_LEVEL=INFO

# hadolint ignore=DL3018 — aria2c pins come from the official project
# (package versions are not available in the Alpine repositories).
RUN apk add --no-cache \
        ca-certificates \
        curl \
        tzdata \
        tini \
        aria2 \
 && aria2c --version | head -1

# The service runs as an unprivileged user; /data holds the database and the
# aria2 session, /downloads receives the files.
RUN addgroup -S -g 1000 bunkr \
 && adduser -S -u 1000 -G bunkr -h /home/bunkr bunkr \
 && mkdir -p /data /downloads \
 && chown -R bunkr:bunkr /data /downloads

WORKDIR /app
COPY --from=builder /out/bunkr-web /usr/local/bin/bunkr-web
COPY --chown=bunkr:bunkr docker/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

USER bunkr

EXPOSE 8765
VOLUME ["/data", "/downloads"]

# The health endpoint reports aria2 readiness, so a broken engine surfaces here.
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD curl -fsS "http://127.0.0.1:${BUNKR_PORT}/api/health" \
        | grep -q '"status":"ok"' || exit 1

# tini reaps zombies and forwards SIGTERM for a clean shutdown.
ENTRYPOINT ["/sbin/tini", "--", "/usr/local/bin/entrypoint.sh"]
CMD ["bunkr-web"]
