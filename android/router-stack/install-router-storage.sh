#!/system/bin/sh
set -eu

SOURCE=/data/local/tmp/23-router-storage.sh
TARGET=/data/adb/service.d/23-router-storage.sh

[ -f "$SOURCE" ] || {
  echo "missing staged file: $SOURCE" >&2
  exit 1
}

cp "$SOURCE" "$TARGET"
chown root:root "$TARGET"
chmod 755 "$TARGET"
restorecon "$TARGET" >/dev/null 2>&1 || true

# The service script is idempotent and safe to start immediately.
/system/bin/sh "$TARGET"

echo "installed: $TARGET"
