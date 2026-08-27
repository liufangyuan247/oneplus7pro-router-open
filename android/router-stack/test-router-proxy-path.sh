#!/system/bin/sh
set -eu

PREFIX=/data/data/com.termux/files/usr
IP="$PREFIX/bin/ip"
SINGBOX="$PREFIX/bin/sing-box"
CURL="$PREFIX/bin/curl"
BASE=/data/adb/router-network
NS=op7proxytest
ROOT_IF=op7test-r
PEER_IF=op7test-n
PID=""

cleanup() {
  "$IP" rule del priority 7990 2>/dev/null || true
  "$IP" route flush table 101 2>/dev/null || true
  "$IP" link del "$ROOT_IF" 2>/dev/null || true
  "$IP" netns del "$NS" 2>/dev/null || true
  [ -n "$PID" ] && kill "$PID" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

ss -lntup 2>/dev/null | grep -q ':17890 ' || {
  echo MIHOMO_ROUTER_MIXED_PORT_DOWN >&2
  exit 1
}

"$SINGBOX" check -c "$BASE/runtime/sing-box.json"
"$SINGBOX" run -c "$BASE/runtime/sing-box.json" > "$BASE/sing-box-path-test.log" 2>&1 &
PID=$!

count=0
while [ ! -e /sys/class/net/op7proxy ] && [ "$count" -lt 10 ]; do
  sleep 1
  count=$((count + 1))
done
[ -e /sys/class/net/op7proxy ] || { echo PROXY_TUN_DOWN >&2; exit 1; }

"$IP" netns del "$NS" 2>/dev/null || true
"$IP" link del "$ROOT_IF" 2>/dev/null || true
"$IP" netns add "$NS"
"$IP" link add "$ROOT_IF" type veth peer name "$PEER_IF"
"$IP" link set "$PEER_IF" netns "$NS"
"$IP" addr add 10.250.0.1/24 dev "$ROOT_IF"
"$IP" link set "$ROOT_IF" up
"$IP" -n "$NS" addr add 10.250.0.2/24 dev "$PEER_IF"
"$IP" -n "$NS" link set lo up
"$IP" -n "$NS" link set "$PEER_IF" up
"$IP" -n "$NS" route add default via 10.250.0.1

"$IP" route replace 10.250.0.0/24 dev "$ROOT_IF" table 101
"$IP" route replace default dev op7proxy table 101
"$IP" rule add priority 7990 from 10.250.0.0/24 ipproto 6 lookup 101

result="$("$IP" netns exec "$NS" "$CURL" -k -sS -I --max-time 15 https://1.1.1.1/ 2>&1)"
printf '%s\n' "$result" | grep -q 'HTTP/' || {
  printf '%s\n' "$result" >&2
  exit 1
}

echo ROUTER_PROXY_PATH_OK
