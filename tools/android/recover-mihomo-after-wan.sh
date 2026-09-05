#!/system/bin/sh
set -eu

PREFIX=/data/data/com.termux/files/usr
HOME_DIR=/data/data/com.termux/files/home
DATA="$HOME_DIR/router-data/mihomo"
SERVICE="$HOME_DIR/router-stack/services/mihomo"
CORE="$PREFIX/bin/mihomo"
SV="$PREFIX/bin/sv"
CURL="$PREFIX/bin/curl"
MANAGER=/data/adb/router-network/router-network
DISABLED="$DATA/disabled"

rollback() {
  printf '%s\n' 'disabled by guarded WAN recovery rollback' > "$DISABLED"
  chmod 600 "$DISABLED"
  chown 10207:10207 "$DISABLED" 2>/dev/null || true
  "$SV" down "$SERVICE" >/dev/null 2>&1 || true
  "$MANAGER" proxy-off >/dev/null 2>&1 || true
  echo 'MIHOMO_RECOVERY_ROLLED_BACK' >&2
  exit 1
}

/system/bin/ip -4 route show table 100 | grep -q '^default dev ppp' || {
  echo 'physical PPPoE route is not ready; Mihomo was left disabled' >&2
  exit 1
}

"$CORE" -t -d "$DATA" -f "$DATA/config.yaml" >/dev/null
rm -f "$DISABLED"
"$SV" up "$SERVICE"

ready=0
attempt=0
while [ "$attempt" -lt 30 ]; do
  if "$CURL" -fsS --max-time 2 http://127.0.0.1:9091/configs >/dev/null 2>&1 &&
     "$PREFIX/bin/ss" -lnt 2>/dev/null | grep -q ':17891 ' &&
     [ -e /sys/class/net/op7tun ] &&
     /system/bin/ip -4 route show table 2022 | grep -q 'dev op7tun'; then
    ready=1
    break
  fi
  attempt=$((attempt + 1))
  sleep 1
done
if [ "$ready" != 1 ]; then
  echo 'Mihomo readiness check failed' >&2
  "$SV" status "$SERVICE" >&2 || true
  "$PREFIX/bin/ss" -lnt >&2 || true
  /system/bin/ip -4 route show table 2022 >&2 || true
  rollback
fi
echo "MIHOMO_LOCAL_READY after=${attempt}s"

# Give URL-test providers a short warm-up window after the controller and TUN
# become ready. This avoids treating the first asynchronous node probe as an
# immediate service failure.
sleep 5

code=$("$CURL" -sS --connect-timeout 8 --max-time 20 \
  --proxy http://127.0.0.1:17891 \
  --output /dev/null --write-out '%{http_code}' \
  https://api.openai.com/v1/models 2>/dev/null || true)
echo "OPENAI_PROXY_HTTP=$code"
case "$code" in 2??|3??|4??) ;; *) rollback ;; esac

key=$(tr -d '\r\n' < "$HOME_DIR/router-data/cpa/api-key")
cpa_code=$("$CURL" -sS --max-time 15 \
  -H "Authorization: Bearer $key" \
  --output /dev/null --write-out '%{http_code}' \
  http://127.0.0.1:8317/v1/models 2>/dev/null || true)
echo "CPA_HTTP=$cpa_code"
[ "$cpa_code" = 200 ] || rollback

echo "MIHOMO_RECOVERED openai_http=$code cpa_http=$cpa_code"
