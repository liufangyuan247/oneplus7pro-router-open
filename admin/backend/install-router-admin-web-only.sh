#!/data/data/com.termux/files/usr/bin/sh
set -eu

HOME_DIR=/data/data/com.termux/files/home
ADMIN_DIR="$HOME_DIR/router-admin"
DATA_DIR="$HOME_DIR/router-data"
STAGE_DIR="$HOME_DIR/.stage"
BACKUP_ROOT="$DATA_DIR/backups/router-admin"
CANDIDATE="$STAGE_DIR/router-admin-web-candidate.tar.gz"
EXPECTED_SHA256="${1:-}"

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

[ -n "$EXPECTED_SHA256" ] || fail "expected archive SHA256 is required"
[ -s "$CANDIDATE" ] || fail "staged web archive is missing"

ACTUAL_SHA256="$(sha256sum "$CANDIDATE" | awk '{print $1}')"
[ "$ACTUAL_SHA256" = "$EXPECTED_SHA256" ] || fail "web archive checksum mismatch"

curl -fsS --max-time 3 http://127.0.0.1:9090/api/health >/dev/null || \
  fail "current router-admin is not healthy; refusing update"

STAMP="$(date '+%Y%m%d-%H%M%S')"
BACKUP_DIR="$BACKUP_ROOT/$STAMP-secondary-navigation"
NEW_DIR="$ADMIN_DIR/web.new"
PREVIOUS_DIR="$ADMIN_DIR/web.prev"
FAILED_DIR="$ADMIN_DIR/web.failed"

mkdir -p "$BACKUP_DIR" "$DATA_DIR/admin"
[ -d "$ADMIN_DIR/web" ] || fail "current web directory is missing"
tar -czf "$BACKUP_DIR/router-admin-web.tar.gz" -C "$ADMIN_DIR" web

rm -rf "$NEW_DIR"
mkdir -p "$NEW_DIR"
tar -xzf "$CANDIDATE" -C "$NEW_DIR"

[ -s "$NEW_DIR/index.html" ] || fail "candidate index.html is missing"
[ -d "$NEW_DIR/_next/static" ] || fail "candidate static chunks are missing"
grep -q '/_next/static/' "$NEW_DIR/index.html" || fail "candidate index has no static chunk references"

rm -rf "$PREVIOUS_DIR"
mv "$ADMIN_DIR/web" "$PREVIOUS_DIR"
mv "$NEW_DIR" "$ADMIN_DIR/web"

healthy=0
attempt=0
while [ "$attempt" -lt 5 ]; do
  if curl -fsS --max-time 3 http://127.0.0.1:9090/api/health >/dev/null 2>&1 && \
     curl -fsS --max-time 3 http://127.0.0.1:9090/ | grep -q '/_next/static/'; then
    healthy=1
    break
  fi
  attempt=$((attempt + 1))
  sleep 1
done

if [ "$healthy" -ne 1 ]; then
  echo "updated web UI failed health checks; restoring previous release" >&2
  rm -rf "$FAILED_DIR"
  [ -d "$ADMIN_DIR/web" ] && mv "$ADMIN_DIR/web" "$FAILED_DIR"
  mv "$PREVIOUS_DIR" "$ADMIN_DIR/web"
  exit 1
fi

printf '%s\n' "$BACKUP_DIR" > "$DATA_DIR/admin/last-router-admin-backup"
rm -f "$CANDIDATE"
echo "ROUTER_ADMIN_WEB_INSTALLED backup=$BACKUP_DIR"
