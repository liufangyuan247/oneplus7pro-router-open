#!/system/bin/sh
set -eu

SOURCE=/data/local/tmp/oos11-v3-candidate-base.img
WORK=/data/local/tmp/op7p-v3-magisk-patch
OUTPUT=/data/local/tmp/oos11-v3-candidate-magisk30.7.img
MAGISK_DIR=/data/adb/magisk

[ -s "$SOURCE" ] || { echo "missing candidate: $SOURCE" >&2; exit 1; }
[ -x "$MAGISK_DIR/magiskboot" ] || { echo "missing Magisk tools" >&2; exit 1; }

case "$WORK" in
  /data/local/tmp/op7p-v3-magisk-patch) rm -rf -- "$WORK" ;;
  *) echo "unsafe work path: $WORK" >&2; exit 1 ;;
esac
rm -f -- "$OUTPUT"
mkdir -p "$WORK"

cp -a "$MAGISK_DIR/boot_patch.sh" "$MAGISK_DIR/util_functions.sh" \
  "$MAGISK_DIR/magiskboot" "$MAGISK_DIR/magiskinit" \
  "$MAGISK_DIR/magisk" "$MAGISK_DIR/init-ld" "$MAGISK_DIR/stub.apk" \
  "$WORK/"
[ ! -d "$MAGISK_DIR/chromeos" ] || cp -a "$MAGISK_DIR/chromeos" "$WORK/"
cp "$SOURCE" "$WORK/input.img"

cd "$WORK"
export BOOTMODE=true
export LEGACYSAR=true
export KEEPVERITY=false
export KEEPFORCEENCRYPT=false
export PATCHVBMETAFLAG=false
export RECOVERYMODE=false

sh ./boot_patch.sh "$WORK/input.img"
[ -s new-boot.img ] || { echo "Magisk did not create new-boot.img" >&2; exit 1; }
cp new-boot.img "$OUTPUT"

echo "OUTPUT=$OUTPUT"
sha256sum "$OUTPUT"
stat -c 'SIZE=%s' "$OUTPUT"
