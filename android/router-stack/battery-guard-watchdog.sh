#!/system/bin/sh
# Independent fail-open supervisor for 30-battery-guard.sh.
# This script never disables charging. It can only force charging on and
# restart a missing or stale primary controller.

STATE_DIR=/data/adb/router-stack
ENABLE_FILE="$STATE_DIR/battery-guard.enabled"
LOG_FILE="$STATE_DIR/battery-guard-watchdog.log"
PID_FILE="$STATE_DIR/battery-guard-watchdog.pid"
GUARD_PID_FILE="$STATE_DIR/battery-guard.pid"
GUARD=/data/adb/service.d/30-battery-guard.sh
CAPACITY=/sys/class/power_supply/battery/capacity
DISABLE_CHARGE=/sys/class/power_supply/battery/op_disable_charge
STATUS_FILE=/data/data/com.termux/files/home/router-data/admin/battery-guard.status
LOW=60
MAX_HEARTBEAT_AGE=45
INTERVAL=20

mkdir -p "$STATE_DIR"
chmod 700 "$STATE_DIR"

process_matches() {
  candidate=$1
  script_name=$2
  [ -n "$candidate" ] || return 1
  [ -r "/proc/$candidate/cmdline" ] || return 1
  command_line=$(tr '\000' ' ' < "/proc/$candidate/cmdline" 2>/dev/null)
  case "$command_line" in
    *"$script_name"*) return 0 ;;
    *) return 1 ;;
  esac
}

if [ -r "$PID_FILE" ]; then
  old_pid=$(cat "$PID_FILE" 2>/dev/null)
  if kill -0 "$old_pid" 2>/dev/null && \
     process_matches "$old_pid" '/data/adb/service.d/31-battery-guard-watchdog.sh'; then
    exit 0
  fi
  rm -f "$PID_FILE"
fi

force_allow_charging() {
  attempt=0
  while [ "$attempt" -lt 3 ]; do
    echo 0 > "$DISABLE_CHARGE" 2>/dev/null || true
    [ "$(cat "$DISABLE_CHARGE" 2>/dev/null)" = "0" ] && return 0
    attempt=$((attempt + 1))
    sleep 1
  done
  return 1
}

valid_capacity() {
  case "$1" in
    ''|*[!0-9]*) return 1 ;;
  esac
  [ "$1" -ge 0 ] 2>/dev/null && [ "$1" -le 100 ] 2>/dev/null
}

(
  cleanup() {
    rm -f "$PID_FILE"
  }
  trap 'cleanup; exit 0' HUP INT TERM
  trap cleanup EXIT

  # Give Magisk service.d and Android power_supply nodes time to settle.
  sleep 15
  while true; do
    capacity=$(cat "$CAPACITY" 2>/dev/null)

    # Invalid telemetry and a low battery always override every other state.
    if ! valid_capacity "$capacity" || [ "$capacity" -le "$LOW" ]; then
      force_allow_charging || true
    fi

    if [ ! -f "$ENABLE_FILE" ]; then
      force_allow_charging || true
      sleep "$INTERVAL"
      continue
    fi

    guard_pid=$(cat "$GUARD_PID_FILE" 2>/dev/null)
    guard_alive=0
    if kill -0 "$guard_pid" 2>/dev/null && \
       process_matches "$guard_pid" '/data/adb/service.d/30-battery-guard.sh'; then
      guard_alive=1
    fi

    updated=$(sed -n 's/^updated=//p' "$STATUS_FILE" 2>/dev/null | head -n 1)
    now=$(date +%s)
    case "$updated" in
      ''|*[!0-9]*) heartbeat_age=999999 ;;
      *) heartbeat_age=$((now - updated)) ;;
    esac

    if [ "$guard_alive" != "1" ] || \
       [ "$heartbeat_age" -lt 0 ] || [ "$heartbeat_age" -gt "$MAX_HEARTBEAT_AGE" ]; then
      # Establish the safe state before killing or starting anything.
      force_allow_charging || true
      if [ "$guard_alive" = "1" ]; then
        kill -9 "$guard_pid" 2>/dev/null || true
        sleep 1
      fi
      rm -f "$GUARD_PID_FILE"
      safe_state=$(cat "$DISABLE_CHARGE" 2>/dev/null)
      printf '%s restarting controller old_pid=%s alive=%s heartbeat_age=%s capacity=%s safe_state=%s\n' \
        "$(date '+%Y-%m-%dT%H:%M:%S%z')" "${guard_pid:-none}" "$guard_alive" "$heartbeat_age" "${capacity:-unknown}" "${safe_state:-unknown}"
      /system/bin/sh "$GUARD" >/dev/null 2>&1 || true
    fi

    sleep "$INTERVAL"
  done
) >> "$LOG_FILE" 2>&1 &

worker=$!
printf '%s\n' "$worker" > "$PID_FILE"
chmod 600 "$PID_FILE"

exit 0
