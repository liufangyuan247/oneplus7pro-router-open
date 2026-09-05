#!/system/bin/sh
set -eu

PREFIX=/data/data/com.termux/files/usr
ARIA_CONF=/data/data/com.termux/files/home/router-data/aria2/aria2.conf
ARIA_SECRET=$("$PREFIX/bin/sed" -n 's/^rpc-secret=//p' "$ARIA_CONF" | "$PREFIX/bin/head" -n 1)
ARIA_AUTH=
[ -n "$ARIA_SECRET" ] && ARIA_AUTH="\"token:$ARIA_SECRET\""

echo 'ROOT_HEADERS'
"$PREFIX/bin/curl" -fsSI http://127.0.0.1:6880/ | sed -n '1,5p'

echo 'RPC_RESULT'
"$PREFIX/bin/curl" -fsS \
    -H 'Content-Type: application/json' \
    --data-binary "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"aria2.getVersion\",\"params\":[$ARIA_AUTH]}" \
    http://127.0.0.1:6880/jsonrpc
echo
