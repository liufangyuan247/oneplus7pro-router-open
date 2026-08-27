#!/system/bin/sh

echo "== required Android packages =="
for pkg in \
  com.android.systemui \
  com.android.settings \
  com.google.android.webview \
  com.google.android.inputmethod.latin \
  com.google.android.networkstack \
  com.google.android.networkstack.tethering \
  com.oneplus.wifiapsettings \
  com.oneplus.filemanager \
  com.google.android.documentsui \
  com.google.android.providers.media.module \
  com.oneplus.providers.media \
  com.oneplus.sdcardservice \
  net.oneplus.launcher \
  com.termux \
  com.topjohnwu.magisk
do
  if pm list packages -e | grep -qx "package:$pkg"; then
    echo "OK $pkg"
  else
    echo "FAIL $pkg"
  fi
done

echo "== links and storage =="
ip -br addr show wlan0 2>/dev/null || ip addr show wlan0
echo "usb=$(getprop sys.usb.config)"
mount | grep -E ' /storage/| /mnt/media_rw/| /mnt/usb_disk'

echo "== router processes =="
ps -A -o PID,PPID,%CPU,ARGS | grep -E 'runsvdir|router-admin|openlist server|aria2c|qbittorrent-nox|caddy run|sshd -D|healthd' | grep -v grep

echo "== removed background processes (must be empty) =="
ps -A -o PID,ARGS | grep -E 'com.google.android.gms|googlequicksearchbox|oneplus.gallery|oneplus.gamespace|qti.ltebc|net.oneplus.odm|opbugreportlite' | grep -v grep || true

echo "== memory and battery =="
cat /proc/meminfo | grep -E 'MemAvailable|SwapFree'
dumpsys battery | grep -E 'AC powered:|USB powered:|status:|level:|voltage:|temperature:'
