#!/data/data/com.termux/files/usr/bin/sh
set -eu

HOME_DIR=/data/data/com.termux/files/home
PREFIX=/data/data/com.termux/files/usr
SERVICE="$HOME_DIR/router-stack/services/cpa-usage-keeper"
SOURCE="$HOME_DIR/.stage/cpa-usage-keeper-run-whole-home"
STAMP="$(date '+%Y%m%d-%H%M%S')"
BACKUP="$HOME_DIR/router-data/cpa-usage-keeper/install-backups/$STAMP-whole-home"
SERVICE_GUARD="$HOME_DIR/router-stack/bin/termux-service-guard"

export HOME="$HOME_DIR" PATH="$PREFIX/bin:/system/bin"
[ -s "$SOURCE" ]
sh -n "$SOURCE"
mkdir -p "$BACKUP"
cp -p "$SERVICE/run" "$BACKUP/service-run"
cp "$SOURCE" "$SERVICE/run.new.$STAMP"
chmod 0755 "$SERVICE/run.new.$STAMP"
mv "$SERVICE/run.new.$STAMP" "$SERVICE/run"
"$SERVICE_GUARD" repair cpa-usage-keeper
sv restart "$SERVICE" >/dev/null 2>&1 || true

attempt=0
while [ "$attempt" -lt 15 ]; do
  if curl -fsS --max-time 2 http://127.0.0.1:8320/healthz >/dev/null 2>&1; then
    if "$HOME_DIR/.stage/verify-pricing-sync.sh"; then
      echo "CPA_USAGE_KEEPER_FOLLOWS_WHOLE_HOME backup=$BACKUP"
      exit 0
    fi
    break
  fi
  attempt=$((attempt + 1))
  sleep 1
done

sv down "$SERVICE" >/dev/null 2>&1 || true
cp -p "$BACKUP/service-run" "$SERVICE/run"
sv up "$SERVICE" >/dev/null 2>&1 || true
echo 'Usage Keeper follow-mode verification failed; previous run script restored' >&2
exit 1
