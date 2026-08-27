#!/data/data/com.termux/files/usr/bin/sh
set -eu

PREFIX=/data/data/com.termux/files/usr
HOME=/data/data/com.termux/files/home
CONFIG="$HOME/router-data/cpa/config.yaml"
SERVICE="$HOME/router-stack/services/cpa"
BACKUPS="$HOME/router-data/backups/cpa"

[ -s "$CONFIG" ] || {
  echo "CPA config is missing: $CONFIG" >&2
  exit 1
}

mkdir -p "$BACKUPS"
stamp=$(date +%Y%m%d-%H%M%S)
cp -p "$CONFIG" "$BACKUPS/config.before-request-logging-$stamp.yaml"

set_scalar() {
  key=$1
  value=$2
  if grep -q "^${key}:" "$CONFIG"; then
    sed -i "s|^${key}:.*|${key}: ${value}|" "$CONFIG"
  else
    printf '\n%s: %s\n' "$key" "$value" >> "$CONFIG"
  fi
}

set_scalar logging-to-file true
set_scalar request-log true
set_scalar logs-max-total-size-mb 128
set_scalar error-logs-max-files 20

chmod 600 "$CONFIG"
"$PREFIX/bin/sv" restart "$SERVICE"

ready=0
for _ in 1 2 3 4 5 6 7 8 9 10; do
  if "$PREFIX/bin/curl" -fsS --max-time 3 http://127.0.0.1:8317/ >/dev/null; then
    ready=1
    break
  fi
  sleep 1
done

[ "$ready" -eq 1 ] || {
  cp -p "$BACKUPS/config.before-request-logging-$stamp.yaml" "$CONFIG"
  "$PREFIX/bin/sv" restart "$SERVICE" >/dev/null 2>&1 || true
  echo "CPA failed after enabling request logging; previous config restored" >&2
  exit 1
}

grep -E '^(logging-to-file|request-log|logs-max-total-size-mb|error-logs-max-files):' "$CONFIG"
echo "CPA_REQUEST_LOGGING_ENABLED"
