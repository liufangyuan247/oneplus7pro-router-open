#!/system/bin/sh
set -eu

EXPECTED_MANAGER_SHA256="${1:-}"
BASE=/data/adb/router-network
PREFIX=/data/data/com.termux/files/usr
HOME_DIR=/data/data/com.termux/files/home
MIHOMO_SERVICE="$HOME_DIR/router-stack/services/mihomo"

[ -n "$EXPECTED_MANAGER_SHA256" ] || {
  echo 'usage: verify-router-recovery.sh EXPECTED_MANAGER_SHA256' >&2
  exit 2
}

actual="$(sha256sum "$BASE/router-network" | awk '{print $1}')"
[ "$actual" = "$EXPECTED_MANAGER_SHA256" ]

update_status="$BASE/runtime/recovery-update-status.$$"
router_status="$BASE/runtime/recovery-router-status.$$"
trap 'rm -f "$update_status" "$router_status"' EXIT INT TERM
"$BASE/safe-update-router-network" status > "$update_status"
grep -q '^pending=no$' "$update_status"
grep -q '^trial=no$' "$update_status"
grep -q '^health=ready$' "$update_status"

set -- $(pidof pppd)
[ "$#" -eq 1 ]
pppd_before="$1"
set -- $(pidof pppoe)
[ "$#" -eq 1 ]
set -- $(pidof mihomo)
[ "$#" -eq 1 ]

"$BASE/router-network" status > "$router_status"
grep -q '^ppp=ppp0$' "$router_status"
grep -q '^pppoe_backend_requested=userspace$' "$router_status"
grep -q '^mihomo_router=available$' "$router_status"
grep -q '^mihomo_tun=up$' "$router_status"
grep -q '^codex_policy=active$' "$router_status"
"$PREFIX/bin/sv" status "$MIHOMO_SERVICE" | grep -q '^run:'

set +e
"$BASE/router-network" pppoe-retry-kernel >/dev/null 2>&1
retry_rc=$?
set -e
[ "$retry_rc" -eq 1 ]
pppd_after="$(cat "$BASE/pppd.pid")"
[ "$pppd_before" = "$pppd_after" ]

openai_code=$("$PREFIX/bin/curl" -sS --connect-timeout 8 --max-time 20 \
  --proxy http://127.0.0.1:17891 \
  --output /dev/null --write-out '%{http_code}' \
  https://api.openai.com/v1/models 2>/dev/null || true)
case "$openai_code" in 2??|3??|4??) ;; *) exit 1 ;; esac

key=$(tr -d '\r\n' < "$HOME_DIR/router-data/cpa/api-key")
cpa_code=$("$PREFIX/bin/curl" -sS --max-time 15 \
  -H "Authorization: Bearer $key" \
  --output /dev/null --write-out '%{http_code}' \
  http://127.0.0.1:8317/v1/models 2>/dev/null || true)
[ "$cpa_code" = 200 ]

echo "ROUTER_RECOVERY_VERIFIED manager=$actual pppd=$pppd_after openai_http=$openai_code cpa_http=$cpa_code"
