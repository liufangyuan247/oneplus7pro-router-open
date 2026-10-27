#!/system/bin/sh
set -eu
BASE=/data/adb/router-adguardhome
PREFIX=/data/data/com.termux/files/usr
HOME_DIR=/data/data/com.termux/files/home
IPTABLES="$PREFIX/bin/iptables"
CHAIN=OP7_ADGUARD
PIDFILE="$BASE/guard.pid"
STATE="$BASE/state"

remove_rules() {
    while "$IPTABLES" -w 2 -t nat -C PREROUTING -j "$CHAIN" 2>/dev/null; do
        "$IPTABLES" -w 2 -t nat -D PREROUTING -j "$CHAIN" || break
    done
    "$IPTABLES" -w 2 -t nat -F "$CHAIN" 2>/dev/null || true
    "$IPTABLES" -w 2 -t nat -X "$CHAIN" 2>/dev/null || true
    if [ "$(cat "$STATE" 2>/dev/null || true)" != bypass ]; then
        echo 'AdGuard DNS bypass: existing dnsmasq handles clients directly'
        echo bypass > "$STATE"
    fi
}

case "${1:-run}" in
    stop)
        pid=$(cat "$PIDFILE" 2>/dev/null || true)
        case "$pid" in ''|*[!0-9]*) ;; *)
            if /system/bin/timeout 2 /system/bin/tr '\000' ' ' < "/proc/$pid/cmdline" 2>/dev/null | grep -Fq "$BASE/dns-guard run"; then
                kill -TERM "$pid" 2>/dev/null || true
            fi
        ;; esac
        remove_rules
        exit 0
        ;;
    run) ;;
    *) exit 2 ;;
esac

mkdir -p "$BASE"
chmod 0700 "$BASE"
old=$(cat "$PIDFILE" 2>/dev/null || true)
case "$old" in ''|*[!0-9]*) ;; *)
    if kill -0 "$old" 2>/dev/null && /system/bin/timeout 2 /system/bin/tr '\000' ' ' < "/proc/$old/cmdline" 2>/dev/null | grep -Fq "$BASE/dns-guard run"; then
        echo 'AdGuard DNS guard is already running' >&2
        exit 1
    fi
;; esac
echo $$ > "$PIDFILE"
trap 'remove_rules; rm -f "$PIDFILE"; exit 0' INT TERM HUP EXIT

while :; do
    lan=$(awk '{print $2; exit}' "$HOME_DIR/router-data/admin/active-interfaces" 2>/dev/null || true)
    case "$lan" in ''|*[!a-zA-Z0-9_.:-]*) lan='' ;; esac
    addr=''
    if [ -n "$lan" ]; then
        addr=$("$PREFIX/bin/ip" -4 -o addr show dev "$lan" scope global 2>/dev/null | awk '{split($4,a,"/");print a[1];exit}')
    fi
    if [ -n "$addr" ] && "$PREFIX/bin/python" "$BASE/dns-health.py"; then
        if ! "$IPTABLES" -w 2 -t nat -C "$CHAIN" -i "$lan" -d "$addr" -p udp --dport 53 -j REDIRECT --to-ports 5354 2>/dev/null ||
           ! "$IPTABLES" -w 2 -t nat -C "$CHAIN" -i "$lan" -d "$addr" -p tcp --dport 53 -j REDIRECT --to-ports 5354 2>/dev/null; then
            "$IPTABLES" -w 2 -t nat -N "$CHAIN" 2>/dev/null || true
            "$IPTABLES" -w 2 -t nat -F "$CHAIN"
            "$IPTABLES" -w 2 -t nat -A "$CHAIN" -i "$lan" -d "$addr" -p udp --dport 53 -j REDIRECT --to-ports 5354
            "$IPTABLES" -w 2 -t nat -A "$CHAIN" -i "$lan" -d "$addr" -p tcp --dport 53 -j REDIRECT --to-ports 5354
        fi
        "$IPTABLES" -w 2 -t nat -C PREROUTING -j "$CHAIN" 2>/dev/null || "$IPTABLES" -w 2 -t nat -I PREROUTING 1 -j "$CHAIN"
        if [ "$(cat "$STATE" 2>/dev/null || true)" != active ]; then
            echo "AdGuard DNS enabled on $lan at $addr; upstream remains dnsmasq"
            echo active > "$STATE"
        fi
    else
        remove_rules
    fi
    sleep 5
done
