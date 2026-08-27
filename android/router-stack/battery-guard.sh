#!/system/bin/sh
# Fail-safe charge hysteresis for an always-powered router appliance.
# Every controller error falls back to charging enabled. Only a healthy,
# current controller is allowed to pause charging above the high threshold.

STATE_DIR=/data/adb/router-stack
ENABLE_FILE="$STATE_DIR/battery-guard.enabled"
LOG_FILE="$STATE_DIR/battery-guard.log"
PID_FILE="$STATE_DIR/battery-guard.pid"
CAPACITY=/sys/class/power_supply/battery/capacity
DISABLE_CHARGE=/sys/class/power_supply/battery/op_disable_charge
USB_PRESENT=/sys/class/power_supply/usb/present
STATUS_DIR=/data/data/com.termux/files/home/router-data/admin
STATUS_FILE="$STATUS_DIR/battery-guard.status"
WATCHDOG=/data/adb/service.d/31-battery-guard-watchdog.sh
LOW=60
HIGH=75
INTERVAL=10

mkdir -p "$STATE_DIR"
chmod 700 "$STATE_DIR"
# Protection is automatically enabled after every boot. Removing the marker
# still disables it for the remainder of the current boot.
touch "$ENABLE_FILE"
chmod 600 "$ENABLE_FILE"

process_matches() {
  candidate=$1
  [ -n "$candidate" ] || return 1
  [ -r "/proc/$candidate/cmdline" ] || return 1
  command_line=$(tr '\000' ' ' < "/proc/$candidate/cmdline" 2>/dev/null)
  case "$command_line" in
    *'/data/adb/service.d/30-battery-guard.sh'*) return 0 ;;
    *) return 1 ;;
  esac
}

if [ -r "$PID_FILE" ]; then
  old_pid=$(cat "$PID_FILE" 2>/dev/null)
  if kill -0 "$old_pid" 2>/dev/null && process_matches "$old_pid"; then
    exit 0
  fi
  rm -f "$PID_FILE"
fi

valid_capacity() {
  case "$1" in
    ''|*[!0-9]*) return 1 ;;
  esac
  [ "$1" -ge 0 ] 2>/dev/null && [ "$1" -le 100 ] 2>/dev/null
}

valid_binary() {
  [ "$1" = "0" ] || [ "$1" = "1" ]
}

write_charge_state() {
  wanted=$1
  attempt=0
  while [ "$attempt" -lt 3 ]; do
    echo "$wanted" > "$DISABLE_CHARGE" 2>/dev/null || true
    actual=$(cat "$DISABLE_CHARGE" 2>/dev/null)
    [ "$actual" = "$wanted" ] && return 0
    attempt=$((attempt + 1))
    sleep 1
  done
  return 1
}

allow_charging() {
  write_charge_state 0
}

write_status() {
  enabled=$1
  paused=$2
  healthy=$3
  capacity=$4
  present=$5
  reason=$6
  mkdir -p "$STATUS_DIR" 2>/dev/null || return 0
  tmp="$STATUS_FILE.tmp.$$"
  {
    echo "enabled=$enabled"
    echo "paused=$paused"
    echo "healthy=$healthy"
    echo "capacity=$capacity"
    echo "usb_present=$present"
    echo "low=$LOW"
    echo "high=$HIGH"
    echo "reason=$reason"
    echo "updated=$(date +%s)"
  } > "$tmp" 2>/dev/null || return 0
  chmod 644 "$tmp" 2>/dev/null || true
  chown 10207:10207 "$tmp" 2>/dev/null || true
  mv -f "$tmp" "$STATUS_FILE" 2>/dev/null || true
}

(
  cleanup_done=0
  cleanup() {
    [ "$cleanup_done" = "1" ] && return 0
    cleanup_done=1
    allow_charging || true
    capacity=$(cat "$CAPACITY" 2>/dev/null)
    present=$(cat "$USB_PRESENT" 2>/dev/null)
    write_status 1 0 0 "${capacity:-unknown}" "${present:-unknown}" controller_exited
    rm -f "$PID_FILE"
  }
  trap 'cleanup; exit 0' HUP INT TERM
  trap cleanup EXIT

  while [ "$(getprop sys.boot_completed)" != "1" ]; do sleep 2; done

  last_applied=unknown
  watchdog_counter=0
  while true; do
    capacity=$(cat "$CAPACITY" 2>/dev/null)
    present=$(cat "$USB_PRESENT" 2>/dev/null)
    current=$(cat "$DISABLE_CHARGE" 2>/dev/null)

    if [ ! -f "$ENABLE_FILE" ]; then
      allow_charging || true
      write_status 0 0 1 "${capacity:-unknown}" "${present:-unknown}" disabled
      sleep "$INTERVAL"
      continue
    fi

    if ! valid_capacity "$capacity" || ! valid_binary "$present" || ! valid_binary "$current"; then
      allow_charging || true
      write_status 1 0 0 "${capacity:-unknown}" "${present:-unknown}" invalid_telemetry
      printf '%s invalid telemetry capacity=%s usb=%s state=%s; charging allowed\n' \
        "$(date '+%Y-%m-%dT%H:%M:%S%z')" "$capacity" "$present" "$current"
      sleep "$INTERVAL"
      continue
    fi

    if [ "$present" != "1" ]; then
      desired=0
      reason=external_power_absent
    elif [ "$capacity" -le "$LOW" ]; then
      desired=0
      reason=low_threshold
    elif [ "$capacity" -ge "$HIGH" ]; then
      desired=1
      reason=high_threshold
    else
      # In the hysteresis band, retain the kernel's actual state. This also
      # makes a watchdog recovery deterministic without relying on RAM state.
      desired=$current
      reason=hysteresis
    fi

    if [ "$current" != "$desired" ]; then
      write_charge_state "$desired" || true
    fi
    actual=$(cat "$DISABLE_CHARGE" 2>/dev/null)
    if [ "$actual" != "$desired" ]; then
      allow_charging || true
      actual=$(cat "$DISABLE_CHARGE" 2>/dev/null)
      write_status 1 0 0 "$capacity" "$present" write_failed
      printf '%s charge-state write failed desired=%s actual=%s; charging allowed\n' \
        "$(date '+%Y-%m-%dT%H:%M:%S%z')" "$desired" "$actual"
    else
      write_status 1 "$actual" 1 "$capacity" "$present" "$reason"
      if [ "$actual" != "$last_applied" ]; then
        printf '%s capacity=%s usb=%s op_disable_charge=%s reason=%s\n' \
          "$(date '+%Y-%m-%dT%H:%M:%S%z')" "$capacity" "$present" "$actual" "$reason"
        last_applied=$actual
      fi
    fi

    # The primary controller also revives the watchdog if it ever disappears.
    watchdog_counter=$((watchdog_counter + 1))
    if [ "$watchdog_counter" -ge 6 ]; then
      if [ -x "$WATCHDOG" ]; then
        /system/bin/sh "$WATCHDOG" >/dev/null 2>&1 || true
      fi
      watchdog_counter=0
    fi
    sleep "$INTERVAL"
  done
) >> "$LOG_FILE" 2>&1 &

worker=$!
printf '%s\n' "$worker" > "$PID_FILE"
chmod 600 "$PID_FILE"

exit 0
