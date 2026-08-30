#!/usr/bin/env sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
MANAGER="$SCRIPT_DIR/../router-network.sh"
TEST_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/router-static-leases.XXXXXX")
case "$TEST_ROOT" in "${TMPDIR:-/tmp}"/router-static-leases.*) ;; *) exit 1 ;; esac
trap 'rm -rf "$TEST_ROOT"' EXIT INT TERM

export ROUTER_NETWORK_SOURCE_ONLY=1
. "$MANAGER"

BASE="$TEST_ROOT/base"
RUNTIME="$BASE/runtime"
CONFIG="$BASE/config"
DNSMASQ_PID="$BASE/dnsmasq.pid"
DNSMASQ_STATIC_HOSTS="$RUNTIME/dnsmasq-static-hosts"
DNSMASQ_HOSTS_FILE="$BASE/dnsmasq-hosts.conf"
DNSMASQ_LEASE_FILES="$TEST_ROOT/dnsmasq.leases"
DNSMASQ="$TEST_ROOT/dnsmasq"
GAWK=awk
LAN_ADDRESS=192.168.2.1/24
mkdir -p "$RUNTIME"

cat > "$CONFIG" <<EOF
WAN_USB_PORT='test-port'
PPPOE_USER='test-user'
PPPOE_PASS='test-pass'
DNSMASQ_HOSTS_FILE='$DNSMASQ_HOSTS_FILE'
LAN_ADDRESS='192.168.2.1/24'
EOF
cat > "$DNSMASQ" <<'EOF'
#!/usr/bin/env sh
[ "${STATIC_LEASE_TEST_REJECT:-0}" != 1 ]
EOF
chmod 700 "$DNSMASQ"
: > "$DNSMASQ_HOSTS_FILE"
: > "$DNSMASQ_LEASE_FILES"

update_static_lease set 02:aa:bb:00:10:20 192.168.2.200 demo-client >/dev/null
[ "$(static_lease_list)" = "$(printf '02:aa:bb:00:10:20\t192.168.2.200\tdemo-client')" ]
grep -Fxq '02:aa:bb:00:10:20,192.168.2.200,demo-client' "$DNSMASQ_STATIC_HOSTS"

# Reapplying a MAC replaces rather than duplicates its single authoritative entry.
update_static_lease set 02:aa:bb:00:10:20 192.168.2.201 demo-client >/dev/null
[ "$(grep -c '^dhcp-host=02:aa:bb:00:10:20,' "$DNSMASQ_HOSTS_FILE")" = 1 ]
[ "$(static_lease_list)" = "$(printf '02:aa:bb:00:10:20\t192.168.2.201\tdemo-client')" ]

# Static, dynamic, router, network and malformed conflicts never alter the source.
printf '%s\n' '0 02:aa:bb:00:10:30 192.168.2.202 other *' > "$DNSMASQ_LEASE_FILES"
before=$(sha256sum "$DNSMASQ_HOSTS_FILE" | awk '{print $1}')
for args in \
  'set 02:aa:bb:00:10:40 192.168.2.201 conflict' \
  'set 02:aa:bb:00:10:40 192.168.2.202 conflict' \
  'set 02:aa:bb:00:10:40 192.168.2.1 router' \
  'set 02:aa:bb:00:10:40 192.168.3.20 outside' \
  'set invalid 192.168.2.203 bad'; do
  # The fixed arguments are test data, not externally supplied shell input.
  # shellcheck disable=SC2086
  if update_static_lease $args >/dev/null 2>&1; then
    echo "invalid static lease unexpectedly succeeded: $args" >&2
    exit 1
  fi
done
[ "$(sha256sum "$DNSMASQ_HOSTS_FILE" | awk '{print $1}')" = "$before" ]

# A dnsmasq validation failure rolls back without publishing the candidate.
export STATIC_LEASE_TEST_REJECT=1
if update_static_lease set 02:aa:bb:00:10:40 192.168.2.203 rejected >/dev/null 2>&1; then
  echo 'dnsmasq rejection unexpectedly succeeded' >&2
  exit 1
fi
unset STATIC_LEASE_TEST_REJECT
[ "$(sha256sum "$DNSMASQ_HOSTS_FILE" | awk '{print $1}')" = "$before" ]

update_static_lease clear 02:aa:bb:00:10:20 >/dev/null
[ -z "$(static_lease_list)" ]
update_static_lease clear 02:aa:bb:00:10:20 >/dev/null

echo STATIC_LEASE_TEST_OK
