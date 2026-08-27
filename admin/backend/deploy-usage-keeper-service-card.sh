#!/data/data/com.termux/files/usr/bin/sh
set -eu

PREFIX=/data/data/com.termux/files/usr
HOME_DIR=/data/data/com.termux/files/home
ADMIN_DIR="$HOME_DIR/router-admin"
DATA_DIR="$HOME_DIR/router-data"
SERVICE_DIR="$HOME_DIR/router-stack/services/router-admin"
STAGE_DIR="$HOME_DIR/.stage"
STAGED_BIN="$STAGE_DIR/router-admin-usage-keeper-card"
STAGED_WEB="$STAGE_DIR/router-admin-web-usage-keeper-card.tar.gz"
BIN="$PREFIX/bin/router-admin"
STAMP=$(date +%Y%m%d-%H%M%S)
BACKUP_DIR="$DATA_DIR/backups/router-admin/$STAMP-usage-keeper-card"
NEW_WEB="$ADMIN_DIR/web.new.$STAMP"
OLD_WEB="$ADMIN_DIR/web.before-usage-keeper.$STAMP"
EXPECTED_BIN_SHA=670e12c5a91178007bdbb49b9b5568b8c640618a661102c0befafc646ff124c7
EXPECTED_WEB_SHA=3c129e357df445927cc8db1755c3d29cac4dec0be1c9bccdfcc0e4c70758f8d4

export HOME="$HOME_DIR" PREFIX PATH="$PREFIX/bin:/system/bin"

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

test -s "$STAGED_BIN" || fail "staged router-admin binary is missing"
test -s "$STAGED_WEB" || fail "staged router-admin web archive is missing"

BIN_SHA=$(sha256sum "$STAGED_BIN" | awk '{print $1}')
WEB_SHA=$(sha256sum "$STAGED_WEB" | awk '{print $1}')
test "$BIN_SHA" = "$EXPECTED_BIN_SHA" || fail "router-admin binary checksum mismatch"
test "$WEB_SHA" = "$EXPECTED_WEB_SHA" || fail "router-admin web checksum mismatch"

curl -fsS --max-time 3 http://127.0.0.1:9090/api/health >/dev/null || \
  fail "current router-admin is not healthy; refusing update"

mkdir -p "$BACKUP_DIR" "$NEW_WEB"
cp -p "$BIN" "$BACKUP_DIR/router-admin"
tar -czf "$BACKUP_DIR/router-admin-web.tar.gz" -C "$ADMIN_DIR" web
tar -xzf "$STAGED_WEB" -C "$NEW_WEB"
test -s "$NEW_WEB/index.html" || fail "new web archive has no index.html"

cp "$STAGED_BIN" "$BIN.new.$STAMP"
chmod 0700 "$BIN.new.$STAMP"
mv "$BIN.new.$STAMP" "$BIN"
mv "$ADMIN_DIR/web" "$OLD_WEB"
mv "$NEW_WEB" "$ADMIN_DIR/web"

sv restart "$SERVICE_DIR" >/dev/null 2>&1 || true

ATTEMPT=0
while test "$ATTEMPT" -lt 12; do
  if curl -fsS --max-time 3 http://127.0.0.1:9090/api/health >/dev/null 2>&1 && \
     curl -fsS --max-time 3 http://127.0.0.1:9090/ >/dev/null 2>&1; then
    printf '%s\n' "$BACKUP_DIR" > "$DATA_DIR/admin/last-router-admin-backup"
    echo ROUTER_ADMIN_USAGE_KEEPER_CARD_DEPLOYED
    exit 0
  fi
  ATTEMPT=$((ATTEMPT + 1))
  sleep 1
done

echo "ERROR: updated router-admin failed health checks; restoring backup" >&2
sv down "$SERVICE_DIR" >/dev/null 2>&1 || true
cp -p "$BACKUP_DIR/router-admin" "$BIN"
mv "$ADMIN_DIR/web" "$ADMIN_DIR/web.failed.$STAMP"
mv "$OLD_WEB" "$ADMIN_DIR/web"
sv up "$SERVICE_DIR" >/dev/null 2>&1 || true
exit 1
