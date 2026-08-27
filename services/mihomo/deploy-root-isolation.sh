#!/system/bin/sh
set -eu

HOME_DIR=/data/data/com.termux/files/home
PREFIX=/data/data/com.termux/files/usr
SERVICE="$HOME_DIR/router-stack/services/mihomo"
RUN="$SERVICE/run"
SOURCE="$HOME_DIR/.stage/mihomo-run-root"
MANAGER=/data/adb/router-network/router-network
STAMP="$(date '+%Y%m%d-%H%M%S')"
BACKUP="$HOME_DIR/router-data/backups/mihomo/$STAMP-root-isolation"
SV="$PREFIX/bin/sv"
CURL="$PREFIX/bin/curl"

export LD_LIBRARY_PATH="$PREFIX/lib"
export PATH="$PREFIX/bin:/system/bin"

[ "$(id -u)" = 0 ] || { echo 'run this deployment through Magisk su' >&2; exit 1; }
[ -s "$SOURCE" ] || { echo 'staged root run script is missing' >&2; exit 1; }
/system/bin/sh -n "$SOURCE"
[ -s "$RUN" ] || { echo 'current Mihomo run script is missing' >&2; exit 1; }
"$CURL" -fsS --max-time 3 http://127.0.0.1:9091/version >/dev/null

mkdir -p "$BACKUP"
cp -p "$RUN" "$BACKUP/run"
proxy_was_enabled=0
grep -Eq "^PROXY_ENABLED=['\"]?1['\"]?$" /data/adb/router-network/config && proxy_was_enabled=1

rollback() {
  "$MANAGER" proxy-off >/dev/null 2>&1 || true
  "$SV" down "$SERVICE" >/dev/null 2>&1 || true
  cp -p "$BACKUP/run" "$RUN"
  chown 10207:10207 "$RUN" 2>/dev/null || true
  chmod 700 "$RUN"
  "$SV" up "$SERVICE" >/dev/null 2>&1 || true
  sleep 3
  [ "$proxy_was_enabled" = 1 ] && "$MANAGER" proxy-on >/dev/null 2>&1 || true
}
trap 'rollback' EXIT INT TERM

"$MANAGER" proxy-off >/dev/null
"$SV" down "$SERVICE" >/dev/null 2>&1 || true
cp "$SOURCE" "$RUN.new.$STAMP"
chown 10207:10207 "$RUN.new.$STAMP"
chmod 700 "$RUN.new.$STAMP"
mv -f "$RUN.new.$STAMP" "$RUN"
"$SV" up "$SERVICE" >/dev/null 2>&1 || true

ready=0
attempt=0
while [ "$attempt" -lt 20 ]; do
  if "$CURL" -fsS --max-time 2 http://127.0.0.1:9091/version >/dev/null 2>&1; then
    ready=1
    break
  fi
  attempt=$((attempt + 1))
  sleep 1
done
[ "$ready" = 1 ] || { echo 'root Mihomo failed its controller health check' >&2; exit 1; }

root_core=0
nonroot_core=0
for pid in $(pidof mihomo 2>/dev/null || true); do
  uid="$(awk '/^Uid:/ { print $2; exit }' "/proc/$pid/status" 2>/dev/null)"
  cmdline="$(tr '\000' ' ' < "/proc/$pid/cmdline" 2>/dev/null)"
  case "$cmdline" in *'/router-data/mihomo/config.yaml'*) ;; *) continue ;; esac
  if [ "$uid" = 0 ]; then root_core=1; else nonroot_core=1; fi
done
[ "$root_core" = 1 ] && [ "$nonroot_core" = 0 ] || {
  echo 'Mihomo process isolation check failed' >&2
  exit 1
}

if [ "$proxy_was_enabled" = 1 ]; then
  "$MANAGER" proxy-on >/dev/null
fi
trap - EXIT INT TERM
chown -R 10207:10207 "$BACKUP" 2>/dev/null || true
echo "MIHOMO_ROOT_ISOLATION_READY backup=$BACKUP proxy_was_enabled=$proxy_was_enabled"
