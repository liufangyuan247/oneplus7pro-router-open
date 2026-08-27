#!/system/bin/sh
PREFIX=/data/data/com.termux/files/usr
PID=$(pidof router-admin | awk '{print $1}')
echo "pid=$PID"
echo '=== process mounts ==='
grep -E '/storage/|/mnt/media_rw/' "/proc/$PID/mounts" || true
echo '=== root storage ==='
ls -ld /storage/* /mnt/media_rw/* 2>/dev/null || true
echo '=== termux identity and df ==='
"$PREFIX/bin/setpriv" \
  --reuid 10207 --regid 10207 --groups 1004,1007,1011,1015,1028,1078,1079,3001,3002,3003,3006,3009,3011,50207 \
  "$PREFIX/bin/sh" -c 'id; df -k /mnt/usb_disk 2>&1; ls -ld /mnt/usb_disk 2>&1'
echo '=== termux through Magisk su ==='
"$PREFIX/bin/setpriv" \
  --reuid 10207 --regid 10207 --groups 1004,1007,1011,1015,1028,1078,1079,3001,3002,3003,3006,3009,3011,50207 \
  /product/bin/su -c "$PREFIX/bin/df -k /mnt/usb_disk"
