#!/system/bin/sh
set -eu

HOME=/data/data/com.termux/files/home
DATA="$HOME/router-data/mihomo"
STACK="$HOME/router-stack"
SERVICE="$STACK/services/mihomo-updater"
UCI_SOURCE=/data/local/tmp/r2s-openclash-uci
UPDATER_SOURCE=/data/local/tmp/mihomo-update-subscription
RUN_SOURCE=/data/local/tmp/mihomo-updater-run

[ -s "$UCI_SOURCE" ] || { echo 'missing staged OpenClash UCI config' >&2; exit 1; }
[ -s "$UPDATER_SOURCE" ] || { echo 'missing updater script' >&2; exit 1; }
[ -s "$RUN_SOURCE" ] || { echo 'missing updater run script' >&2; exit 1; }

URL=$(sed -nE "s/^[[:space:]]*option address '(https?:\/\/[^']+)'.*/\1/p" "$UCI_SOURCE" | head -n 1)
[ -n "$URL" ] || { echo 'no subscription URL found' >&2; exit 1; }

printf '%s\n' "$URL" > "$DATA/subscription.url"
cp "$UPDATER_SOURCE" "$STACK/bin/mihomo-update-subscription"
mkdir -p "$SERVICE"
cp "$RUN_SOURCE" "$SERVICE/run"

chown u0_a207:u0_a207 "$SERVICE" "$DATA/subscription.url" "$STACK/bin/mihomo-update-subscription" "$SERVICE/run"
chmod 0600 "$DATA/subscription.url"
chmod 0700 "$STACK/bin/mihomo-update-subscription" "$SERVICE/run"
restorecon "$DATA/subscription.url" "$STACK/bin/mihomo-update-subscription" "$SERVICE/run" >/dev/null 2>&1 || true

/data/data/com.termux/files/usr/bin/sv up "$SERVICE"
echo MIHOMO_SUBSCRIPTION_UPDATER_INSTALLED
