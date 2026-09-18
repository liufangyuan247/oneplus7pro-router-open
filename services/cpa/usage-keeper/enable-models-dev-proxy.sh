#!/data/data/com.termux/files/usr/bin/sh
set -eu

HOME_DIR=/data/data/com.termux/files/home
PREFIX=/data/data/com.termux/files/usr
SERVICE_DIR="$HOME_DIR/router-stack/services/cpa-usage-keeper"
STAGED_RUN="$HOME_DIR/.stage/cpa-usage-keeper-run"
STAMP=$(date +%Y%m%d-%H%M%S)
BACKUP_DIR="$HOME_DIR/router-data/cpa-usage-keeper/install-backups/$STAMP-models-dev-proxy"
SERVICE_GUARD="$HOME_DIR/router-stack/bin/termux-service-guard"

export HOME="$HOME_DIR" PREFIX PATH="$PREFIX/bin:/system/bin"

test -s "$STAGED_RUN"
sh -n "$STAGED_RUN"
curl --proxy http://127.0.0.1:17891 -fsS --connect-timeout 5 --max-time 12 \
  https://models.dev/api.json >/dev/null

mkdir -p "$BACKUP_DIR"
cp -p "$SERVICE_DIR/run" "$BACKUP_DIR/service-run"
cp "$STAGED_RUN" "$SERVICE_DIR/run.new.$STAMP"
chmod 0755 "$SERVICE_DIR/run.new.$STAMP"
mv "$SERVICE_DIR/run.new.$STAMP" "$SERVICE_DIR/run"
"$SERVICE_GUARD" repair cpa-usage-keeper
sv restart "$SERVICE_DIR" >/dev/null 2>&1 || true

ATTEMPT=0
while test "$ATTEMPT" -lt 12; do
  if curl -fsS --max-time 2 http://127.0.0.1:8320/healthz >/dev/null 2>&1; then
    echo CPA_USAGE_KEEPER_MODELS_DEV_PROXY_ENABLED
    exit 0
  fi
  ATTEMPT=$((ATTEMPT + 1))
  sleep 1
done

sv down "$SERVICE_DIR" >/dev/null 2>&1 || true
cp -p "$BACKUP_DIR/service-run" "$SERVICE_DIR/run"
sv up "$SERVICE_DIR" >/dev/null 2>&1 || true
echo 'ERROR: Usage Keeper failed health checks; previous run script restored' >&2
exit 1
