#!/data/data/com.termux/files/usr/bin/sh
set -eu

PREFIX=/data/data/com.termux/files/usr
HOME_DIR=/data/data/com.termux/files/home
SERVICE_ROOT="$HOME_DIR/router-stack/services"
SERVICE_DIR="$SERVICE_ROOT/cpa-usage-keeper"
DATA_ROOT="$HOME_DIR/router-data/cpa-usage-keeper"
WORK_DIR="$DATA_ROOT/data"
ENV_FILE="$DATA_ROOT/.env"
BIN="$PREFIX/bin/cpa-usage-keeper"
STAGED_BIN="$HOME_DIR/.stage/cpa-usage-keeper-v2.0.0-android"
STAGED_INFO="$STAGED_BIN.build.json"
MANAGEMENT_KEY_FILE="$HOME_DIR/router-data/cpa/management-key"
ADMIN_PASSWORD_FILE="$HOME_DIR/router-data/admin/password"
BUILD_INFO="$DATA_ROOT/build-info.json"
STAMP=$(date +%Y%m%d-%H%M%S)
BACKUP_DIR="$DATA_ROOT/install-backups/$STAMP"
SERVICE_GUARD="$HOME_DIR/router-stack/bin/termux-service-guard"
CPA_SERVICE="$SERVICE_ROOT/cpa"
WATCHDOG_SERVICE="$SERVICE_ROOT/cpa-usage-keeper-watchdog"
HEALTH_SERVICE="$SERVICE_ROOT/healthd"
PAUSED_CPA=0
PAUSED_WATCHDOG=0
PAUSED_HEALTH=0
STOPPED_KEEPER=0
ACTIVATED=0
COMMITTED=0
HAD_BIN=0
HAD_ENV=0
HAD_RUN=0
HAD_LOG_RUN=0
HAD_BUILD_INFO=0

export HOME="$HOME_DIR" PREFIX PATH="$PREFIX/bin:/system/bin"
umask 077

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

dotenv_escape() {
  printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'
}

test -s "$STAGED_BIN" || fail "staged binary is missing"
test -s "$STAGED_INFO" || fail "staged build manifest is missing"
test -s "$MANAGEMENT_KEY_FILE" || fail "CPA management key is missing"
test -s "$ADMIN_PASSWORD_FILE" || fail "router admin password is missing"

EXPECTED_SHA256=$("$PREFIX/bin/jq" -r 'select(.service == "cpa-usage-keeper" and .product == "CPA Usage Keeper") | .sha256 // empty' "$STAGED_INFO")
EXPECTED_VERSION=$("$PREFIX/bin/jq" -r '.customVersion // empty' "$STAGED_INFO")
UPSTREAM_VERSION=$("$PREFIX/bin/jq" -r '.upstreamVersion // empty' "$STAGED_INFO")
UPSTREAM_REPOSITORY=$("$PREFIX/bin/jq" -r '.upstreamRepository // empty' "$STAGED_INFO")
test -n "$EXPECTED_SHA256" || fail "invalid build manifest"
test -n "$EXPECTED_VERSION" || fail "build manifest has no custom version"
test -n "$UPSTREAM_VERSION" || fail "build manifest has no upstream version"
test "$UPSTREAM_REPOSITORY" = "Willxup/cpa-usage-keeper" || fail "unexpected upstream repository"
ACTUAL_SHA256=$(sha256sum "$STAGED_BIN" | awk '{print $1}')
test "$ACTUAL_SHA256" = "$EXPECTED_SHA256" || fail "staged binary checksum mismatch"
chmod 0755 "$STAGED_BIN"

VERSION_OUTPUT=$(env -u LD_PRELOAD -u TERMUX_EXEC__PROC_SELF_EXE /system/bin/sh -c \
  "'$STAGED_BIN' --version")
test "$VERSION_OUTPUT" = "$EXPECTED_VERSION" || fail "unexpected binary version"
command -v python >/dev/null || fail "Python with sqlite3 is required for database backup"
test -s "$HOME_DIR/.stage/backup-database.py" || fail "database backup helper is missing"
if test -s "$ENV_FILE"; then
  grep -qx "WORK_DIR=$WORK_DIR" "$ENV_FILE" || fail "custom WORK_DIR requires a matching backup path"
fi
CPA_UPSTREAM=$(jq -r '.upstreamVersion // empty' "$HOME_DIR/router-data/cpa/build-info.json")
test "$(printf '%s\n' v8.0.12 "$CPA_UPSTREAM" | sort -V | head -n 1)" = v8.0.12 || fail "Keeper v2 requires CPA v8.0.12+"

mkdir -p "$BACKUP_DIR" "$WORK_DIR" "$SERVICE_DIR/log/main"
if test -f "$BIN"; then HAD_BIN=1; cp -p "$BIN" "$BACKUP_DIR/cpa-usage-keeper"; fi
if test -f "$ENV_FILE"; then HAD_ENV=1; cp -p "$ENV_FILE" "$BACKUP_DIR/app.env"; fi
if test -f "$SERVICE_DIR/run"; then HAD_RUN=1; cp -p "$SERVICE_DIR/run" "$BACKUP_DIR/service-run"; fi
if test -f "$SERVICE_DIR/log/run"; then HAD_LOG_RUN=1; cp -p "$SERVICE_DIR/log/run" "$BACKUP_DIR/log-run"; fi
if test -f "$BUILD_INFO"; then HAD_BUILD_INFO=1; cp -p "$BUILD_INFO" "$BACKUP_DIR/build-info.json"; fi

MANAGEMENT_KEY=$(tr -d '\r\n' < "$MANAGEMENT_KEY_FILE")
LOGIN_PASSWORD=$(tr -d '\r\n' < "$ADMIN_PASSWORD_FILE")
test -n "$MANAGEMENT_KEY" || fail "CPA management key is empty"
test -n "$LOGIN_PASSWORD" || fail "router admin password is empty"

# v2 migrates the database. Pause producers and the watchdog before taking a
# verified SQLite snapshot; never let v1 reopen a partially migrated v2 DB.
rollback_and_resume() {
  result=$?
  trap - EXIT INT TERM
  if test "$COMMITTED" -eq 0; then
    if test "$ACTIVATED" -eq 1; then
      if ! sv -w 60 down "$SERVICE_DIR"; then
        echo "Cannot stop Keeper v2; refusing to restore its open database. Backup: $BACKUP_DIR" >&2
        exit 1
      fi
      if test -s "$BACKUP_DIR/app.db"; then
        mkdir -p "$BACKUP_DIR/failed-v2"
        for name in app.db app.db-wal app.db-shm; do
          if test -f "$WORK_DIR/$name"; then mv "$WORK_DIR/$name" "$BACKUP_DIR/failed-v2/$name"; fi
        done
        cp -p "$BACKUP_DIR/app.db" "$WORK_DIR/app.db"
      fi
      if test "$HAD_BIN" -eq 1; then
        cp -p "$BACKUP_DIR/cpa-usage-keeper" "$BIN.rollback"
        mv -f "$BIN.rollback" "$BIN"
      else
        rm -f "$BIN"
      fi
      if test "$HAD_ENV" -eq 1; then cp -p "$BACKUP_DIR/app.env" "$ENV_FILE"; fi
      if test "$HAD_RUN" -eq 1; then cp -p "$BACKUP_DIR/service-run" "$SERVICE_DIR/run"; fi
      if test "$HAD_LOG_RUN" -eq 1; then cp -p "$BACKUP_DIR/log-run" "$SERVICE_DIR/log/run"; fi
      if test "$HAD_BUILD_INFO" -eq 1; then
        cp -p "$BACKUP_DIR/build-info.json" "$BUILD_INFO"
      else
        rm -f "$BUILD_INFO"
      fi
      for name in installed-version installed-sha256; do
        if test -f "$BACKUP_DIR/$name"; then cp -p "$BACKUP_DIR/$name" "$DATA_ROOT/$name"; fi
      done
    fi
    if test "$STOPPED_KEEPER" -eq 1; then sv up "$SERVICE_DIR" >/dev/null 2>&1 || true; fi
    echo "Upgrade failed; previous installation and pre-upgrade database restored. Backup: $BACKUP_DIR" >&2
  fi
  if test "$PAUSED_CPA" -eq 1; then sv up "$CPA_SERVICE" >/dev/null 2>&1 || true; fi
  if test "$PAUSED_WATCHDOG" -eq 1; then sv up "$WATCHDOG_SERVICE" >/dev/null 2>&1 || true; fi
  if test "$PAUSED_HEALTH" -eq 1; then sv up "$HEALTH_SERVICE" >/dev/null 2>&1 || true; fi
  exit "$result"
}
trap rollback_and_resume EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
# healthd otherwise restarts CPA after three failed probes during maintenance.
if sv status "$HEALTH_SERVICE" 2>/dev/null | grep -q '^run:'; then
  PAUSED_HEALTH=1
  sv -w 30 down "$HEALTH_SERVICE"
fi
if sv status "$WATCHDOG_SERVICE" 2>/dev/null | grep -q '^run:'; then
  PAUSED_WATCHDOG=1
  sv -w 30 down "$WATCHDOG_SERVICE"
fi
if sv status "$CPA_SERVICE" 2>/dev/null | grep -q '^run:'; then
  PAUSED_CPA=1
  sv -w 60 down "$CPA_SERVICE"
fi
if sv status "$SERVICE_DIR" 2>/dev/null | grep -q '^run:'; then
  STOPPED_KEEPER=1
  sv -w 60 down "$SERVICE_DIR"
fi
if test -s "$WORK_DIR/app.db"; then
  python "$HOME_DIR/.stage/backup-database.py" "$WORK_DIR/app.db" "$BACKUP_DIR/app.db"
fi
for name in installed-version installed-sha256; do
  if test -f "$DATA_ROOT/$name"; then cp -p "$DATA_ROOT/$name" "$BACKUP_DIR/$name"; fi
done

MANAGEMENT_KEY_ESCAPED=$(dotenv_escape "$MANAGEMENT_KEY")
LOGIN_PASSWORD_ESCAPED=$(dotenv_escape "$LOGIN_PASSWORD")

ENV_NEW="$DATA_ROOT/.env.new.$STAMP"
{
  printf '%s\n' 'CPA_BASE_URL=http://127.0.0.1:8317'
  printf 'CPA_MANAGEMENT_KEY="%s"\n' "$MANAGEMENT_KEY_ESCAPED"
  printf '%s\n' \
    'REDIS_QUEUE_ADDR=127.0.0.1:8317' \
    'REDIS_QUEUE_TLS=false' \
    'APP_HOST=0.0.0.0' \
    'APP_PORT=8320' \
    'CPA_PUBLIC_URL=http://192.168.2.1:8317' \
    'AUTH_ENABLED=true'
  printf 'LOGIN_PASSWORD="%s"\n' "$LOGIN_PASSWORD_ESCAPED"
  printf '%s\n' \
    'AUTH_SESSION_TTL=168h' \
    'TZ=Asia/Shanghai' \
    "WORK_DIR=$WORK_DIR" \
    'LOG_LEVEL=info' \
    'LOG_FILE_ENABLED=true' \
    'LOG_RETENTION_DAYS=7' \
    'BACKUP_ENABLED=true' \
    'BACKUP_INTERVAL=24h' \
    'BACKUP_RETENTION_DAYS=7' \
    'QUOTA_REFRESH_WORKER_LIMIT=2'
} > "$ENV_NEW"
if test "$HAD_ENV" -eq 1; then cp -p "$BACKUP_DIR/app.env" "$ENV_NEW"; fi
chmod 0600 "$ENV_NEW"

RUN_NEW="$SERVICE_DIR/run.new.$STAMP"
cat > "$RUN_NEW" <<'EOF'
#!/data/data/com.termux/files/usr/bin/sh
exec 2>&1
export HOME='/data/data/com.termux/files/home' PREFIX='/data/data/com.termux/files/usr'
export PATH='/data/data/com.termux/files/usr/bin:/system/bin'
export TMPDIR='/data/data/com.termux/files/usr/tmp'
export NO_PROXY='127.0.0.1,localhost,::1' no_proxy="$NO_PROXY"
exec env -u LD_PRELOAD -u TERMUX_EXEC__PROC_SELF_EXE /system/bin/sh -c \
  "exec /data/data/com.termux/files/usr/bin/cpa-usage-keeper --env /data/data/com.termux/files/home/router-data/cpa-usage-keeper/.env"
EOF
if test "$HAD_RUN" -eq 1; then cp -p "$BACKUP_DIR/service-run" "$RUN_NEW"; fi
chmod 0755 "$RUN_NEW"

LOG_RUN_NEW="$SERVICE_DIR/log/run.new.$STAMP"
cat > "$LOG_RUN_NEW" <<'EOF'
#!/data/data/com.termux/files/usr/bin/sh
exec /data/data/com.termux/files/usr/bin/svlogd -tt /data/data/com.termux/files/home/router-stack/services/cpa-usage-keeper/log/main
EOF
if test "$HAD_LOG_RUN" -eq 1; then cp -p "$BACKUP_DIR/log-run" "$LOG_RUN_NEW"; fi
chmod 0755 "$LOG_RUN_NEW"
printf '%s\n' 's1000000' 'n5' > "$SERVICE_DIR/log/main/config"
chmod 0600 "$SERVICE_DIR/log/main/config"

BIN_NEW="$BIN.new.$STAMP"
cp "$STAGED_BIN" "$BIN_NEW"
chmod 0700 "$BIN_NEW"
ACTIVATED=1
mv -f "$BIN_NEW" "$BIN"
cp "$STAGED_INFO" "$BUILD_INFO.new.$STAMP"
chmod 0600 "$BUILD_INFO.new.$STAMP"
mv -f "$BUILD_INFO.new.$STAMP" "$BUILD_INFO"
mv -f "$ENV_NEW" "$ENV_FILE"
mv -f "$RUN_NEW" "$SERVICE_DIR/run"
mv -f "$LOG_RUN_NEW" "$SERVICE_DIR/log/run"
printf '%s\n' "$EXPECTED_VERSION" > "$DATA_ROOT/installed-version"
printf '%s  %s\n' "$EXPECTED_SHA256" "$BIN" > "$DATA_ROOT/installed-sha256"

"$SERVICE_GUARD" repair cpa-usage-keeper

sv restart "$SERVICE_DIR" >/dev/null 2>&1 || true
COUNT=0
while test "$COUNT" -lt 1800; do
  INDEX_HTML=""
  if curl -fsS --max-time 2 http://127.0.0.1:8320/readyz >/dev/null 2>&1 && \
     INDEX_HTML=$(curl -fsS --max-time 3 http://127.0.0.1:8320/ 2>/dev/null) && \
     printf '%s' "$INDEX_HTML" | grep -qi '<!doctype html'; then
    COMMITTED=1
    echo CPA_USAGE_KEEPER_INSTALL_OK
    echo "BACKUP=$BACKUP_DIR"
    echo "URL=http://192.168.2.1:8320"
    exit 0
  fi
  STATUS=$(curl -fsS --max-time 3 http://127.0.0.1:8320/api/v1/startup/status 2>/dev/null || true)
  if printf '%s' "$STATUS" | jq -e '.phase == "failed"' >/dev/null 2>&1; then
    fail "v2 database migration failed: $STATUS"
  fi
  if test $((COUNT % 15)) -eq 0; then printf 'STARTUP=%s\n' "$STATUS"; fi
  COUNT=$((COUNT + 1))
  sleep 2
done
fail "v2 did not become ready within one hour"
