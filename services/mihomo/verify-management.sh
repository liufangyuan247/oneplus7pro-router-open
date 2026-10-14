#!/data/data/com.termux/files/usr/bin/sh
set -eu

PREFIX=/data/data/com.termux/files/usr
HOME=/data/data/com.termux/files/home
CURL="$PREFIX/bin/curl"
JQ="$PREFIX/bin/jq"
ADMIN=http://127.0.0.1:9090
CONTROLLER=http://127.0.0.1:9091
SERVICE="$HOME/router-stack/services/mihomo"
DISABLED="$HOME/router-data/mihomo/disabled"

wait_controller() {
    attempt=0
    while [ "$attempt" -lt 20 ]; do
        if "$CURL" -fsS --max-time 2 "$CONTROLLER/version" >/dev/null 2>&1; then
            return 0
        fi
        attempt=$((attempt + 1))
        sleep 1
    done
    return 1
}

echo API_STATUS
"$CURL" -fsS "$ADMIN/api/mihomo/status" | "$JQ" '{online,mode,mixedPort,allowLan,tunEnabled}'

echo SUBSCRIPTIONS
SUBSCRIPTIONS=$("$CURL" -fsS "$ADMIN/api/mihomo/subscriptions")
printf '%s' "$SUBSCRIPTIONS" | "$JQ" '{activeId,itemCount:(.items|length),autoUpdateHours,sources:[.items[].source]}'
ACTIVE_ID=$(printf '%s' "$SUBSCRIPTIONS" | "$JQ" -r '.activeId // ""')
if [ -n "$ACTIVE_ID" ]; then
    "$JQ" -nc --arg id "$ACTIVE_ID" '{id:$id,action:"update"}' | \
        "$CURL" -fsS -X PATCH -H 'Content-Type: application/json' --data-binary @- \
        "$ADMIN/api/mihomo/subscriptions" >/dev/null
    wait_controller
    echo SUBSCRIPTION_UPDATE_OK
fi

echo DASHBOARD_PROXY
"$CURL" -fsS "$ADMIN/mihomo-dashboard/" | grep -q '<title>MetaCubeXD</title>'
"$CURL" -fsS "$ADMIN/mihomo-dashboard/config.js" | grep -q '/mihomo-api'
"$CURL" -fsS "$ADMIN/mihomo-api/version" | "$JQ" -r .version

echo STOP_START
"$JQ" -nc '{Name:"mihomo",Action:"stop"}' | \
    "$CURL" -fsS -X POST -H 'Content-Type: application/json' --data-binary @- \
    "$ADMIN/api/services/action" >/dev/null
[ -f "$DISABLED" ]
if "$CURL" -fsS --max-time 2 "$CONTROLLER/version" >/dev/null 2>&1; then
    echo 'Mihomo remained online after stop' >&2
    exit 1
fi
"$JQ" -nc '{Name:"mihomo",Action:"start"}' | \
    "$CURL" -fsS -X POST -H 'Content-Type: application/json' --data-binary @- \
    "$ADMIN/api/services/action" >/dev/null
[ ! -f "$DISABLED" ]
wait_controller
echo STOP_START_OK

echo PERSISTED_SELECTION
"$CURL" -fsS "$CONTROLLER/proxies/Codex-%E6%97%A5%E6%9C%AC" | "$JQ" -r .now

echo EGRESS
"$CURL" -sS --connect-timeout 8 --max-time 20 \
    --proxy socks5h://127.0.0.1:17891 \
    --output /dev/null --write-out '%{http_code} %{time_total}\n' \
    https://api.openai.com/v1/models

echo CODEX_COUNTRY
CODEX_COUNTRY=$("$CURL" -fsS --max-time 20 \
    --proxy socks5h://127.0.0.1:17891 \
    https://www.cloudflare.com/cdn-cgi/trace | "$PREFIX/bin/awk" -F= '$1 == "loc" { print $2 }')
echo "$CODEX_COUNTRY"
[ "$CODEX_COUNTRY" = JP ] || { echo 'Codex listener is not using a Japanese exit' >&2; exit 1; }

echo SAFETY
"$CURL" -fsS "$CONTROLLER/configs" | "$JQ" '{mixedPort:."mixed-port",allowLan:."allow-lan",bindAddress:."bind-address",tun:.tun.enable}'
if ! ss -lntup 2>/dev/null | grep -q ':17891 '; then
    echo 'Codex dedicated listener 17891 is missing' >&2
    exit 1
fi
if ss -lntup 2>/dev/null | grep -q ':7890 '; then
    echo 'reserved FlClash port 7890 is unexpectedly active' >&2
    exit 1
fi
if ip rule show 2>/dev/null | grep -Eq '^800[01]:.*lookup 101'; then
    echo 'LAN transparent proxy policy is unexpectedly active' >&2
    exit 1
fi
echo SAFETY_OK
