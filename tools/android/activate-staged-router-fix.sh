#!/system/bin/sh
set -eu

BASE=/data/adb/router-network
SERVICE=/data/adb/service.d/22-router-network.sh
SUPERVISOR_PID="$BASE/supervisor.pid"
MANAGER_PID="$BASE/manager.pid"
PPPD_PID="$BASE/pppd.pid"
EXPECTED_MANAGER_SHA256="${1:-}"

[ -n "$EXPECTED_MANAGER_SHA256" ] || {
  echo 'usage: activate-staged-router-fix.sh EXPECTED_MANAGER_SHA256' >&2
  exit 2
}

process_alive() {
  pid="$1"
  case "$pid" in ''|*[!0-9]*) return 1 ;; esac
  kill -0 "$pid" 2>/dev/null
}

stop_pidfile_process() {
  pidfile="$1"
  [ -s "$pidfile" ] || return 0
  pid="$(cat "$pidfile" 2>/dev/null || true)"
  process_alive "$pid" || { rm -f "$pidfile"; return 0; }
  kill -TERM "$pid" 2>/dev/null || true
  elapsed=0
  while process_alive "$pid" && [ "$elapsed" -lt 10 ]; do
    sleep 1
    elapsed=$((elapsed + 1))
  done
  process_alive "$pid" && return 1
  rm -f "$pidfile"
}

restore_staged_files() {
  stop_pidfile_process "$SUPERVISOR_PID" || true
  stop_pidfile_process "$MANAGER_PID" || true
  if [ -f "$BASE/router-network.pending" ] && [ -s "$BASE/router-network.backup" ]; then
    cp -p "$BASE/router-network.backup" "$BASE/router-network.recovering"
    chmod 700 "$BASE/router-network.recovering"
    mv -f "$BASE/router-network.recovering" "$BASE/router-network"
  fi
  if [ -f "$BASE/22-router-network.sh.pending" ] && [ -s "$BASE/22-router-network.sh.backup" ]; then
    cp -p "$BASE/22-router-network.sh.backup" "$SERVICE.recovering"
    chmod 755 "$SERVICE.recovering"
    mv -f "$SERVICE.recovering" "$SERVICE"
  fi
  rm -f "$BASE/router-network.pending" "$BASE/router-network.trial" \
    "$BASE/22-router-network.sh.pending" "$BASE/22-router-network.sh.trial"
  /system/bin/sh "$SERVICE"
}

pppd_before="$(cat "$PPPD_PID" 2>/dev/null || true)"
stop_pidfile_process "$SUPERVISOR_PID"
stop_pidfile_process "$MANAGER_PID"
/system/bin/sh "$SERVICE"

attempt=0
while [ "$attempt" -lt 15 ]; do
  sleep 5
  status="$BASE/runtime/activation-status.$$"
  "$BASE/safe-update-router-network" status > "$status" 2>&1 || true
  actual_manager_sha256="$(sha256sum "$BASE/router-network" | awk '{print $1}')"
  if grep -q '^pending=no$' "$status" && [ "$actual_manager_sha256" != "$EXPECTED_MANAGER_SHA256" ]; then
    rm -f "$status"
    echo "manager candidate was rolled back: actual=$actual_manager_sha256 expected=$EXPECTED_MANAGER_SHA256" >&2
    exit 1
  fi
  if grep -q '^pending=no$' "$status" && [ "$actual_manager_sha256" = "$EXPECTED_MANAGER_SHA256" ] &&
     grep -q '^health=ready$' "$status" &&
     [ ! -f "$BASE/22-router-network.sh.pending" ] &&
     [ -s "$SUPERVISOR_PID" ] && process_alive "$(cat "$SUPERVISOR_PID")" &&
     [ -s "$MANAGER_PID" ] && process_alive "$(cat "$MANAGER_PID")"; then
    rm -f "$status"
    pppd_after="$(cat "$PPPD_PID" 2>/dev/null || true)"
    echo "STAGED_ROUTER_FIX_ACTIVE pppd_before=$pppd_before pppd_after=$pppd_after"
    exit 0
  fi
  rm -f "$status"
  attempt=$((attempt + 1))
done

echo 'staged router fix did not confirm healthy; restoring previous scripts' >&2
restore_staged_files
exit 1
