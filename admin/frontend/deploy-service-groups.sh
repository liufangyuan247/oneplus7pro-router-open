#!/data/data/com.termux/files/usr/bin/sh
set -eu
umask 077
export HOME=/data/data/com.termux/files/home PREFIX=/data/data/com.termux/files/usr
export PATH="$PREFIX/bin:/system/bin"
STAGE="$HOME/.stage/service-groups"
ADMIN="$HOME/router-admin"
STAMP=$(date '+%Y%m%d-%H%M%S')
BACKUP="$HOME/router-data/backups/router-admin/$STAMP-service-groups"
NEW_WEB="$ADMIN/web.groups.$STAMP"
[ "$#" -eq 1 ]
[ "$(sha256sum "$STAGE/web.tar.gz" | cut -d ' ' -f1)" = "$1" ]
curl -fsS --max-time 3 http://127.0.0.1:9090/api/health >/dev/null
mkdir -p "$BACKUP" "$NEW_WEB"
find "$HOME/router-data/mihomo/subscription-cache" -type f -name '*.yaml' -exec sha256sum {} \; > "$BACKUP/subscription-cache.sha256"
tar -xzf "$STAGE/web.tar.gz" -C "$NEW_WEB"
[ -s "$NEW_WEB/index.html" ]
[ -d "$NEW_WEB/_next/static" ]
DONE=0
rollback() {
    result=$?
    if [ "$DONE" -eq 0 ] && [ -d "$BACKUP/web" ]; then
        [ ! -d "$ADMIN/web" ] || mv "$ADMIN/web" "$BACKUP/web.failed"
        mv "$BACKUP/web" "$ADMIN/web"
        echo 'Previous frontend restored' >&2
    fi
    exit "$result"
}
trap rollback EXIT
mv "$ADMIN/web" "$BACKUP/web"
mv "$NEW_WEB" "$ADMIN/web"
curl -fsS --max-time 5 http://127.0.0.1:9090/ | grep -q '/_next/static/'
sha256sum -c "$BACKUP/subscription-cache.sha256" >/dev/null
DONE=1
printf '%s\n' "$BACKUP" > "$HOME/router-data/admin/last-router-admin-backup"
echo "SERVICE_GROUPS_DEPLOYED backup=$BACKUP"
