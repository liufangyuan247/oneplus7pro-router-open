#!/system/bin/sh
set +e

PREFIX=/data/data/com.termux/files/usr
IP=/system/bin/ip
TC=/system/bin/tc
[ -x "$PREFIX/bin/ip" ] && IP="$PREFIX/bin/ip"
[ -x "$PREFIX/bin/tc" ] && TC="$PREFIX/bin/tc"

echo '=== TIME / LOAD ==='
date '+%F %T %z'
cat /proc/uptime
cat /proc/loadavg
cat /proc/meminfo | grep -E '^(MemTotal|MemFree|MemAvailable|Buffers|Cached|SwapTotal|SwapFree|Slab|SReclaimable|SUnreclaim):'

echo '=== LINK COUNTERS ==='
for name in eth0 eth1 ppp0; do
  net="/sys/class/net/$name"
  [ -d "$net" ] || continue
  printf '%s oper=%s carrier=%s speed=%s duplex=%s qlen=%s\n' \
    "$name" "$(cat "$net/operstate" 2>/dev/null)" \
    "$(cat "$net/carrier" 2>/dev/null)" "$(cat "$net/speed" 2>/dev/null)" \
    "$(cat "$net/duplex" 2>/dev/null)" "$(cat "$net/tx_queue_len" 2>/dev/null)"
  for counter in rx_bytes tx_bytes rx_packets tx_packets rx_errors tx_errors rx_dropped tx_dropped multicast collisions; do
    printf ' %s=%s' "$counter" "$(cat "$net/statistics/$counter" 2>/dev/null)"
  done
  echo
done

echo '=== DRIVER ERROR COUNTERS ==='
for name in eth0 eth1; do
  [ -x "$PREFIX/bin/ethtool" ] || break
  echo "[$name]"
  "$PREFIX/bin/ethtool" -S "$name" 2>/dev/null | \
    grep -Ei 'drop|error|fail|miss|timeout|reset|abort|overflow|underflow|lost|fifo|carrier|collision' | \
    grep -Eiv ': 0$'
done

echo '=== QDISC ==='
"$TC" -s qdisc show dev ppp0 2>/dev/null

echo '=== CONNECTIVITY ==='
ping -c 10 -W 2 100.91.128.1
ping -c 10 -W 2 223.5.5.5

echo '=== BATTERY / THERMAL ==='
dumpsys battery | grep -E 'AC powered|USB powered|status:|level:|voltage:|temperature:'
for path in /sys/class/power_supply/battery/current_now \
            /sys/class/power_supply/battery/voltage_now \
            /sys/class/power_supply/battery/capacity \
            /sys/class/power_supply/usb/present \
            /sys/class/power_supply/usb/online; do
  [ -r "$path" ] && echo "$path=$(cat "$path")"
done
for zone in /sys/class/thermal/thermal_zone*; do
  [ -r "$zone/temp" ] || continue
  temp="$(cat "$zone/temp" 2>/dev/null)"
  [ "${temp:-0}" -ge 45000 ] 2>/dev/null || continue
  printf '%s type=%s temp=%s\n' "${zone##*/}" "$(cat "$zone/type" 2>/dev/null)" "$temp"
done

echo '=== SERVICE SUPERVISORS ==='
for pid in $(pidof runsvdir 2>/dev/null); do
  printf 'pid=%s ppid=' "$pid"
  sed -n 's/^PPid:[[:space:]]*//p' "/proc/$pid/status" 2>/dev/null
  printf ' cmd='
  tr '\000' ' ' < "/proc/$pid/cmdline" 2>/dev/null
  printf ' cwd='
  readlink "/proc/$pid/cwd" 2>/dev/null
  echo
done
ps -A | grep -E 'openlist|aria2c|caddy|tailscaled|sshd|vlmcsd|qbittorrent'

echo '=== SCREEN-OFF SETTINGS ==='
for namespace in secure system global; do
  echo "[$namespace]"
  settings list "$namespace" | grep -Ei 'aod|ambient|doze|double.*tap|gesture|pickup|wake' | sort
done

echo '=== KERNEL ALERT COUNTS ==='
dmesg | grep -c 'WARNING: CPU:'
dmesg | grep -c 'net_rx_action+0x1fc/0x4a8'
dmesg | grep -c 'unbalanced disables for pm8150l_l9'
dmesg | grep -c 'NETDEV WATCHDOG'
dmesg | grep -c 'reset SuperSpeed USB device'
dmesg | grep -Ei 'Kernel panic|Unable to handle|BUG:|WARNING: CPU:|NETDEV WATCHDOG|reset SuperSpeed USB device|ax_usb_nic.*(fail|error)' | tail -80
