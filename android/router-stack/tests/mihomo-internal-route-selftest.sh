#!/system/bin/sh
set -eu

MANAGER="${MANAGER:-/data/local/tmp/router-network.candidate}"
TEST_ROOT="$(mktemp -d /data/local/tmp/router-mihomo-route.XXXXXX)"

cleanup() {
  case "$TEST_ROOT" in /data/local/tmp/router-mihomo-route.*) rm -rf "$TEST_ROOT" ;; esac
}
trap cleanup EXIT INT TERM

export ROUTER_NETWORK_SOURCE_ONLY=1
. "$MANAGER"

BASE="$TEST_ROOT/base"
RUNTIME="$BASE/runtime"
ACTIVE="$BASE/active"
DATA_PLANE_STATE="$RUNTIME/lan-data-plane"
ADMIN_INTERFACES="$RUNTIME/active-interfaces"
LOG="$BASE/router-network.log"
IP="$TEST_ROOT/ip"
export MIHOMO_RULE_STATE="$TEST_ROOT/rules"
export MIHOMO_RULE_COMMANDS="$TEST_ROOT/commands"
mkdir -p "$RUNTIME"
: > "$MIHOMO_RULE_STATE"
: > "$MIHOMO_RULE_COMMANDS"

cat > "$IP" <<'EOF'
#!/system/bin/sh
printf '%s\n' "$*" >> "$MIHOMO_RULE_COMMANDS"
[ "${1:-}" = rule ] || exit 0
case "${2:-}" in
  show)
    if [ "${3:-}" = priority ] && [ -n "${4:-}" ]; then
      grep "^${4}:" "$MIHOMO_RULE_STATE" 2>/dev/null || true
    else
      cat "$MIHOMO_RULE_STATE"
    fi
    ;;
  add)
    priority="$4"
    destination="$6"
    table="$8"
    printf '%s: from all to %s lookup %s\n' "$priority" "$destination" "$table" >> "$MIHOMO_RULE_STATE"
    ;;
  del)
    priority="$4"
    if [ "${5:-}" = to ] && [ "${7:-}" = lookup ]; then
      destination="$6"
      table="$8"
      awk -v p="$priority" -v d="$destination" -v t="$table" \
        '$0 != p ": from all to " d " lookup " t' "$MIHOMO_RULE_STATE" > "$MIHOMO_RULE_STATE.new"
    else
      awk -v p="$priority" '$0 !~ ("^" p ":")' "$MIHOMO_RULE_STATE" > "$MIHOMO_RULE_STATE.new"
    fi
    mv "$MIHOMO_RULE_STATE.new" "$MIHOMO_RULE_STATE"
    ;;
esac
EOF
chmod 700 "$IP"

[ "$RULE_MIHOMO_INTERNAL" -lt 8100 ]

ensure_mihomo_internal_policy
ensure_mihomo_internal_policy
[ "$(grep -c '^rule add priority 8099 to 198\.18\.0\.0/30 lookup 2022$' "$MIHOMO_RULE_COMMANDS")" -eq 1 ]
[ "$(grep -c '^8099: from all to 198\.18\.0\.0/30 lookup 2022$' "$MIHOMO_RULE_STATE")" -eq 1 ]

remove_mihomo_internal_policy
! grep -q '^8099:' "$MIHOMO_RULE_STATE"

printf '%s\n' '8099: from all fwmark 0x1 lookup 77' > "$MIHOMO_RULE_STATE"
: > "$MIHOMO_RULE_COMMANDS"
remove_mihomo_internal_policy
grep -q '^8099: from all fwmark 0x1 lookup 77$' "$MIHOMO_RULE_STATE"
! grep -q '^rule del priority 8099' "$MIHOMO_RULE_COMMANDS"
if ensure_mihomo_internal_policy; then
  echo 'foreign priority-8099 rule was incorrectly accepted' >&2
  exit 1
fi
grep -q '^8099: from all fwmark 0x1 lookup 77$' "$MIHOMO_RULE_STATE"

printf '%s\n' '8099: from all to 198.18.0.0/30 lookup 2022' > "$MIHOMO_RULE_STATE"
desired_data_plane_state() { echo direct; }
ensure_dnsmasq() { :; }
reconcile_lan_data_plane eth1
! grep -q '^8099:' "$MIHOMO_RULE_STATE"

printf '%s\n' '8099: from all to 198.18.0.0/30 lookup 2022' > "$MIHOMO_RULE_STATE"
stop_dnsmasq() { :; }
stop_ppp() { :; }
cleanup_legacy_proxy_artifacts() { :; }
remove_jump() { :; }
remove_routes() { :; }
cleanup_runtime
! grep -q '^8099:' "$MIHOMO_RULE_STATE"

echo MIHOMO_INTERNAL_ROUTE_TEST_OK
