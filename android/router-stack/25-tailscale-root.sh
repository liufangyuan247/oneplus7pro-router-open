#!/system/bin/sh
set -u

BASE=/data/adb/tailscale
BIN="$BASE/bin"
STATE="$BASE/state/tailscaled.state"
SOCKET="$BASE/run/tailscaled.sock"
PIDFILE="$BASE/run/tailscaled.pid"
SUPERVISOR_PIDFILE="$BASE/run/supervisor.pid"
LOG="$BASE/log/tailscaled.log"
SUPERVISOR_LOG="$BASE/log/supervisor.log"
UNDERLAY="$BASE/bin/tailscale-underlay"

export HOME="$BASE"
export XDG_CACHE_HOME="$BASE/cache"
export TMPDIR="$BASE/tmp"

mkdir -p "$BASE/run" "$BASE/log" "$BASE/cache" "$BASE/tmp" "$BASE/state"
chmod 700 "$BASE" "$BASE/run" "$BASE/state"

process_is() {
  pid="$1"
  expected="$2"
  [ -n "$pid" ] && [ -r "/proc/$pid/exe" ] || return 1
  actual="$(readlink "/proc/$pid/exe" 2>/dev/null)"
  [ "$actual" = "$expected" ] || [ "$actual" = "$expected (deleted)" ]
}

supervisor_alive() {
  [ -s "$SUPERVISOR_PIDFILE" ] || return 1
  pid="$(cat "$SUPERVISOR_PIDFILE" 2>/dev/null)"
  [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null || return 1
  tr '\000' ' ' < "/proc/$pid/cmdline" 2>/dev/null | \
    grep -Fq '25-tailscale-root.sh supervise'
}

daemon_alive() {
  [ -s "$PIDFILE" ] || return 1
  pid="$(cat "$PIDFILE" 2>/dev/null)"
  process_is "$pid" "$BIN/tailscaled"
}

rotate_log() {
  size=0
  [ -f "$LOG" ] && size="$(wc -c < "$LOG" 2>/dev/null)"
  if [ "${size:-0}" -gt 2097152 ]; then
    rm -f "$LOG.2"
    [ ! -f "$LOG.1" ] || mv -f "$LOG.1" "$LOG.2"
    mv -f "$LOG" "$LOG.1"
  fi
}

log_supervisor() {
  printf '%s %s\n' "$(date '+%F %T')" "$*" >> "$SUPERVISOR_LOG"
}

if [ "${1:-}" != supervise ]; then
  if supervisor_alive; then
    exit 0
  fi
  /system/bin/sh "$0" supervise >> "$SUPERVISOR_LOG" 2>&1 &
  exit 0
fi

if supervisor_alive && [ "$(cat "$SUPERVISOR_PIDFILE" 2>/dev/null)" != "$$" ]; then
  exit 0
fi
echo $$ > "$SUPERVISOR_PIDFILE"
trap 'rm -f "$SUPERVISOR_PIDFILE"' EXIT
trap 'exit 0' INT TERM

count=0
while [ "$(getprop sys.boot_completed)" != "1" ] && [ "$count" -lt 180 ]; do
  sleep 2
  count=$((count + 1))
done

log_supervisor "supervisor started pid=$$"
while :; do
  if [ -f "$BASE/enabled" ] && [ -x "$BIN/tailscaled" ]; then
    sysctl -w net.ipv4.ip_forward=1 >/dev/null 2>&1 || true
    sysctl -w net.ipv6.conf.all.forwarding=1 >/dev/null 2>&1 || true
    if ! "$UNDERLAY" >/dev/null 2>&1; then
      log_supervisor 'underlay route unavailable; retrying'
      sleep 3
      continue
    fi

    if ! daemon_alive; then
      stale="$(cat "$PIDFILE" 2>/dev/null || true)"
      [ -z "$stale" ] || log_supervisor "discarding stale tailscaled pid=$stale"
      rotate_log
      rm -f "$PIDFILE" "$SOCKET"
      "$BIN/tailscaled" \
        --state="$STATE" \
        --socket="$SOCKET" \
        --port=41641 >> "$LOG" 2>&1 &
      echo $! > "$PIDFILE"
      log_supervisor "tailscaled started pid=$(cat "$PIDFILE")"
      sleep 5
    fi
  fi
  sleep 15
done
