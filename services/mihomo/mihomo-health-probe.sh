#!/data/data/com.termux/files/usr/bin/sh
set -u

PREFIX=/data/data/com.termux/files/usr
HOME_DIR=/data/data/com.termux/files/home
DATA="$HOME_DIR/router-data/mihomo"
STATE="$HOME_DIR/router-data/health"
CONFIG="$DATA/config.yaml"
PIDFILE="$DATA/supervised-core.pid"
CURL="$PREFIX/bin/curl"
SU="$PREFIX/bin/su"
STATUS="$STATE/mihomo-data-plane.status"
LAST_PID="$STATE/mihomo-data-plane.last-pid"
LAST_RESTART="$STATE/mihomo-data-plane.last-restart"
BYPASS="$HOME_DIR/router-stack/bin/mihomo-client-bypass"
COOLDOWN_SECONDS=300

mkdir -p "$STATE"

write_status() {
    state="$1"
    reason="$2"
    detail="${3:-}"
    tmp="$STATUS.new.$$"
    {
        echo "timestamp=$(date '+%F %T')"
        echo "state=$state"
        echo "reason=$reason"
        echo "detail=$detail"
    } > "$tmp"
    chmod 600 "$tmp"
    mv "$tmp" "$STATUS"
}

write_scalar() {
    path="$1"
    value="$2"
    tmp="$path.new.$$"
    echo "$value" > "$tmp" || return 1
    chmod 600 "$tmp" || return 1
    mv "$tmp" "$path"
}

fail() {
    write_status failed "$1" "${2:-}"
    echo "mihomo health failed: $1 ${2:-}" >&2
    exit 1
}

http_ok() {
    case "$1" in
        2??|3??|4??) return 0 ;;
        *) return 1 ;;
    esac
}

"$CURL" -fsS --max-time 3 http://127.0.0.1:9091/version >/dev/null 2>&1 ||
    fail controller_unavailable

pid="$(cat "$PIDFILE" 2>/dev/null || true)"
case "$pid" in
    ''|*[!0-9]*) fail supervised_pid_missing ;;
esac

pids="$("$SU" -c 'pidof mihomo' 2>/dev/null || true)"
set -- $pids
[ "$#" = 1 ] && [ "$1" = "$pid" ] || fail singleton_mismatch "pidfile=$pid pids=$pids"

uid="$("$SU" -c "awk '/^Uid:/ { print \$2; exit }' /proc/$pid/status" 2>/dev/null || true)"
[ "$uid" = 0 ] || fail core_not_root "pid=$pid uid=$uid"
cmdline="$("$SU" -c "tr '\\000' ' ' < /proc/$pid/cmdline" 2>/dev/null || true)"
case "$cmdline" in
    *"$CONFIG"*) ;;
    *) fail core_identity_mismatch "pid=$pid" ;;
esac

# Reconcile the reserved priority-4990 source rules after boot, WAN recovery,
# or any external policy-rule flush. This is intentionally checked before the
# TUN invariant so configured clients never silently fall back into op7tun.
if [ -x "$BYPASS" ]; then
    "$SU" -c "'$BYPASS' apply" >/dev/null 2>&1 || fail client_bypass_unhealthy
fi

# Android denies the Termux app UID access to the inet_diag netlink socket.
# Read listener state through the already-authorized Magisk root bridge.
listeners="$("$SU" -c "$PREFIX/bin/ss -lnt" 2>/dev/null || true)"
echo "$listeners" | grep -q '127\.0\.0\.1:17890 ' || fail listener_17890_missing
echo "$listeners" | grep -q '127\.0\.0\.1:17891 ' || fail listener_17891_missing

"$SU" -c "test -e /sys/class/net/op7tun && \
    /system/bin/ip rule show priority 8102 | /system/bin/grep -q 'lookup 2022' && \
    /system/bin/ip route show table 2022 | /system/bin/grep -q 'dev op7tun'" \
    >/dev/null 2>&1 || fail tun_route_missing

previous_pid="$(cat "$LAST_PID" 2>/dev/null || true)"
now="$(date +%s)"
if [ -n "$previous_pid" ] && [ "$previous_pid" != "$pid" ]; then
    write_scalar "$LAST_RESTART" "$now" || fail state_write_failed last_restart
fi
write_scalar "$LAST_PID" "$pid" || fail state_write_failed last_pid

proxy_code="$("$CURL" -sS --connect-timeout 5 --max-time 15 \
    --proxy http://127.0.0.1:17891 --output /dev/null --write-out '%{http_code}' \
    https://chatgpt.com/cdn-cgi/trace 2>/dev/null || true)"
if ! http_ok "$proxy_code"; then
    proxy_code="$("$CURL" -sS --connect-timeout 5 --max-time 15 \
        --proxy http://127.0.0.1:17891 --output /dev/null --write-out '%{http_code}' \
        https://api.openai.com/v1/models 2>/dev/null || true)"
fi

if http_ok "$proxy_code"; then
    write_status ok data_plane_ready "pid=$pid code=$proxy_code"
    exit 0
fi

# Do not churn Mihomo while PPPoE or the upstream WAN itself is unavailable.
direct_code="$("$CURL" -sS --connect-timeout 5 --max-time 12 --noproxy '*' \
    --output /dev/null --write-out '%{http_code}' \
    https://connectivitycheck.gstatic.com/generate_204 2>/dev/null || true)"
if ! http_ok "$direct_code"; then
    write_status degraded wan_unavailable "proxy=$proxy_code direct=$direct_code"
    exit 0
fi

# A PID change means healthd or the user has just restarted the core. Suppress
# repeated data-plane restarts for five minutes, while local invariant failures
# above remain actionable immediately.
last_restart="$(cat "$LAST_RESTART" 2>/dev/null || echo 0)"
case "$last_restart" in ''|*[!0-9]*) last_restart=0 ;; esac
age=$((now - last_restart))
if [ "$last_restart" -gt 0 ] && [ "$age" -lt "$COOLDOWN_SECONDS" ]; then
    write_status degraded restart_cooldown "pid=$pid age=$age proxy=$proxy_code"
    exit 0
fi

fail proxy_egress_unavailable "proxy=$proxy_code direct=$direct_code"
