# syntax=docker/dockerfile:1.7
# ============================================================================
#  BunkrDownloader · Web Control Panel
#  Multi-stage build: builder (deps) + runtime (slim)
#  Default command starts the Web UI on :8765
# ============================================================================

# ---------- 1. Build dependencies layer --------------------------------------
FROM python:3.12-slim AS builder

ENV PIP_DISABLE_PIP_VERSION_CHECK=1 \
    PIP_NO_CACHE_DIR=1 \
    PYTHONDONTWRITEBYTECODE=1

WORKDIR /build

# System deps needed at build time for wheels (aiohttp brings most)
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        build-essential \
        gcc \
        libffi-dev \
    && rm -rf /var/lib/apt/lists/*

# Install Python deps into a prefix we can copy to the runtime stage
COPY requirements.txt ./
RUN pip install --prefix=/install \
        --no-cache-dir \
        -r requirements.txt

# ---------- 2. Runtime layer -------------------------------------------------
FROM python:3.12-slim AS runtime

LABEL org.opencontainers.image.title="BunkrDownloader Web" \
      org.opencontainers.image.description="Web control panel for BunkrDownloader with SQLite resume" \
      org.opencontainers.image.source="https://github.com/Lysagxra/BunkrDownloader" \
      org.opencontainers.image.licenses="MIT"

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PYTHONFAULTHANDLER=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1 \
    PIP_NO_CACHE_DIR=1

# tini-style signal handling + curl for healthcheck
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        tini \
        curl \
        ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --create-home --shell /bin/bash --uid 1000 bunkr

WORKDIR /app

# Copy only the prebuilt Python site-packages from the builder
COPY --from=builder /install /usr/local

# Copy the application code
COPY --chown=bunkr:bunkr src/       ./src/
COPY --chown=bunkr:bunkr web/       ./web/
COPY --chown=bunkr:bunkr web_main.py ./
COPY --chown=bunkr:bunkr README.md   ./
COPY --chown=bunkr:bunkr requirements.txt ./

# Persistent / working directories
RUN mkdir -p /data /downloads \
    && chown -R bunkr:bunkr /data /downloads

USER bunkr

# Defaults — override at runtime with `docker run -e ...`
ENV BUNKR_HOST=0.0.0.0 \
    BUNKR_PORT=8765 \
    BUNKR_DB=/data/state.db \
    BUNKR_LOG_LEVEL=INFO

EXPOSE 8765

# Healthcheck hits the JSON endpoint
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD curl -fsS "http://127.0.0.1:${BUNKR_PORT}/api/health" || exit 1

# tini reaps zombies and forwards signals (SIGTERM → clean shutdown)
ENTRYPOINT ["/usr/bin/tini", "--"]
CMD ["python3", "web_main.py", \
     "--host", "0.0.0.0", \
     "--port", "8765", \
     "--db", "/data/state.db", \
     "--log-level", "INFO"]
