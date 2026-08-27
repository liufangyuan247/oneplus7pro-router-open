#!/system/bin/sh
set -eu

BASE=/data/adb/router-network
STAGED=/data/data/com.termux/files/usr/tmp/router-network.new
TARGET="$BASE/router-network"
CONFIG="$BASE/config"

[ -f "$STAGED" ] || { echo "missing $STAGED" >&2; exit 1; }
sh -n "$STAGED"

cp -p "$TARGET" "$BASE/router-network.before-auto-lan-20260824"
cp -p "$CONFIG" "$BASE/config.before-auto-lan-20260824"
cp "$STAGED" "$BASE/router-network.next"
chmod 700 "$BASE/router-network.next"
mv -f "$BASE/router-network.next" "$TARGET"

if grep -q '^WAN_USB_PORT=' "$CONFIG"; then
  sed -i "s/^WAN_USB_PORT=.*/WAN_USB_PORT='2-1.4'/" "$CONFIG"
else
  printf "%s\n" "WAN_USB_PORT='2-1.4'" >> "$CONFIG"
fi
if grep -q '^LAN_MODE=' "$CONFIG"; then
  sed -i "s/^LAN_MODE=.*/LAN_MODE='auto'/" "$CONFIG"
else
  printf "%s\n" "LAN_MODE='auto'" >> "$CONFIG"
fi
chmod 600 "$CONFIG"

"$TARGET" validate

# The Magisk service wrapper restarts the manager with the new executable.
# Existing PPPoE, DHCP and firewall state remains in place during this brief
# manager-only restart.
old_pid="$(cat "$BASE/manager.pid" 2>/dev/null || true)"
if [ -n "$old_pid" ] && kill -0 "$old_pid" 2>/dev/null; then
  kill "$old_pid"
fi

count=0
while [ "$count" -lt 15 ]; do
  new_pid="$(cat "$BASE/manager.pid" 2>/dev/null || true)"
  if [ -n "$new_pid" ] && [ "$new_pid" != "$old_pid" ] && kill -0 "$new_pid" 2>/dev/null; then
    break
  fi
  sleep 1
  count=$((count + 1))
done

new_pid="$(cat "$BASE/manager.pid" 2>/dev/null || true)"
if [ -z "$new_pid" ] || ! kill -0 "$new_pid" 2>/dev/null; then
  /system/bin/sh /data/adb/service.d/22-router-network.sh
  sleep 2
fi

"$TARGET" status
echo AUTO_NIC_POLICY_DEPLOYED
