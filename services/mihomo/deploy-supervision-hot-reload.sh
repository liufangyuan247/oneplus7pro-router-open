#!/system/bin/sh
set -eu

HOME_DIR=/data/data/com.termux/files/home
PREFIX=/data/data/com.termux/files/usr
SERVICE="$HOME_DIR/router-stack/services/mihomo"
RUN="$SERVICE/run"
UPDATER="$HOME_DIR/router-stack/bin/mihomo-update-subscription"
RUN_SOURCE="$HOME_DIR/.stage/mihomo-run-root.stability"
UPDATER_SOURCE="$HOME_DIR/.stage/mihomo-update-subscription.stability"
DATA="$HOME_DIR/router-data/mihomo"
CONFIG="$DATA/config.yaml"
STAMP="$(date '+%Y%m%d-%H%M%S')"
BACKUP="$HOME_DIR/router-data/backups/mihomo/$STAMP-supervision"
SV="$PREFIX/bin/sv"
CURL="$PREFIX/bin/curl"

export PATH="$PREFIX/bin:/system/bin"
export LD_LIBRARY_PATH="$PREFIX/lib"

[ "$(id -u)" = 0 ] || { echo 'deployment requires Magisk root' >&2; exit 1; }
[ -s "$RUN" ] && [ -s "$UPDATER" ]
[ -s "$RUN_SOURCE" ] && [ -s "$UPDATER_SOURCE" ]
"$PREFIX/bin/sh" -n "$RUN_SOURCE"
"$PREFIX/bin/sh" -n "$UPDATER_SOURCE"
"$PREFIX/bin/mihomo" -t -d "$DATA" -f "$CONFIG" >/dev/null
mkdir -p "$BACKUP"
cp -p "$RUN" "$BACKUP/run"
cp -p "$UPDATER" "$BACKUP/mihomo-update-subscription"
cp -p "$CONFIG" "$BACKUP/config.yaml"
chmod 700 "$BACKUP"

matching_pids() {
  for pid in $(pidof mihomo 2>/dev/null || true); do
    cmdline="$(tr '\000' ' ' < "/proc/$pid/cmdline" 2>/dev/null || true)"
    case "$cmdline" in *"$CONFIG"*) echo "$pid" ;; esac
  done
}

stop_matching_cores() {
  pids="$(matching_pids)"
  [ -z "$pids" ] || kill -TERM $pids 2>/dev/null || true
  attempt=0
  while [ -n "$(matching_pids)" ] && [ "$attempt" -lt 15 ]; do
    sleep 1
    attempt=$((attempt + 1))
  done
  pids="$(matching_pids)"
  [ -z "$pids" ] || kill -KILL $pids 2>/dev/null || true
  rm -f "$DATA/supervised-core.pid"
}

wait_ready() {
  attempt=0
  while [ "$attempt" -lt 30 ]; do
    if "$CURL" -fsS --max-time 2 http://127.0.0.1:9091/version >/dev/null 2>&1; then
      return 0
    fi
    attempt=$((attempt + 1))
    sleep 1
  done
  return 1
}

verify_single_supervised_core() {
  pid="$(cat "$DATA/supervised-core.pid" 2>/dev/null || true)"
  case "$pid" in ''|*[!0-9]*) return 1 ;; esac
  [ "$(matching_pids | wc -l)" = 1 ]
  [ "$(matching_pids)" = "$pid" ]
  [ "$(awk '/^Uid:/ { print $2; exit }' "/proc/$pid/status" 2>/dev/null)" = 0 ]
  ss -lnt 2>/dev/null | grep -q '127\.0\.0\.1:17890 '
  ss -lnt 2>/dev/null | grep -q '127\.0\.0\.1:17891 '
}

rollback() {
  trap - EXIT INT TERM
  "$SV" down "$SERVICE" >/dev/null 2>&1 || true
  stop_matching_cores
  cp -p "$BACKUP/run" "$RUN"
  cp -p "$BACKUP/mihomo-update-subscription" "$UPDATER"
  chown 10207:10207 "$RUN" "$UPDATER" 2>/dev/null || true
  chmod 700 "$RUN" "$UPDATER"
  "$SV" up "$SERVICE" >/dev/null 2>&1 || true
}
trap rollback EXIT INT TERM

"$SV" down "$SERVICE" >/dev/null 2>&1 || true
stop_matching_cores
cp "$RUN_SOURCE" "$RUN.new.$STAMP"
cp "$UPDATER_SOURCE" "$UPDATER.new.$STAMP"
chown 10207:10207 "$RUN.new.$STAMP" "$UPDATER.new.$STAMP"
chmod 700 "$RUN.new.$STAMP" "$UPDATER.new.$STAMP"
mv -f "$RUN.new.$STAMP" "$RUN"
mv -f "$UPDATER.new.$STAMP" "$UPDATER"
"$SV" up "$SERVICE"
wait_ready
verify_single_supervised_core
core_pid="$(cat "$DATA/supervised-core.pid")"

trap - EXIT INT TERM
chown -R 10207:10207 "$BACKUP" 2>/dev/null || true
echo "MIHOMO_SUPERVISION_READY core_pid=$core_pid backup=$BACKUP"
