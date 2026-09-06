#!/system/bin/sh
set -eu

STAGE="${ROUTER_STAGE:-/data/local/tmp/oneplus7pro-router}"
BASE="${ROUTER_NETWORK_BASE:-/data/adb/router-network}"
SERVICE_DIR="${MAGISK_SERVICE_DIR:-/data/adb/service.d}"
CONFIG_SRC="${ROUTER_CONFIG_SRC:-$STAGE/configs/router-network.conf}"
MANAGER_SRC="$STAGE/android/router-stack/router-network.sh"
SERVICE_SRC="$STAGE/android/router-stack/22-router-network.sh"

for file in "$CONFIG_SRC" "$MANAGER_SRC" "$SERVICE_SRC"; do
  [ -s "$file" ] || { echo "missing staged file: $file" >&2; exit 1; }
done

mkdir -p "$BASE/runtime" "$SERVICE_DIR"
chmod 700 "$BASE" "$BASE/runtime"
umask 077

timestamp="$(date '+%Y%m%d-%H%M%S')"
[ ! -s "$BASE/router-network" ] || cp -p "$BASE/router-network" "$BASE/router-network.backup-$timestamp"
[ ! -s "$BASE/config" ] || cp -p "$BASE/config" "$BASE/config.backup-$timestamp"

cp "$MANAGER_SRC" "$BASE/router-network.new"
cp "$CONFIG_SRC" "$BASE/config.new"
cp "$SERVICE_SRC" "$SERVICE_DIR/22-router-network.sh.new"
chmod 700 "$BASE/router-network.new"
chmod 600 "$BASE/config.new"
chmod 755 "$SERVICE_DIR/22-router-network.sh.new"

mv -f "$BASE/router-network.new" "$BASE/router-network"
mv -f "$BASE/config.new" "$BASE/config"
mv -f "$SERVICE_DIR/22-router-network.sh.new" "$SERVICE_DIR/22-router-network.sh"
chown root:root "$BASE/router-network" "$BASE/config" "$SERVICE_DIR/22-router-network.sh" 2>/dev/null || true
restorecon "$BASE/router-network" "$BASE/config" "$SERVICE_DIR/22-router-network.sh" >/dev/null 2>&1 || true

"$BASE/router-network" validate
echo "installed but not armed"
echo "review $BASE/config, then run: touch $BASE/armed && sh $SERVICE_DIR/22-router-network.sh"
