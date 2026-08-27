#!/system/bin/sh
set -eu

BASE=/data/adb/tailscale
BIN="$BASE/bin"
TS="$BIN/tailscale"
DAEMON="$BIN/tailscaled"
SOCKET="$BASE/run/tailscaled.sock"
PIDFILE="$BASE/run/tailscaled.pid"
SUPERVISOR_PIDFILE="$BASE/run/supervisor.pid"
SERVICE=/data/adb/service.d/25-tailscale-root.sh
ENABLED="$BASE/enabled"
LOGIN_PID="$BASE/run/login.pid"
LOGIN_LOG="$BASE/run/login.log"
CONTROL_LOG="$BASE/log/control.log"
JQ=/data/data/com.termux/files/usr/bin/jq
TIMEOUT=/data/data/com.termux/files/usr/bin/timeout
SUBNET=192.168.2.0/24

export HOME="$BASE"
export XDG_CACHE_HOME="$BASE/cache"
export TMPDIR="$BASE/tmp"
export LD_LIBRARY_PATH=/data/data/com.termux/files/usr/lib

mkdir -p "$BASE/run" "$BASE/log" "$BASE/cache" "$BASE/tmp" "$BASE/state"
chmod 700 "$BASE" "$BASE/run" "$BASE/state"

process_is() {
  pid="$1"
  expected="$2"
  [ -n "$pid" ] && [ -r "/proc/$pid/exe" ] || return 1
  actual="$(readlink "/proc/$pid/exe" 2>/dev/null)"
  [ "$actual" = "$expected" ] || [ "$actual" = "$expected (deleted)" ]
}

daemon_alive() {
  [ -s "$PIDFILE" ] || return 1
  process_is "$(cat "$PIDFILE" 2>/dev/null)" "$DAEMON"
}

supervisor_alive() {
  [ -s "$SUPERVISOR_PIDFILE" ] || return 1
  pid="$(cat "$SUPERVISOR_PIDFILE" 2>/dev/null)"
  [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null || return 1
  tr '\000' ' ' < "/proc/$pid/cmdline" 2>/dev/null | \
    grep -Fq '25-tailscale-root.sh supervise'
}

login_alive() {
  [ -s "$LOGIN_PID" ] || return 1
  pid="$(cat "$LOGIN_PID" 2>/dev/null)"
  [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null
}

log_control() {
  printf '%s %s\n' "$(date '+%F %T')" "$*" >> "$CONTROL_LOG"
}

extract_auth_url() {
  grep -Eo 'https://login\.tailscale\.com/a/[A-Za-z0-9_-]+' "$LOGIN_LOG" 2>/dev/null | tail -n 1
}

start_daemon() {
  [ -x "$TS" ] && [ -x "$DAEMON" ] && [ -x "$SERVICE" ] || {
    echo 'tailscale runtime is incomplete' >&2
    return 1
  }
  touch "$ENABLED"
  chmod 600 "$ENABLED"
  if ! supervisor_alive; then
    rm -f "$SUPERVISOR_PIDFILE"
    /system/bin/sh "$SERVICE"
  fi
  attempt=0
  while [ "$attempt" -lt 25 ]; do
    daemon_alive && [ -S "$SOCKET" ] && return 0
    attempt=$((attempt + 1))
    sleep 1
  done
  echo 'tailscaled did not become ready' >&2
  return 1
}

stop_login() {
  if login_alive; then
    kill "$(cat "$LOGIN_PID")" 2>/dev/null || true
  fi
  rm -f "$LOGIN_PID"
}

stop_daemon() {
  stop_login
  rm -f "$ENABLED"
  if daemon_alive; then
    pid="$(cat "$PIDFILE")"
    kill "$pid" 2>/dev/null || true
    attempt=0
    while kill -0 "$pid" 2>/dev/null && [ "$attempt" -lt 10 ]; do
      attempt=$((attempt + 1))
      sleep 1
    done
    kill -9 "$pid" 2>/dev/null || true
  fi
  # Close a narrow race where the supervisor started a replacement just
  # before it observed that the enabled flag was removed.
  sleep 1
  if daemon_alive; then
    kill "$(cat "$PIDFILE")" 2>/dev/null || true
    sleep 1
  fi
  rm -f "$PIDFILE" "$SOCKET"
}

restart_daemon() {
  stop_login
  touch "$ENABLED"
  if daemon_alive; then
    kill "$(cat "$PIDFILE")" 2>/dev/null || true
  fi
  rm -f "$PIDFILE" "$SOCKET"
  start_daemon
}

login_start() {
  force="$1"
  start_daemon
  stop_login
  : > "$LOGIN_LOG"
  chmod 600 "$LOGIN_LOG"
  force_flag=''
  [ "$force" = 1 ] && force_flag='--force-reauth'
  # Keep the authorization command detached so the authenticated admin API
  # can return the official URL immediately. It expires on its own after five
  # minutes and never stores an auth key.
  nohup "$TIMEOUT" 300 "$TS" --socket="$SOCKET" up \
    --accept-dns=false \
    --accept-routes=false \
    --advertise-routes="$SUBNET" \
    --snat-subnet-routes=true \
    $force_flag > "$LOGIN_LOG" 2>&1 &
  echo $! > "$LOGIN_PID"
  chmod 600 "$LOGIN_PID"

  attempt=0
  while [ "$attempt" -lt 12 ]; do
    url="$(extract_auth_url || true)"
    if [ -n "$url" ]; then
      printf 'auth_url=%s\n' "$url"
      log_control "authorization requested force=$force"
      return 0
    fi
    login_alive || break
    attempt=$((attempt + 1))
    sleep 1
  done

  # A stored node key can complete `up` without a browser authorization URL.
  backend="$("$TS" --socket="$SOCKET" status --json 2>/dev/null | "$JQ" -r '.BackendState // "unknown"' 2>/dev/null || true)"
  if [ "$backend" = Running ]; then
    rm -f "$LOGIN_PID"
    echo 'auth_url='
    log_control "authorization completed without browser force=$force"
    return 0
  fi
  tail -n 8 "$LOGIN_LOG" >&2 || true
  echo 'Tailscale did not provide an authorization URL' >&2
  return 1
}

status() {
  echo "installed=$([ -x "$TS" ] && [ -x "$DAEMON" ] && echo yes || echo no)"
  echo "enabled=$([ -f "$ENABLED" ] && echo yes || echo no)"
  echo "supervisor=$([ -n "$(cat "$SUPERVISOR_PIDFILE" 2>/dev/null || true)" ] && supervisor_alive && echo running || echo stopped)"
  echo "daemon=$([ -n "$(cat "$PIDFILE" 2>/dev/null || true)" ] && daemon_alive && echo running || echo stopped)"
  if login_alive; then
    echo 'login_pending=yes'
    url="$(extract_auth_url || true)"
  else
    rm -f "$LOGIN_PID"
    echo 'login_pending=no'
    url=''
  fi
  echo "auth_url=$url"

  if ! daemon_alive || [ ! -S "$SOCKET" ]; then
    echo 'backend=Stopped'
    echo 'online=no'
    echo 'login_required=no'
    echo 'hostname='
    echo 'ip='
    echo 'advertise_routes='
    echo 'primary_routes='
    echo 'route_status=off'
    return 0
  fi

  json="$BASE/run/status.$$.json"
  prefs="$BASE/run/prefs.$$.json"
  trap 'rm -f "$json" "$prefs"' EXIT INT TERM
  "$TS" --socket="$SOCKET" status --json > "$json" 2>/dev/null || true
  "$TS" --socket="$SOCKET" debug prefs > "$prefs" 2>/dev/null || true
  backend="$("$JQ" -r '.BackendState // "unknown"' "$json" 2>/dev/null || echo unknown)"
  online="$("$JQ" -r '(.Self.Online // false) | if . then "yes" else "no" end' "$json" 2>/dev/null || echo no)"
  hostname="$("$JQ" -r '.Self.HostName // ""' "$json" 2>/dev/null || true)"
  ip="$("$JQ" -r '(.TailscaleIPs // [""])[0]' "$json" 2>/dev/null || true)"
  primary="$("$JQ" -r '(.Self.PrimaryRoutes // []) | join(",")' "$json" 2>/dev/null || true)"
  advertised="$("$JQ" -r '(.AdvertiseRoutes // []) | join(",")' "$prefs" 2>/dev/null || true)"
  login_required=no
  case "$backend" in NeedsLogin|NoState) login_required=yes ;; esac
  route_status=off
  case ",$advertised," in *",$SUBNET,"*) route_status=configured ;; esac
  case ",$primary," in *",$SUBNET,"*) route_status=active ;; esac
  echo "backend=$backend"
  echo "online=$online"
  echo "login_required=$login_required"
  echo "hostname=$hostname"
  echo "ip=$ip"
  echo "advertise_routes=$advertised"
  echo "primary_routes=$primary"
  echo "route_status=$route_status"
  rm -f "$json" "$prefs"
  trap - EXIT INT TERM
}

case "${1:-status}" in
  status) status ;;
  start) start_daemon; log_control 'service started'; status ;;
  stop) stop_daemon; log_control 'service stopped'; status ;;
  restart) restart_daemon; log_control 'service restarted'; status ;;
  login) login_start 0 ;;
  reauth) login_start 1 ;;
  *) echo 'usage: tailscale-control {status|start|stop|restart|login|reauth}' >&2; exit 2 ;;
esac
