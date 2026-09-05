#!/system/bin/sh

# Load only modules built for the exact running kernel, then maintain the
# reversible network-performance settings as interfaces are recreated or
# hot-plugged. The historical filename is retained for deployment compatibility.

BASE=/data/adb/router-network
MODULE_DIR="$BASE/modules/4.14.190-perf+"
LOG="$BASE/fq-codel.log"
PIDFILE="$BASE/performance.pid"
EXPECTED_RELEASE='4.14.190-perf+'
EXPECTED_VERSION='#2 SMP PREEMPT Tue Aug 25 18:11:57 UTC 2026'
RPS_CPUS='f0'
RPS_FLOW_COUNT='4096'
RPS_SOCKET_FLOWS='32768'

log() {
  printf '%s %s\n' "$(date '+%F %T')" "$*" >> "$LOG"
}

process_alive() {
  pid="$1"
  case "$pid" in ''|*[!0-9]*) return 1 ;; esac
  kill -0 "$pid" 2>/dev/null || return 1
  [ -r "/proc/$pid/cmdline" ] || return 1
  tr '\000' ' ' < "/proc/$pid/cmdline" 2>/dev/null | grep -q '21-router-fq-codel.sh.*supervise'
}

load_checked_module() {
  name="$1"
  expected_sha256="$2"
  path="$MODULE_DIR/$name.ko"

  [ ! -d "/sys/module/$name" ] || return 0
  [ -s "$path" ] || { log "skip: module missing: $name"; return 1; }
  actual_sha256="$(sha256sum "$path" | awk '{print $1}')"
  [ "$actual_sha256" = "$expected_sha256" ] || {
    log "skip: checksum mismatch: $name actual=$actual_sha256"
    return 1
  }
  if /system/bin/insmod "$path" >> "$LOG" 2>&1; then
    log "loaded module: $name"
  else
    log "failed to load module: $name"
    return 1
  fi
}

write_if_changed() {
  path="$1"
  value="$2"
  [ -w "$path" ] || return 1
  current="$(cat "$path" 2>/dev/null || true)"
  [ "$current" = "$value" ] || printf '%s\n' "$value" > "$path"
}

apply_global_settings() {
  if [ -d /sys/module/tcp_bbr ] && grep -qw bbr /proc/sys/net/ipv4/tcp_available_congestion_control; then
    write_if_changed /proc/sys/net/ipv4/tcp_congestion_control bbr || log 'failed: tcp_congestion_control=bbr'
  fi
  if [ -d /sys/module/sch_fq ]; then
    write_if_changed /proc/sys/net/core/default_qdisc fq || log 'failed: default_qdisc=fq'
  fi
  write_if_changed /proc/sys/net/core/rps_sock_flow_entries "$RPS_SOCKET_FLOWS" || log 'failed: rps_sock_flow_entries'
}

apply_interface_settings() {
  for net_path in /sys/class/net/eth* /sys/class/net/ppp*; do
    [ -e "$net_path" ] || continue
    for queue in "$net_path"/queues/rx-*; do
      [ -d "$queue" ] || continue
      write_if_changed "$queue/rps_cpus" "$RPS_CPUS" || true
      write_if_changed "$queue/rps_flow_cnt" "$RPS_FLOW_COUNT" || true
    done
    for queue in "$net_path"/queues/tx-*; do
      [ -d "$queue" ] || continue
      write_if_changed "$queue/xps_cpus" "$RPS_CPUS" || true
    done
  done
}

supervise() {
  if [ -s "$PIDFILE" ] && process_alive "$(cat "$PIDFILE" 2>/dev/null)"; then
    exit 0
  fi
  echo $$ > "$PIDFILE"
  trap 'rm -f "$PIDFILE"' EXIT INT TERM

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

  # fq_codel remains the explicit PPP egress qdisc. fq supplies pacing for
  # router-originated BBR sockets; IFB/TBF are safe capabilities for later
  # rate-aware SQM, without installing an arbitrary bandwidth cap here.
  load_checked_module sch_fq_codel bda616e31b08920bf5c3c926e7ba35df3a92d59134389e8f937d80697f4d03b1 || true
  load_checked_module ifb 2d5eb74b27367632997cfef59864dc3558f70ebde74d8af7822c026309bbb777 || true
  load_checked_module sch_tbf b40e129087c9a4dae9ce2703157e7929391a55d2e2013498c82edf75c7664c97 || true
  load_checked_module sch_fq 2412daf61f00e2d36a738bfb35bf52ab10efbd255fc427e023cdc0057fdc6c4d || true
  load_checked_module tcp_bbr b7480ba6587a1077d9c14014735d361ba08d8018298a1b266d939910cec21eef || true

  apply_global_settings
  apply_interface_settings
  log "ready: bbr=$(cat /proc/sys/net/ipv4/tcp_congestion_control) default_qdisc=$(cat /proc/sys/net/core/default_qdisc) rps_cpus=$RPS_CPUS"

  # PPP and USB Ethernet devices are recreated during redial/hot-plug. Keep
  # only the small per-queue settings in a slow idempotent reconciliation loop.
  while true; do
    apply_global_settings
    apply_interface_settings
    sleep 30
  done
}

if [ "${1:-}" = supervise ]; then
  supervise
else
  /system/bin/sh "$0" supervise >> "$LOG" 2>&1 &
fi

exit 0
