#!/data/data/com.termux/files/usr/bin/sh
set -eu

PREFIX=/data/data/com.termux/files/usr
HOME=/data/data/com.termux/files/home

echo SERVICES
"$PREFIX/bin/sv" status \
    "$HOME/router-stack/services/mihomo" \
    "$HOME/router-stack/services/mihomo-updater" \
    "$HOME/router-stack/services/cpa" \
    "$HOME/router-stack/services/router-admin"

echo CPA_CONFIG
"$PREFIX/bin/awk" '/^(usage-statistics-enabled|proxy-url):/ { print }' \
    "$HOME/router-data/cpa/config.yaml"

echo CPA_HTTP
KEY=$(tr -d '\r\n' < "$HOME/router-data/cpa/api-key")
"$PREFIX/bin/curl" -sS --max-time 15 --output /dev/null \
    --write-out '%{http_code} %{time_total}\n' \
    -H "Authorization: Bearer $KEY" \
    http://127.0.0.1:8317/v1/models

echo AUTH_COUNT
find "$HOME/router-data/cpa/auth" -maxdepth 1 -type f 2>/dev/null | wc -l

echo OPENAI_EGRESS
"$PREFIX/bin/curl" -fsS http://127.0.0.1:9091/proxies/Codex-%E6%97%A5%E6%9C%AC | "$PREFIX/bin/jq" -r .now
"$PREFIX/bin/curl" -sS --max-time 20 \
    --noproxy '*' \
    --output /dev/null --write-out '%{http_code} %{time_total}\n' \
    https://api.openai.com/v1/models

echo LISTENERS
ss -lntup 2>/dev/null | grep -E ':(7890|17890|17891|9091) ' || true

echo ROUTER_POLICY
"$PREFIX/bin/su" -c '/data/adb/router-network/router-network status'
"$PREFIX/bin/su" -c '/data/data/com.termux/files/usr/bin/ip -4 rule show; /data/data/com.termux/files/usr/bin/ip -4 route show table 102'
