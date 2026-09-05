#!/system/bin/sh
set -eu

base=/data/adb/router-network
config="$base/config"
manager="$base/router-network"
runtime="$base/runtime"
log="$base/kernel-pppoe-trial.log"
marker="$runtime/kernel-pppoe-trial-confirmed"
watchdog_pid="$runtime/kernel-pppoe-trial-watchdog.pid"
rollback_lock="$runtime/kernel-pppoe-trial-rollback.lock"
prefix=/data/data/com.termux/files/usr

log_msg() {
  printf '%s %s\n' "$(date '+%F %T')" "$*" >> "$log"
}

http_ready() {
  code="$($prefix/bin/curl -sS --connect-timeout 5 --max-time 15 \
    --proxy http://127.0.0.1:17891 --output /dev/null --write-out '%{http_code}' \
    https://api.openai.com/v1/models 2>/dev/null || true)"
  case "$code" in 2??|3??|4??) return 0 ;; *) return 1 ;; esac
}

kernel_ready() {
  status="$runtime/kernel-trial-status.$$"
  "$manager" status > "$status" 2>&1 || { rm -f "$status"; return 1; }
  grep -q '^active=yes$' "$status" &&
    grep -q '^ppp=ppp0$' "$status" &&
    grep -q '^pppoe_backend_active=kernel$' "$status" &&
    grep -q '^pppoe_kernel_session=active$' "$status" &&
    grep -q '^mihomo_tun=up$' "$status" &&
    grep -q '^codex_policy=active$' "$status" &&
    [ -f "$runtime/pppoe-kernel-proven" ] &&
    http_ready
  result=$?
  rm -f "$status"
  return "$result"
}

userspace_ready() {
  [ -n "$(pidof pppoe 2>/dev/null || true)" ] || return 1
  status="$runtime/userspace-fallback-status.$$"
  "$manager" status > "$status" 2>&1 || { rm -f "$status"; return 1; }
  grep -q '^active=yes$' "$status" &&
    grep -q '^ppp=ppp0$' "$status" &&
    grep -q '^mihomo_tun=up$' "$status" &&
    grep -q '^codex_policy=active$' "$status" &&
    http_ready
  result=$?
  rm -f "$status"
  return "$result"
}

restore_userspace() {
  backup="$1"
  mkdir "$rollback_lock" 2>/dev/null || return 0
  if [ -s "$backup" ]; then
    cp -p "$backup" "$config.recovering"
    chmod 600 "$config.recovering"
    chown root:root "$config.recovering" 2>/dev/null || true
    mv -f "$config.recovering" "$config"
  fi
  if ! userspace_ready; then
    "$manager" pppoe-retry-userspace >> "$log" 2>&1 || true
  fi
  rmdir "$rollback_lock" 2>/dev/null || true
  log_msg 'restored stable userspace PPPoE configuration'
}

stop_watchdog() {
  pid="$(cat "$watchdog_pid" 2>/dev/null || true)"
  case "$pid" in
    ''|*[!0-9]*) ;;
    *)
      for child in $(ps -A -o PID,PPID | awk -v parent="$pid" '$2 == parent { print $1 }'); do
        kill -KILL "$child" 2>/dev/null || true
      done
      kill -KILL "$pid" 2>/dev/null || true
      ;;
  esac
  rm -f "$watchdog_pid"
}

watchdog() {
  backup="$1"
  sleep 150
  if [ -f "$marker" ] || kernel_ready; then
    touch "$marker"
    log_msg 'watchdog observed a healthy kernel PPPoE path'
  else
    log_msg 'watchdog timed out; forcing userspace rollback'
    restore_userspace "$backup"
  fi
  rm -f "$watchdog_pid"
}

start_trial() {
  [ -s "$config" ]
  [ -x "$manager" ]
  userspace_ready || {
    echo 'current userspace router is not healthy; refusing kernel trial' >&2
    exit 1
  }

  timestamp="$(date '+%Y%m%d-%H%M%S')"
  mkdir -p "$base/backups" "$runtime"
  backup="$base/backups/config.pre-kernel-pppoe-$timestamp"
  cp -p "$config" "$backup"
  chmod 600 "$backup"

  candidate="$runtime/config.kernel-pppoe.$$"
  awk '
    !/^PPPOE_BACKEND=/ &&
    !/^PPPOE_KERNEL_TRIAL_SECONDS=/ &&
    !/^PPPOE_KERNEL_DOWN_FALLBACK_SECONDS=/ { print }
  ' "$config" > "$candidate"
  {
    echo "PPPOE_BACKEND='auto'"
    echo "PPPOE_KERNEL_TRIAL_SECONDS='45'"
    echo "PPPOE_KERNEL_DOWN_FALLBACK_SECONDS='120'"
  } >> "$candidate"
  chmod 600 "$candidate"
  chown root:root "$candidate" 2>/dev/null || true
  mv -f "$candidate" "$config"

  if ! "$manager" validate >> "$log" 2>&1; then
    restore_userspace "$backup"
    echo 'kernel PPPoE preflight failed; userspace configuration restored' >&2
    exit 1
  fi

  rm -f "$marker" "$rollback_lock"
  cp "$0" "$base/safe-kernel-pppoe-trial.new"
  chmod 700 "$base/safe-kernel-pppoe-trial.new"
  mv -f "$base/safe-kernel-pppoe-trial.new" "$base/safe-kernel-pppoe-trial"
  /system/bin/nohup /system/bin/sh "$base/safe-kernel-pppoe-trial" watchdog "$backup" \
    >> "$log" 2>&1 </dev/null &
  echo $! > "$watchdog_pid"
  log_msg "trial started backup=$backup watchdog=$(cat "$watchdog_pid")"

  if ! "$manager" pppoe-retry-kernel >> "$log" 2>&1; then
    restore_userspace "$backup"
    echo 'kernel PPPoE retry could not start; userspace restored' >&2
    exit 1
  fi

  elapsed=0
  successes=0
  while [ "$elapsed" -lt 120 ]; do
    sleep 5
    elapsed=$((elapsed + 5))
    if kernel_ready; then
      successes=$((successes + 1))
    else
      successes=0
    fi
    if [ "$successes" -ge 3 ]; then
      touch "$marker"
      stop_watchdog
      log_msg 'kernel PPPoE confirmed by three end-to-end checks'
      echo "KERNEL_PPPOE_CONFIRMED backup=$backup"
      return 0
    fi
  done

  restore_userspace "$backup"
  echo 'kernel PPPoE did not pass end-to-end checks; userspace restored' >&2
  exit 1
}

case "${1:-start}" in
  start) start_trial ;;
  watchdog) [ "$#" -eq 2 ] || exit 2; watchdog "$2" ;;
  *) echo "usage: $0 [start|watchdog BACKUP]" >&2; exit 2 ;;
esac
