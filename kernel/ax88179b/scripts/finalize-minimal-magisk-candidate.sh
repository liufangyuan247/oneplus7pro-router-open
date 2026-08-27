#!/system/bin/sh
set -eu

export PATH=/data/data/com.termux/files/usr/bin:/system/bin:/system/xbin
MAGISKBOOT=/data/adb/magisk/magiskboot
[ -x "$MAGISKBOOT" ] || MAGISKBOOT=/data/local/tmp/op7p-v3-magisk-patch/magiskboot
BASE=${1:-/data/local/tmp/oos11-v3-candidate-base.img}
OUTPUT=${2:-/data/local/tmp/oos11-v3-candidate-magisk30.7-minimal-final.img}
WORK=${3:-/data/local/tmp/op7p-minimal-finalize}

[ -s "$BASE" ] || { echo "missing candidate: $BASE" >&2; exit 1; }
case "$WORK" in
  /data/local/tmp/op7p-*-minimal-finalize|/data/local/tmp/op7p-minimal-finalize)
    rm -rf -- "$WORK"
    ;;
  *) echo "unsafe work path: $WORK" >&2; exit 1 ;;
esac
case "$OUTPUT" in
  /data/local/tmp/*.img) ;;
  *) echo "unsafe output path: $OUTPUT" >&2; exit 1 ;;
esac
rm -f -- "$OUTPUT"
mkdir -p "$WORK"
cp "$BASE" "$WORK/input.img"

cd "$WORK"
"$MAGISKBOOT" unpack input.img >/dev/null
sha256sum ramdisk.cpio dtb > preserved-before.sha256

"$MAGISKBOOT" hexpatch kernel \
  736B69705F696E697472616D667300 \
  77616E745F696E697472616D667300

"$MAGISKBOOT" repack input.img >/dev/null
[ -s new-boot.img ] || { echo "repack did not create new-boot.img" >&2; exit 1; }
cp new-boot.img "$OUTPUT"

mkdir verify
cd verify
"$MAGISKBOOT" unpack "$OUTPUT" >/dev/null
cmp -s ramdisk.cpio ../ramdisk.cpio
cmp -s dtb ../dtb
set +e
"$MAGISKBOOT" cpio ramdisk.cpio test >/dev/null 2>&1
cpio_status=$?
set -e
[ "$cpio_status" -eq 1 ]
grep -aFq 'want_initramfs' kernel
if grep -aFq 'skip_initramfs' kernel; then
  echo 'unpatched skip_initramfs marker remains' >&2
  exit 1
fi

echo "OUTPUT=$OUTPUT"
echo "CPIO_STATUS=$cpio_status"
echo 'PRESERVED_COMPONENTS:'
sha256sum ramdisk.cpio dtb
echo 'FINAL_IMAGE:'
sha256sum "$OUTPUT"
stat -c 'SIZE=%s' "$OUTPUT"
