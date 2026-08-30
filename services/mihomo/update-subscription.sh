#!/data/data/com.termux/files/usr/bin/sh
set -eu

PREFIX=/data/data/com.termux/files/usr
HOME=/data/data/com.termux/files/home
DATA="$HOME/router-data/mihomo"
SERVICE="$HOME/router-stack/services/mihomo"
URL_FILE="$DATA/subscription.url"
TARGET="$DATA/config.yaml"
BACKUP="$DATA/config.last-good.yaml"
SETTINGS="$DATA/runtime-settings.yaml"
MODE_FILE="$DATA/runtime-mode"
DISABLED="$DATA/disabled"
LOCK="$DATA/.subscription-update.lock"
WORK="$DATA/.subscription-update.$$"
SOCAT="$PREFIX/bin/socat"
SU="$PREFIX/bin/su"
CORE_PIDFILE="$DATA/supervised-core.pid"
MANAGED_AI_DOMAINS="$DATA/ai-proxy-domains.txt"
CUSTOM_AI_DOMAINS="$DATA/custom-ai-proxy-domains.txt"
MANAGED_RULE_DOMAINS="$DATA/selective-proxy-domains.txt"
CUSTOM_RULE_DOMAINS="$DATA/custom-proxy-domains.txt"

[ -s "$URL_FILE" ] || { echo 'subscription URL is not configured' >&2; exit 1; }
if ! mkdir "$LOCK" 2>/dev/null; then
    echo 'subscription update already running'
    exit 0
fi
mkdir -p "$WORK"
trap 'rm -rf "$WORK" "$LOCK"' EXIT INT TERM
DOMAIN_POLICY_SNAPSHOT="$WORK/domain-policy"
touch "$DOMAIN_POLICY_SNAPSHOT"
for domain_file in "$MANAGED_AI_DOMAINS" "$CUSTOM_AI_DOMAINS"; do
    [ ! -f "$domain_file" ] || "$PREFIX/bin/awk" '
        { line=$0; sub(/[[:space:]]*#.*/, "", line); gsub(/\r/, "", line); if (line != "") print "AI\t" line }
    ' "$domain_file" >> "$DOMAIN_POLICY_SNAPSHOT"
done
if ! grep -q '^AI' "$DOMAIN_POLICY_SNAPSHOT"; then
    cat << 'EOF_DOMAINS' >> "$DOMAIN_POLICY_SNAPSHOT"
AI	openai.com
AI	chatgpt.com
AI	oaistatic.com
AI	oaiusercontent.com
AI	cloudcode-pa.googleapis.com
AI	daily-cloudcode-pa.googleapis.com
AI	oauth2.googleapis.com
AI	generativelanguage.googleapis.com
EOF_DOMAINS
fi
for domain_file in "$MANAGED_RULE_DOMAINS" "$CUSTOM_RULE_DOMAINS"; do
    [ ! -f "$domain_file" ] || "$PREFIX/bin/awk" '
        { line=$0; sub(/[[:space:]]*#.*/, "", line); gsub(/\r/, "", line); if (line != "") print "RULE\t" line }
    ' "$domain_file" >> "$DOMAIN_POLICY_SNAPSHOT"
done

"$PREFIX/bin/awk" -F '\t' '
    NF != 2 || $1 !~ /^(AI|RULE)$/ || $2 !~ /^([A-Za-z0-9_*-]+\.)*[A-Za-z0-9_*-]+$/ {
        print "invalid managed proxy domain at line " NR > "/dev/stderr"
        bad=1
    }
    END { exit bad }
' "$DOMAIN_POLICY_SNAPSHOT"

MODE=rule
if [ -s "$MODE_FILE" ]; then
    candidate=$(tr -d '\r\n' < "$MODE_FILE")
    case "$candidate" in
        rule|global|direct) MODE="$candidate" ;;
    esac
fi

URL=$(cat "$URL_FILE")
RAW="$WORK/raw.yaml"
ADAPTED="$WORK/adapted.yaml"

download_subscription() {
    if "$PREFIX/bin/curl" -fsS --max-time 2 http://127.0.0.1:9091/version >/dev/null 2>&1; then
        "$PREFIX/bin/curl" -fsSL \
            --proxy http://127.0.0.1:17890 \
            --connect-timeout 15 --max-time 90 --retry 2 \
            --user-agent 'clash.meta' --output "$RAW" "$URL" && return 0
    fi
    "$PREFIX/bin/curl" -fsSL \
        --connect-timeout 15 --max-time 90 --retry 2 \
        --user-agent 'clash.meta' --output "$RAW" "$URL"
}

download_subscription
[ "$(wc -c < "$RAW")" -ge 512 ] || { echo 'subscription response is unexpectedly small' >&2; exit 1; }

# Parse raw YAML, inject household RULE and AI proxy groups, TUN, DNS, and Sniffer
"$PREFIX/bin/awk" -v desired_mode="$MODE" -v domain_policy_file="$DOMAIN_POLICY_SNAPSHOT" '
function top_level(line) { return line ~ /^[A-Za-z0-9_-]+:[[:space:]]*/ }
function emit_proxy_groups(indent, child) {
    child=indent "  "
    print indent "- name: \"家庭RULE自动\""
    print child "type: url-test"
    print child "include-all-proxies: true"
    print child "url: https://www.gstatic.com/generate_204"
    print child "interval: 300"
    print child "tolerance: 150"
    print child "lazy: true"
    print child "timeout: 5000"
    print child "max-failed-times: 2"
    print indent "- name: \"家庭RULE\""
    print child "type: select"
    print child "proxies: [\"家庭RULE自动\", \"DIRECT\"]"
    print child "include-all-proxies: true"
    print indent "- name: \"AI-日本自动\""
    print child "type: url-test"
    print child "include-all-proxies: true"
    print child "filter: \"(?i)🇯🇵|日本|Japan|(^|[^A-Za-z])JP([^A-Za-z]|$)\""
    print child "exclude-filter: \"(?i)日本.*(03|04|05)\""
    print child "url: https://www.gstatic.com/generate_204"
    print child "interval: 60"
    print child "tolerance: 250"
    print child "lazy: false"
    print child "timeout: 5000"
    print child "max-failed-times: 2"
    print indent "- name: \"AI\""
    print child "type: select"
    print child "proxies: [\"AI-日本自动\", \"DIRECT\"]"
    print child "include-all-proxies: true"
    print child "filter: \"(?i)🇯🇵|日本|Japan|(^|[^A-Za-z])JP([^A-Za-z]|$)\""
    print child "exclude-filter: \"(?i)日本.*(03|04|05)\""
}
function emit_ai_listener() {
    print "listeners:"
    print "  - name: openai-selective"
    print "    type: mixed"
    print "    listen: 127.0.0.1"
    print "    port: 17891"
    print "    proxy: \"AI\""
    print "    udp: true"
}
function emit_sniffer() {
    print "sniffer:"
    print "  enable: true"
    print "  force-dns-mapping: true"
    print "  parse-pure-ip: true"
    print "  sniff:"
    print "    HTTP:"
    print "      ports: [80, 8080-8880]"
    print "      override-destination: true"
    print "    TLS:"
    print "      ports: [443, 8443]"
    print "    QUIC:"
    print "      ports: [443, 8443]"
}
function emit_tun() {
    print "tun:"
    print "  enable: true"
    print "  stack: gvisor"
    print "  device: op7tun"
    print "  auto-route: true"
    print "  auto-redirect: false"
    print "  auto-detect-interface: false"
    print "  strict-route: false"
    print "  iproute2-table-index: 2022"
    print "  iproute2-rule-index: 8100"
    print "  dns-hijack:"
    print "    - any:53"
    print "    - tcp://any:53"
    print "  route-exclude-address:"
    print "    - 127.0.0.0/8"
    print "    - 10.0.0.0/8"
    print "    - 172.16.0.0/12"
    print "    - 192.168.0.0/16"
    print "    - 100.64.0.0/10"
    print "    - 169.254.0.0/16"
    print "    - 224.0.0.0/4"
    print "  exclude-uid:"
    print "    - 0"
}
function emit_dns() {
    print "dns:"
    print "  enable: true"
    print "  listen: 127.0.0.1:1053"
    print "  ipv6: false"
    print "  cache-algorithm: arc"
    print "  enhanced-mode: fake-ip"
    print "  fake-ip-range: 198.18.0.1/16"
    print "  fake-ip-filter-mode: whitelist"
    print "  fake-ip-filter:"
    for (i=1; i<=dns_ai_count; i++) print "    - \"+." dns_ai_domains[i] "\""
    for (i=1; i<=dns_rule_count; i++) print "    - \"+." dns_rule_domains[i] "\""
    print "  respect-rules: false"
    print "  default-nameserver:"
    print "    - 223.5.5.5"
    print "    - 119.29.29.29"
    print "  proxy-server-nameserver:"
    print "    - 223.5.5.5"
    print "    - 119.29.29.29"
    print "  direct-nameserver:"
    print "    - 223.5.5.5"
    print "    - 119.29.29.29"
    print "  direct-nameserver-follow-policy: false"
    print "  nameserver-policy:"
    for (i=1; i<=dns_ai_count; i++) {
        print "    \"+." dns_ai_domains[i] "\":"
        print "      - https://1.1.1.1/dns-query#AI"
        print "      - https://8.8.8.8/dns-query#AI"
    }
    for (i=1; i<=dns_rule_count; i++) {
        print "    \"+." dns_rule_domains[i] "\":"
        print "      - https://1.1.1.1/dns-query#家庭RULE"
        print "      - https://8.8.8.8/dns-query#家庭RULE"
    }
    print "  nameserver:"
    print "    - 223.5.5.5"
    print "    - 119.29.29.29"
}
BEGIN {
    skip=0
    while ((getline policy_line < domain_policy_file) > 0) {
        policy=policy_line
        sub(/\t.*/, "", policy)
        domain=policy_line
        sub(/^[^\t]*\t/, "", domain)
        policy_key=policy SUBSEP domain
        if (domain == "" || dns_seen[policy_key]) continue
        dns_seen[policy_key]=1
        if (policy == "AI") dns_ai_domains[++dns_ai_count]=domain
        else if (policy == "RULE") dns_rule_domains[++dns_rule_count]=domain
    }
    close(domain_policy_file)
}
{
    if (skip) {
        if (top_level($0)) skip=0
        else next
    }
    if ($0 ~ /^proxy-groups:[[:space:]]*/) {
        print "proxy-groups:"
        emit_proxy_groups("  ")
        groups=1
        skip=1
        next
    }
    # A subscription supplies transport nodes only. Its policy groups, remote
    # rule providers and rules are intentionally discarded so a vendor update
    # can never expand the set of household traffic sent through the proxy.
    if ($0 ~ /^(rule-providers|rules):[[:space:]]*/) {
        skip=1
        next
    }
    if ($0 ~ /^(dns|tun|listeners|profile|sniffer):[[:space:]]*/) {
        key=$0
        sub(/:.*/, "", key)
        if (key == "dns") {
            emit_dns()
            dns=1
        } else if (key == "tun") {
            emit_tun()
            tun=1
        } else if (key == "listeners") {
            emit_ai_listener()
            listeners=1
        } else if (key == "sniffer") {
            emit_sniffer()
            sniffer=1
        } else {
            print "profile:"
            print "  store-selected: true"
            print "  store-fake-ip: true"
            profile=1
        }
        skip=1
        next
    }
    if ($0 ~ /^mixed-port:[[:space:]]*/) { print "mixed-port: 17890"; mixed=1; next }
    if ($0 ~ /^port:[[:space:]]*/) { print "port: 0"; port=1; next }
    if ($0 ~ /^socks-port:[[:space:]]*/) { print "socks-port: 0"; socks=1; next }
    if ($0 ~ /^redir-port:[[:space:]]*/) { print "redir-port: 0"; redir=1; next }
    if ($0 ~ /^tproxy-port:[[:space:]]*/) { print "tproxy-port: 0"; tproxy=1; next }
    if ($0 ~ /^allow-lan:[[:space:]]*/) { print "allow-lan: false"; allow=1; next }
    if ($0 ~ /^bind-address:[[:space:]]*/) { print "bind-address: 127.0.0.1"; bind=1; next }
    if ($0 ~ /^mode:[[:space:]]*/) { print "mode: " desired_mode; mode=1; next }
    if ($0 ~ /^find-process-mode:[[:space:]]*/) { print "find-process-mode: off"; process_mode=1; next }
    if ($0 ~ /^interface-name:[[:space:]]*/) { print "interface-name: ppp0"; outbound_interface=1; next }
    if ($0 ~ /^external-controller:[[:space:]]*/) { print "external-controller: 127.0.0.1:9091"; controller=1; next }
    if ($0 ~ /^external-ui:[[:space:]]*/) { print "external-ui: ui"; ui=1; next }
    if ($0 ~ /^secret:[[:space:]]*/) { print "secret: \"\""; secret=1; next }
    if ($0 ~ /^ipv6:[[:space:]]*/) { print "ipv6: false"; ipv6=1; next }
    print
}
END {
    if (!groups) { print "proxy-groups:"; emit_proxy_groups("  ") }
    if (!mixed) print "mixed-port: 17890"
    if (!port) print "port: 0"
    if (!socks) print "socks-port: 0"
    if (!redir) print "redir-port: 0"
    if (!tproxy) print "tproxy-port: 0"
    if (!allow) print "allow-lan: false"
    if (!bind) print "bind-address: 127.0.0.1"
    if (!mode) print "mode: " desired_mode
    if (!process_mode) print "find-process-mode: off"
    if (!outbound_interface) print "interface-name: ppp0"
    if (!controller) print "external-controller: 127.0.0.1:9091"
    if (!ui) print "external-ui: ui"
    if (!secret) print "secret: \"\""
    if (!ipv6) print "ipv6: false"
    if (!dns) emit_dns()
    if (!tun) emit_tun()
    if (!listeners) emit_ai_listener()
    if (!sniffer) emit_sniffer()
    if (!profile) { print "profile:"; print "  store-selected: true"; print "  store-fake-ip: true" }
}
' "$RAW" > "$ADAPTED"

# Append the complete locally managed policy. No subscription rule survives.
WITH_RULES="$WORK/with-rules.yaml"
"$PREFIX/bin/awk" '
function emit_managed_rules(indent) {
    # Private subnets & Tailscale direct bypass
    print indent "- IP-CIDR,127.0.0.0/8,DIRECT,no-resolve"
    print indent "- IP-CIDR,192.168.0.0/16,DIRECT,no-resolve"
    print indent "- IP-CIDR,10.0.0.0/8,DIRECT,no-resolve"
    print indent "- IP-CIDR,172.16.0.0/12,DIRECT,no-resolve"
    print indent "- IP-CIDR,100.64.0.0/10,DIRECT,no-resolve"
    print indent "- IP-CIDR6,fe80::/10,DIRECT,no-resolve"
    print indent "- IP-CIDR6,fd00::/8,DIRECT,no-resolve"

    # Telegram clients commonly use MTProto connections to literal IP
    # addresses, so domain suffixes alone do not cover the mobile app.
    print indent "- IP-CIDR,91.108.0.0/16,家庭RULE,no-resolve"
    print indent "- IP-CIDR,95.161.64.0/20,家庭RULE,no-resolve"
    print indent "- IP-CIDR,149.154.160.0/20,家庭RULE,no-resolve"

    # AI services keep a stable Japanese exit; the general household allowlist
    # can select any subscription node. Everything else remains direct.
    for (i=1; i<=ai_domain_count; i++) {
        print indent "- DOMAIN-SUFFIX," ai_domains[i] ",AI"
    }
    for (i=1; i<=rule_domain_count; i++) {
        print indent "- DOMAIN-SUFFIX," rule_domains[i] ",家庭RULE"
    }
}
NR == FNR {
    policy=$1
    domain=$0
    sub(/^[^\t]*\t/, "", domain)
    if (domain != "" && !(domain in seen_domain)) {
        if (policy == "AI") ai_domains[++ai_domain_count]=domain
        else if (policy == "RULE") rule_domains[++rule_domain_count]=domain
        seen_domain[domain]=1
    }
    next
}
{ print }
END {
    print ""
    print "rules:"
    emit_managed_rules("  ")
    print "  - MATCH,DIRECT"
}
' "$DOMAIN_POLICY_SNAPSHOT" "$ADAPTED" > "$WITH_RULES"
mv "$WITH_RULES" "$ADAPTED"

if [ -s "$SETTINGS" ]; then
    WITH_SETTINGS="$WORK/with-settings.yaml"
    "$PREFIX/bin/awk" '
BEGIN { count=split("mixed-port port socks-port redir-port tproxy-port allow-lan bind-address", order, " ") }
NR==FNR {
    if ($0 ~ /^[a-z-]+:[[:space:]]*/) {
        key=$0
        sub(/:.*/, "", key)
        override[key]=$0
    }
    next
}
{
    line=$0
    if (line ~ /^[a-z-]+:[[:space:]]*/) {
        key=line
        sub(/:.*/, "", key)
        if (key in override) {
            if (!(key in seen)) print override[key]
            seen[key]=1
            next
        }
    }
    print line
}
END {
    for (i=1; i<=count; i++) {
        key=order[i]
        if ((key in override) && !(key in seen)) print override[key]
    }
}
' "$SETTINGS" "$ADAPTED" > "$WITH_SETTINGS"
    mv "$WITH_SETTINGS" "$ADAPTED"
fi

chmod 600 "$ADAPTED"
"$PREFIX/bin/mihomo" -t -d "$DATA" -f "$ADAPTED"

if [ "${MIHOMO_UPDATE_VALIDATE_ONLY:-0}" = 1 ]; then
    candidate="$DATA/config.unified-candidate.yaml"
    cp "$ADAPTED" "$candidate"
    chmod 600 "$candidate"
    echo "SUBSCRIPTION_CANDIDATE_READY path=$candidate"
    exit 0
fi

[ -s "$TARGET" ] && cp -p "$TARGET" "$BACKUP"
mv "$ADAPTED" "$TARGET"

if [ -f "$DISABLED" ]; then
    echo SUBSCRIPTION_UPDATED_CORE_DISABLED
    exit 0
fi

controller_ready() {
    "$PREFIX/bin/curl" -fsS --max-time 2 http://127.0.0.1:9091/version >/dev/null 2>&1
}

ports_ready() {
    [ -x "$SOCAT" ] || return 1
    for port in 9091 17890 17891; do
        "$SOCAT" -T1 -u /dev/null "TCP:127.0.0.1:$port,connect-timeout=1" >/dev/null 2>&1 || return 1
    done
}

core_identity_ready() {
    supervised="$(cat "$CORE_PIDFILE" 2>/dev/null || true)"
    case "$supervised" in ''|*[!0-9]*) return 1 ;; esac
    [ -x "$SU" ] || return 1

    # Mihomo runs as root for native TUN. Android hides root-owned /proc entries
    # from the Termux UID, so perform only the identity checks through Magisk su.
    uid=$("$SU" -c "awk '/^Uid:/ { print \$2; exit }' /proc/$supervised/status" 2>/dev/null || true)
    [ "$uid" = 0 ] || return 1
    cmdline=$("$SU" -c "tr '\\000' ' ' < /proc/$supervised/cmdline" 2>/dev/null || true)
    case "$cmdline" in *"$TARGET"*) ;; *) return 1 ;; esac
    pids=$("$SU" -c 'pidof mihomo' 2>/dev/null || true)
    set -- $pids
    [ "$#" = 1 ] && [ "$1" = "$supervised" ]
}

wait_ready() {
    attempt=0
    while [ "$attempt" -lt 15 ]; do
        controller_ready && ports_ready && core_identity_ready && return 0
        attempt=$((attempt + 1))
        sleep 1
    done
    return 1
}

hot_reload() {
    controller_ready || return 1
    "$PREFIX/bin/curl" -fsS --max-time 5 \
        -X PUT -H 'Content-Type: application/json' \
        --data-binary "{\"path\":\"$TARGET\"}" \
        'http://127.0.0.1:9091/configs?force=true' >/dev/null
    wait_ready
}

restart_core() {
    "$PREFIX/bin/sv" down "$SERVICE" >/dev/null 2>&1 || true
    "$PREFIX/bin/sv" up "$SERVICE"
    wait_ready
}

activate_target() {
    if hot_reload; then
        return 0
    fi
    restart_core
}

if ! activate_target; then
    if [ -s "$BACKUP" ]; then
        cp -p "$BACKUP" "$TARGET"
        activate_target || true
    fi
    echo 'updated config failed health checks; restored previous config' >&2
    exit 1
fi

echo SUBSCRIPTION_UPDATED
