#!/data/data/com.termux/files/usr/bin/sh
set -eu
umask 077

HOME_DIR=/data/data/com.termux/files/home
PREFIX=/data/data/com.termux/files/usr
export HOME="$HOME_DIR" PREFIX PATH="$PREFIX/bin:/system/bin"
STAGE="$HOME/.stage"
BIN="$PREFIX/bin/router-admin"
ADMIN="$HOME/router-admin"
SERVICE="$HOME/router-stack/services/router-admin"
STAMP=$(date '+%Y%m%d-%H%M%S')
BACKUP="$HOME/router-data/backups/router-admin/$STAMP-samba-card"
NEW_WEB="$ADMIN/web.samba.$STAMP"
ACTIVATED=0
DONE=0

fail() { echo "ERROR: $*" >&2; exit 1; }
[ "$#" -eq 2 ] || fail 'expected binary and web archive SHA256 arguments'
[ "$(sha256sum "$STAGE/router-admin-samba" | cut -d ' ' -f1)" = "$1" ] || fail 'binary checksum mismatch'
[ "$(sha256sum "$STAGE/router-admin-samba-web.tar.gz" | cut -d ' ' -f1)" = "$2" ] || fail 'web checksum mismatch'
curl -fsS --max-time 3 http://127.0.0.1:9090/api/health >/dev/null
python -c 'import socket; s=socket.create_connection(("127.0.0.1",445),3); s.close()'
mkdir -p "$BACKUP" "$NEW_WEB"
cp -p "$BIN" "$BACKUP/router-admin"
if [ -d "$HOME/router-data/mihomo/subscription-cache" ]; then
    find "$HOME/router-data/mihomo/subscription-cache" -type f -name '*.yaml' -exec sha256sum {} \; > "$BACKUP/subscription-cache.sha256"
fi
tar -xzf "$STAGE/router-admin-samba-web.tar.gz" -C "$NEW_WEB"
[ -s "$NEW_WEB/index.html" ] || fail 'missing index'
[ -d "$NEW_WEB/_next/static" ] || fail 'missing static assets'

rollback() {
    result=$?
    trap - EXIT INT TERM
    if [ "$ACTIVATED" -eq 1 ] && [ "$DONE" -eq 0 ]; then
        cp -p "$BACKUP/router-admin" "$BIN.rollback.$STAMP"
        mv -f "$BIN.rollback.$STAMP" "$BIN"
        if [ -d "$BACKUP/web" ]; then
            [ ! -d "$ADMIN/web" ] || mv "$ADMIN/web" "$BACKUP/web.failed"
            mv "$BACKUP/web" "$ADMIN/web"
        fi
        sv restart "$SERVICE" >/dev/null 2>&1 || true
        echo "Previous admin restored: $BACKUP" >&2
    fi
    exit "$result"
}
trap rollback EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

cp "$STAGE/router-admin-samba" "$BIN.new.$STAMP"
chmod 0700 "$BIN.new.$STAMP"
ACTIVATED=1
mv "$ADMIN/web" "$BACKUP/web"
mv "$NEW_WEB" "$ADMIN/web"
mv -f "$BIN.new.$STAMP" "$BIN"
sv restart "$SERVICE" >/dev/null 2>&1 || true
ready=0
for attempt in $(seq 1 20); do
    if curl -fsS --max-time 3 http://127.0.0.1:9090/api/snapshot | python -c 'import json,sys; d=json.load(sys.stdin); assert any(s["Name"]=="samba" and s["State"]=="运行中" for s in d["services"])' >/dev/null 2>&1 &&
       curl -fsS --max-time 3 http://127.0.0.1:9090/ | grep -q '/_next/static/'; then
        ready=1
        break
    fi
    sleep 1
done
[ "$ready" -eq 1 ] || fail 'updated admin health check failed'
if [ -s "$BACKUP/subscription-cache.sha256" ]; then
    sha256sum -c "$BACKUP/subscription-cache.sha256" >/dev/null || fail 'subscription caches changed'
fi
DONE=1
printf '%s\n' "$BACKUP" > "$HOME/router-data/admin/last-router-admin-backup"
echo "SAMBA_ADMIN_DEPLOYED backup=$BACKUP"
