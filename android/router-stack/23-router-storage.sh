#!/system/bin/sh
# Keep a configured removable-volume path stable on Android and refresh
# OpenList when the disk is hot-plugged or removed.

CONFIG="${ROUTER_STORAGE_CONFIG:-/data/adb/router-storage.conf}"
VOLUME_UUID=""
VOLUME_LABEL="ROUTER_DISK"
COMPAT=/mnt/usb_disk
MOUNT_NTFS=/system_ext/bin/mount.ntfs
[ -r "$CONFIG" ] && . "$CONFIG"
[ -n "$VOLUME_UUID" ] || {
  echo "storage UUID is missing; copy configs/router-storage.example to $CONFIG" >&2
  exit 1
}
SOURCE="${SOURCE:-/mnt/media_rw/$VOLUME_UUID}"
PREFIX=/data/data/com.termux/files/usr
OPENLIST_SERVICE=/data/data/com.termux/files/home/router-stack/services/openlist
LOGROOT=/data/adb/router-storage
LOGFILE="$LOGROOT/storage.log"
LOCKDIR=/dev/.router-storage-monitor
NTFSFIX="$LOGROOT/bin/ntfsfix"
NTFSLIB="$LOGROOT/bin"

mkdir -p "$LOGROOT"
chmod 700 "$LOGROOT"

log() {
  printf '%s %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*" >> "$LOGFILE"
}

prepare_compat_path() {
  if [ -L "$COMPAT" ]; then
    current=$(readlink "$COMPAT" 2>/dev/null || true)
    [ "$current" = "$SOURCE" ] || ln -snf "$SOURCE" "$COMPAT"
    return
  fi

  if [ -e "$COMPAT" ]; then
    log "refusing to replace existing non-symlink path: $COMPAT"
    return 1
  fi

  ln -s "$SOURCE" "$COMPAT"
}

refresh_openlist() {
  [ -x "$PREFIX/bin/sv" ] || return 0
  [ -d "$OPENLIST_SERVICE" ] || return 0
  "$PREFIX/bin/sv" restart "$OPENLIST_SERVICE" >> "$LOGFILE" 2>&1 || \
    log "OpenList restart request failed"
}

find_volume_device() {
  for device in /dev/block/sd*[0-9]; do
    [ -b "$device" ] || continue
    if blkid "$device" 2>/dev/null | grep -qi "UUID=\"$VOLUME_UUID\""; then
      printf '%s\n' "$device"
      return 0
    fi
  done
  return 1
}

mount_volume() {
  device="$1"
  mkdir -p "$SOURCE"
  fstype=$(blkid "$device" 2>/dev/null | grep -o 'TYPE="[^"]*"' | cut -d'"' -f2)
  if [ "$fstype" = "exfat" ]; then
    /system/bin/mount -t exfat \
      -o rw,uid=1023,gid=1023,fmask=0007,dmask=0007,noatime,nodiratime,utf8 \
      "$device" "$SOURCE" >> "$LOGFILE" 2>&1
  elif [ "$fstype" = "ntfs" ]; then
    "$MOUNT_NTFS" \
      -o force,rw,uid=1023,gid=1023,fmask=0007,dmask=0007,shortname=mixed,utf8,noatime,nodiratime,big_writes,async \
      "$device" "$SOURCE" >> "$LOGFILE" 2>&1
  else
    /system/bin/mount "$device" "$SOURCE" >> "$LOGFILE" 2>&1
  fi
}

repair_volume() {
  device="$1"
  fstype=$(blkid "$device" 2>/dev/null | grep -o 'TYPE="[^"]*"' | cut -d'"' -f2)
  if [ "$fstype" = "exfat" ]; then
    [ -x "$PREFIX/bin/fsck.exfat" ] || return 1
    log "mount failed; running fsck.exfat once for $device"
    "$PREFIX/bin/fsck.exfat" -p "$device" >> "$LOGFILE" 2>&1
    return $?
  fi
  [ -x "$NTFSFIX" ] || return 1
  log "mount failed; running ntfsfix once for $device"
  LD_LIBRARY_PATH="$NTFSLIB" "$NTFSFIX" "$device" >> "$LOGFILE" 2>&1
}

# Termux targets an old Android API and therefore needs both the legacy grants
# and Android 11's all-files app-op for removable-storage access.
pm grant com.termux android.permission.READ_EXTERNAL_STORAGE >/dev/null 2>&1 || true
pm grant com.termux android.permission.WRITE_EXTERNAL_STORAGE >/dev/null 2>&1 || true
cmd appops set com.termux LEGACY_STORAGE allow >/dev/null 2>&1 || true
cmd appops set com.termux MANAGE_EXTERNAL_STORAGE allow >/dev/null 2>&1 || true

prepare_compat_path || exit 1
log "watching label=$VOLUME_LABEL uuid=$VOLUME_UUID source=$SOURCE"

if ! mkdir "$LOCKDIR" 2>/dev/null; then
  log "monitor already running"
  exit 0
fi

(
  trap 'rmdir "$LOCKDIR" >/dev/null 2>&1 || true' EXIT INT TERM
  previous=unknown
  attempted_device=""
  while :; do
    current=absent
    if grep -q " $SOURCE " /proc/mounts 2>/dev/null; then
      current=mounted
    else
      device="$(find_volume_device 2>/dev/null || true)"
      if [ -n "$device" ]; then
        current=detected
        if mount_volume "$device"; then
          current=mounted
          attempted_device=""
          log "mounted $device at $SOURCE"
        elif [ "$attempted_device" != "$device" ]; then
          attempted_device="$device"
          if repair_volume "$device" && mount_volume "$device"; then
            current=mounted
            log "repaired and mounted $device at $SOURCE"
          else
            log "automatic mount failed for $device; will retry after reconnect"
          fi
        fi
      else
        attempted_device=""
      fi
    fi

    if [ "$current" != "$previous" ]; then
      if [ "$current" = mounted ]; then
        log "disk mounted; refreshing OpenList"
      elif [ "$current" = detected ]; then
        log "disk detected but not mounted"
      else
        log "disk absent; refreshing OpenList"
      fi
      refresh_openlist
      previous=$current
    fi
    sleep 5
  done
) &

exit 0
