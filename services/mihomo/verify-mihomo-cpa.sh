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
"$PREFIX/bin/curl" -fsS http://127.0.0.1:9091/proxies/AI | "$PREFIX/bin/jq" -r .now
"$PREFIX/bin/curl" -sS --max-time 20 \
    --proxy http://127.0.0.1:17891 \
    --output /dev/null --write-out '%{http_code} %{time_total}\n' \
    https://api.openai.com/v1/models

echo ANTIGRAVITY_EGRESS
"$PREFIX/bin/curl" -fsS http://127.0.0.1:9091/proxies/Antigravity | "$PREFIX/bin/jq" -r .now
while IFS= read -r domain; do
    case "$domain" in ''|'#'*) continue ;; esac
    grep -Fq -- "- DOMAIN-SUFFIX,$domain,Antigravity" \
        "$HOME/router-data/mihomo/config.yaml"
done < "$HOME/router-data/mihomo/antigravity-proxy-domains.txt"
"$PREFIX/bin/curl" -sS --max-time 20 \
    --proxy http://127.0.0.1:17892 \
    --output /dev/null --write-out '%{http_code} %{time_total}\n' \
    https://cloudcode-pa.googleapis.com/
antigravity_auth=0
for file in "$HOME/router-data/cpa/auth"/*.json; do
    [ -f "$file" ] || continue
    if "$PREFIX/bin/jq" -e '.type == "antigravity"' "$file" >/dev/null 2>&1; then
        "$PREFIX/bin/jq" -e '.proxy_url == "http://127.0.0.1:17892"' "$file" >/dev/null
        antigravity_auth=$((antigravity_auth + 1))
    fi
done
[ "$antigravity_auth" -gt 0 ]
echo "auth_files=$antigravity_auth proxy=http://127.0.0.1:17892"

echo LISTENERS
ss -lntup 2>/dev/null | grep -E ':(7890|17890|17891|17892|9091) ' || true

echo ROUTER_POLICY
"$PREFIX/bin/su" -c '/data/adb/router-network/router-network status'
"$PREFIX/bin/su" -c '/data/data/com.termux/files/usr/bin/ip -4 rule show priority 5000; /data/data/com.termux/files/usr/bin/ip -4 route show table 2022'
