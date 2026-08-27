#!/system/bin/sh

echo '--- TIME ---'
date
uptime

echo '--- ROUTER STATUS ---'
/data/adb/router-network/router-network status 2>&1 || true
ip -4 route show
ip -4 rule show

echo '--- INTERFACE COUNTERS ---'
cat /proc/net/dev

echo '--- ROUTER LOG ---'
tail -100 /data/adb/router-network/router-network.log 2>/dev/null
echo '--- PPPD LOG ---'
tail -120 /data/adb/router-network/pppd.log 2>/dev/null
echo '--- DNSMASQ LOG ---'
tail -80 /data/adb/router-network/dnsmasq.log 2>/dev/null

echo '--- KERNEL USB/NET EVENTS ---'
dmesg 2>/dev/null | grep -Ei \
  'usb|r8152|rtl815|ax881|eth0|eth1|ppp|disconnect|reset|under.voltage|voltage' | tail -160

echo '--- CPU ---'
top -b -n 1 2>/dev/null | head -35

echo '--- OPENLIST HEALTH/LOG ---'
ls -l /data/data/com.termux/files/home/router-data/health/openlist.fail 2>/dev/null || true
cat /data/data/com.termux/files/home/router-data/health/openlist.fail 2>/dev/null || true
tail -120 /data/data/com.termux/files/home/router-stack/services/openlist/log/main/current 2>/dev/null
