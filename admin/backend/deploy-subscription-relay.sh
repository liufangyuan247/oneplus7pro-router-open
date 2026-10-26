#!/data/data/com.termux/files/usr/bin/sh
set -eu

HOME_DIR=/data/data/com.termux/files/home
PREFIX=/data/data/com.termux/files/usr
export PATH="$PREFIX/bin:/system/bin"
STAGE="$HOME_DIR/.stage"
BIN="$PREFIX/bin/router-admin"
ADMIN="$HOME_DIR/router-admin"
SERVICE="$HOME_DIR/router-stack/services/router-admin"
MIHOMO="$HOME_DIR/router-data/mihomo"
STAMP=$(date '+%Y%m%d-%H%M%S')
BACKUP="$HOME_DIR/router-data/backups/router-admin/$STAMP-subscription-relay"
NEW_WEB="$ADMIN/web.relay.$STAMP"
ACTIVATED=0
COMMITTED=0

fail() { echo "ERROR: $*" >&2; exit 1; }
test "$#" -eq 2 || fail 'expected binary and web archive SHA256 arguments'
test "$(sha256sum "$STAGE/router-admin-relay" | awk '{print $1}')" = "$1" || fail 'binary checksum mismatch'
test "$(sha256sum "$STAGE/router-admin-relay-web.tar.gz" | awk '{print $1}')" = "$2" || fail 'web archive checksum mismatch'
curl -fsS --max-time 3 http://127.0.0.1:9090/api/health >/dev/null
mkdir -p "$BACKUP" "$NEW_WEB"
cp -p "$BIN" "$BACKUP/router-admin"
cp -p "$MIHOMO/subscriptions.json" "$BACKUP/subscriptions.json"
sha256sum "$MIHOMO"/subscription-cache/*.yaml > "$BACKUP/cache-sha256"
tar -xzf "$STAGE/router-admin-relay-web.tar.gz" -C "$NEW_WEB"
test -s "$NEW_WEB/index.html" || fail 'missing index'
test -d "$NEW_WEB/_next/static" || fail 'missing static assets'

rollback() {
  result=$?
  trap - EXIT INT TERM
  if test "$ACTIVATED" -eq 1 && test "$COMMITTED" -eq 0; then
    cp -p "$BACKUP/router-admin" "$BIN.rollback.$STAMP"
    mv -f "$BIN.rollback.$STAMP" "$BIN"
    if test -d "$BACKUP/web"; then
      if test -d "$ADMIN/web"; then mv "$ADMIN/web" "$BACKUP/web.failed"; fi
      mv "$BACKUP/web" "$ADMIN/web"
    fi
    sv restart "$SERVICE" >/dev/null 2>&1 || true
    echo "Previous admin release restored: $BACKUP" >&2
  fi
  exit "$result"
}
trap rollback EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

cp "$STAGE/router-admin-relay" "$BIN.new.$STAMP"
chmod 0700 "$BIN.new.$STAMP"
ACTIVATED=1
mv "$ADMIN/web" "$BACKUP/web"
mv "$NEW_WEB" "$ADMIN/web"
mv -f "$BIN.new.$STAMP" "$BIN"
sv restart "$SERVICE" >/dev/null 2>&1 || true

ready=0
id=$(jq -r '.items[0].id' "$MIHOMO/subscriptions.json")
for attempt in $(seq 1 20); do
  if curl -fsS --max-time 3 http://127.0.0.1:9090/api/health >/dev/null 2>&1 &&
     curl -fsS --max-time 3 "http://127.0.0.1:9090/api/mihomo/subscriptions/relay?id=$id" | jq -e 'has("enabled") and has("path")' >/dev/null 2>&1 &&
     curl -fsS --max-time 3 http://127.0.0.1:9090/ | grep -q '/_next/static/'; then
    ready=1
    break
  fi
  sleep 1
done
test "$ready" -eq 1 || fail 'updated admin is not healthy'
sha256sum -c "$BACKUP/cache-sha256" >/dev/null || fail 'local subscription caches changed'
COMMITTED=1
printf '%s\n' "$BACKUP" > "$HOME_DIR/router-data/admin/last-router-admin-backup"
echo "SUBSCRIPTION_RELAY_DEPLOYED backup=$BACKUP"
