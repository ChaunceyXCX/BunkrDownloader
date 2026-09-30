#!/bin/sh
# ============================================================================
#  BunkrDownloader · container entrypoint
#
#  1. makes sure the persistent directories exist and are writable
#  2. surfaces a clear message when the JWT secret is unset
#  3. execs the server so signals reach it directly
# ============================================================================
set -e

DATA_DIR="${BUNKR_DATA_DIR:-/data}"
DOWNLOAD_DIR="${BUNKR_DOWNLOAD_DIR:-/downloads}"

for dir in "$DATA_DIR" "$DOWNLOAD_DIR"; do
    [ -d "$dir" ] || mkdir -p "$dir" 2>/dev/null || true
    if [ -d "$dir" ] && [ ! -w "$dir" ]; then
        echo "bunkr: $dir is not writable by uid $(id -u)." >&2
        echo "bunkr: fix it on the host, e.g.  chown -R 1000:1000 ./downloads" >&2
        exit 1
    fi
done

# A generated secret keeps sessions valid across restarts but loses them when
# the data volume is wiped; warn so that behaviour is not a surprise.
if [ -z "${BUNKR_JWT_SECRET:-}" ]; then
    echo "bunkr: BUNKR_JWT_SECRET is unset - a random secret is generated in" \
         "$DATA_DIR/.jwt_secret. Set it explicitly to share sessions across replicas." >&2
fi

# Allow both `entrypoint.sh` and `entrypoint.sh bunkr-web` invocations.
if [ "${1:-}" = "bunkr-web" ]; then
    shift
fi

exec /usr/local/bin/bunkr-web "$@"
