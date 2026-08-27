#!/system/bin/sh
# Transactional installer for the fail-safe battery controller.

STATE_DIR=/data/adb/router-stack
SERVICE_DIR=/data/adb/service.d
STAGE=/data/data/com.termux/files/home/.stage
PRIMARY=$SERVICE_DIR/30-battery-guard.sh
WATCHDOG=$SERVICE_DIR/31-battery-guard-watchdog.sh
STAGED_PRIMARY=$STAGE/30-battery-guard.sh
STAGED_WATCHDOG=$STAGE/31-battery-guard-watchdog.sh
CHARGE_NODE=/sys/class/power_supply/battery/op_disable_charge
CAPACITY_NODE=/sys/class/power_supply/battery/capacity
USB_PRESENT_NODE=/sys/class/power_supply/usb/present
STATUS_FILE=/data/data/com.termux/files/home/router-data/admin/battery-guard.status

fail() {
  echo "BATTERY_GUARD_DEPLOY_FAILED: $*" >&2
  return 1
}

script_pids() {
  needle=$1
  for process in /proc/[0-9]*; do
    [ -r "$process/cmdline" ] || continue
    command_line=$(tr '\000' ' ' < "$process/cmdline" 2>/dev/null)
    case "$command_line" in
      *"$needle"*) echo "${process##*/}" ;;
    esac
  done
}

stop_script() {
  needle=$1
  pids=$(script_pids "$needle")
  for process in $pids; do kill "$process" 2>/dev/null || true; done
  sleep 2
  for process in $pids; do
    kill -0 "$process" 2>/dev/null && kill -9 "$process" 2>/dev/null || true
  done
}

pid_matches() {
  pid_file=$1
  needle=$2
  process=$(cat "$pid_file" 2>/dev/null)
  [ -n "$process" ] || return 1
  kill -0 "$process" 2>/dev/null || return 1
  [ -r "/proc/$process/cmdline" ] || return 1
  command_line=$(tr '\000' ' ' < "/proc/$process/cmdline" 2>/dev/null)
  case "$command_line" in
    *"$needle"*) return 0 ;;
    *) return 1 ;;
  esac
}

status_value() {
  key=$1
  sed -n "s/^$key=//p" "$STATUS_FILE" 2>/dev/null | head -n 1
}

carrier() {
  cat "/sys/class/net/$1/carrier" 2>/dev/null || echo missing
}

[ -s "$STAGED_PRIMARY" ] || { fail "missing $STAGED_PRIMARY"; exit 1; }
[ -s "$STAGED_WATCHDOG" ] || { fail "missing $STAGED_WATCHDOG"; exit 1; }

mkdir -p "$STATE_DIR/backups/battery-guard"
stamp=$(date '+%Y%m%d-%H%M%S')
backup="$STATE_DIR/backups/battery-guard/$stamp"
mkdir -p "$backup"
[ -f "$PRIMARY" ] && cp -p "$PRIMARY" "$backup/30-battery-guard.sh"
if [ -f "$WATCHDOG" ]; then
  cp -p "$WATCHDOG" "$backup/31-battery-guard-watchdog.sh"
  touch "$backup/watchdog-existed"
fi

pre_eth0=$(carrier eth0)
pre_eth1=$(carrier eth1)
if [ -d /sys/class/net/ppp0 ]; then pre_ppp=1; else pre_ppp=0; fi

# A deployment interruption must leave the safe state: charging allowed.
echo 0 > "$CHARGE_NODE" 2>/dev/null || true
stop_script '/data/adb/service.d/31-battery-guard-watchdog.sh'
stop_script '/data/adb/service.d/30-battery-guard.sh'
rm -f "$STATE_DIR/battery-guard.pid" "$STATE_DIR/battery-guard-watchdog.pid"

cp "$STAGED_PRIMARY" "$PRIMARY.new"
cp "$STAGED_WATCHDOG" "$WATCHDOG.new"
chmod 755 "$PRIMARY.new" "$WATCHDOG.new"
mv -f "$PRIMARY.new" "$PRIMARY"
mv -f "$WATCHDOG.new" "$WATCHDOG"

/system/bin/sh "$WATCHDOG"
/system/bin/sh "$PRIMARY"

healthy=0
attempt=0
while [ "$attempt" -lt 30 ]; do
  now=$(date +%s)
  updated=$(status_value updated)
  case "$updated" in
    ''|*[!0-9]*) age=999999 ;;
    *) age=$((now - updated)) ;;
  esac
  if pid_matches "$STATE_DIR/battery-guard.pid" '/data/adb/service.d/30-battery-guard.sh' && \
     pid_matches "$STATE_DIR/battery-guard-watchdog.pid" '/data/adb/service.d/31-battery-guard-watchdog.sh' && \
     [ "$(status_value enabled)" = "1" ] && [ "$(status_value healthy)" = "1" ] && \
     [ "$age" -ge 0 ] && [ "$age" -le 20 ]; then
    healthy=1
    break
  fi
  attempt=$((attempt + 1))
  sleep 1
done

capacity=$(cat "$CAPACITY_NODE" 2>/dev/null)
usb_present=$(cat "$USB_PRESENT_NODE" 2>/dev/null)
charge_state=$(cat "$CHARGE_NODE" 2>/dev/null)
state_ok=1
if [ "$usb_present" = "1" ] && [ "${capacity:-0}" -ge 75 ] 2>/dev/null; then
  [ "$charge_state" = "1" ] || state_ok=0
elif [ "${capacity:-100}" -le 60 ] 2>/dev/null; then
  [ "$charge_state" = "0" ] || state_ok=0
fi
[ "$pre_eth0" != "1" ] || [ "$(carrier eth0)" = "1" ] || state_ok=0
[ "$pre_eth1" != "1" ] || [ "$(carrier eth1)" = "1" ] || state_ok=0
[ "$pre_ppp" != "1" ] || [ -d /sys/class/net/ppp0 ] || state_ok=0

if [ "$healthy" != "1" ] || [ "$state_ok" != "1" ]; then
  echo 0 > "$CHARGE_NODE" 2>/dev/null || true
  stop_script '/data/adb/service.d/31-battery-guard-watchdog.sh'
  stop_script '/data/adb/service.d/30-battery-guard.sh'
  if [ -f "$backup/30-battery-guard.sh" ]; then
    cp -p "$backup/30-battery-guard.sh" "$PRIMARY"
  fi
  if [ -f "$backup/watchdog-existed" ]; then
    cp -p "$backup/31-battery-guard-watchdog.sh" "$WATCHDOG"
  else
    rm -f "$WATCHDOG"
  fi
  chmod 755 "$PRIMARY" 2>/dev/null || true
  [ ! -f "$WATCHDOG" ] || chmod 755 "$WATCHDOG"
  rm -f "$STATE_DIR/battery-guard.pid" "$STATE_DIR/battery-guard-watchdog.pid"
  /system/bin/sh "$PRIMARY" >/dev/null 2>&1 || true
  fail "health=$healthy state_ok=$state_ok; restored $backup"
  exit 1
fi

printf '%s\n' "$backup" > "$STATE_DIR/last-battery-guard-backup"
echo "BATTERY_GUARD_DEPLOYED backup=$backup capacity=$capacity usb=$usb_present paused=$charge_state"
