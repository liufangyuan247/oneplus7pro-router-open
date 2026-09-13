#!/system/bin/sh
set -eu

# Measure the local ceiling of Mihomo's LAN TPROXY data path without using the
# ISP link. Two network namespaces act as a LAN client and a WAN server. A
# second, temporary Mihomo process is used so the production process and its
# configuration never need to be reloaded.

PREFIX=/data/data/com.termux/files/usr
IP="$PREFIX/bin/ip"
IPTABLES="$PREFIX/bin/iptables"
IPERF="$PREFIX/bin/iperf3"
MIHOMO="$PREFIX/bin/mihomo"
LAN_NS=op7mhblan
WAN_NS=op7mhbwan
LAN_ROOT=mhb-lan-r
LAN_PEER=mhb-lan-n
WAN_ROOT=mhb-wan-r
WAN_PEER=mhb-wan-n
LAN_NET=10.206.0.0/24
LAN_CLIENT=10.206.0.2
LAN_GATEWAY=10.206.0.1
WAN_NET=203.0.113.0/24
WAN_SERVER=203.0.113.2
WAN_GATEWAY=203.0.113.1
TPROXY_PORT=18894
IPERF_PORT=15201
MARK=0x40000000/0x40000000
TABLE=2024
RULE_TPROXY=4994
RULE_WAN=4996
RULE_LAN=4997
RUNTIME="/data/local/tmp/op7-mihomo-ceiling.$$"
MIHOMO_PID=
SERVER_PID=

cleanup() {
  set +e
  [ -n "$SERVER_PID" ] && kill "$SERVER_PID" 2>/dev/null
  [ -n "$MIHOMO_PID" ] && kill "$MIHOMO_PID" 2>/dev/null
  [ -n "$MIHOMO_PID" ] && wait "$MIHOMO_PID" 2>/dev/null
  "$IPTABLES" -w 2 -t mangle -D PREROUTING -i "$LAN_ROOT" -s "$LAN_NET" \
    -p tcp -j TPROXY --on-ip 127.0.0.1 --on-port "$TPROXY_PORT" \
    --tproxy-mark "$MARK" 2>/dev/null
  "$IPTABLES" -w 2 -D INPUT -i "$LAN_ROOT" -m mark --mark "$MARK" \
    -j ACCEPT 2>/dev/null
  "$IP" rule del priority "$RULE_TPROXY" iif "$LAN_ROOT" fwmark "$MARK" \
    lookup "$TABLE" 2>/dev/null
  "$IP" rule del priority "$RULE_WAN" to "$WAN_NET" lookup main 2>/dev/null
  "$IP" rule del priority "$RULE_LAN" to "$LAN_NET" lookup main 2>/dev/null
  "$IP" route flush table "$TABLE" 2>/dev/null
  "$IP" link del "$LAN_ROOT" 2>/dev/null
  "$IP" link del "$WAN_ROOT" 2>/dev/null
  "$IP" netns del "$LAN_NS" 2>/dev/null
  "$IP" netns del "$WAN_NS" 2>/dev/null
  case "$RUNTIME" in /data/local/tmp/op7-mihomo-ceiling.*) rm -rf "$RUNTIME" ;; esac
}
trap cleanup EXIT INT TERM

for priority in "$RULE_TPROXY" "$RULE_WAN" "$RULE_LAN"; do
  if "$IP" rule show priority "$priority" 2>/dev/null | grep -q "^${priority}:"; then
    echo "reserved benchmark rule priority is already occupied: $priority" >&2
    exit 1
  fi
done
for iface in "$LAN_ROOT" "$WAN_ROOT"; do
  if "$IP" link show "$iface" >/dev/null 2>&1; then
    echo "reserved benchmark interface already exists: $iface" >&2
    exit 1
  fi
done

mkdir -p "$PREFIX/var/run/netns" "$RUNTIME"
cat > "$RUNTIME/config.yaml" <<EOF
mixed-port: 0
allow-lan: false
mode: rule
log-level: warning
ipv6: false
listeners:
  - name: ceiling-tproxy
    type: tproxy
    listen: 0.0.0.0
    port: $TPROXY_PORT
    udp: false
rules:
  - MATCH,DIRECT
EOF

"$IP" netns add "$LAN_NS"
"$IP" netns add "$WAN_NS"
"$IP" link add "$LAN_ROOT" type veth peer name "$LAN_PEER"
"$IP" link add "$WAN_ROOT" type veth peer name "$WAN_PEER"
"$IP" link set "$LAN_PEER" netns "$LAN_NS"
"$IP" link set "$WAN_PEER" netns "$WAN_NS"
"$IP" address add "$LAN_GATEWAY/24" dev "$LAN_ROOT"
"$IP" address add "$WAN_GATEWAY/24" dev "$WAN_ROOT"
"$IP" link set "$LAN_ROOT" up
"$IP" link set "$WAN_ROOT" up
"$IP" -n "$LAN_NS" link set lo up
"$IP" -n "$LAN_NS" address add "$LAN_CLIENT/24" dev "$LAN_PEER"
"$IP" -n "$LAN_NS" link set "$LAN_PEER" up
"$IP" -n "$LAN_NS" route add default via "$LAN_GATEWAY"
"$IP" -n "$WAN_NS" link set lo up
"$IP" -n "$WAN_NS" address add "$WAN_SERVER/24" dev "$WAN_PEER"
"$IP" -n "$WAN_NS" link set "$WAN_PEER" up
"$IP" -n "$WAN_NS" route add default via "$WAN_GATEWAY"

"$IP" route replace local default dev lo table "$TABLE"
"$IP" rule add priority "$RULE_TPROXY" iif "$LAN_ROOT" fwmark "$MARK" lookup "$TABLE"
"$IP" rule add priority "$RULE_WAN" to "$WAN_NET" lookup main
"$IP" rule add priority "$RULE_LAN" to "$LAN_NET" lookup main
"$IPTABLES" -w 2 -I INPUT 1 -i "$LAN_ROOT" -m mark --mark "$MARK" -j ACCEPT
"$IPTABLES" -w 2 -t mangle -I PREROUTING 1 -i "$LAN_ROOT" -s "$LAN_NET" \
  -p tcp -j TPROXY --on-ip 127.0.0.1 --on-port "$TPROXY_PORT" \
  --tproxy-mark "$MARK"

"$MIHOMO" -d "$RUNTIME" -f "$RUNTIME/config.yaml" >"$RUNTIME/mihomo.log" 2>&1 &
MIHOMO_PID=$!
attempt=0
while [ "$attempt" -lt 50 ]; do
  if "$PREFIX/bin/ss" -lnt 2>/dev/null | grep -q ":${TPROXY_PORT} "; then
    break
  fi
  kill -0 "$MIHOMO_PID" 2>/dev/null || {
    cat "$RUNTIME/mihomo.log" >&2
    exit 1
  }
  sleep 0.1
  attempt=$((attempt + 1))
done
"$PREFIX/bin/ss" -lnt 2>/dev/null | grep -q ":${TPROXY_PORT} " || {
  echo 'temporary Mihomo listener did not become ready' >&2
  cat "$RUNTIME/mihomo.log" >&2
  exit 1
}

run_test() {
  label="$1"
  shift
  "$IP" netns exec "$WAN_NS" "$IPERF" -s -1 -p "$IPERF_PORT" \
    >"$RUNTIME/iperf-server.log" 2>&1 &
  SERVER_PID=$!
  sleep 0.3
  echo "=== $label ==="
  "$IP" netns exec "$LAN_NS" "$IPERF" -c "$WAN_SERVER" -p "$IPERF_PORT" \
    --connect-timeout 3000 "$@"
  wait "$SERVER_PID"
  SERVER_PID=
}

echo "production_mihomo_pid=$(pidof mihomo | tr ' ' ',')"
echo "temporary_mihomo_pid=$MIHOMO_PID"
run_test 'LAN -> Mihomo TPROXY -> WAN, single TCP stream' -P 1 -t 10 -O 2
run_test 'LAN -> Mihomo TPROXY -> WAN, four TCP streams' -P 4 -t 12 -O 2
run_test 'WAN -> Mihomo TPROXY -> LAN, four TCP streams' -P 4 -t 12 -O 2 -R
echo '=== benchmark counters ==='
"$IPTABLES" -w 2 -t mangle -vnL PREROUTING | grep "$LAN_ROOT" || true
echo "temperature_millic=$(cat /sys/class/thermal/thermal_zone78/temp 2>/dev/null || echo unknown)"
echo 'MIHOMO_TPROXY_CEILING_TEST_COMPLETE'
