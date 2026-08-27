#!/system/bin/sh

BASE=/data/adb/router-network
MODULE="$BASE/modules/4.14.190-perf+/sch_fq_codel.ko"
LOG="$BASE/fq-codel.log"
EXPECTED_RELEASE='4.14.190-perf+'
EXPECTED_VERSION='#2 SMP PREEMPT Tue Aug 25 18:11:57 UTC 2026'
EXPECTED_SHA256='bda616e31b08920bf5c3c926e7ba35df3a92d59134389e8f937d80697f4d03b1'

log() {
  printf '%s %s\n' "$(date '+%F %T')" "$*" >> "$LOG"
}

(
  count=0
  while [ "$(getprop sys.boot_completed)" != 1 ] && [ "$count" -lt 180 ]; do
    sleep 2
    count=$((count + 1))
  done

  release="$(uname -r)"
  version="$(uname -v)"
  if [ "$release" != "$EXPECTED_RELEASE" ] || [ "$version" != "$EXPECTED_VERSION" ]; then
    log "skip: running kernel is $release $version"
    exit 0
  fi

  [ -s "$MODULE" ] || { log "skip: module missing"; exit 0; }
  actual_sha256="$(sha256sum "$MODULE" | awk '{print $1}')"
  [ "$actual_sha256" = "$EXPECTED_SHA256" ] || {
    log "skip: module checksum mismatch"
    exit 0
  }

  [ -d /sys/module/sch_fq_codel ] && { log "ready: sch_fq_codel already loaded"; exit 0; }
  if /system/bin/insmod "$MODULE" >> "$LOG" 2>&1; then
    log "ready: sch_fq_codel loaded"
  else
    log "failed: insmod sch_fq_codel"
  fi
) &

exit 0
