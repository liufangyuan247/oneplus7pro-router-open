#!/system/bin/sh
PREFIX=/data/data/com.termux/files/usr
STACK=/data/data/com.termux/files/home/router-stack

echo '=== power supplies ==='
for supply in /sys/class/power_supply/*; do
  echo "--- $(basename "$supply") ---"
  cat "$supply/uevent" 2>/dev/null | grep -E 'POWER_SUPPLY_(STATUS|CAPACITY|TEMP|VOLTAGE_NOW|CURRENT_NOW|CURRENT_MAX|VOLTAGE_MAX|INPUT_CURRENT|TYPE|USB_TYPE|ONLINE|CHARGE_TYPE)=' || true
done

echo '=== thermal zones ==='
for zone in /sys/class/thermal/thermal_zone*; do
  type=$(cat "$zone/type" 2>/dev/null)
  temp=$(cat "$zone/temp" 2>/dev/null)
  case "$type" in
    *cpu*|*CPU*|*gpu*|*GPU*|*battery*|*quiet*|*skin*|*xo*|*ddr*) echo "$(basename "$zone") $type $temp" ;;
  esac
done

echo '=== interfaces ==='
"$PREFIX/bin/ip" -j address show 2>/dev/null || "$PREFIX/bin/ip" address show
echo '=== routes ==='
"$PREFIX/bin/ip" -j route show table main 2>/dev/null || "$PREFIX/bin/ip" route show table main
echo '=== neighbours ==='
"$PREFIX/bin/ip" -j neigh show 2>/dev/null || "$PREFIX/bin/ip" neigh show

echo '=== leases ==='
for lease in /data/misc/dhcp/dnsmasq.leases /data/misc/dhcp/*.leases /data/data/com.termux/files/home/router-data/dnsmasq/*.leases; do
  [ -f "$lease" ] || continue
  echo "--- $lease ---"
  cat "$lease"
done

echo '=== services ==='
"$PREFIX/bin/sv" status "$STACK/services"/* 2>/dev/null || true
