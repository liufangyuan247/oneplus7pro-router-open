#!/system/bin/sh
set -eu

PREFIX=/data/data/com.termux/files/usr
FIO="$PREFIX/bin/fio"
TARGET_DIR="${1:-${ROUTER_STORAGE_PATH:-/mnt/usb_disk}}"
TEST_FILE="$TARGET_DIR/.op7-router-fio-benchmark.tmp"

cleanup() {
  rm -f "$TEST_FILE"
}
trap cleanup EXIT INT TERM

[ -x "$FIO" ] || {
  echo "fio not found: $FIO" >&2
  exit 1
}
[ -d "$TARGET_DIR" ] || {
  echo "SSD mount not found: $TARGET_DIR" >&2
  exit 1
}
[ ! -e "$TEST_FILE" ] || {
  echo "Refusing to overwrite existing test path: $TEST_FILE" >&2
  exit 1
}

echo '=== SSD sequential write (4 GiB, direct I/O, fsync at end) ==='
"$FIO" \
  --name=ssd-write \
  --filename="$TEST_FILE" \
  --size=4G \
  --rw=write \
  --bs=1M \
  --ioengine=sync \
  --direct=1 \
  --end_fsync=1 \
  --group_reporting

echo '=== SSD sequential read (4 GiB, direct I/O) ==='
"$FIO" \
  --name=ssd-read \
  --filename="$TEST_FILE" \
  --size=4G \
  --rw=read \
  --bs=1M \
  --ioengine=sync \
  --direct=1 \
  --group_reporting
