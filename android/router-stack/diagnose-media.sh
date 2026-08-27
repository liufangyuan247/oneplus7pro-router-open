#!/system/bin/sh

echo '--- TAILSCALE ---'
/data/adb/tailscale/bin/tailscale \
  --socket=/data/adb/tailscale/run/tailscaled.sock status 2>&1 | head -30
/data/adb/tailscale/bin/tailscale \
  --socket=/data/adb/tailscale/run/tailscaled.sock ip -4 2>&1

echo '--- LINKS ---'
for nic in eth0 eth1 wlan0; do
  printf '%s carrier=' "$nic"
  cat "/sys/class/net/$nic/carrier" 2>/dev/null || printf '?\n'
  printf '%s speed=' "$nic"
  cat "/sys/class/net/$nic/speed" 2>/dev/null || printf '?\n'
done

echo '--- USB ---'
for usbdev in /sys/bus/usb/devices/*; do
  [ -f "$usbdev/product" ] || continue
  printf '%s product=' "$(basename "$usbdev")"
  tr '\n' ' ' < "$usbdev/product"
  printf ' speed='
  cat "$usbdev/speed" 2>/dev/null || printf '?\n'
done

echo '--- LAN USB NIC ---'
for attr in idVendor idProduct manufacturer product bcdDevice; do
  printf '%s=' "$attr"
  cat "/sys/bus/usb/devices/2-1.1/$attr" 2>/dev/null || printf '?\n'
done
readlink /sys/class/net/eth1/device/driver 2>/dev/null || true
/data/data/com.termux/files/usr/bin/ethtool -i eth1 2>/dev/null || true
/data/data/com.termux/files/usr/bin/ethtool -k eth1 2>/dev/null || true
/data/data/com.termux/files/usr/bin/ethtool -S eth1 2>/dev/null || true
/data/data/com.termux/files/usr/bin/tc -s qdisc show dev eth1 2>/dev/null || true
cat /proc/modules | grep -E 'r8152|cdc|usbnet' || true

echo '--- DISK ---'
mount | grep -E '/mnt/media_rw/|/mnt/usb_disk' || true
df -h /mnt/usb_disk 2>/dev/null || true

echo '--- OPENLIST ---'
pidof openlist || true
ps -A | grep openlist || true
find /data/data/com.termux/files/home -maxdepth 6 -type f -mmin -120 2>/dev/null |
  grep -E 'openlist|/log/' | head -40

echo '--- LOAD ---'
cat /proc/loadavg
cat /proc/meminfo | head -5
