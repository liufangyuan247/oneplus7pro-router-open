#!/system/bin/sh
set -eu

BASE=/data/adb/router-network
SERVICE=/data/adb/service.d/22-router-network.sh
GUARD=/data/adb/service.d/01-router-rollback-guard.sh
UPDATER="$BASE/safe-update-router-network"
CANDIDATE="${1:-/data/local/tmp/router-network-v2.candidate}"
STARTED=0

restart_on_error() {
  if [ "$STARTED" = 0 ]; then
    rm -f "$BASE/supervisor.pid" "$BASE/manager.pid"
    /system/bin/sh "$SERVICE" >> "$BASE/service.log" 2>&1 || true
  fi
}
trap restart_on_error EXIT INT TERM

/system/bin/sh -n "$CANDIDATE"
"$CANDIDATE" validate

if [ -s "$BASE/supervisor.pid" ]; then
  kill "$(cat "$BASE/supervisor.pid")" 2>/dev/null || true
fi
if [ -s "$BASE/manager.pid" ]; then
  kill "$(cat "$BASE/manager.pid")" 2>/dev/null || true
fi
sleep 2
rm -f "$BASE/supervisor.pid" "$BASE/manager.pid"

"$UPDATER" stage "$CANDIDATE"
/system/bin/sh "$GUARD" >/dev/null 2>&1
/system/bin/sh "$SERVICE"
STARTED=1
trap - EXIT INT TERM
echo LIVE_TRIAL_STARTED
