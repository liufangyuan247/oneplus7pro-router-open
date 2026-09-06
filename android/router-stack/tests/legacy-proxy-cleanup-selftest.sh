#!/system/bin/sh
set -eu

MANAGER=/data/local/tmp/router-network.candidate
TEST_ROOT="$(mktemp -d /data/local/tmp/router-legacy-cleanup.XXXXXX)"

cleanup() {
  case "$TEST_ROOT" in /data/local/tmp/router-legacy-cleanup.*) rm -rf "$TEST_ROOT" ;; esac
}
trap cleanup EXIT INT TERM

export ROUTER_NETWORK_SOURCE_ONLY=1
. "$MANAGER"

BASE="$TEST_ROOT/base"
RUNTIME="$BASE/runtime"
LOG="$BASE/router-network.log"
IP="$TEST_ROOT/ip"
mkdir -p "$RUNTIME" "$BASE/proxy.lock" "$BASE/codex-proxy.lock"

cat > "$IP" <<'EOF'
#!/system/bin/sh
printf '%s\n' "$*" >> "$LEGACY_CLEANUP_COMMANDS"
case "$1:$2" in
  rule:del) exit 1 ;;
  route:show) exit 0 ;;
  *) exit 0 ;;
esac
EOF
chmod 700 "$IP"
export LEGACY_CLEANUP_COMMANDS="$TEST_ROOT/commands"

for path in \
  "$BASE/sing-box.pid" "$BASE/codex-sing-box.pid" "$BASE/codex-dnsmasq.pid" \
  "$RUNTIME/sing-box.json" "$RUNTIME/mihomo-endpoint-cidrs" \
  "$RUNTIME/codex-sing-box.json" "$RUNTIME/codex-dnsmasq.conf" \
  "$RUNTIME/dnsmasq-openai-dns-v2" "$RUNTIME/dnsmasq-openai-hosts-v1" \
  "$RUNTIME/openai-addresses" "$RUNTIME/openai-hosts" \
  "$RUNTIME/openai-refresh-at" "$RUNTIME/openai-targets"; do
  : > "$path"
done
: > "$RUNTIME/openai-addresses.new.123"
: > "$BASE/proxy.lock/owner"
: > "$BASE/codex-proxy.lock/owner"

cleanup_legacy_proxy_artifacts

for path in \
  "$BASE/sing-box.pid" "$BASE/codex-sing-box.pid" "$BASE/codex-dnsmasq.pid" \
  "$RUNTIME/sing-box.json" "$RUNTIME/codex-sing-box.json" \
  "$RUNTIME/codex-dnsmasq.conf" "$RUNTIME/openai-addresses"; do
  [ ! -e "$path" ]
done
[ ! -d "$BASE/proxy.lock" ]
[ ! -d "$BASE/codex-proxy.lock" ]
grep -q '^rule del priority 8000$' "$LEGACY_CLEANUP_COMMANDS"
grep -q '^route flush table 101$' "$LEGACY_CLEANUP_COMMANDS"
grep -q 'removed obsolete sing-box/Codex transition data plane' "$LOG"

echo LEGACY_PROXY_CLEANUP_TEST_OK
