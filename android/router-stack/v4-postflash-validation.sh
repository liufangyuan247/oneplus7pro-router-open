#!/system/bin/sh

echo BOOT_A_SHA256
sha256sum /dev/block/by-name/boot_a

echo MODULES
lsmod | grep -E 'sch_fq_codel|r8152|ax_usb_nic' || true

echo SERVICES
/data/data/com.termux/files/usr/bin/sv status \
  /data/data/com.termux/files/home/router-stack/services/* 2>/dev/null || true

echo STARTUP
ps -A | grep -E 'router-network|tailscaled|mihomo|vlmcsd' || true

echo ALERTS
dmesg | grep -i -E \
  'kernel panic|NETDEV WATCHDOG|work.*weight|napi.*weight|Unable to handle kernel|Call trace' | tail -n 40
