#!/system/bin/sh
set -eu

GUARD=/data/local/tmp/01-router-rollback-guard.sh
SUPERVISOR=/data/local/tmp/22-router-network-safe.sh
STABLE_MANAGER=/data/local/tmp/router-safety-fake-stable.sh
FAILED_MANAGER=/data/local/tmp/router-safety-fake-failed.sh
ROOT="$(mktemp -d /data/local/tmp/router-safety-test.XXXXXX)"
BASE="$ROOT/base"
SERVICE="$ROOT/22-router-network.sh"
FAKE_PPP_PID=

cleanup() {
  [ -z "$FAKE_PPP_PID" ] || kill "$FAKE_PPP_PID" 2>/dev/null || true
  if [ -s "$BASE/supervisor.pid" ]; then
    kill "$(cat "$BASE/supervisor.pid" 2>/dev/null)" 2>/dev/null || true
  fi
  if [ -s "$BASE/manager.pid" ]; then
    kill "$(cat "$BASE/manager.pid" 2>/dev/null)" 2>/dev/null || true
  fi
  case "$ROOT" in /data/local/tmp/router-safety-test.*) rm -rf "$ROOT" ;; esac
}
trap cleanup EXIT INT TERM

mkdir -p "$BASE/runtime"
cp "$SUPERVISOR" "$SERVICE"
chmod 755 "$SERVICE"

cp "$STABLE_MANAGER" "$BASE/router-network.backup"
cp "$FAILED_MANAGER" "$BASE/router-network"
chmod 700 "$BASE/router-network.backup" "$BASE/router-network"
/system/bin/sleep 30 &
FAKE_PPP_PID=$!
printf '%s\n' "$FAKE_PPP_PID" > "$BASE/pppd.pid"
printf 'pppd_pid=%s\n' "$FAKE_PPP_PID" > "$BASE/router-network.pending"
: > "$BASE/runtime/pppoe-backend"
: > "$BASE/router-network.trial"
ROUTER_SAFETY_BASE="$BASE" ROUTER_SERVICE_PATH="$SERVICE" /system/bin/sh "$GUARD"
cmp -s "$BASE/router-network" "$BASE/router-network.backup"
[ ! -e "$BASE/router-network.pending" ]
[ ! -e "$BASE/router-network.trial" ]
kill -0 "$FAKE_PPP_PID"
kill "$FAKE_PPP_PID"
wait "$FAKE_PPP_PID" 2>/dev/null || true
FAKE_PPP_PID=
echo 'PASS previous-boot rollback preserves adopted PPPoE'

cp "$FAILED_MANAGER" "$BASE/router-network"
chmod 700 "$BASE/router-network"
: > "$BASE/router-network.pending"
rm -f "$BASE/router-network.trial"
ROUTER_SAFETY_BASE="$BASE" ROUTER_SERVICE_PATH="$SERVICE" ROUTER_ROLLBACK_TIMEOUT=2 /system/bin/sh "$GUARD"
ROUTER_SAFETY_BASE="$BASE" ROUTER_SERVICE_PATH="$SERVICE" ROUTER_ROLLBACK_TIMEOUT=2 /system/bin/sh "$GUARD"
cmp -s "$BASE/router-network" "$FAILED_MANAGER"
[ -e "$BASE/router-network.pending" ]
[ "$(cat "$BASE/router-network.trial")" = "$(cat /proc/sys/kernel/random/boot_id)" ]
rm -f "$BASE/router-network.pending" "$BASE/router-network.trial"
echo 'PASS duplicate same-boot guard invocation preserves candidate'

cp "$FAILED_MANAGER" "$BASE/router-network"
chmod 700 "$BASE/router-network"
: > "$BASE/router-network.pending"
rm -f "$BASE/router-network.trial"
ROUTER_SAFETY_BASE="$BASE" ROUTER_SERVICE_PATH="$SERVICE" ROUTER_ROLLBACK_TIMEOUT=2 /system/bin/sh "$GUARD"
sleep 3
cmp -s "$BASE/router-network" "$BASE/router-network.backup"
[ ! -e "$BASE/router-network.pending" ]
echo 'PASS same-boot timeout rollback'

cp "$STABLE_MANAGER" "$BASE/router-network"
chmod 700 "$BASE/router-network"
cp "$SERVICE" "$BASE/22-router-network.sh.backup"
: > "$BASE/22-router-network.sh.pending"
rm -f "$BASE/22-router-network.sh.trial"
ROUTER_SAFETY_BASE="$BASE" ROUTER_SERVICE_PATH="$SERVICE" ROUTER_SKIP_BOOT_WAIT=1 ROUTER_HEALTH_INTERVAL=1 ROUTER_HEALTH_SUCCESSES=2 ROUTER_HEALTH_TIMEOUT=5 /system/bin/sh "$SUPERVISOR" supervise &
test_supervisor=$!
sleep 4
[ ! -e "$BASE/22-router-network.sh.pending" ]
kill "$test_supervisor" 2>/dev/null || true
[ ! -s "$BASE/manager.pid" ] || kill "$(cat "$BASE/manager.pid")" 2>/dev/null || true
sleep 1
echo 'PASS supervisor confirmation'

cp "$STABLE_MANAGER" "$BASE/router-network"
chmod 700 "$BASE/router-network"
ROUTER_SAFETY_BASE="$BASE" ROUTER_SERVICE_PATH="$SERVICE" ROUTER_SKIP_BOOT_WAIT=1 ROUTER_HEALTH_INTERVAL=1 ROUTER_HEALTH_SUCCESSES=2 ROUTER_HEALTH_TIMEOUT=5 /system/bin/sh "$SERVICE"
sleep 2
[ -s "$BASE/supervisor.pid" ]
first_supervisor="$(cat "$BASE/supervisor.pid")"
kill -0 "$first_supervisor" 2>/dev/null
[ -s "$BASE/manager.pid" ]
first_manager="$(cat "$BASE/manager.pid")"
kill -0 "$first_manager" 2>/dev/null
ROUTER_SAFETY_BASE="$BASE" ROUTER_SERVICE_PATH="$SERVICE" ROUTER_SKIP_BOOT_WAIT=1 \
  ROUTER_HEALTH_INTERVAL=1 ROUTER_HEALTH_SUCCESSES=2 ROUTER_HEALTH_TIMEOUT=5 \
  /system/bin/sh "$SERVICE"
sleep 1
[ "$(cat "$BASE/supervisor.pid")" = "$first_supervisor" ]
[ "$(cat "$BASE/manager.pid")" = "$first_manager" ]
kill "$first_supervisor" 2>/dev/null || true
kill "$first_manager" 2>/dev/null || true
sleep 1
[ ! -e "$BASE/runtime/supervisor.lock" ]
echo 'PASS nonblocking singleton service entrypoint'

cp "$STABLE_MANAGER" "$BASE/router-network.backup"
cp "$FAILED_MANAGER" "$BASE/router-network"
chmod 700 "$BASE/router-network.backup" "$BASE/router-network"
: > "$BASE/router-network.pending"
: > "$BASE/router-network.trial"
ROUTER_SAFETY_BASE="$BASE" ROUTER_SERVICE_PATH="$SERVICE" ROUTER_SKIP_BOOT_WAIT=1 ROUTER_HEALTH_INTERVAL=1 ROUTER_HEALTH_SUCCESSES=2 ROUTER_HEALTH_TIMEOUT=4 /system/bin/sh "$SUPERVISOR" supervise &
test_supervisor=$!
sleep 4
cmp -s "$BASE/router-network" "$BASE/router-network.backup"
[ ! -e "$BASE/router-network.pending" ]
kill "$test_supervisor" 2>/dev/null || true
sleep 1
echo 'PASS failed-manager rollback'

echo ALL_TESTS_PASSED
