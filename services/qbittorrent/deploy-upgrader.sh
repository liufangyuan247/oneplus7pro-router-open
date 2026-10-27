#!/data/data/com.termux/files/usr/bin/sh
set -eu
umask 077
export HOME=/data/data/com.termux/files/home PREFIX=/data/data/com.termux/files/usr
export PATH="$PREFIX/bin:/system/bin"
STAGE="$HOME/.stage/qbittorrent-upgrade"
BIN="$PREFIX/bin/router-admin"
HELPER="$PREFIX/bin/upgrade-qbittorrent.py"
SERVICE="$HOME/router-stack/services/router-admin"
STAMP=$(date '+%Y%m%d-%H%M%S')
BACKUP="$HOME/router-data/backups/router-admin/$STAMP-qbittorrent-upgrader"
[ "$#" -eq 2 ]
[ "$(sha256sum "$STAGE/router-admin-qbittorrent" | cut -d ' ' -f1)" = "$1" ]
[ "$(sha256sum "$STAGE/upgrade-qbittorrent.py" | cut -d ' ' -f1)" = "$2" ]
curl -fsS --max-time 3 http://127.0.0.1:9090/api/health >/dev/null
mkdir -p "$BACKUP"
cp -p "$BIN" "$BACKUP/router-admin"
[ ! -f "$HELPER" ] || cp -p "$HELPER" "$BACKUP/upgrade-qbittorrent.py"
find "$HOME/router-data/mihomo/subscription-cache" -type f -name '*.yaml' -exec sha256sum {} \; > "$BACKUP/subscription-cache.sha256"
DONE=0
rollback() {
    result=$?
    if [ "$DONE" -eq 0 ]; then
        cp -p "$BACKUP/router-admin" "$BIN.rollback.$STAMP"
        mv -f "$BIN.rollback.$STAMP" "$BIN"
        [ ! -f "$BACKUP/upgrade-qbittorrent.py" ] || cp -p "$BACKUP/upgrade-qbittorrent.py" "$HELPER"
        sv restart "$SERVICE" >/dev/null 2>&1 || true
        echo 'Admin upgrade failed; previous backend restored' >&2
    fi
    exit "$result"
}
trap rollback EXIT
cp "$STAGE/upgrade-qbittorrent.py" "$HELPER.new"
chmod 0700 "$HELPER.new"
mv -f "$HELPER.new" "$HELPER"
cp "$STAGE/router-admin-qbittorrent" "$BIN.new.$STAMP"
chmod 0700 "$BIN.new.$STAMP"
mv -f "$BIN.new.$STAMP" "$BIN"
sv restart "$SERVICE" >/dev/null 2>&1 || true
ready=0
for attempt in $(seq 1 20); do
    if curl -fsS --max-time 3 http://127.0.0.1:9090/api/health >/dev/null 2>&1; then ready=1; break; fi
    sleep 1
done
[ "$ready" -eq 1 ]
sha256sum -c "$BACKUP/subscription-cache.sha256" >/dev/null
DONE=1
printf '%s\n' "$BACKUP" > "$HOME/router-data/admin/last-router-admin-backup"
echo "QBITTORRENT_UPGRADER_DEPLOYED backup=$BACKUP"
