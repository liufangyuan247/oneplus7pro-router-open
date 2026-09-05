#!/system/bin/sh
set -eu

BASE=/data/adb/router-network
GUARD_SRC=/data/local/tmp/01-router-rollback-guard.sh
SERVICE_SRC=/data/local/tmp/22-router-network-safe.sh
UPDATER_SRC=/data/local/tmp/safe-update-router-network.sh
GUARD=/data/adb/service.d/01-router-rollback-guard.sh
SERVICE=/data/adb/service.d/22-router-network.sh
MANAGER="$BASE/router-network"

for file in "$GUARD_SRC" "$SERVICE_SRC" "$UPDATER_SRC" "$MANAGER" "$SERVICE"; do
  [ -s "$file" ] || { echo "missing $file" >&2; exit 1; }
  /system/bin/sh -n "$file"
done

mkdir -p "$BASE/runtime"
chmod 700 "$BASE" "$BASE/runtime"

if [ ! -s "$BASE/router-network.backup" ]; then
  cp -p "$MANAGER" "$BASE/router-network.backup.new"
  chmod 700 "$BASE/router-network.backup.new"
  mv -f "$BASE/router-network.backup.new" "$BASE/router-network.backup"
fi

if [ ! -s "$BASE/22-router-network.sh.backup" ]; then
  cp -p "$SERVICE" "$BASE/22-router-network.sh.backup.new"
  chmod 755 "$BASE/22-router-network.sh.backup.new"
  mv -f "$BASE/22-router-network.sh.backup.new" "$BASE/22-router-network.sh.backup"
fi

cp -p "$GUARD_SRC" "$GUARD.new"
chmod 755 "$GUARD.new"
mv -f "$GUARD.new" "$GUARD"
cp -p "$GUARD" "$BASE/01-router-rollback-guard.sh.backup"
chmod 700 "$BASE/01-router-rollback-guard.sh.backup"

cp -p "$UPDATER_SRC" "$BASE/safe-update-router-network.new"
chmod 700 "$BASE/safe-update-router-network.new"
mv -f "$BASE/safe-update-router-network.new" "$BASE/safe-update-router-network"

current_hash="$(sha256sum "$SERVICE" | awk '{print $1}')"
candidate_hash="$(sha256sum "$SERVICE_SRC" | awk '{print $1}')"
if [ "$current_hash" != "$candidate_hash" ]; then
  # Roll back to the service that is actually running now, not to an older
  # one-time bootstrap backup left by a previous upgrade.
  cp -p "$SERVICE" "$BASE/22-router-network.sh.backup.new"
  chmod 755 "$BASE/22-router-network.sh.backup.new"
  mv -f "$BASE/22-router-network.sh.backup.new" "$BASE/22-router-network.sh.backup"
  cp -p "$SERVICE_SRC" "$SERVICE.new"
  chmod 755 "$SERVICE.new"
  mv -f "$SERVICE.new" "$SERVICE"
  {
    echo "installed=$(date '+%F %T')"
    echo "active_sha256=$candidate_hash"
    echo "backup_sha256=$current_hash"
  } > "$BASE/22-router-network.sh.pending.new"
  chmod 600 "$BASE/22-router-network.sh.pending.new"
  mv -f "$BASE/22-router-network.sh.pending.new" "$BASE/22-router-network.sh.pending"
  rm -f "$BASE/22-router-network.sh.trial"
fi

chown root:root "$GUARD" "$SERVICE" "$BASE/router-network.backup" \
  "$BASE/22-router-network.sh.backup" "$BASE/01-router-rollback-guard.sh.backup" \
  "$BASE/safe-update-router-network" 2>/dev/null || true
sync

echo "manager_baseline=$(sha256sum "$BASE/router-network.backup" | awk '{print $1}')"
echo "supervisor_active=$(sha256sum "$SERVICE" | awk '{print $1}')"
echo "supervisor_backup=$(sha256sum "$BASE/22-router-network.sh.backup" | awk '{print $1}')"
echo 'INSTALLED; live network unchanged; supervisor trial begins on next boot'
