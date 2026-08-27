#!/system/bin/sh
set -eu

for volume in /storage/* /mnt/usb_disk; do
  [ -d "$volume" ] || continue
  touch "$volume/.nomedia"
  [ -f "$volume/.nomedia" ] && echo "NOMEDIA_OK $volume"
done

am force-stop com.google.android.providers.media.module >/dev/null 2>&1 || true
echo EXTERNAL_MEDIA_SCAN_DISABLED
