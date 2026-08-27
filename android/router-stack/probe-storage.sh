#!/system/bin/sh
set -u

echo '=== USB devices ==='
for device in /sys/bus/usb/devices/*; do
  [ -f "$device/idVendor" ] || continue
  vendor=$(cat "$device/idVendor" 2>/dev/null)
  product=$(cat "$device/idProduct" 2>/dev/null)
  maker=$(cat "$device/manufacturer" 2>/dev/null || true)
  name=$(cat "$device/product" 2>/dev/null || true)
  printf '%s:%s  %s  %s\n' "$vendor" "$product" "$maker" "$name"
done

echo '=== Block devices ==='
for block in /sys/class/block/*; do
  [ -f "$block/partition" ] && continue
  path=$(readlink -f "$block" 2>/dev/null || true)
  case "$path" in
    */virtual/*|*ufshc*) continue ;;
  esac
  dev=$(basename "$block")
  size=$(cat "$block/size" 2>/dev/null || echo 0)
  model=$(cat "$block/device/model" 2>/dev/null || true)
  removable=$(cat "$block/removable" 2>/dev/null || echo '?')
  printf '%-12s sectors=%-14s removable=%s model=%s path=%s\n' "$dev" "$size" "$removable" "$model" "$path"
done

echo '=== External-storage mounts ==='
mount | grep -E '/mnt/media_rw|/storage/' || true

echo '=== Filesystem space ==='
df -h 2>/dev/null | grep -E 'Filesystem|/mnt/media_rw|/storage/' || true
