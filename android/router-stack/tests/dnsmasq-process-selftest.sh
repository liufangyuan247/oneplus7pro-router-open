#!/system/bin/sh
set -eu

MANAGER=/data/local/tmp/router-network.candidate
TEST_ROOT="$(mktemp -d /data/local/tmp/router-dnsmasq-test.XXXXXX)"

cleanup() {
  [ -z "${test_dnsmasq:-}" ] || kill "$test_dnsmasq" 2>/dev/null || true
  case "$TEST_ROOT" in /data/local/tmp/router-dnsmasq-test.*) rm -rf "$TEST_ROOT" ;; esac
}
trap cleanup EXIT INT TERM

export ROUTER_NETWORK_SOURCE_ONLY=1
. "$MANAGER"

BASE="$TEST_ROOT/base"
RUNTIME="$BASE/runtime"
DNSMASQ_PID="$BASE/dnsmasq.pid"
DNSMASQ_POLICY="$RUNTIME/dnsmasq-policy"
DNSMASQ_STATIC_SCHEMA_PID="$RUNTIME/dnsmasq-static-hosts-v1.pid"
LOG="$BASE/router-network.log"
DNSMASQ_LOG="$BASE/dnsmasq.log"
mkdir -p "$RUNTIME"
cat > "$RUNTIME/dnsmasq.conf" <<EOF
listen-address=127.0.0.1
bind-interfaces
port=55353
no-resolv
server=1.1.1.1
EOF

"$DNSMASQ" --keep-in-foreground --conf-file="$RUNTIME/dnsmasq.conf" >> "$DNSMASQ_LOG" 2>&1 &
test_dnsmasq=$!
sleep 1
kill -0 "$test_dnsmasq"

# A stale pidfile must be repaired by adopting only the process using this
# test's exact config path.  The production dnsmasq, if present, is untouched.
echo 999999 > "$DNSMASQ_PID"
adopt_dnsmasq
[ "$(cat "$DNSMASQ_PID")" = "$test_dnsmasq" ]
managed_dnsmasq_alive

# Starting again must retain the adopted process instead of forking a second.
start_dnsmasq test0 direct
[ "$(cat "$DNSMASQ_PID")" = "$test_dnsmasq" ]
kill -0 "$test_dnsmasq"

echo DNSMASQ_PROCESS_TEST_OK
