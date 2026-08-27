#!/system/bin/sh
set -eu

PREFIX=/data/data/com.termux/files/usr
FIO="$PREFIX/bin/fio"
CADDY="$PREFIX/bin/caddy"
TARGET_DIR="${1:-${ROUTER_STORAGE_PATH:-/mnt/usb_disk}}"
TEST_NAME=op7-router-http-benchmark.bin
TEST_FILE="$TARGET_DIR/$TEST_NAME"

cleanup() {
  rm -f "$TEST_FILE"
}
trap cleanup EXIT INT TERM

[ -x "$FIO" ] || exit 1
[ -x "$CADDY" ] || exit 1
[ -d "$TARGET_DIR" ] || exit 1
[ ! -e "$TEST_FILE" ] || {
  echo "Refusing to overwrite existing test path: $TEST_FILE" >&2
  exit 1
}

echo 'Creating 1 GiB direct-I/O test file on SSD...'
"$FIO" \
  --name=http-source \
  --filename="$TEST_FILE" \
  --size=1G \
  --rw=write \
  --bs=1M \
  --ioengine=sync \
  --direct=1 \
  --end_fsync=1 \
  --group_reporting >/dev/null

echo "READY http://192.168.2.107:18080/$TEST_NAME"
"$CADDY" file-server --listen 0.0.0.0:18080 --root "$TARGET_DIR"
