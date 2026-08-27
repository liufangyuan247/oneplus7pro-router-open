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

mkdir -p "$BASE"
chmod 700 "$BASE" 2>/dev/null || true

log() {
  printf '%s %s\n' "$(date '+%F %T')" "$*" >> "$LOG"
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
  restore_script "$MANAGER_BACKUP" "$MANAGER" 700 manager || return 1
  rm -f "$MANAGER_PENDING" "$MANAGER_TRIAL"
  if [ -s "$MANAGER_PID" ]; then
    kill "$(cat "$MANAGER_PID" 2>/dev/null)" 2>/dev/null || true
  fi
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

if [ -f "$MANAGER_PENDING" ]; then
  if [ -f "$MANAGER_TRIAL" ]; then
    log "unconfirmed manager trial found during boot"
    rollback_manager || true
  else
    : > "$MANAGER_TRIAL"
    manager_watch=1
    log "manager trial armed"
  fi
fi

if [ -f "$SERVICE_PENDING" ]; then
  if [ -f "$SERVICE_TRIAL" ]; then
    log "unconfirmed supervisor trial found during boot"
    rollback_service || true
  else
    : > "$SERVICE_TRIAL"
    service_watch=1
    log "supervisor trial armed"
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
