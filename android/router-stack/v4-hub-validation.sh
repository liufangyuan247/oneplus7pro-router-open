#!/system/bin/sh

TERMUX_PREFIX=/data/data/com.termux/files/usr

echo KERNEL
uname -a
echo ROOT
id

echo NET
for dev in eth0 eth1; do
  echo "[$dev]"
  "$TERMUX_PREFIX/bin/ethtool" -i "$dev" 2>/dev/null || true
  "$TERMUX_PREFIX/bin/ethtool" "$dev" 2>/dev/null || true
  ip -s link show "$dev" 2>/dev/null || true
done

echo ROUTER
/data/adb/router-network/router-network status

echo ADDRESSES
ip -4 address show
ip -4 route show

echo QUEUE
cat /sys/class/net/ppp0/tx_queue_len 2>/dev/null || true
tc qdisc show dev ppp0 2>/dev/null || true

echo BATTERY
dumpsys battery

echo USB_POWER
for path in \
  /sys/class/power_supply/*/online \
  /sys/class/power_supply/*/current_now \
  /sys/class/power_supply/*/voltage_now \
  /sys/class/power_supply/*/type \
  /sys/class/power_supply/*/usb_type; do
  [ -r "$path" ] || continue
  printf '%s=' "$path"
  cat "$path"
done

echo ALERTS
dmesg | grep -i -E 'panic|watchdog|napi|work.*weight|r8152|ax_usb|disconnect|reset|over-current|thermal' | tail -n 100

echo SERVICES
"$TERMUX_PREFIX/bin/sv" status \
  /data/data/com.termux/files/home/router-stack/services/router-admin \
  /data/data/com.termux/files/home/router-stack/services/mihomo \
  /data/data/com.termux/files/home/router-stack/services/openlist \
  /data/data/com.termux/files/home/router-stack/services/aria2 \
  /data/data/com.termux/files/home/router-stack/services/cpa \
  /data/data/com.termux/files/home/router-stack/services/sshd 2>/dev/null || true

echo CONNECTIVITY
"$TERMUX_PREFIX/bin/curl" -sS -o /dev/null \
  -w 'router_cloudflare=%{http_code} connect=%{time_connect} total=%{time_total}\n' \
  --max-time 12 https://cp.cloudflare.com/generate_204 || true

echo KMS
cat /data/adb/vlmcsd/vlmcsd.pid 2>/dev/null || true
ss -lntp 2>/dev/null | grep ':1688' || true
