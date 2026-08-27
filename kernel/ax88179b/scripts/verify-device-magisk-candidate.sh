#!/system/bin/sh
set -eu

export PATH=/data/data/com.termux/files/usr/bin:/system/bin:/system/xbin
MAGISKBOOT=/data/adb/magisk/magiskboot
BASE=/data/local/tmp/oos11-v3-candidate-base.img
PATCHED=/data/local/tmp/oos11-v3-candidate-magisk30.7.img
CURRENT=/data/local/tmp/oos11-current-boot-a-for-v3-verify.img
ROOT=/data/local/tmp/op7p-v3-verify

for image in "$BASE" "$PATCHED"; do
  [ -s "$image" ] || { echo "missing image: $image" >&2; exit 1; }
done

case "$ROOT" in
  /data/local/tmp/op7p-v3-verify) rm -rf -- "$ROOT" ;;
  *) echo "unsafe verify path: $ROOT" >&2; exit 1 ;;
esac
rm -f -- "$CURRENT"
mkdir -p "$ROOT/base" "$ROOT/patched" "$ROOT/current"
dd if=/dev/block/bootdevice/by-name/boot_a of="$CURRENT" bs=4M status=none

for item in base patched current; do
  case "$item" in
    base) image=$BASE ;;
    patched) image=$PATCHED ;;
    current) image=$CURRENT ;;
  esac
  (
    cd "$ROOT/$item"
    "$MAGISKBOOT" unpack "$image" >/dev/null
    sha256sum kernel ramdisk.cpio dtb
    set +e
    "$MAGISKBOOT" cpio ramdisk.cpio test >/dev/null 2>&1
    status=$?
    set -e
    echo "CPIO_STATUS=$status"
  ) > "$ROOT/$item.summary"
done

echo '=== BASE ==='
cat "$ROOT/base.summary"
echo '=== PATCHED ==='
cat "$ROOT/patched.summary"
echo '=== CURRENT ==='
cat "$ROOT/current.summary"

echo '=== REQUIRED COMPARISONS ==='
cmp -s "$ROOT/base/dtb" "$ROOT/patched/dtb" && echo 'PATCHED_DTB_EQUALS_BASE=yes' || echo 'PATCHED_DTB_EQUALS_BASE=no'
cmp -s "$ROOT/current/dtb" "$ROOT/patched/dtb" && echo 'PATCHED_DTB_EQUALS_CURRENT=yes' || echo 'PATCHED_DTB_EQUALS_CURRENT=no'
cmp -s "$ROOT/base/kernel" "$ROOT/patched/kernel" && echo 'PATCHED_KERNEL_EQUALS_RAW=no-change' || echo 'PATCHED_KERNEL_EQUALS_RAW=changed'
changed_bytes=$(cmp -l "$ROOT/base/kernel" "$ROOT/patched/kernel" | wc -l | tr -d ' ' || true)
echo "MAGISK_KERNEL_CHANGED_BYTES=$changed_bytes"

echo '=== KERNEL INITRAMFS MARKERS ==='
for item in base patched current; do
  printf '%s ' "$item"
  grep -aob 'skip_initramfs' "$ROOT/$item/kernel" | head -n 2 || true
  printf '%s ' "$item"
  grep -aob 'want_initramfs' "$ROOT/$item/kernel" | head -n 2 || true
done

echo '=== PATCHED MAGISK CONFIG ==='
(
  cd "$ROOT/patched"
  "$MAGISKBOOT" cpio ramdisk.cpio 'extract .backup/.magisk magisk-config' >/dev/null
  cat magisk-config
)

echo '=== IMAGE HASHES ==='
sha256sum "$CURRENT" "$BASE" "$PATCHED"
