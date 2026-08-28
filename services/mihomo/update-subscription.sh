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
CORE_PIDFILE="$DATA/supervised-core.pid"
OPENAI_DOMAINS=/data/adb/router-network/openai-domains

[ -s "$URL_FILE" ] || { echo 'subscription URL is not configured' >&2; exit 1; }
if ! mkdir "$LOCK" 2>/dev/null; then
    echo 'subscription update already running'
    exit 0
fi
mkdir -p "$WORK"
trap 'rm -rf "$WORK" "$LOCK"' EXIT INT TERM
DOMAIN_SNAPSHOT="$WORK/openai-domains"
"$PREFIX/bin/su" -c "cat $OPENAI_DOMAINS" > "$DOMAIN_SNAPSHOT" 2>/dev/null || {
    echo 'shared OpenAI domain list is missing or unreadable' >&2
    exit 1
}
[ -s "$DOMAIN_SNAPSHOT" ] || { echo 'shared OpenAI domain list is empty' >&2; exit 1; }

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

"$PREFIX/bin/curl" -fL \
    --connect-timeout 15 \
    --max-time 90 \
    --retry 2 \
    --user-agent 'clash.meta' \
    --output "$RAW" \
    "$URL"
[ "$(wc -c < "$RAW")" -ge 512 ] || { echo 'subscription response is unexpectedly small' >&2; exit 1; }

"$PREFIX/bin/awk" -v desired_mode="$MODE" '
function top_level(line) { return line ~ /^[A-Za-z0-9_-]+:[[:space:]]*/ }
function emit_codex_groups(indent, child) {
    child=indent "  "
    print indent "- name: \"Codex-日本自动\""
    print child "type: url-test"
    print child "include-all-proxies: true"
    print child "filter: \"(?i)🇯🇵|日本|Japan|(^|[^A-Za-z])JP([^A-Za-z]|$)\""
    print child "exclude-filter: \"(?i)日本.*04\""
    print child "url: https://chatgpt.com/cdn-cgi/trace"
    print child "interval: 30"
    print child "tolerance: 1000"
    print child "lazy: false"
    print child "timeout: 5000"
    print child "max-failed-times: 2"
    print indent "- name: \"Codex-日本\""
    print child "type: select"
    print child "proxies: [\"Codex-日本自动\"]"
    print child "include-all-proxies: true"
    print child "filter: \"(?i)🇯🇵|日本|Japan|(^|[^A-Za-z])JP([^A-Za-z]|$)\""
}
function emit_codex_listener() {
    print "listeners:"
    print "  - name: openai-selective"
    print "    type: mixed"
    print "    listen: 127.0.0.1"
    print "    port: 17891"
    print "    proxy: \"Codex-日本\""
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
BEGIN { skip=0; group_pending=0 }
{
    if (skip) {
        if (top_level($0)) skip=0
        else next
    }
    if (group_pending) {
        if ($0 ~ /^[[:space:]]*-/) {
            indent=$0
            sub(/[^[:space:]].*$/, "", indent)
            emit_codex_groups(indent)
            group_pending=0
        } else if (top_level($0)) {
            emit_codex_groups("  ")
            group_pending=0
        }
    }
    if ($0 ~ /^proxy-groups:[[:space:]]*/) {
        print "proxy-groups:"
        groups=1
        group_pending=1
        next
    }
    if ($0 ~ /^(dns|tun|listeners|profile|sniffer):[[:space:]]*/) {
        key=$0
        sub(/:.*/, "", key)
        if (key == "dns") {
            print "dns:"
            print "  enable: false"
            dns=1
        } else if (key == "tun") {
            print "tun:"
            print "  enable: false"
            tun=1
        } else if (key == "listeners") {
            emit_codex_listener()
            listeners=1
        } else if (key == "sniffer") {
            emit_sniffer()
            sniffer=1
        } else {
            print "profile:"
            print "  store-selected: true"
            print "  store-fake-ip: false"
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
    if ($0 ~ /^external-controller:[[:space:]]*/) { print "external-controller: 127.0.0.1:9091"; controller=1; next }
    if ($0 ~ /^external-ui:[[:space:]]*/) { print "external-ui: ui"; ui=1; next }
    if ($0 ~ /^secret:[[:space:]]*/) { print "secret: \"\""; secret=1; next }
    if ($0 ~ /^ipv6:[[:space:]]*/) { print "ipv6: false"; ipv6=1; next }
    print
}
END {
    if (group_pending) emit_codex_groups("  ")
    if (!groups) { print "proxy-groups:"; emit_codex_groups("  ") }
    if (!mixed) print "mixed-port: 17890"
    if (!port) print "port: 0"
    if (!socks) print "socks-port: 0"
    if (!redir) print "redir-port: 0"
    if (!tproxy) print "tproxy-port: 0"
    if (!allow) print "allow-lan: false"
    if (!bind) print "bind-address: 127.0.0.1"
    if (!mode) print "mode: " desired_mode
    if (!controller) print "external-controller: 127.0.0.1:9091"
    if (!ui) print "external-ui: ui"
    if (!secret) print "secret: \"\""
    if (!ipv6) print "ipv6: false"
    if (!dns) { print "dns:"; print "  enable: false" }
    if (!tun) { print "tun:"; print "  enable: false" }
    if (!listeners) emit_codex_listener()
    if (!sniffer) emit_sniffer()
    if (!profile) { print "profile:"; print "  store-selected: true"; print "  store-fake-ip: false" }
}
' "$RAW" > "$ADAPTED"

# Prepend the shared OpenAI rules ahead of subscription-provided rules. The
# 17891 listener is used by the selective TUN, while these rules keep the
# generic 17890/full-house path on the exact same Codex-日本 policy group.
WITH_OPENAI_RULES="$WORK/with-openai-rules.yaml"
"$PREFIX/bin/awk" '
function emit_openai_rules(indent) {
    for (i=1; i<=domain_count; i++)
        print indent "- DOMAIN-SUFFIX," domains[i] ",Codex-日本"
}
NR == FNR {
    line=$0
    sub(/[[:space:]]*#.*/, "", line)
    gsub(/\r/, "", line)
    if (line != "" && !(line in seen_domain)) {
        domains[++domain_count]=line
        seen_domain[line]=1
    }
    next
}
!saw_rules && /^rules:[[:space:]]*$/ {
    print
    saw_rules=1
    rules_pending=1
    next
}
rules_pending {
    if ($0 ~ /^[[:space:]]*-/) {
        indent=$0
        sub(/[^[:space:]].*$/, "", indent)
        emit_openai_rules(indent)
        rules_pending=0
        inserted=1
    } else if ($0 ~ /^[A-Za-z0-9_-]+:[[:space:]]*/) {
        emit_openai_rules("  ")
        rules_pending=0
        inserted=1
    }
}
{ print }
END {
    if (rules_pending) {
        emit_openai_rules("  ")
    } else if (!saw_rules) {
        print "rules:"
        emit_openai_rules("  ")
        print "  - MATCH,DIRECT"
    }
}
' "$DOMAIN_SNAPSHOT" "$ADAPTED" > "$WITH_OPENAI_RULES"
mv "$WITH_OPENAI_RULES" "$ADAPTED"

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
    [ -r "/proc/$supervised/status" ] && [ -r "/proc/$supervised/cmdline" ] || return 1
    [ "$(awk '/^Uid:/ { print $2; exit }' "/proc/$supervised/status" 2>/dev/null)" = 0 ] || return 1
    tr '\000' ' ' < "/proc/$supervised/cmdline" 2>/dev/null | grep -Fq "$TARGET" || return 1

    matching=0
    for pid in $(pidof mihomo 2>/dev/null || true); do
        [ -r "/proc/$pid/cmdline" ] || continue
        if tr '\000' ' ' < "/proc/$pid/cmdline" 2>/dev/null | grep -Fq "$TARGET"; then
            matching=$((matching + 1))
            [ "$pid" = "$supervised" ] || return 1
        fi
    done
    [ "$matching" = 1 ]
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
    # A validated subscription can normally be applied without dropping CPA
    # or active household sessions. A supervised restart remains the fallback
    # if the controller is unavailable.
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

# Refresh the shared OpenAI target set after node or rule changes. This is
# independent of the optional whole-home proxy.
MANAGER=/data/adb/router-network/router-network
if "$PREFIX/bin/su" -c "$MANAGER status" 2>/dev/null | grep -q '^codex_selective_proxy=enabled$'; then
    "$PREFIX/bin/su" -c "$MANAGER codex-refresh" >/dev/null 2>&1 || \
        echo 'warning: OpenAI selective route refresh will be retried by the network manager' >&2
fi
if "$PREFIX/bin/su" -c "$MANAGER status" 2>/dev/null | grep -q '^whole_home_proxy=enabled$'; then
    "$PREFIX/bin/su" -c "$MANAGER proxy-refresh" >/dev/null 2>&1 || \
        echo 'warning: whole-home endpoint bypass refresh will be retried by the network manager' >&2
fi

echo SUBSCRIPTION_UPDATED
