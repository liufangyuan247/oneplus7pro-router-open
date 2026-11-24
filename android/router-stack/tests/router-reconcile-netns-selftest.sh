#!/system/bin/sh
# Invoke with unshare -n; refuses to change the original network namespace.
set -eu
: "${ROUTER_NETWORK_TEST_MANAGER:?set candidate path}"
: "${ROUTER_ORIGINAL_NETNS:?set original network namespace}"
[ "$(readlink /proc/self/ns/net)" != "$ROUTER_ORIGINAL_NETNS" ] || {
  echo 'Refusing to change the original network namespace' >&2
  exit 2
}
export ROUTER_NETWORK_SOURCE_ONLY=1
. "$ROUTER_NETWORK_TEST_MANAGER"
REAL_IP="$IP"
TEST_ROOT="$(mktemp -d /data/local/tmp/router-reconcile-test.XXXXXX)"
trap 'case "$TEST_ROOT" in /data/local/tmp/router-reconcile-test.*) rm -rf "$TEST_ROOT" ;; esac' EXIT INT TERM

"$IP" link set lo up
for iface in wancheck0 lancheck0 pppcheck0; do
  "$IP" link add "$iface" type dummy
  "$IP" link set "$iface" up
done
"$IP" -4 addr add 203.0.113.2/32 dev pppcheck0
cat > "$TEST_ROOT/ip" <<EOF
#!/system/bin/sh
case "\$*" in
  *'addr replace '*|*'route replace '*|*'rule add '*|*'rule del '*)
    printf '%s\n' "\$*" >> "$TEST_ROOT/writes"
    ;;
esac
if [ -f "$TEST_ROOT/fail-route-read" ] && [ "\$*" = '-4 route show table 100' ]; then
  printf 'default dev pppcheck0 scope link\n'
  exit 1
fi
if [ -f "$TEST_ROOT/fail-address-read" ]; then
  case "\$*" in '-4 -o addr show dev '*) exit 1 ;; esac
fi
exec "$REAL_IP" "\$@"
EOF
chmod 700 "$TEST_ROOT/ip"
IP="$TEST_ROOT/ip"

reconcile() {
  ensure_ipv4_address lancheck0 "$LAN_ADDRESS"
  apply_routes wancheck0 lancheck0 pppcheck0
}
check_routes() {
  snapshot="$("$REAL_IP" -4 route show table 100)"
  managed_route_present "$LAN_SUBNET" lancheck0 192.168.2.1 "$snapshot"
  managed_route_present "$MODEM_SUBNET" wancheck0 192.168.1.2 "$snapshot"
  managed_route_present default pppcheck0 '' "$snapshot"
}

reconcile
ipv4_address_present lancheck0 "$LAN_ADDRESS"
ipv4_address_present wancheck0 "$MODEM_ADDRESS"
check_routes
: > "$TEST_ROOT/writes"
for iteration in 1 2 3; do reconcile; done
[ ! -s "$TEST_ROOT/writes" ]
echo 'PASS healthy reconciliation emits no network writes'

"$REAL_IP" -4 addr del "$LAN_ADDRESS" dev lancheck0
"$REAL_IP" -4 addr del "$MODEM_ADDRESS" dev wancheck0
reconcile
ipv4_address_present lancheck0 "$LAN_ADDRESS"
ipv4_address_present wancheck0 "$MODEM_ADDRESS"
check_routes
echo 'PASS missing LAN and modem addresses repaired'

"$REAL_IP" -4 route replace "$LAN_SUBNET" dev wancheck0 src 192.168.1.2 table 100
reconcile
check_routes
"$REAL_IP" -4 route replace "$LAN_SUBNET" dev lancheck0 src 192.168.1.2 table 100
reconcile
check_routes
echo 'PASS wrong route interface and source repaired'

"$REAL_IP" -4 route del "$MODEM_SUBNET" table 100
"$REAL_IP" -4 route add "$MODEM_SUBNET" dev wancheck0 src 192.168.1.2 metric 10 table 100
reconcile
check_routes
"$REAL_IP" -4 route del default table 100
"$REAL_IP" -4 route add default via 192.168.1.1 dev wancheck0 table 100
reconcile
check_routes
echo 'PASS missing/metric/gateway route corruption repaired'

"$REAL_IP" -4 route del default dev pppcheck0 table main metric 50
"$REAL_IP" -6 route del default dev pppcheck0 table main metric 50
"$REAL_IP" rule del priority "$RULE_WAN"
reconcile
"$REAL_IP" -4 route show table main | grep -q '^default dev pppcheck0.*metric 50'
"$REAL_IP" -6 route show table main | grep -q '^default dev pppcheck0.*metric 50'
"$REAL_IP" rule show priority "$RULE_WAN" | grep -q 'lookup 100'
echo 'PASS IPv4/IPv6 defaults and policy rule repaired'

: > "$TEST_ROOT/fail-route-read"
: > "$TEST_ROOT/writes"
reconcile
[ "$(grep -c 'route replace ' "$TEST_ROOT/writes")" -eq 3 ]
rm "$TEST_ROOT/fail-route-read"
: > "$TEST_ROOT/fail-address-read"
reconcile
rm "$TEST_ROOT/fail-address-read"
check_routes
ipv4_address_present lancheck0 "$LAN_ADDRESS"
ipv4_address_present wancheck0 "$MODEM_ADDRESS"
echo 'PASS failed state reads retain repair behavior'

# Exact prefix checks must not confuse another address with the required CIDR.
"$REAL_IP" -4 addr del "$LAN_ADDRESS" dev lancheck0
"$REAL_IP" -4 addr add 192.168.2.10/24 dev lancheck0
! ipv4_address_present lancheck0 "$LAN_ADDRESS"
reconcile
ipv4_address_present lancheck0 "$LAN_ADDRESS"
echo ROUTER_RECONCILE_TEST_OK
