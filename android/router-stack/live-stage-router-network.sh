#!/system/bin/sh
set -eu

BASE=/data/adb/router-network
SERVICE=/data/adb/service.d/22-router-network.sh
GUARD=/data/adb/service.d/01-router-rollback-guard.sh
UPDATER="$BASE/safe-update-router-network"
CANDIDATE="${1:-/data/local/tmp/router-network-v2.candidate}"
STARTED=0

process_alive() {
  pid="$1"
  kill -0 "$pid" 2>/dev/null || return 1
  [ -r "/proc/$pid/stat" ] || return 1
  [ "$(awk '{print $3}' "/proc/$pid/stat" 2>/dev/null)" != Z ]
}

stop_pidfile_process() {
  pidfile="$1"
  label="$2"
  [ -s "$pidfile" ] || return 0
  pid="$(cat "$pidfile" 2>/dev/null || true)"
  case "$pid" in ''|*[!0-9]*) return 1 ;; esac
  if process_alive "$pid"; then
    kill "$pid" 2>/dev/null || true
    elapsed=0
    while process_alive "$pid" && [ "$elapsed" -lt 30 ]; do
      sleep 1
      elapsed=$((elapsed + 1))
    done
    if process_alive "$pid"; then
      echo "$label did not stop within 30 seconds" >&2
      return 1
    fi
  fi
  rm -f "$pidfile"
}

restart_on_error() {
  if [ "$STARTED" = 0 ]; then
    rm -f "$BASE/supervisor.pid" "$BASE/manager.pid"
    /system/bin/sh "$SERVICE" >> "$BASE/service.log" 2>&1 || true
  fi
}
trap restart_on_error EXIT INT TERM

/system/bin/sh -n "$CANDIDATE"
"$CANDIDATE" validate

stop_pidfile_process "$BASE/supervisor.pid" supervisor
stop_pidfile_process "$BASE/manager.pid" manager

"$UPDATER" stage "$CANDIDATE"
/system/bin/sh "$GUARD" >/dev/null 2>&1
/system/bin/sh "$SERVICE"
STARTED=1
trap - EXIT INT TERM
echo LIVE_TRIAL_STARTED
