#!/system/bin/sh
set -eu

BASE=/data/adb/router-network
ACTIVE="$BASE/router-network"
BACKUP="$BASE/router-network.backup"
PENDING="$BASE/router-network.pending"
TRIAL="$BASE/router-network.trial"
RUNTIME="$BASE/runtime"
LOG="$BASE/update.log"

log() {
  printf '%s %s\n' "$(date '+%F %T')" "$*" >> "$LOG"
}

health_check() {
  status="$RUNTIME/update-status.$$"
  "$ACTIVE" status > "$status" 2>&1 || return 1
  grep -q '^armed=yes$' "$status" || return 1
  grep -q '^active=yes$' "$status" || return 1
  ppp="$(sed -n 's/^ppp=//p' "$status" | head -n 1)"
  lan="$(sed -n 's/^lan=\([^ ]*\).*/\1/p' "$status" | head -n 1)"
  dnsmasq_pid="$(sed -n 's/^dnsmasq=//p' "$status" | head -n 1)"
  [ -n "$ppp" ] && [ "$ppp" != down ] || return 1
  [ -n "$lan" ] && [ "$lan" != missing ] || return 1
  [ -n "$dnsmasq_pid" ] && [ "$dnsmasq_pid" != down ] || return 1
  kill -0 "$dnsmasq_pid" 2>/dev/null || return 1
  /system/bin/ip -4 addr show dev "$lan" | grep -q 'inet 192\.168\.2\.1/24' || return 1
  /system/bin/ip -4 route show table 100 | grep -q "^default dev $ppp" || return 1
  if grep -q '^traffic_engine=mihomo$' "$status"; then
    grep -q '^mihomo_router=available$' "$status" || return 1
    grep -q '^mihomo_tun=up$' "$status" || return 1
  fi
}

status() {
  echo "active_sha256=$(sha256sum "$ACTIVE" 2>/dev/null | awk '{print $1}')"
  echo "backup_sha256=$(sha256sum "$BACKUP" 2>/dev/null | awk '{print $1}')"
  echo "pending=$([ -f "$PENDING" ] && echo yes || echo no)"
  echo "trial=$([ -f "$TRIAL" ] && echo yes || echo no)"
  if health_check; then
    echo 'health=ready'
  else
    echo 'health=not-ready'
  fi
  rm -f "$RUNTIME/update-status.$$"
}

rollback() {
  [ -s "$BACKUP" ] || { echo 'backup missing' >&2; exit 1; }
  /system/bin/sh -n "$BACKUP"
  cp -p "$BACKUP" "$ACTIVE.recovering"
  chown root:root "$ACTIVE.recovering" 2>/dev/null || true
  chmod 700 "$ACTIVE.recovering"
  mv -f "$ACTIVE.recovering" "$ACTIVE"
  rm -f "$PENDING" "$TRIAL"
  sync
  log "manual rollback installed $(sha256sum "$ACTIVE" | awk '{print $1}')"
  echo 'ROLLED_BACK; running daemon was not restarted'
}

stage() {
  candidate="$1"
  [ -f "$candidate" ] || { echo "candidate missing: $candidate" >&2; exit 1; }
  [ ! -e "$PENDING" ] || { echo 'another candidate is already pending' >&2; exit 1; }
  [ -s "$ACTIVE" ] || { echo 'active manager missing' >&2; exit 1; }
  /system/bin/sh -n "$ACTIVE"
  /system/bin/sh -n "$candidate"
  health_check || { rm -f "$RUNTIME/update-status.$$"; echo 'current router is not healthy; refusing update' >&2; exit 1; }
  rm -f "$RUNTIME/update-status.$$"

  cp -p "$candidate" "$BASE/router-network.candidate"
  chown root:root "$BASE/router-network.candidate" 2>/dev/null || true
  chmod 700 "$BASE/router-network.candidate"
  "$BASE/router-network.candidate" validate >/dev/null

  active_hash="$(sha256sum "$ACTIVE" | awk '{print $1}')"
  candidate_hash="$(sha256sum "$BASE/router-network.candidate" | awk '{print $1}')"
  if [ "$active_hash" = "$candidate_hash" ]; then
    rm -f "$BASE/router-network.candidate"
    echo 'NO_CHANGE'
    exit 0
  fi

  cp -p "$ACTIVE" "$BACKUP.new"
  chown root:root "$BACKUP.new" 2>/dev/null || true
  chmod 700 "$BACKUP.new"
  mv -f "$BACKUP.new" "$BACKUP"
  mv -f "$BASE/router-network.candidate" "$ACTIVE"
  {
    echo "installed=$(date '+%F %T')"
    echo "active_sha256=$candidate_hash"
    echo "backup_sha256=$active_hash"
    echo "pppd_pid=$(cat "$BASE/pppd.pid" 2>/dev/null || true)"
  } > "$PENDING.new"
  chmod 600 "$PENDING.new"
  mv -f "$PENDING.new" "$PENDING"
  rm -f "$TRIAL"
  sync
  log "staged candidate=$candidate_hash backup=$active_hash"
  echo 'STAGED; running daemon unchanged; candidate will be trialed on next boot'
}

mkdir -p "$BASE" "$RUNTIME"
chmod 700 "$BASE" "$RUNTIME"

case "${1:-status}" in
  status) status ;;
  stage) [ "$#" -eq 2 ] || { echo "usage: $0 stage CANDIDATE" >&2; exit 2; }; stage "$2" ;;
  rollback) rollback ;;
  *) echo "usage: $0 {status|stage CANDIDATE|rollback}" >&2; exit 2 ;;
esac
