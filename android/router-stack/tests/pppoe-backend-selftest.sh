#!/bin/sh
set -eu

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
MANAGER="$SCRIPT_DIR/../router-network.sh"
TEST_ROOT="$(mktemp -d)"
trap 'rm -rf -- "$TEST_ROOT"' EXIT HUP INT TERM

ROUTER_NETWORK_SOURCE_ONLY=1 . "$MANAGER"

RUNTIME="$TEST_ROOT/runtime"
mkdir -p "$RUNTIME"
PPPD_OPTIONS="$RUNTIME/pppd.options"
LOG="$TEST_ROOT/router-network.log"
PPPOE_BACKEND_STATE="$RUNTIME/pppoe-backend"
PPPOE_BACKEND_PID="$RUNTIME/pppoe-backend-pid"
PPPOE_BACKEND_STARTED_AT="$RUNTIME/pppoe-backend-started-at"
PPPOE_KERNEL_PROVEN="$RUNTIME/pppoe-kernel-proven"
PPPOE_KERNEL_FAILED="$RUNTIME/pppoe-kernel-failed"
PPPOE_SWITCH_LOCK="$RUNTIME/pppoe-switching"
PPPOE_KERNEL_DOWN_SINCE="$RUNTIME/pppoe-kernel-down-since"
PPPD_PID="$TEST_ROOT/pppd.pid"
PPPOE_PLUGIN="$TEST_ROOT/rp-pppoe.so"
PPPOE_USER='test-user'
PPPOE_PASS='test-pass'
PPPOE='/test/pppoe'
printf '%s\n' 'test plugin placeholder' > "$PPPOE_PLUGIN"

# Empty process scans must be successful under the manager's `set -e`; a
# false status here would terminate the daemon even though there is no error.
managed_pppd_pids >/dev/null
managed_pppoe_helper_pids >/dev/null

PPPOE_BACKEND=auto
[ "$(select_pppoe_backend)" = kernel ]
touch "$PPPOE_KERNEL_FAILED"
[ "$(select_pppoe_backend)" = userspace ]
rm -f "$PPPOE_KERNEL_FAILED"

write_ppp_options eth9 kernel
grep -Fq "plugin \"$PPPOE_PLUGIN\"" "$RUNTIME/pppd.options"
grep -qx 'eth9' "$RUNTIME/pppd.options"
grep -qx '+ipv6' "$RUNTIME/pppd.options"
! grep -q '^pty ' "$RUNTIME/pppd.options"

write_ppp_options eth9 userspace
grep -Fq 'pty "/test/pppoe -I eth9 -T 20 -m 1412 -U"' "$RUNTIME/pppd.options"
grep -qx '+ipv6' "$RUNTIME/pppd.options"
! grep -q '^plugin ' "$RUNTIME/pppd.options"

stop_ppp() {
  rm -f "$PPPOE_BACKEND_STATE" "$PPPOE_BACKEND_PID" \
    "$PPPOE_BACKEND_STARTED_AT" "$PPPOE_KERNEL_PROVEN"
  echo stop >> "$TEST_ROOT/actions"
}
start_ppp() {
  echo "start:$1" >> "$TEST_ROOT/actions"
}
pppoe_switch_holdoff() { :; }

printf '%s\n' kernel > "$PPPOE_BACKEND_STATE"
date +%s | awk '{ print $1 - 120 }' > "$PPPOE_BACKEND_STARTED_AT"
PPPOE_KERNEL_TRIAL_SECONDS=90
fallback_unproven_kernel_pppoe eth9
[ -f "$PPPOE_KERNEL_FAILED" ]
[ "$(sed -n '1p' "$TEST_ROOT/actions")" = stop ]
[ "$(sed -n '2p' "$TEST_ROOT/actions")" = start:eth9 ]

rm -f "$PPPOE_KERNEL_FAILED"
printf '%s\n' kernel > "$PPPOE_BACKEND_STATE"
printf '%s\n' 4242 > "$PPPOE_BACKEND_PID"
printf '%s\n' 4242 > "$PPPD_PID"
managed_pppd_pid_matches() { [ "$1" = 4242 ]; }
mock_ip() {
  case "$*" in
    "-4 addr show dev ppp9") echo 'inet 192.0.2.1/32' ;;
    "-4 route show table 100") echo 'default dev ppp9 scope link' ;;
  esac
}
IP=mock_ip
kernel_pppoe_session_ready() { return 0; }
kernel_pppoe_proxy_probe() { echo 401; }
# Whole-home direct mode must not block the loopback selective proxy probe.
mihomo_policy_ready() { return 1; }
code="$(kernel_pppoe_transport_ready ppp9)"
[ "$code" = 401 ]
record_pppoe_backend_success ppp9
[ -f "$PPPOE_KERNEL_PROVEN" ]

rm -f "$PPPOE_KERNEL_PROVEN"
printf '%s\n' 9999 > "$PPPD_PID"
! record_pppoe_backend_success ppp9
[ ! -f "$PPPOE_KERNEL_PROVEN" ]

: > "$TEST_ROOT/actions"
printf '%s\n' kernel > "$PPPOE_BACKEND_STATE"
touch "$PPPOE_KERNEL_PROVEN"
date +%s | awk '{ print $1 - 180 }' > "$PPPOE_KERNEL_DOWN_SINCE"
PPPOE_KERNEL_DOWN_FALLBACK_SECONDS=120
fallback_unproven_kernel_pppoe eth9
[ -f "$PPPOE_KERNEL_FAILED" ]
[ "$(sed -n '1p' "$TEST_ROOT/actions")" = stop ]
[ "$(sed -n '2p' "$TEST_ROOT/actions")" = start:eth9 ]

echo 'PASS PPPoE backend selection, rendering, PID binding and guarded initial/established fallback'
