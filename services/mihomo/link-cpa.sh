#!/data/data/com.termux/files/usr/bin/sh
set -eu

PREFIX=/data/data/com.termux/files/usr
HOME=/data/data/com.termux/files/home
STACK="$HOME/router-stack"
DATA="$HOME/router-data/cpa"
CONFIG="$DATA/config.yaml"
SERVICE="$STACK/services/cpa"
BACKUPS="$HOME/router-data/backups/cpa"

[ -s "$CONFIG" ] || { echo 'CPA config is missing' >&2; exit 1; }
MANAGER=/data/adb/router-network/router-network
STATUS="$DATA/router-policy-status.$$"
"$PREFIX/bin/su" -c "$MANAGER status" > "$STATUS"
trap 'rm -f "$STATUS"' EXIT INT TERM
grep -q '^whole_home_proxy=disabled$' "$STATUS" || { echo 'whole-home proxy must remain disabled for this check' >&2; exit 1; }
grep -q '^codex_tun=up$' "$STATUS" || { echo 'router OpenAI selective TUN is not ready' >&2; exit 1; }
grep -q '^codex_policy=active$' "$STATUS" || { echo 'router OpenAI selective policy is not active' >&2; exit 1; }
CODE=$("$PREFIX/bin/curl" -sS --connect-timeout 8 --max-time 20 \
    --noproxy '*' \
    --output /dev/null --write-out '%{http_code}' \
    https://api.openai.com/v1/models 2>/dev/null || true)
case "$CODE" in
    2??|3??|4??) ;;
    *) echo 'router OpenAI selective egress is unavailable; CPA was not changed' >&2; exit 1 ;;
esac

STAMP=$(date '+%Y%m%d-%H%M%S')
mkdir -p "$BACKUPS"
BACKUP="$BACKUPS/config.before-mihomo-$STAMP.yaml"
cp -p "$CONFIG" "$BACKUP"

"$PREFIX/bin/awk" '
/^usage-statistics-enabled:[[:space:]]*/ { print "usage-statistics-enabled: true"; usage=1; next }
/^proxy-url:[[:space:]]*/ { print "proxy-url: \"\""; proxy=1; next }
{ print }
END {
    if (!usage) print "usage-statistics-enabled: true"
    if (!proxy) print "proxy-url: \"\""
}
' "$CONFIG" > "$CONFIG.new"
chmod 600 "$CONFIG.new"
mv "$CONFIG.new" "$CONFIG"

"$PREFIX/bin/sv" restart "$SERVICE" >/dev/null 2>&1 || true
ready=0
attempt=0
while [ "$attempt" -lt 15 ]; do
    if "$PREFIX/bin/curl" -fsS --max-time 2 http://127.0.0.1:8317/ >/dev/null 2>&1; then
        ready=1
        break
    fi
    attempt=$((attempt + 1))
    sleep 1
done

if [ "$ready" -ne 1 ]; then
    cp -p "$BACKUP" "$CONFIG"
    "$PREFIX/bin/sv" restart "$SERVICE" >/dev/null 2>&1 || true
    echo 'CPA failed after proxy activation; previous config restored' >&2
    exit 1
fi

printf '%s\n' "$BACKUP" > "$DATA/last-proxy-link-backup"
echo CPA_USES_ROUTER_OPENAI_POLICY
