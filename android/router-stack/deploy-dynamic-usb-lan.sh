#!/system/bin/sh
set -eu

SOURCE=/data/data/com.termux/files/home/20-oneplus-router.sh.new
DEST=/data/adb/service.d/20-oneplus-router.sh
BACKUPS=/data/adb/router-stack/backups
CONFIG=/data/adb/router-network/config

[ -f "$SOURCE" ] || { echo "missing $SOURCE" >&2; exit 1; }
/system/bin/sh -n "$SOURCE"

mkdir -p "$BACKUPS"
chmod 700 "$BACKUPS"
cp "$DEST" "$BACKUPS/20-oneplus-router.sh.pre-dynamic-usb-lan"
chmod 600 "$BACKUPS/20-oneplus-router.sh.pre-dynamic-usb-lan"
cp "$SOURCE" "$DEST"
chmod 755 "$DEST"

# Files in service.d are executed by Magisk regardless of whether their names
# look like backups. Keep historical copies non-executable to prevent duplicate
# keepers and supervisors on every boot.
for old in \
  /data/adb/service.d/20-oneplus-router.sh.before-cpa-20260824 \
  /data/adb/service.d/20-oneplus-router.sh.pre-stability-20260822
do
  [ -e "$old" ] && chmod 600 "$old"
done

# LAN identity is deliberately empty: the router manager selects the only
# non-CM418 Ethernet adapter, or the only linked one when extras are present.
sed -i "s/^LAN_VIDPID=.*/LAN_VIDPID=''/" "$CONFIG"
sed -i "s/^LAN_MAC=.*/LAN_MAC=''/" "$CONFIG"
chmod 600 "$CONFIG"

# Stop only the duplicated keeper shells. Their supervised services continue
# running; the replacement keeper reconnects to the existing supervisor.
for pid in $(ps -A -o PID,ARGS | awk \
  '$0 ~ /busybox sh \/data\/adb\/service.d\/20-oneplus-router[.]sh($|[.])/ {print $1}')
do
  kill "$pid" 2>/dev/null || true
done
sleep 1
/system/bin/sh "$DEST"
sleep 2

echo DYNAMIC_USB_LAN_INSTALLED
sed -E 's/^PPPOE_(USER|PASS)=.*/PPPOE_\1=REDACTED/' "$CONFIG"
/data/adb/router-network/router-network status
ps -A -o PID,PPID,ARGS | awk \
  '$0 ~ /service[.]d\/20-oneplus-router[.]sh$/ {print}'
