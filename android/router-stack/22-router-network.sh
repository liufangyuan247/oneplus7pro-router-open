#!/system/bin/sh

BASE="${ROUTER_SAFETY_BASE:-/data/adb/router-network}"
SERVICE="${ROUTER_SERVICE_PATH:-/data/adb/service.d/22-router-network.sh}"
MANAGER="$BASE/router-network"
MANAGER_BACKUP="$BASE/router-network.backup"
MANAGER_PENDING="$BASE/router-network.pending"
MANAGER_TRIAL="$BASE/router-network.trial"
SERVICE_PENDING="$BASE/22-router-network.sh.pending"
SERVICE_TRIAL="$BASE/22-router-network.sh.trial"
PIDFILE="$BASE/supervisor.pid"
LOG="$BASE/service.log"
TIMEOUT_BIN=/data/data/com.termux/files/usr/bin/timeout
HEALTH_INTERVAL="${ROUTER_HEALTH_INTERVAL:-10}"
HEALTH_SUCCESSES="${ROUTER_HEALTH_SUCCESSES:-3}"
HEALTH_TIMEOUT="${ROUTER_HEALTH_TIMEOUT:-150}"
PPPD_OPTIONS="$BASE/runtime/pppd.options"
PPPOE_BACKEND_STATE="$BASE/runtime/pppoe-backend"
PPPOE=/data/data/com.termux/files/usr/bin/pppoe

if [ "${1:-}" != supervise ]; then
  /system/bin/sh "$SERVICE" supervise >> "$LOG" 2>&1 &
  exit 0
fi

log() {
  printf '%s %s\n' "$(date '+%F %T')" "$*" >> "$LOG"
}

process_alive() {
  pid="$1"
  kill -0 "$pid" 2>/dev/null || return 1
  [ -r "/proc/$pid/stat" ] || return 1
  [ "$(awk '{print $3}' "/proc/$pid/stat" 2>/dev/null)" != Z ]
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
  # A manager that never selected a new backend has no state marker and keeps
  # the pre-upgrade PPPoE session intact.  If it did select one, rollback must
  # also remove its pppd/helper processes before the stable manager restarts.
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

restore_manager() {
  [ -s "$MANAGER_BACKUP" ] || {
    log "manager rollback failed: backup missing"
    return 1
  }
  /system/bin/sh -n "$MANAGER_BACKUP" >/dev/null 2>&1 || {
    log "manager rollback failed: backup syntax invalid"
    return 1
  }
  cp -p "$MANAGER_BACKUP" "$MANAGER.recovering" || return 1
  chown root:root "$MANAGER.recovering" 2>/dev/null || true
  chmod 700 "$MANAGER.recovering" || return 1
  mv -f "$MANAGER.recovering" "$MANAGER" || return 1
  rm -f "$MANAGER_PENDING" "$MANAGER_TRIAL"
  sync
  log "manager rolled back to backup"
}

manager_status() {
  output="$BASE/runtime/supervisor-status.$$"
  mkdir -p "$BASE/runtime"
  if [ -x "$TIMEOUT_BIN" ]; then
    "$TIMEOUT_BIN" 5 "$MANAGER" status > "$output" 2>&1 || return 1
  else
    "$MANAGER" status > "$output" 2>&1 || return 1
  fi
  grep -q '^armed=yes$' "$output"
}

basic_health() {
  child="$1"
  process_alive "$child" || return 1
  manager_status || return 1
  lan="$(sed -n 's/^lan=\([^ ]*\).*/\1/p' "$BASE/runtime/supervisor-status.$$" | head -n 1)"
  carrier="$(sed -n 's/^lan=[^ ]* carrier=\([^ ]*\).*/\1/p' "$BASE/runtime/supervisor-status.$$" | head -n 1)"
  if [ -n "$lan" ] && [ "$lan" != missing ] && [ "$carrier" = 1 ]; then
    /system/bin/ip -4 addr show dev "$lan" 2>/dev/null | grep -q 'inet 192\.168\.2\.1/24' || return 1
    dnsmasq_pid="$(sed -n 's/^dnsmasq=//p' "$BASE/runtime/supervisor-status.$$" | head -n 1)"
    [ -n "$dnsmasq_pid" ] && [ "$dnsmasq_pid" != down ] && kill -0 "$dnsmasq_pid" 2>/dev/null || return 1
  fi
  return 0
}

full_health() {
  child="$1"
  basic_health "$child" || return 1
  grep -q '^active=yes$' "$BASE/runtime/supervisor-status.$$" || return 1
  ppp="$(sed -n 's/^ppp=//p' "$BASE/runtime/supervisor-status.$$" | head -n 1)"
  [ -n "$ppp" ] && [ "$ppp" != down ] || return 1
  /system/bin/ip -4 route show table 100 2>/dev/null | grep -q "^default dev $ppp" || return 1
  if grep -q '^codex_selective_proxy=enabled$' "$BASE/runtime/supervisor-status.$$"; then
    grep -q '^codex_tun=up$' "$BASE/runtime/supervisor-status.$$" || return 1
    grep -q '^codex_policy=active$' "$BASE/runtime/supervisor-status.$$" || return 1
    targets="$(sed -n 's/^codex_targets=//p' "$BASE/runtime/supervisor-status.$$" | head -n 1)"
    case "$targets" in ''|*[!0-9]*|0) return 1 ;; esac
  fi
  return 0
}

confirm_trials() {
  child="$1"
  elapsed=0
  successes=0
  while [ "$elapsed" -lt "$HEALTH_TIMEOUT" ] && process_alive "$child"; do
    sleep "$HEALTH_INTERVAL"
    elapsed=$((elapsed + HEALTH_INTERVAL))
    healthy=1
    if [ -f "$MANAGER_PENDING" ]; then
      full_health "$child" || healthy=0
    else
      basic_health "$child" || healthy=0
    fi
    if [ "$healthy" = 1 ]; then
      successes=$((successes + 1))
    else
      successes=0
    fi
    if [ "$successes" -ge "$HEALTH_SUCCESSES" ]; then
      if [ -f "$MANAGER_PENDING" ]; then
        rm -f "$MANAGER_PENDING" "$MANAGER_TRIAL"
        log "manager candidate confirmed healthy"
      fi
      if [ -f "$SERVICE_PENDING" ]; then
        rm -f "$SERVICE_PENDING" "$SERVICE_TRIAL"
        log "supervisor candidate confirmed healthy"
      fi
      return 0
    fi
  done
  return 1
}

if [ -s "$PIDFILE" ] && process_alive "$(cat "$PIDFILE" 2>/dev/null)"; then
  exit 0
fi
echo $$ > "$PIDFILE"
trap 'rm -f "$PIDFILE" "$BASE/runtime/supervisor-status.$$"' EXIT
trap 'exit 0' INT TERM

if [ "${ROUTER_SKIP_BOOT_WAIT:-0}" != 1 ]; then
  count=0
  while [ "$(getprop sys.boot_completed)" != 1 ] && [ "$count" -lt 180 ]; do
    sleep 2
    count=$((count + 1))
  done
fi

while [ -x "$MANAGER" ]; do
  if ! /system/bin/sh -n "$MANAGER" >/dev/null 2>&1; then
    log "manager syntax check failed"
    restore_manager || break
  fi

  "$MANAGER" daemon >> "$LOG" 2>&1 &
  child=$!

  if [ -f "$MANAGER_PENDING" ] || [ -f "$SERVICE_PENDING" ]; then
    if ! confirm_trials "$child"; then
      if [ -f "$MANAGER_PENDING" ]; then
        log "manager candidate failed health confirmation"
        kill "$child" 2>/dev/null || true
        sleep 1
        quiesce_candidate_ppp || true
        restore_manager || true
      fi
    fi
  fi

  wait "$child"
  result=$?
  if [ -f "$MANAGER_PENDING" ]; then
    log "manager candidate exited before confirmation: rc=$result"
    quiesce_candidate_ppp || true
    restore_manager || true
  else
    log "manager exited: rc=$result; restarting in 5 seconds"
  fi
  sleep 5
done

exit 0
