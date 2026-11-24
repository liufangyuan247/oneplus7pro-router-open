#!/system/bin/sh
# Uses fake sysfs/state only; does not change production networking.
set -eu
: "${ROUTER_NETWORK_TEST_MANAGER:?set candidate path}"
export ROUTER_NETWORK_SOURCE_ONLY=1
. "$ROUTER_NETWORK_TEST_MANAGER"
TEST_ROOT="$(mktemp -d /data/local/tmp/router-polling-test.XXXXXX)"
trap 'case "$TEST_ROOT" in /data/local/tmp/router-polling-test.*) rm -rf "$TEST_ROOT" ;; esac' EXIT INT TERM

printf 'one line\n' > "$TEST_ROOT/value"
[ "$(read_line "$TEST_ROOT/value")" = 'one line' ]
printf 'no newline' > "$TEST_ROOT/value"
[ "$(read_line "$TEST_ROOT/value")" = 'no newline' ]
: > "$TEST_ROOT/value"
! read_line "$TEST_ROOT/value"
! read_line "$TEST_ROOT/missing"
printf ' leading and trailing  \n' > "$TEST_ROOT/value"
[ "$(read_line "$TEST_ROOT/value")" = ' leading and trailing  ' ]

NET_CLASS="$TEST_ROOT/net"
mkdir -p "$NET_CLASS/eth0" "$NET_CLASS/eth1" "$NET_CLASS/eth2"
printf 'aa:bb:cc:dd:ee:00\n' > "$NET_CLASS/eth0/address"
printf 'aa:bb:cc:dd:ee:01\n' > "$NET_CLASS/eth1/address"
printf 'aa:bb:cc:dd:ee:02\n' > "$NET_CLASS/eth2/address"
printf '1\n' > "$NET_CLASS/eth0/carrier"
printf '0\n' > "$NET_CLASS/eth1/carrier"
printf '1\n' > "$NET_CLASS/eth2/carrier"
WAN_MAC=AA:BB:CC:DD:EE:00
LAN_MAC=AA:BB:CC:DD:EE:01
[ "$(resolve_wan_iface)" = eth0 ]
[ "$(resolve_lan_iface eth0)" = eth1 ]
has_carrier eth0
! has_carrier eth1
! has_carrier missing
LAN_MAC=''
[ "$(resolve_lan_iface eth0)" = eth2 ]
printf '1\n' > "$NET_CLASS/eth1/carrier"
! resolve_lan_iface eth0
rm -rf "$NET_CLASS/eth2"
[ "$(resolve_lan_iface eth0)" = eth1 ]

cat > "$TEST_ROOT/ip" <<EOF
#!/system/bin/sh
printf '%s\n' "\$*" >> "$TEST_ROOT/ip-calls"
EOF
chmod 700 "$TEST_ROOT/ip"
IP="$TEST_ROOT/ip"
printf '0x1003\n' > "$NET_CLASS/eth0/flags"
ensure_link_up eth0
[ ! -e "$TEST_ROOT/ip-calls" ]
printf '0x1002\n' > "$NET_CLASS/eth0/flags"
ensure_link_up eth0
[ "$(read_line "$TEST_ROOT/ip-calls")" = 'link set eth0 up' ]
: > "$TEST_ROOT/ip-calls"
printf 'malformed\n' > "$NET_CLASS/eth0/flags"
ensure_link_up eth0
[ "$(read_line "$TEST_ROOT/ip-calls")" = 'link set eth0 up' ]

process_alive_pid "$$"
! process_alive_pid invalid
! process_alive_pid 999999
printf '%s\n' "$$" > "$TEST_ROOT/pid"
pid_alive "$TEST_ROOT/pid"
printf '999999\n' > "$TEST_ROOT/pid"
! pid_alive "$TEST_ROOT/pid"
echo ROUTER_POLLING_TEST_OK
