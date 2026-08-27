#!/data/data/com.termux/files/usr/bin/sh
set -eu

PREFIX=/data/data/com.termux/files/usr
HOME=/data/data/com.termux/files/home
CURL="$PREFIX/bin/curl"
JQ="$PREFIX/bin/jq"
ADMIN=http://127.0.0.1:9090
DISABLED="$HOME/router-data/mihomo/disabled"

restore_core() {
    if [ -f "$DISABLED" ]; then
        "$JQ" -nc '{Name:"mihomo",Action:"start"}' | \
            "$CURL" -fsS -X POST -H 'Content-Type: application/json' \
            --data-binary @- "$ADMIN/api/services/action" >/dev/null 2>&1 || true
    fi
}
trap restore_core EXIT INT TERM

echo ENABLE_ANDROID_PROXY
"$JQ" -nc '{enabled:true}' | \
    "$CURL" -fsS -X PUT -H 'Content-Type: application/json' --data-binary @- \
    "$ADMIN/api/mihomo/system-proxy" | "$JQ" -e '.enabled == true and .address == "127.0.0.1:17891"' >/dev/null
[ "$(su -c '/system/bin/settings get global http_proxy')" = 127.0.0.1:17891 ]
"$CURL" -sS --max-time 20 --proxy http://127.0.0.1:17891 \
    --output /dev/null --write-out '%{http_code}\n' \
    https://www.youtube.com/generate_204 | grep -q '^204$'

echo STOP_AND_OFFLINE_API
"$JQ" -nc '{Name:"mihomo",Action:"stop"}' | \
    "$CURL" -fsS -X POST -H 'Content-Type: application/json' --data-binary @- \
    "$ADMIN/api/services/action" >/dev/null
sleep 1
case "$(su -c '/system/bin/settings get global http_proxy')" in null|:0|'') ;; *) exit 1 ;; esac
"$CURL" -fsS "$ADMIN/api/mihomo/status" | "$JQ" -e '.online == false' >/dev/null
"$CURL" -fsS "$ADMIN/api/mihomo/settings" | "$JQ" -e '.mixedPort == 17890' >/dev/null
"$CURL" -fsS "$ADMIN/api/mihomo/proxies" | "$JQ" -e '.online == false and (.groups | length) == 0' >/dev/null
"$CURL" -fsS "$ADMIN/api/mihomo/connections" | "$JQ" -e '.online == false and (.connections | length) == 0' >/dev/null

echo RESTART_AND_REENABLE
"$JQ" -nc '{Name:"mihomo",Action:"start"}' | \
    "$CURL" -fsS -X POST -H 'Content-Type: application/json' --data-binary @- \
    "$ADMIN/api/services/action" >/dev/null
attempt=0
while [ "$attempt" -lt 25 ]; do
    "$CURL" -fsS --max-time 2 http://127.0.0.1:9091/version >/dev/null 2>&1 && break
    attempt=$((attempt + 1))
    sleep 1
done
"$CURL" -fsS --max-time 2 http://127.0.0.1:9091/version >/dev/null
"$JQ" -nc '{enabled:true}' | \
    "$CURL" -fsS -X PUT -H 'Content-Type: application/json' --data-binary @- \
    "$ADMIN/api/mihomo/system-proxy" | "$JQ" -e '.address == "127.0.0.1:17891"' >/dev/null
trap - EXIT INT TERM
echo OFFLINE_AND_PROXY_OK
