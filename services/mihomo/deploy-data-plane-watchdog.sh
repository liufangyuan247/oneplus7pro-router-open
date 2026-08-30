#!/system/bin/sh
set -eu

HOME_DIR=/data/data/com.termux/files/home
PREFIX=/data/data/com.termux/files/usr
STACK="$HOME_DIR/router-stack"
STAGE="$HOME_DIR/.stage"
SOURCE="$STAGE/mihomo-health-probe"
PROBE="$STACK/bin/mihomo-health-probe"
HEALTHD="$STACK/bin/healthd"
SERVICE="$STACK/services/healthd"
STAMP="$(date '+%Y%m%d-%H%M%S')"
BACKUP="$HOME_DIR/router-data/backups/mihomo/$STAMP-data-plane-watchdog"

[ "$(id -u)" = 0 ] || { echo 'deployment requires Magisk root' >&2; exit 1; }
[ -s "$SOURCE" ] || { echo 'missing staged mihomo-health-probe' >&2; exit 1; }
[ -s "$HEALTHD" ] || { echo 'healthd is missing' >&2; exit 1; }
"$PREFIX/bin/sh" -n "$SOURCE"

mkdir -p "$BACKUP"
cp -p "$HEALTHD" "$BACKUP/healthd"
[ ! -f "$PROBE" ] || cp -p "$PROBE" "$BACKUP/mihomo-health-probe"

cp "$SOURCE" "$PROBE.new.$STAMP"
chown 10207:10207 "$PROBE.new.$STAMP"
chmod 700 "$PROBE.new.$STAMP"
mv -f "$PROBE.new.$STAMP" "$PROBE"

"$PREFIX/bin/awk" '
/^[[:space:]]*probe mihomo / {
    print "    probe mihomo /data/data/com.termux/files/home/router-stack/bin/mihomo-health-probe"
    installed=1
    next
}
/^  sleep 60$/ && !installed {
    print "  if [ -d \"$SERVICES/mihomo\" ] && [ ! -f \"/data/data/com.termux/files/home/router-data/mihomo/disabled\" ]; then"
    print "    probe mihomo /data/data/com.termux/files/home/router-stack/bin/mihomo-health-probe"
    print "  fi"
    installed=1
}
{ print }
END { if (!installed) exit 42 }
' "$HEALTHD" > "$HEALTHD.new.$STAMP"
"$PREFIX/bin/sh" -n "$HEALTHD.new.$STAMP"
chown 10207:10207 "$HEALTHD.new.$STAMP"
chmod 700 "$HEALTHD.new.$STAMP"
mv -f "$HEALTHD.new.$STAMP" "$HEALTHD"

for state_file in "$HOME_DIR"/router-data/health/mihomo-data-plane.*; do
    [ ! -e "$state_file" ] || chown 10207:10207 "$state_file"
done

if ! /system/bin/run-as com.termux "$PROBE"; then
    cp -p "$BACKUP/healthd" "$HEALTHD"
    if [ -s "$BACKUP/mihomo-health-probe" ]; then
        cp -p "$BACKUP/mihomo-health-probe" "$PROBE"
    else
        rm -f "$PROBE"
    fi
    echo 'data-plane probe failed before activation; restored previous files' >&2
    exit 1
fi

"$PREFIX/bin/sv" restart "$SERVICE" >/dev/null
sleep 1
"$PREFIX/bin/sv" status "$SERVICE"
printf '%s\n' "$BACKUP" > "$HOME_DIR/router-data/mihomo/last-data-plane-watchdog-backup"
rm -f "$SOURCE"
echo "MIHOMO_DATA_PLANE_WATCHDOG_READY backup=$BACKUP"
