#!/data/data/com.termux/files/usr/bin/sh
set -u

PREFIX="${PREFIX:-/data/data/com.termux/files/usr}"
HOME_DIR="${CPA_TERMUX_HOME:-/data/data/com.termux/files/home}"
DATA="${CPA_DATA_DIR:-$HOME_DIR/router-data/cpa}"
CONFIG="${CPA_CONFIG_PATH:-$DATA/config.yaml}"
AUTH_DIR="${CPA_AUTH_DIR:-$DATA/auth}"
CURL="${CPA_CURL_BIN:-$PREFIX/bin/curl}"
CPA_BIN="${CPA_BINARY:-$PREFIX/bin/cli-proxy-api}"
WAIT_SECONDS="${CPA_START_WAIT_SECONDS:-60}"
WAIT_INTERVAL="${CPA_START_WAIT_INTERVAL:-2}"
MODEL_CATALOG_URL="${CPA_MODEL_CATALOG_URL:-https://raw.githubusercontent.com/router-for-me/models/refs/heads/main/models.json}"
ANTIGRAVITY_PROBE_URL="${CPA_ANTIGRAVITY_PROBE_URL:-https://cloudcode-pa.googleapis.com/}"

case "$WAIT_SECONDS" in
    ''|*[!0-9]*) WAIT_SECONDS=60 ;;
esac
case "$WAIT_INTERVAL" in
    ''|*[!0-9]*) WAIT_INTERVAL=2 ;;
esac

needs_catalog_proxy=0
needs_antigravity_proxy=0
if [ -s "$CONFIG" ] && grep -Eq '^proxy-url:[[:space:]]*"?http://127\.0\.0\.1:17891/?"?[[:space:]]*$' "$CONFIG"; then
    needs_catalog_proxy=1
fi
for auth_file in "$AUTH_DIR"/*.json; do
    [ -f "$auth_file" ] || continue
    if grep -Eq '"proxy_url"[[:space:]]*:[[:space:]]*"http://127\.0\.0\.1:17892/?"' "$auth_file"; then
        needs_antigravity_proxy=1
        break
    fi
done

http_reachable() {
    case "$1" in
        2??|3??|4??) return 0 ;;
        *) return 1 ;;
    esac
}

catalog_ready() {
    [ "$needs_catalog_proxy" -eq 1 ] || return 0
    "$CURL" -fsS --connect-timeout 2 --max-time 6 \
        --proxy http://127.0.0.1:17891 \
        --output /dev/null "$MODEL_CATALOG_URL" >/dev/null 2>&1
}

antigravity_ready() {
    [ "$needs_antigravity_proxy" -eq 1 ] || return 0
    code="$("$CURL" -sS --connect-timeout 2 --max-time 6 \
        --proxy http://127.0.0.1:17892 \
        --output /dev/null --write-out '%{http_code}' \
        "$ANTIGRAVITY_PROBE_URL" 2>/dev/null || true)"
    http_reachable "$code"
}

wait_for_egress() {
    [ "$needs_catalog_proxy" -eq 1 ] || [ "$needs_antigravity_proxy" -eq 1 ] || return 0

    started="$(date +%s)"
    announced=0
    while ! catalog_ready || ! antigravity_ready; do
        now="$(date +%s)"
        elapsed=$((now - started))
        if [ "$elapsed" -ge "$WAIT_SECONDS" ]; then
            echo "cpa-start: proxy egress was not ready after ${elapsed}s; starting CPA in degraded mode"
            return 1
        fi
        if [ "$announced" -eq 0 ]; then
            echo "cpa-start: waiting up to ${WAIT_SECONDS}s for Mihomo-backed CPA egress"
            announced=1
        fi
        sleep "$WAIT_INTERVAL"
    done

    now="$(date +%s)"
    elapsed=$((now - started))
    echo "cpa-start: required proxy egress is ready after ${elapsed}s"
    return 0
}

wait_for_egress || true
cd "$DATA" || exit 1
exec "$CPA_BIN" -config "$CONFIG"
