#!/system/bin/sh
set -eu

PREFIX=/data/data/com.termux/files/usr
IP="$PREFIX/bin/ip"
IPERF="$PREFIX/bin/iperf3"
IPTABLES="$PREFIX/bin/iptables"
WAN_NS=op7wan
LAN_NS=op7lan
WAN_ROOT=sim-wan-r
WAN_PEER=sim-wan-n
LAN_ROOT=sim-lan-r
LAN_PEER=sim-lan-n
OLD_FORWARD=$(cat /proc/sys/net/ipv4/ip_forward)
SERVER_PID=

cleanup() {
  set +e
  [ -n "$SERVER_PID" ] && kill "$SERVER_PID" 2>/dev/null
  "$IPTABLES" -t nat -D POSTROUTING -s 10.204.0.0/24 -o "$WAN_ROOT" -j MASQUERADE 2>/dev/null
  "$IPTABLES" -D FORWARD -i "$LAN_ROOT" -o "$WAN_ROOT" -j ACCEPT 2>/dev/null
  "$IPTABLES" -D FORWARD -i "$WAN_ROOT" -o "$LAN_ROOT" -j ACCEPT 2>/dev/null
  "$IP" rule del priority 5 to 10.203.0.0/24 lookup main 2>/dev/null
  "$IP" rule del priority 6 to 10.204.0.0/24 lookup main 2>/dev/null
  "$IP" link del "$WAN_ROOT" 2>/dev/null
  "$IP" link del "$LAN_ROOT" 2>/dev/null
  "$IP" netns del "$WAN_NS" 2>/dev/null
  "$IP" netns del "$LAN_NS" 2>/dev/null
  echo "$OLD_FORWARD" > /proc/sys/net/ipv4/ip_forward
  rm -f /data/local/tmp/op7-iperf-server.log
}
trap cleanup EXIT INT TERM

mkdir -p "$PREFIX/var/run/netns"
"$IP" netns del "$WAN_NS" 2>/dev/null || true
"$IP" netns del "$LAN_NS" 2>/dev/null || true
"$IP" link del "$WAN_ROOT" 2>/dev/null || true
"$IP" link del "$LAN_ROOT" 2>/dev/null || true

"$IP" netns add "$WAN_NS"
"$IP" netns add "$LAN_NS"
"$IP" link add "$WAN_ROOT" type veth peer name "$WAN_PEER"
"$IP" link add "$LAN_ROOT" type veth peer name "$LAN_PEER"
"$IP" link set "$WAN_PEER" netns "$WAN_NS"
"$IP" link set "$LAN_PEER" netns "$LAN_NS"

"$IP" address add 10.203.0.1/24 dev "$WAN_ROOT"
"$IP" address add 10.204.0.1/24 dev "$LAN_ROOT"
"$IP" link set "$WAN_ROOT" up
"$IP" link set "$LAN_ROOT" up

"$IP" -n "$WAN_NS" link set lo up
"$IP" -n "$WAN_NS" address add 10.203.0.2/24 dev "$WAN_PEER"
"$IP" -n "$WAN_NS" link set "$WAN_PEER" up
"$IP" -n "$WAN_NS" route add default via 10.203.0.1

"$IP" -n "$LAN_NS" link set lo up
"$IP" -n "$LAN_NS" address add 10.204.0.2/24 dev "$LAN_PEER"
"$IP" -n "$LAN_NS" link set "$LAN_PEER" up
"$IP" -n "$LAN_NS" route add default via 10.204.0.1

# Android's netd policy rules normally reject unmarked traffic before the
# conventional main-table fallback. These narrowly scoped rules make the two
# synthetic directly connected networks routable for this benchmark only.
"$IP" rule add priority 5 to 10.203.0.0/24 lookup main
"$IP" rule add priority 6 to 10.204.0.0/24 lookup main

echo 1 > /proc/sys/net/ipv4/ip_forward
"$IPTABLES" -t nat -A POSTROUTING -s 10.204.0.0/24 -o "$WAN_ROOT" -j MASQUERADE
"$IPTABLES" -I FORWARD 1 -i "$LAN_ROOT" -o "$WAN_ROOT" -j ACCEPT
"$IPTABLES" -I FORWARD 1 -i "$WAN_ROOT" -o "$LAN_ROOT" -j ACCEPT
"$IP" netns exec "$WAN_NS" "$IPTABLES" -I INPUT 1 -i "$WAN_PEER" -j ACCEPT
"$IP" netns exec "$WAN_NS" "$IPTABLES" -I OUTPUT 1 -o "$WAN_PEER" -j ACCEPT
"$IP" netns exec "$LAN_NS" "$IPTABLES" -I INPUT 1 -i "$LAN_PEER" -j ACCEPT
"$IP" netns exec "$LAN_NS" "$IPTABLES" -I OUTPUT 1 -o "$LAN_PEER" -j ACCEPT

echo '=== Connectivity diagnostics ==='
echo '-- root routes --'
"$IP" route show
echo '-- LAN namespace routes --'
"$IP" -n "$LAN_NS" route show
echo '-- WAN namespace routes --'
"$IP" -n "$WAN_NS" route show
echo '-- LAN to local gateway --'
"$IP" netns exec "$LAN_NS" /system/bin/ping -c 1 -W 1 10.204.0.1 || true
echo '-- root to WAN endpoint --'
/system/bin/ping -c 1 -W 1 10.203.0.2 || true
echo '-- LAN through router to WAN endpoint --'
"$IP" netns exec "$LAN_NS" /system/bin/ping -c 1 -W 1 10.203.0.2 || true
echo '-- link counters and ARP state --'
"$IP" -s link show "$WAN_ROOT" || true
"$IP" -s link show "$LAN_ROOT" || true
"$IP" -n "$WAN_NS" -s link show "$WAN_PEER" || true
"$IP" -n "$LAN_NS" -s link show "$LAN_PEER" || true
"$IP" neigh show || true
"$IP" -n "$WAN_NS" neigh show || true
"$IP" -n "$LAN_NS" neigh show || true
echo '-- FORWARD counters --'
"$IPTABLES" -vnL FORWARD || true
echo '-- namespace firewall counters --'
"$IP" netns exec "$WAN_NS" "$IPTABLES" -vnL INPUT || true
"$IP" netns exec "$WAN_NS" "$IPTABLES" -vnL OUTPUT || true
"$IP" netns exec "$LAN_NS" "$IPTABLES" -vnL INPUT || true
"$IP" netns exec "$LAN_NS" "$IPTABLES" -vnL OUTPUT || true

"$IP" netns exec "$WAN_NS" "$IPERF" -s -1 \
  > /data/local/tmp/op7-iperf-server.log 2>&1 &
SERVER_PID=$!
sleep 1
echo '-- WAN listener --'
"$IP" netns exec "$WAN_NS" "$PREFIX/bin/ss" -lntp || true
echo '-- iperf server startup log --'
cat /data/local/tmp/op7-iperf-server.log || true

echo '=== Simulated LAN to WAN (4 TCP streams, NAT) ==='
"$IP" netns exec "$LAN_NS" "$IPERF" -c 10.203.0.2 -P 4 -t 10 --connect-timeout 5000
wait "$SERVER_PID" || true
SERVER_PID=

"$IP" netns exec "$WAN_NS" "$IPERF" -s -1 \
  > /data/local/tmp/op7-iperf-server.log 2>&1 &
SERVER_PID=$!
sleep 1

echo '=== Simulated WAN to LAN (4 TCP streams, reverse over NAT state) ==='
"$IP" netns exec "$LAN_NS" "$IPERF" -c 10.203.0.2 -P 4 -t 10 -R --connect-timeout 5000
wait "$SERVER_PID" || true
SERVER_PID=
