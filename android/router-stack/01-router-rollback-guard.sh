#!/system/bin/sh

BASE="${ROUTER_SAFETY_BASE:-/data/adb/router-network}"
SERVICE="${ROUTER_SERVICE_PATH:-/data/adb/service.d/22-router-network.sh}"
MANAGER="$BASE/router-network"
MANAGER_BACKUP="$BASE/router-network.backup"
MANAGER_PENDING="$BASE/router-network.pending"
MANAGER_TRIAL="$BASE/router-network.trial"
SERVICE_BACKUP="$BASE/22-router-network.sh.backup"
SERVICE_PENDING="$BASE/22-router-network.sh.pending"
SERVICE_TRIAL="$BASE/22-router-network.sh.trial"
SUPERVISOR_PID="$BASE/supervisor.pid"
MANAGER_PID="$BASE/manager.pid"
LOG="$BASE/rollback.log"
TIMEOUT="${ROUTER_ROLLBACK_TIMEOUT:-180}"
PPPD_OPTIONS="$BASE/runtime/pppd.options"
PPPOE_BACKEND_STATE="$BASE/runtime/pppoe-backend"
PPPOE=/data/data/com.termux/files/usr/bin/pppoe

mkdir -p "$BASE"
chmod 700 "$BASE" 2>/dev/null || true

log() {
  printf '%s %s\n' "$(date '+%F %T')" "$*" >> "$LOG"
}

candidate_ppp_pids() {
  for pid in $(pidof pppd pppoe 2>/dev/null || true); do
    [ -r "/proc/$pid/cmdline" ] || continue
    cmdline="$(tr '\000' ' ' < "/proc/$pid/cmdline" 2>/dev/null || true)"
    case "$cmdline" in
      "/system/bin/pppd file $PPPD_OPTIONS"*|"$PPPOE "*)
        echo "$pid"
        ;;
    esac
  done
  return 0
}

quiesce_candidate_ppp() {
  # A live manager update normally adopts the existing PPPoE session. Do not
  # tear that session down merely because the control-plane trial timed out.
  # Only quiesce PPP when the candidate actually replaced the recorded PID.
  baseline_pid="$(sed -n 's/^pppd_pid=//p' "$MANAGER_PENDING" 2>/dev/null | head -n 1)"
  current_pid="$(cat "$BASE/pppd.pid" 2>/dev/null || true)"
  case "$baseline_pid:$current_pid" in
    *[!0-9:]*|:*) ;;
    *)
      if [ "$baseline_pid" = "$current_pid" ] && kill -0 "$current_pid" 2>/dev/null; then
        log "preserving adopted PPPoE pid=$current_pid during manager rollback"
        return 0
      fi
      ;;
  esac
  [ -s "$PPPOE_BACKEND_STATE" ] || return 0
  pids="$(candidate_ppp_pids)"
  for pid in $pids; do kill -TERM "$pid" 2>/dev/null || true; done
  elapsed=0
  while [ -n "$(candidate_ppp_pids)" ] && [ "$elapsed" -lt 10 ]; do
    sleep 1
    elapsed=$((elapsed + 1))
  done
  pids="$(candidate_ppp_pids)"
  for pid in $pids; do kill -KILL "$pid" 2>/dev/null || true; done
  rm -f "$BASE/pppd.pid" "$BASE/runtime/pppoe-backend" \
    "$BASE/runtime/pppoe-backend-pid" "$BASE/runtime/pppoe-backend-started-at" \
    "$BASE/runtime/pppoe-kernel-proven" "$BASE/runtime/pppoe-switching"
  log "candidate PPPoE processes quiesced for rollback"
}

restore_script() {
  backup="$1"
  target="$2"
  mode="$3"
  label="$4"
  [ -s "$backup" ] || {
    log "rollback failed: missing $label backup"
    return 1
  }
  /system/bin/sh -n "$backup" >/dev/null 2>&1 || {
    log "rollback failed: invalid $label backup"
    return 1
  }
  cp -p "$backup" "$target.recovering" || return 1
  chown root:root "$target.recovering" 2>/dev/null || true
  chmod "$mode" "$target.recovering" || return 1
  mv -f "$target.recovering" "$target" || return 1
  sync
  log "restored $label backup"
}

rollback_manager() {
  if [ -s "$MANAGER_PID" ]; then
    kill "$(cat "$MANAGER_PID" 2>/dev/null)" 2>/dev/null || true
    sleep 1
  fi
  quiesce_candidate_ppp || true
  restore_script "$MANAGER_BACKUP" "$MANAGER" 700 manager || return 1
  rm -f "$MANAGER_PENDING" "$MANAGER_TRIAL"
}

rollback_service() {
  restore_script "$SERVICE_BACKUP" "$SERVICE" 755 supervisor || return 1
  rm -f "$SERVICE_PENDING" "$SERVICE_TRIAL"
  if [ -s "$SUPERVISOR_PID" ]; then
    kill "$(cat "$SUPERVISOR_PID" 2>/dev/null)" 2>/dev/null || true
  fi
  /system/bin/sh "$SERVICE" >> "$BASE/service.log" 2>&1 &
}

manager_watch=0
service_watch=0
boot_id="$(cat /proc/sys/kernel/random/boot_id 2>/dev/null || true)"

# Magisk can invoke service.d entries more than once during the same boot.
# Store the boot ID in each trial marker so a duplicate invocation does not
# mistake the trial it just armed for an unconfirmed trial from an older boot.
# An empty legacy marker still counts as stale and is rolled back.

if [ -f "$MANAGER_PENDING" ]; then
  if [ -f "$MANAGER_TRIAL" ]; then
    trial_boot_id="$(cat "$MANAGER_TRIAL" 2>/dev/null || true)"
    if [ -n "$boot_id" ] && [ "$trial_boot_id" = "$boot_id" ]; then
      log "duplicate manager guard invocation ignored for boot=$boot_id"
    else
      log "unconfirmed manager trial found from previous boot"
      rollback_manager || true
    fi
  else
    printf '%s\n' "$boot_id" > "$MANAGER_TRIAL.new.$$"
    mv -f "$MANAGER_TRIAL.new.$$" "$MANAGER_TRIAL"
    manager_watch=1
    log "manager trial armed for boot=$boot_id"
  fi
fi

if [ -f "$SERVICE_PENDING" ]; then
  if [ -f "$SERVICE_TRIAL" ]; then
    trial_boot_id="$(cat "$SERVICE_TRIAL" 2>/dev/null || true)"
    if [ -n "$boot_id" ] && [ "$trial_boot_id" = "$boot_id" ]; then
      log "duplicate supervisor guard invocation ignored for boot=$boot_id"
    else
      log "unconfirmed supervisor trial found from previous boot"
      rollback_service || true
    fi
  else
    printf '%s\n' "$boot_id" > "$SERVICE_TRIAL.new.$$"
    mv -f "$SERVICE_TRIAL.new.$$" "$SERVICE_TRIAL"
    service_watch=1
    log "supervisor trial armed for boot=$boot_id"
  fi
fi

if [ "$manager_watch" = 1 ] || [ "$service_watch" = 1 ]; then
  (
    sleep "$TIMEOUT"
    if [ -f "$MANAGER_PENDING" ]; then
      log "manager trial timed out"
      rollback_manager || true
    fi
    if [ -f "$SERVICE_PENDING" ]; then
      log "supervisor trial timed out"
      rollback_service || true
    fi
  ) &
fi

exit 0
