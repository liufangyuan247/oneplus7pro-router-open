#!/data/data/com.termux/files/usr/bin/sh
set -eu

PREFIX=/data/data/com.termux/files/usr
CURL="$PREFIX/bin/curl"
JQ="$PREFIX/bin/jq"
CONTROLLER=http://127.0.0.1:9091
MODE_FILE=/data/data/com.termux/files/home/router-data/mihomo/runtime-mode

ORIGINAL=$($CURL -fsS "$CONTROLLER/configs" | $JQ -r .mode)
restore_mode() {
    $JQ -nc --arg mode "$ORIGINAL" '{mode:$mode}' | \
        $CURL -fsS -X PATCH -H 'Content-Type: application/json' \
        --data-binary @- "$CONTROLLER/configs" >/dev/null 2>&1 || true
    printf '%s\n' "$ORIGINAL" > "$MODE_FILE"
}
trap restore_mode EXIT INT TERM

for mode in direct rule global; do
    $JQ -nc --arg mode "$mode" '{mode:$mode}' | \
        $CURL -fsS -X PATCH -H 'Content-Type: application/json' \
        --data-binary @- "$CONTROLLER/configs" >/dev/null
    sleep 1
    actual=$($CURL -fsS "$CONTROLLER/configs" | $JQ -r .mode)
    generic=$($CURL --proxy socks5h://127.0.0.1:17890 --max-time 20 -fsS \
        https://www.cloudflare.com/cdn-cgi/trace 2>/dev/null | \
        sed -n 's/^loc=/loc=/p' || true)
    cpa=$($CURL --proxy socks5h://127.0.0.1:17891 --max-time 20 -fsS \
        https://www.cloudflare.com/cdn-cgi/trace 2>/dev/null | \
        sed -n 's/^loc=/loc=/p' || true)
    youtube=$($CURL --proxy socks5h://127.0.0.1:17890 --max-time 20 -sS \
        -o /dev/null -w '%{http_code}' \
        https://www.youtube.com/generate_204 2>/dev/null || true)
    [ "$cpa" = loc=JP ] || {
        echo "CPA isolation failed in $mode mode: ${cpa:-unreachable}" >&2
        exit 1
    }
    echo "$actual generic:${generic:-unreachable} cpa:$cpa youtube:${youtube:-000}"
done

restore_mode
trap - EXIT INT TERM
echo MODE_ISOLATION_OK
