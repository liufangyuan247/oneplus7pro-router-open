#!/system/bin/sh
set +e

PREFIX=/data/data/com.termux/files/usr
IP=/system/bin/ip
TC=/system/bin/tc
[ -x "$PREFIX/bin/ip" ] && IP="$PREFIX/bin/ip"
[ -x "$PREFIX/bin/tc" ] && TC="$PREFIX/bin/tc"

echo '=== IDENTITY ==='
uname -a
id
getprop sys.boot_completed

echo '=== USB ==='
for dev in /sys/bus/usb/devices/*; do
  [ -r "$dev/idVendor" ] || continue
  [ -r "$dev/idProduct" ] || continue
  printf '%s %s:%s config=%s speed=%s product=%s\n' \
    "${dev##*/}" \
    "$(cat "$dev/idVendor")" "$(cat "$dev/idProduct")" \
    "$(cat "$dev/bConfigurationValue" 2>/dev/null)" \
    "$(cat "$dev/speed" 2>/dev/null)" \
    "$(cat "$dev/product" 2>/dev/null)"
done

echo '=== INTERFACES ==='
for net in /sys/class/net/*; do
  name="${net##*/}"
  case "$name" in
    eth*|wan*|lan*|ppp*|usb*) ;;
    *) continue ;;
  esac
  printf '%s oper=%s carrier=%s speed=%s duplex=%s qlen=%s driver=%s\n' \
    "$name" \
    "$(cat "$net/operstate" 2>/dev/null)" \
    "$(cat "$net/carrier" 2>/dev/null)" \
    "$(cat "$net/speed" 2>/dev/null)" \
    "$(cat "$net/duplex" 2>/dev/null)" \
    "$(cat "$net/tx_queue_len" 2>/dev/null)" \
    "$(basename "$(readlink -f "$net/device/driver" 2>/dev/null)" 2>/dev/null)"
  [ -x "$PREFIX/bin/ethtool" ] && "$PREFIX/bin/ethtool" -i "$name" 2>/dev/null
done

echo '=== ADDRESSES ==='
"$IP" -o addr show
echo '=== ROUTES ==='
"$IP" route show table main
echo '=== RULES ==='
"$IP" rule show

echo '=== QDISCS ==='
"$TC" qdisc show
echo '=== MODULE ==='
grep '^sch_fq_codel ' /proc/modules

echo '=== SERVICES ==='
ps -A | grep -E 'runsvdir|openlist|aria2c|caddy|sshd|vlmcsd|qbittorrent'

echo '=== NETWORK LOG ==='
tail -80 /data/adb/router-network/router-network.log 2>/dev/null
tail -40 /data/adb/router-network/fq-codel.log 2>/dev/null

echo '=== KERNEL ALERTS ==='
dmesg | grep -Ei 'Kernel panic|Unable to handle|BUG:|Call trace|Unknown symbol|module verification failed|ax_usb_nic.*fail|ax_usb_nic.*error|usb.*reset|NETDEV WATCHDOG' | tail -120

echo '=== BATTERY ==='
dumpsys battery | grep -E 'AC powered|USB powered|status:|level:|voltage:|temperature:'
