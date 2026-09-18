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
STAGED_BIN="$HOME_DIR/.stage/cpa-usage-keeper-v1.14.8-android"
MANAGEMENT_KEY_FILE="$HOME_DIR/router-data/cpa/management-key"
ADMIN_PASSWORD_FILE="$HOME_DIR/router-data/admin/password"
EXPECTED_SHA256=4cbd3ba408f10b899c35db96134ac56e4b3a8b429d507269bd40bfa765f2a052
STAMP=$(date +%Y%m%d-%H%M%S)
BACKUP_DIR="$DATA_ROOT/install-backups/$STAMP"
SERVICE_GUARD="$HOME_DIR/router-stack/bin/termux-service-guard"
FAILED=0
HAD_BIN=0
HAD_ENV=0
HAD_RUN=0
HAD_LOG_RUN=0

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
test -s "$MANAGEMENT_KEY_FILE" || fail "CPA management key is missing"
test -s "$ADMIN_PASSWORD_FILE" || fail "router admin password is missing"

ACTUAL_SHA256=$(sha256sum "$STAGED_BIN" | awk '{print $1}')
test "$ACTUAL_SHA256" = "$EXPECTED_SHA256" || fail "staged binary checksum mismatch"
chmod 0755 "$STAGED_BIN"

VERSION_OUTPUT=$(env -u LD_PRELOAD -u TERMUX_EXEC__PROC_SELF_EXE /system/bin/sh -c \
  "'$STAGED_BIN' --version")
test "$VERSION_OUTPUT" = "v1.14.8-android" || fail "unexpected binary version"

mkdir -p "$BACKUP_DIR" "$WORK_DIR" "$SERVICE_DIR/log/main"
if test -f "$BIN"; then HAD_BIN=1; cp -p "$BIN" "$BACKUP_DIR/cpa-usage-keeper"; fi
if test -f "$ENV_FILE"; then HAD_ENV=1; cp -p "$ENV_FILE" "$BACKUP_DIR/app.env"; fi
if test -f "$SERVICE_DIR/run"; then HAD_RUN=1; cp -p "$SERVICE_DIR/run" "$BACKUP_DIR/service-run"; fi
if test -f "$SERVICE_DIR/log/run"; then HAD_LOG_RUN=1; cp -p "$SERVICE_DIR/log/run" "$BACKUP_DIR/log-run"; fi

MANAGEMENT_KEY=$(tr -d '\r\n' < "$MANAGEMENT_KEY_FILE")
LOGIN_PASSWORD=$(tr -d '\r\n' < "$ADMIN_PASSWORD_FILE")
test -n "$MANAGEMENT_KEY" || fail "CPA management key is empty"
test -n "$LOGIN_PASSWORD" || fail "router admin password is empty"

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
chmod 0755 "$RUN_NEW"

LOG_RUN_NEW="$SERVICE_DIR/log/run.new.$STAMP"
cat > "$LOG_RUN_NEW" <<'EOF'
#!/data/data/com.termux/files/usr/bin/sh
exec /data/data/com.termux/files/usr/bin/svlogd -tt /data/data/com.termux/files/home/router-stack/services/cpa-usage-keeper/log/main
EOF
chmod 0755 "$LOG_RUN_NEW"
printf '%s\n' 's1000000' 'n5' > "$SERVICE_DIR/log/main/config"
chmod 0600 "$SERVICE_DIR/log/main/config"

BIN_NEW="$BIN.new.$STAMP"
cp "$STAGED_BIN" "$BIN_NEW"
chmod 0755 "$BIN_NEW"
mv -f "$BIN_NEW" "$BIN"
mv -f "$ENV_NEW" "$ENV_FILE"
mv -f "$RUN_NEW" "$SERVICE_DIR/run"
mv -f "$LOG_RUN_NEW" "$SERVICE_DIR/log/run"
printf '%s\n' 'v1.14.8-android' > "$DATA_ROOT/installed-version"
printf '%s  %s\n' "$EXPECTED_SHA256" "$BIN" > "$DATA_ROOT/installed-sha256"

"$SERVICE_GUARD" repair

sv up "$SERVICE_DIR" >/dev/null 2>&1 || true
COUNT=0
while test "$COUNT" -lt 15; do
  if curl -fsS --max-time 2 http://127.0.0.1:8320/healthz >/dev/null 2>&1; then
    echo CPA_USAGE_KEEPER_INSTALL_OK
    echo "URL=http://192.168.2.1:8320"
    exit 0
  fi
  COUNT=$((COUNT + 1))
  sleep 1
done

sv down "$SERVICE_DIR" >/dev/null 2>&1 || true
if test "$HAD_BIN" -eq 1 && test "$HAD_ENV" -eq 1 && test "$HAD_RUN" -eq 1; then
  cp -p "$BACKUP_DIR/cpa-usage-keeper" "$BIN"
  cp -p "$BACKUP_DIR/app.env" "$ENV_FILE"
  cp -p "$BACKUP_DIR/service-run" "$SERVICE_DIR/run"
  if test "$HAD_LOG_RUN" -eq 1; then
    cp -p "$BACKUP_DIR/log-run" "$SERVICE_DIR/log/run"
  fi
  sv up "$SERVICE_DIR" >/dev/null 2>&1 || true
  echo "ERROR: health check failed; previous installation was restored" >&2
else
  echo "ERROR: health check failed; first installation has been left stopped" >&2
fi
echo "Backup: $BACKUP_DIR" >&2
exit 1
