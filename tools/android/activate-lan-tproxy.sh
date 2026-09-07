#!/system/bin/sh
set -eu

PREFIX=/data/data/com.termux/files/usr
DATA=/data/data/com.termux/files/home/router-data/mihomo
CONFIG="$DATA/config.yaml"
CORE="$PREFIX/bin/mihomo"
CURL="$PREFIX/bin/curl"
SS="$PREFIX/bin/ss"
PORT=17893
STAMP="$(date '+%Y%m%d-%H%M%S')"
BACKUP="$DATA/config.yaml.backup-pre-tproxy-$STAMP"
CANDIDATE="$DATA/config.yaml.tproxy.$$"

cleanup() {
  rm -f "$CANDIDATE"
}
trap cleanup EXIT INT TERM

if grep -q 'name: lan-transparent' "$CONFIG"; then
  "$CORE" -t -d "$DATA" -f "$CONFIG" >/dev/null
  "$CURL" -fsS --max-time 5 -X PUT -H 'Content-Type: application/json' \
    --data-binary "{\"path\":\"$CONFIG\"}" \
    'http://127.0.0.1:9091/configs?force=true' >/dev/null
  "$SS" -lntu | grep -q ":$PORT "
  echo "LAN_TPROXY_CONFIG_PRESENT port=$PORT"
  exit 0
fi

awk -v port="$PORT" '
  !added && /^sniffer:[[:space:]]*$/ {
    print "  - name: lan-transparent"
    print "    type: tproxy"
    print "    listen: 0.0.0.0"
    print "    port: " port
    print "    udp: true"
    added=1
  }
  { print }
  END { if (!added) exit 2 }
' "$CONFIG" > "$CANDIDATE"
chmod 600 "$CANDIDATE"
chown 10207:10207 "$CANDIDATE" 2>/dev/null || true
"$CORE" -t -d "$DATA" -f "$CANDIDATE" >/dev/null

cp -p "$CONFIG" "$BACKUP"
cp -p "$CANDIDATE" "$CONFIG.new"
mv -f "$CONFIG.new" "$CONFIG"

if ! "$CURL" -fsS --max-time 5 -X PUT -H 'Content-Type: application/json' \
    --data-binary "{\"path\":\"$CONFIG\"}" \
    'http://127.0.0.1:9091/configs?force=true' >/dev/null || \
   ! "$SS" -lntu | grep -q ":$PORT "; then
  cp -p "$BACKUP" "$CONFIG.recovering"
  mv -f "$CONFIG.recovering" "$CONFIG"
  "$CURL" -fsS --max-time 5 -X PUT -H 'Content-Type: application/json' \
    --data-binary "{\"path\":\"$CONFIG\"}" \
    'http://127.0.0.1:9091/configs?force=true' >/dev/null || true
  echo 'LAN TPROXY activation failed; restored previous config' >&2
  exit 1
fi

echo "LAN_TPROXY_CONFIG_ACTIVE port=$PORT backup=$BACKUP"
