#!/usr/bin/env sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
START="$SCRIPT_DIR/cpa-start.sh"
WORK="${TMPDIR:-/tmp}/cpa-start-test.$$"
trap 'rm -rf "$WORK"' EXIT INT TERM
mkdir -p "$WORK/bin" "$WORK/data/auth"

cat > "$WORK/bin/cpa" <<'EOF'
#!/data/data/com.termux/files/usr/bin/sh
printf 'CPA_EXEC %s\n' "$*"
EOF
chmod +x "$WORK/bin/cpa"

cat > "$WORK/bin/curl" <<'EOF'
#!/data/data/com.termux/files/usr/bin/sh
count_file="$CPA_TEST_CURL_COUNT"
count=$(cat "$count_file" 2>/dev/null || echo 0)
count=$((count + 1))
printf '%s\n' "$count" > "$count_file"
if [ "$count" -le "${CPA_TEST_FAIL_CALLS:-0}" ]; then
    exit 7
fi
case "$*" in
    *--write-out*) printf '404' ;;
esac
EOF
chmod +x "$WORK/bin/curl"

run_start() {
    CPA_DATA_DIR="$WORK/data" \
    CPA_CONFIG_PATH="$WORK/data/config.yaml" \
    CPA_AUTH_DIR="$WORK/data/auth" \
    CPA_CURL_BIN="$WORK/bin/curl" \
    CPA_BINARY="$WORK/bin/cpa" \
    CPA_TEST_CURL_COUNT="$WORK/curl-count" \
    CPA_START_WAIT_INTERVAL=0 \
    "$START"
}

printf 'proxy-url: ""\n' > "$WORK/data/config.yaml"
rm -f "$WORK/curl-count"
output=$(run_start)
printf '%s\n' "$output" | grep -q '^CPA_EXEC -config '
[ ! -e "$WORK/curl-count" ] || { echo 'direct configuration unexpectedly probed Mihomo' >&2; exit 1; }

printf 'proxy-url: "http://127.0.0.1:17891"\n' > "$WORK/data/config.yaml"
printf '{"type":"antigravity","proxy_url":"http://127.0.0.1:17892"}\n' > "$WORK/data/auth/antigravity.json"
rm -f "$WORK/curl-count"
output=$(CPA_TEST_FAIL_CALLS=2 CPA_START_WAIT_SECONDS=5 run_start)
printf '%s\n' "$output" | grep -q 'waiting up to 5s'
printf '%s\n' "$output" | grep -q 'required proxy egress is ready'
printf '%s\n' "$output" | grep -q '^CPA_EXEC -config '

rm -f "$WORK/curl-count"
output=$(CPA_TEST_FAIL_CALLS=99 CPA_START_WAIT_SECONDS=0 run_start)
printf '%s\n' "$output" | grep -q 'starting CPA in degraded mode'
printf '%s\n' "$output" | grep -q '^CPA_EXEC -config '

echo CPA_START_TEST_OK
