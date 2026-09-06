#!/system/bin/sh
set -eu

BASE=/data/adb/router-network
CONFIG="$BASE/config"
ARMED="$BASE/armed"
ACTIVE="$BASE/active"
PIDFILE="$BASE/manager.pid"
DNSMASQ_PID="$BASE/dnsmasq.pid"
PPPD_PID="$BASE/pppd.pid"
LOG="$BASE/router-network.log"
PPPD_LOG="$BASE/pppd.log"
DNSMASQ_LOG="$BASE/dnsmasq.log"
RUNTIME="$BASE/runtime"
MANAGER_LOCK="$RUNTIME/manager.lock"
PPPD_OPTIONS="$RUNTIME/pppd.options"
PPPOE_BACKEND_STATE="$RUNTIME/pppoe-backend"
PPPOE_BACKEND_PID="$RUNTIME/pppoe-backend-pid"
PPPOE_BACKEND_STARTED_AT="$RUNTIME/pppoe-backend-started-at"
PPPOE_KERNEL_PROVEN="$RUNTIME/pppoe-kernel-proven"
PPPOE_KERNEL_FAILED="$RUNTIME/pppoe-kernel-failed"
PPPOE_SWITCH_LOCK="$RUNTIME/pppoe-switching"
PPPOE_KERNEL_DOWN_SINCE="$RUNTIME/pppoe-kernel-down-since"
DNSMASQ_UNIFIED_SCHEMA="$RUNTIME/dnsmasq-unified-mihomo-v1"
DNSMASQ_POLICY="$RUNTIME/dnsmasq-policy"
DNSMASQ_STATIC_HOSTS="$RUNTIME/dnsmasq-static-hosts"
DNSMASQ_STATIC_SCHEMA_PID="$RUNTIME/dnsmasq-static-hosts-v1.pid"
DNSMASQ_LEASE_FILES="${ROUTER_DNSMASQ_LEASE_FILES:-/data/data/com.termux/files/home/router-data/dnsmasq/dnsmasq.leases /data/misc/dhcp/dnsmasq.leases}"
DATA_PLANE_STATE="$RUNTIME/lan-data-plane"
ADMIN_INTERFACES=/data/data/com.termux/files/home/router-data/admin/active-interfaces
LOG_MAX_BYTES="${ROUTER_LOG_MAX_BYTES:-1048576}"
LOG_KEEP_BYTES="${ROUTER_LOG_KEEP_BYTES:-262144}"
LOG_MAINTENANCE_LOOPS="${ROUTER_LOG_MAINTENANCE_LOOPS:-720}"

PREFIX=/data/data/com.termux/files/usr
IP="$PREFIX/bin/ip"
IPTABLES="$PREFIX/bin/iptables"
TC="$PREFIX/bin/tc"
DNSMASQ="$PREFIX/bin/dnsmasq"
PPPOE="$PREFIX/bin/pppoe"
CURL="$PREFIX/bin/curl"
GAWK="$PREFIX/bin/awk"
SS="$PREFIX/bin/ss"
MIHOMO_DATA=/data/data/com.termux/files/home/router-data/mihomo
MIHOMO_MODE_FILE="$MIHOMO_DATA/runtime-mode"
MIHOMO_CONTROLLER=http://127.0.0.1:9091
MIHOMO_TUN=op7tun
PPPD=/system/bin/pppd
PPPOE_PLUGIN_DEFAULT="$BASE/plugins/pppd-2.4.7/rp-pppoe.so"

TABLE_WAN=100
TABLE_MIHOMO=2022
RULE_MIHOMO_LAN=5000
MIHOMO_INTERNAL_CIDR=198.18.0.0/30
RULE_MIHOMO_INTERNAL=8099
RULE_WAN=9000
LAN_ADDRESS=192.168.2.1/24
LAN_SUBNET=192.168.2.0/24
MODEM_ADDRESS=192.168.1.2/24
MODEM_SUBNET=192.168.1.0/24
NET_CLASS=/sys/class/net

log() {
  printf '%s %s\n' "$(date '+%F %T')" "$*" >> "$LOG"
}

trim_log_in_place() {
  target="$1"
  [ -f "$target" ] || return 0
  size="$(wc -c < "$target" 2>/dev/null || echo 0)"
  case "$size" in ''|*[!0-9]*) return 0 ;; esac
  [ "$size" -le "$LOG_MAX_BYTES" ] && return 0
  tail -c "$LOG_KEEP_BYTES" "$target" > "$target.trim.$$" 2>/dev/null || {
    rm -f "$target.trim.$$"
    return 0
  }
  # Keep the inode used by long-running children and their O_APPEND handles.
  cat "$target.trim.$$" > "$target"
  rm -f "$target.trim.$$"
}

trim_runtime_logs() {
  for target in "$LOG" "$PPPD_LOG" "$DNSMASQ_LOG"; do
    trim_log_in_place "$target"
  done
}

load_config() {
  [ -r "$CONFIG" ] || { log "missing config"; return 1; }
  # shellcheck disable=SC1090
  . "$CONFIG"
  LAN_MODE="${LAN_MODE:-auto}"
  PPP_TXQUEUELEN="${PPP_TXQUEUELEN:-1024}"
  PPP_QDISC="${PPP_QDISC:-auto}"
  # Keep the missing-key fallback conservative for upgrades from old configs.
  # Current installations explicitly set auto, which prefers kernel PPPoE and
  # retains the proven userspace implementation as an automatic fallback.
  PPPOE_BACKEND="${PPPOE_BACKEND:-userspace}"
  PPPOE_PLUGIN="${PPPOE_PLUGIN:-$PPPOE_PLUGIN_DEFAULT}"
  PPPOE_KERNEL_TRIAL_SECONDS="${PPPOE_KERNEL_TRIAL_SECONDS:-90}"
  PPPOE_KERNEL_DOWN_FALLBACK_SECONDS="${PPPOE_KERNEL_DOWN_FALLBACK_SECONDS:-120}"
  case "$PPP_TXQUEUELEN" in
    default) ;;
    ''|*[!0-9]*) log "invalid PPP_TXQUEUELEN=$PPP_TXQUEUELEN"; return 1 ;;
    *) [ "$PPP_TXQUEUELEN" -ge 3 ] && [ "$PPP_TXQUEUELEN" -le 65535 ] || {
      log "PPP_TXQUEUELEN out of range: $PPP_TXQUEUELEN"
      return 1
    } ;;
  esac
  case "$PPP_QDISC" in
    auto|fq_codel|keep) ;;
    *) log "invalid PPP_QDISC=$PPP_QDISC"; return 1 ;;
  esac
  case "$PPPOE_BACKEND" in
    auto|kernel|userspace) ;;
    *) log "invalid PPPOE_BACKEND=$PPPOE_BACKEND"; return 1 ;;
  esac
  case "$PPPOE_KERNEL_TRIAL_SECONDS" in
    ''|*[!0-9]*) log "invalid PPPOE_KERNEL_TRIAL_SECONDS=$PPPOE_KERNEL_TRIAL_SECONDS"; return 1 ;;
    *) [ "$PPPOE_KERNEL_TRIAL_SECONDS" -ge 15 ] && [ "$PPPOE_KERNEL_TRIAL_SECONDS" -le 300 ] || {
      log "PPPOE_KERNEL_TRIAL_SECONDS out of range: $PPPOE_KERNEL_TRIAL_SECONDS"
      return 1
    } ;;
  esac
  case "$PPPOE_KERNEL_DOWN_FALLBACK_SECONDS" in
    ''|*[!0-9]*) log "invalid PPPOE_KERNEL_DOWN_FALLBACK_SECONDS=$PPPOE_KERNEL_DOWN_FALLBACK_SECONDS"; return 1 ;;
    *) [ "$PPPOE_KERNEL_DOWN_FALLBACK_SECONDS" -ge 30 ] && [ "$PPPOE_KERNEL_DOWN_FALLBACK_SECONDS" -le 600 ] || {
      log "PPPOE_KERNEL_DOWN_FALLBACK_SECONDS out of range: $PPPOE_KERNEL_DOWN_FALLBACK_SECONDS"
      return 1
    } ;;
  esac
  { [ -n "${WAN_MAC:-}" ] || [ -n "${WAN_USB_PORT:-}" ] || [ -n "${WAN_VIDPID:-}" ]; } && \
    [ -n "${PPPOE_USER:-}" ] && [ -n "${PPPOE_PASS:-}" ]
}

iface_vidpid() {
  sed -n 's/^PRODUCT=\([^/]*\/[^/]*\)\/.*/\1/p' "$1/device/uevent" 2>/dev/null | tr 'A-F' 'a-f'
}

iface_usb_port() {
  node="$(readlink -f "$1/device" 2>/dev/null)"
  while [ -n "$node" ] && [ "$node" != / ]; do
    if [ -f "$node/idVendor" ] && [ -f "$node/idProduct" ]; then
      basename "$node"
      return 0
    fi
    node="$(dirname "$node")"
  done
  return 1
}

resolve_wan_iface() {
  # The CM418 built-in RTL8153 is the permanent WAN. Its hardware MAC is the
  # strongest identity and survives Android ethX renumbering and LAN swaps.
  if [ -n "${WAN_MAC:-}" ]; then
    for path in "$NET_CLASS"/eth*; do
      [ -e "$path" ] || continue
      mac="$(cat "$path/address" 2>/dev/null | tr 'A-F' 'a-f')"
      [ "$mac" = "$(printf '%s' "$WAN_MAC" | tr 'A-F' 'a-f')" ] && {
        basename "$path"
        return 0
      }
    done
  fi

  # USB topology is the second stable identity: 2-1.4 is the Ethernet
  # controller built into this CM418 rather than a replaceable USB adapter.
  if [ -n "${WAN_USB_PORT:-}" ]; then
    for path in "$NET_CLASS"/eth*; do
      [ -e "$path" ] || continue
      [ "$(iface_usb_port "$path" 2>/dev/null)" = "$WAN_USB_PORT" ] && {
        basename "$path"
        return 0
      }
    done
  fi

  # Backward-compatible fallback. Only accept an unambiguous VID/PID match;
  # two adapters can legitimately use the same chipset.
  selected="" count=0
  for path in "$NET_CLASS"/eth*; do
    [ -e "$path" ] || continue
    if [ "$(iface_vidpid "$path")" = "${WAN_VIDPID:-missing}" ]; then
      selected="$(basename "$path")"
      count=$((count + 1))
    fi
  done
  [ "$count" -eq 1 ] && { echo "$selected"; return 0; }
  return 1
}

resolve_lan_iface() {
  wan="$1"
  selected="" count=0 carrier_selected="" carrier_count=0

  # With the built-in CM418 port reserved for WAN, the one remaining USB
  # Ethernet interface is LAN regardless of its chipset, VID/PID or ethX name.
  for path in "$NET_CLASS"/eth*; do
    [ -e "$path" ] || continue
    iface="$(basename "$path")"
    [ "$iface" = "$wan" ] && continue
    selected="$iface"
    count=$((count + 1))
    if has_carrier "$iface"; then
      carrier_selected="$iface"
      carrier_count=$((carrier_count + 1))
    fi
  done
  [ "$count" -eq 1 ] && { echo "$selected"; return 0; }

  # If extra USB NICs are attached, retain the previous LAN by MAC when it is
  # still present; otherwise accept exactly one linked non-WAN interface.
  if [ -n "${LAN_MAC:-}" ]; then
    for path in "$NET_CLASS"/eth*; do
      [ -e "$path" ] || continue
      iface="$(basename "$path")"
      [ "$iface" = "$wan" ] && continue
      mac="$(cat "$path/address" 2>/dev/null | tr 'A-F' 'a-f')"
      [ "$mac" = "$(printf '%s' "$LAN_MAC" | tr 'A-F' 'a-f')" ] && {
        echo "$iface"
        return 0
      }
    done
  fi
  [ "$carrier_count" -eq 1 ] && { echo "$carrier_selected"; return 0; }
  return 1
}

has_carrier() {
  [ -n "$1" ] && [ "$(cat "/sys/class/net/$1/carrier" 2>/dev/null)" = "1" ]
}

pid_alive() {
  [ -s "$1" ] || return 1
  pid="$(cat "$1" 2>/dev/null)"
  process_alive_pid "$pid"
}

process_alive_pid() {
  pid="$1"
  case "$pid" in ''|*[!0-9]*) return 1 ;; esac
  kill -0 "$pid" 2>/dev/null || return 1
  [ -r "/proc/$pid/stat" ] || return 1
  [ "$(awk '{print $3}' "/proc/$pid/stat" 2>/dev/null)" != Z ]
}

read_process_cmdline() {
  pid="$1"
  case "$pid" in ''|*[!0-9]*) return 1 ;; esac
  [ -r "/proc/$pid/cmdline" ] || return 1
  # A procfs cmdline descriptor on this kernel can spin forever when its
  # process exits during the read. Never let identity checks pin a CPU or
  # wedge the network manager.
  /system/bin/timeout 2 tr '\000' ' ' < "/proc/$pid/cmdline" 2>/dev/null
}

write_pidfile() {
  value="$1"
  target="$2"
  printf '%s\n' "$value" > "$target.new.$$"
  chmod 600 "$target.new.$$" 2>/dev/null || true
  mv -f "$target.new.$$" "$target"
}

manager_process_matches() {
  pid="$1"
  process_alive_pid "$pid" || return 1
  cmdline="$(read_process_cmdline "$pid" || true)"
  case "$cmdline" in *"$0 daemon"*) return 0 ;; *) return 1 ;; esac
}

release_manager_lock() {
  if [ "$(cat "$PIDFILE" 2>/dev/null || true)" = "$$" ]; then
    rm -f "$PIDFILE"
  fi
  if [ "$(cat "$MANAGER_LOCK/pid" 2>/dev/null || true)" = "$$" ]; then
    rm -f "$MANAGER_LOCK/pid"
    rmdir "$MANAGER_LOCK" 2>/dev/null || true
  fi
}

acquire_manager_lock() {
  mkdir -p "$RUNTIME"
  existing="$(cat "$PIDFILE" 2>/dev/null || true)"
  if manager_process_matches "$existing"; then
    return 1
  fi
  attempts=0
  while [ "$attempts" -lt 3 ]; do
    if mkdir "$MANAGER_LOCK" 2>/dev/null; then
      printf '%s\n' "$$" > "$MANAGER_LOCK/pid"
      write_pidfile "$$" "$PIDFILE"
      return 0
    fi
    owner="$(cat "$MANAGER_LOCK/pid" 2>/dev/null || true)"
    if manager_process_matches "$owner"; then
      return 1
    fi
    rm -f "$MANAGER_LOCK/pid"
    rmdir "$MANAGER_LOCK" 2>/dev/null || true
    attempts=$((attempts + 1))
  done
  return 1
}

managed_dnsmasq_pid_matches() {
  pid="$1"
  process_alive_pid "$pid" || return 1
  cmdline="$(read_process_cmdline "$pid" || true)"
  case "$cmdline" in
    "$DNSMASQ --keep-in-foreground --conf-file=$RUNTIME/dnsmasq.conf"*) return 0 ;;
    *) return 1 ;;
  esac
}

find_managed_dnsmasq_pid() {
  best=""
  best_started=""
  for candidate in $(pidof dnsmasq 2>/dev/null || true); do
    managed_dnsmasq_pid_matches "$candidate" || continue
    started="$(awk '{print $22}' "/proc/$candidate/stat" 2>/dev/null || echo 0)"
    case "$started" in ''|*[!0-9]*) continue ;; esac
    if [ -z "$best" ] || [ "$started" -lt "$best_started" ]; then
      best="$candidate"
      best_started="$started"
    fi
  done
  [ -n "$best" ] || return 1
  printf '%s\n' "$best"
}

managed_dnsmasq_alive() {
  [ -s "$DNSMASQ_PID" ] || return 1
  managed_dnsmasq_pid_matches "$(cat "$DNSMASQ_PID" 2>/dev/null || true)"
}

adopt_dnsmasq() {
  tracked="$(cat "$DNSMASQ_PID" 2>/dev/null || true)"
  if managed_dnsmasq_pid_matches "$tracked"; then
    return 0
  fi
  adopted="$(find_managed_dnsmasq_pid 2>/dev/null || true)"
  if [ -n "$adopted" ]; then
    write_pidfile "$adopted" "$DNSMASQ_PID"
    log "adopted existing DHCP/DNS process pid=$adopted"
    return 0
  fi
  rm -f "$DNSMASQ_PID"
  return 1
}

managed_pppd_pid_matches() {
  pid="$1"
  case "$pid" in ''|*[!0-9]*) return 1 ;; esac
  cmdline="$(read_process_cmdline "$pid" || true)"
  case "$cmdline" in
    "$PPPD file $PPPD_OPTIONS"*) return 0 ;;
    *) return 1 ;;
  esac
}

managed_pppd_pids() {
  # pidof narrows this to the one or two relevant processes.  Scanning every
  # Android process and spawning tr for each entry made a controlled redial
  # take minutes and could outlive its rollback watchdog.
  for pid in $(pidof pppd 2>/dev/null || true); do
    managed_pppd_pid_matches "$pid" && echo "$pid"
  done
  return 0
}

managed_pppoe_helper_pid_matches() {
  pid="$1"
  case "$pid" in ''|*[!0-9]*) return 1 ;; esac
  cmdline="$(read_process_cmdline "$pid" || true)"
  case "$cmdline" in "$PPPOE "*) return 0 ;; *) return 1 ;; esac
}

managed_pppoe_helper_pids() {
  for pid in $(pidof pppoe 2>/dev/null || true); do
    managed_pppoe_helper_pid_matches "$pid" && echo "$pid"
  done
  return 0
}

managed_ppp_pids() {
  { managed_pppd_pids; managed_pppoe_helper_pids; } | awk '!seen[$0]++'
}

wait_for_managed_ppp_down() {
  remaining="${1:-10}"
  while [ "$remaining" -gt 0 ]; do
    [ -z "$(managed_ppp_pids)" ] && [ -z "$(find_ppp 2>/dev/null || true)" ] && return 0
    sleep 1
    remaining=$((remaining - 1))
  done
  [ -z "$(managed_ppp_pids)" ] && [ -z "$(find_ppp 2>/dev/null || true)" ]
}

verify_kernel_pppoe_plugin() {
  wan="$1"
  [ -s "$PPPOE_PLUGIN" ] || return 1
  "$PPPD" plugin "$PPPOE_PLUGIN" "$wan" dryrun noauth noipdefault nodetach \
    >/dev/null 2>> "$PPPD_LOG"
}

select_pppoe_backend() {
  case "$PPPOE_BACKEND" in
    userspace) echo userspace ;;
    kernel) [ -s "$PPPOE_PLUGIN" ] && echo kernel || return 1 ;;
    auto)
      if [ -s "$PPPOE_PLUGIN" ] && [ ! -f "$PPPOE_KERNEL_FAILED" ]; then
        echo kernel
      else
        echo userspace
      fi
      ;;
  esac
}

write_ppp_options() {
  wan="$1"
  backend="$2"
  umask 077
  if [ "$backend" = kernel ]; then
    cat > "$PPPD_OPTIONS" <<EOF
plugin "$PPPOE_PLUGIN"
$wan
user "$PPPOE_USER"
password "$PPPOE_PASS"
noauth
noipdefault
nodefaultroute
hide-password
persist
maxfail 0
holdoff 5
lcp-echo-interval 10
lcp-echo-failure 3
mtu 1492
mru 1492
nodetach
EOF
  else
    cat > "$PPPD_OPTIONS" <<EOF
pty "$PPPOE -I $wan -T 20 -m 1412 -U"
user "$PPPOE_USER"
password "$PPPOE_PASS"
noauth
noipdefault
nodefaultroute
hide-password
persist
maxfail 0
holdoff 5
lcp-echo-interval 10
lcp-echo-failure 3
mtu 1492
mru 1492
nodetach
EOF
  fi
}

start_ppp() {
  wan="$1"
  [ ! -f "$PPPOE_SWITCH_LOCK" ] || return 0
  if pid_alive "$PPPD_PID"; then
    pid="$(cat "$PPPD_PID" 2>/dev/null || true)"
    backend_pid="$(cat "$PPPOE_BACKEND_PID" 2>/dev/null || true)"
    if managed_pppd_pid_matches "$pid" && \
       { [ -z "$backend_pid" ] || [ "$backend_pid" = "$pid" ]; }; then
      return 0
    fi
    log "inconsistent PPPoE process state detected; rebuilding the managed session"
    stop_ppp || return 1
  else
    running_pids="$(managed_pppd_pids)"
    if [ -n "$running_pids" ] || [ -n "$(managed_pppoe_helper_pids)" ] || \
       [ -n "$(find_ppp 2>/dev/null || true)" ]; then
      log "untracked PPPoE process or interface detected; quiescing before start"
      stop_ppp || return 1
    fi
  fi
  backend="$(select_pppoe_backend 2>/dev/null || true)"
  if [ -z "$backend" ]; then
    log "kernel PPPoE requested but plugin is unavailable: $PPPOE_PLUGIN"
    return 1
  fi
  if [ "$backend" = kernel ] && ! verify_kernel_pppoe_plugin "$wan"; then
    if [ "$PPPOE_BACKEND" = auto ]; then
      touch "$PPPOE_KERNEL_FAILED"
      backend=userspace
      log "kernel PPPoE plugin preflight failed; falling back to userspace"
    else
      log "kernel PPPoE plugin preflight failed"
      return 1
    fi
  fi
  previous_backend="$(cat "$PPPOE_BACKEND_STATE" 2>/dev/null || true)"
  if [ "$previous_backend" != "$backend" ]; then
    printf '%s\n' "$backend" > "$PPPOE_BACKEND_STATE"
    date +%s > "$PPPOE_BACKEND_STARTED_AT"
    rm -f "$PPPOE_KERNEL_PROVEN"
  fi
  write_ppp_options "$wan" "$backend"
  log "starting PPPoE on $wan backend=$backend"
  "$PPPD" file "$PPPD_OPTIONS" >> "$PPPD_LOG" 2>&1 &
  pid=$!
  echo "$pid" > "$PPPD_PID"
  echo "$pid" > "$PPPOE_BACKEND_PID"
}

stop_ppp() {
  pids="$(managed_ppp_pids)"
  for pid in $pids; do kill -TERM "$pid" 2>/dev/null || true; done
  if ! wait_for_managed_ppp_down 10; then
    pids="$(managed_ppp_pids)"
    for pid in $pids; do kill -KILL "$pid" 2>/dev/null || true; done
    wait_for_managed_ppp_down 5 || {
      log "managed PPPoE processes or interfaces did not quiesce"
      return 1
    }
  fi
  rm -f "$PPPD_PID" "$PPPOE_BACKEND_STATE" "$PPPOE_BACKEND_PID" \
    "$PPPOE_BACKEND_STARTED_AT" "$PPPOE_KERNEL_PROVEN" "$PPPOE_KERNEL_DOWN_SINCE"
}

kernel_pppoe_session_ready() {
  awk 'NR > 1 { found=1 } END { exit(found ? 0 : 1) }' /proc/net/pppoe 2>/dev/null || return 1
}

kernel_pppoe_proxy_probe() {
  # The whole-home data plane may intentionally be in direct mode while the
  # loopback selective proxy remains active for Codex and CPA. Probe that
  # listener directly instead of requiring Mihomo's rule/global LAN mode.
  # Try the lightweight trace endpoint first and retain the API endpoint as a
  # fallback so a transient edge-specific failure does not reject PPPoE.
  for url in https://chatgpt.com/cdn-cgi/trace https://api.openai.com/v1/models; do
    code="$($CURL -sS --connect-timeout 5 --max-time 12 \
      --proxy http://127.0.0.1:17891 \
      --output /dev/null --write-out '%{http_code}' \
      "$url" 2>/dev/null || true)"
    case "$code" in
      2??|3??|4??) printf '%s\n' "$code"; return 0 ;;
    esac
  done
  return 1
}

kernel_pppoe_transport_ready() {
  ppp="$1"
  kernel_pppoe_session_ready || return 1
  "$IP" -4 route show table "$TABLE_WAN" 2>/dev/null | grep -q "^default dev $ppp" || return 1

  # An address and a kernel PPPoE socket do not prove that the routed data
  # plane works.  The previous implementation marked the trial successful at
  # that point and could strand Mihomo/CPA behind a dead session.  Confirm the
  # same selective proxy path used by Codex before making the kernel backend
  # sticky. Any normal HTTP response proves proxy DNS, TLS and WAN egress.
  kernel_pppoe_proxy_probe
}

record_pppoe_backend_success() {
  ppp="$1"
  [ "$(cat "$PPPOE_BACKEND_STATE" 2>/dev/null || true)" = kernel ] || return 0
  [ ! -f "$PPPOE_KERNEL_PROVEN" ] || {
    rm -f "$PPPOE_KERNEL_DOWN_SINCE"
    return 0
  }
  started_pid="$(cat "$PPPOE_BACKEND_PID" 2>/dev/null || true)"
  tracked_pid="$(cat "$PPPD_PID" 2>/dev/null || true)"
  [ -n "$started_pid" ] && [ "$started_pid" = "$tracked_pid" ] || return 1
  managed_pppd_pid_matches "$started_pid" || return 1
  "$IP" -4 addr show dev "$ppp" 2>/dev/null | grep -q 'inet ' || return 1
  code="$(kernel_pppoe_transport_ready "$ppp")" || return 1
  [ -f "$PPPOE_KERNEL_PROVEN" ] || {
    touch "$PPPOE_KERNEL_PROVEN"
    rm -f "$PPPOE_KERNEL_DOWN_SINCE"
    log "kernel PPPoE end-to-end data path established: openai_http=$code"
  }
}

pppoe_switch_holdoff() {
  sleep 10
}

fallback_unproven_kernel_pppoe() {
  wan="$1"
  [ "$PPPOE_BACKEND" = auto ] || return 1
  [ "$(cat "$PPPOE_BACKEND_STATE" 2>/dev/null || true)" = kernel ] || return 1
  now="$(date +%s)"
  if [ -f "$PPPOE_KERNEL_PROVEN" ]; then
    if [ ! -s "$PPPOE_KERNEL_DOWN_SINCE" ]; then
      printf '%s\n' "$now" > "$PPPOE_KERNEL_DOWN_SINCE"
      return 1
    fi
    started="$(cat "$PPPOE_KERNEL_DOWN_SINCE" 2>/dev/null || echo 0)"
    threshold="$PPPOE_KERNEL_DOWN_FALLBACK_SECONDS"
  else
    started="$(cat "$PPPOE_BACKEND_STARTED_AT" 2>/dev/null || echo 0)"
    threshold="$PPPOE_KERNEL_TRIAL_SECONDS"
  fi
  [ "$started" -gt 0 ] 2>/dev/null || return 1
  [ $((now - started)) -ge "$threshold" ] || return 1
  touch "$PPPOE_KERNEL_FAILED"
  stop_ppp
  # Give the access concentrator time to discard the old session before the
  # userspace client starts discovery.  Immediate reconnects caused repeated
  # PADS/LCP failures in the field.
  pppoe_switch_holdoff
  log "kernel PPPoE health failed after ${threshold}s; falling back to userspace"
  start_ppp "$wan"
}

find_ppp() {
  for path in /sys/class/net/ppp*; do
    [ -e "$path" ] || continue
    iface="$(basename "$path")"
    if "$IP" -4 addr show dev "$iface" 2>/dev/null | grep -q 'inet '; then
      echo "$iface"
      return 0
    fi
  done
  return 1
}

apply_ppp_queue() {
  ppp="$1"

  if [ "$PPP_TXQUEUELEN" != default ]; then
    current_qlen="$(cat "$NET_CLASS/$ppp/tx_queue_len" 2>/dev/null || true)"
    if [ "$current_qlen" != "$PPP_TXQUEUELEN" ]; then
      if "$IP" link set dev "$ppp" txqueuelen "$PPP_TXQUEUELEN"; then
        log "set $ppp txqueuelen=$PPP_TXQUEUELEN"
      else
        log "failed to set $ppp txqueuelen=$PPP_TXQUEUELEN"
      fi
    fi
  fi

  [ "$PPP_QDISC" != keep ] || return 0
  failure_marker="$RUNTIME/fq-codel-unavailable.$ppp"
  if [ ! -d /sys/module/sch_fq_codel ]; then
    if [ "$PPP_QDISC" = fq_codel ] && [ ! -f "$failure_marker" ]; then
      log "sch_fq_codel is not loaded; retaining the current qdisc on $ppp"
      touch "$failure_marker"
    fi
    return 0
  fi

  if "$TC" qdisc show dev "$ppp" 2>/dev/null | grep -q '^qdisc fq_codel '; then
    rm -f "$failure_marker"
    return 0
  fi

  if "$TC" qdisc replace dev "$ppp" root fq_codel 2>> "$LOG"; then
    rm -f "$failure_marker"
    log "set $ppp root qdisc=fq_codel"
  elif [ ! -f "$failure_marker" ]; then
    log "failed to set $ppp root qdisc=fq_codel; existing qdisc retained"
    touch "$failure_marker"
  fi
}

render_static_hosts() {
  source_file="$1"
  output="$2"
  "$GAWK" '
    /^[[:space:]]*dhcp-host[[:space:]]*=/ {
      sub(/^[[:space:]]*dhcp-host[[:space:]]*=[[:space:]]*/, "")
      print
    }
  ' "$source_file" > "$output"
}

write_static_hosts_runtime() {
  candidate="$DNSMASQ_STATIC_HOSTS.new.$$"
  if [ -n "${DNSMASQ_HOSTS_FILE:-}" ] && [ -r "$DNSMASQ_HOSTS_FILE" ]; then
    render_static_hosts "$DNSMASQ_HOSTS_FILE" "$candidate"
  else
    : > "$candidate"
  fi
  chmod 600 "$candidate"
  mv -f "$candidate" "$DNSMASQ_STATIC_HOSTS"
}

static_lease_list() {
  load_config
  [ -n "${DNSMASQ_HOSTS_FILE:-}" ] && [ -r "$DNSMASQ_HOSTS_FILE" ] || return 0
  "$GAWK" '
    /^[[:space:]]*dhcp-host[[:space:]]*=/ {
      sub(/^[[:space:]]*dhcp-host[[:space:]]*=[[:space:]]*/, "")
      count=split($0, item, ",")
      mac=tolower(item[1]); ip=item[2]; name=(count >= 3 ? item[3] : "")
      gsub(/[[:space:]]/, "", mac); gsub(/[[:space:]]/, "", ip)
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", name)
      if (mac ~ /^([0-9a-f]{2}:){5}[0-9a-f]{2}$/ && ip ~ /^[0-9.]+$/) {
        printf "%s\t%s\t%s\n", mac, ip, name
      }
    }
  ' "$DNSMASQ_HOSTS_FILE"
}

validate_static_lease() {
  mac="$1" ip="$2" hostname="${3:-}"
  echo "$mac" | "$GAWK" '/^([0-9a-f]{2}:){5}[0-9a-f]{2}$/ { ok=1 } END { exit !ok }' || {
    echo "invalid MAC address" >&2; return 1
  }
  case "$hostname" in
    '') ;;
    [A-Za-z0-9]* )
      echo "$hostname" | "$GAWK" 'length <= 63 && /^[A-Za-z0-9][A-Za-z0-9._-]*$/ { ok=1 } END { exit !ok }' || {
        echo "invalid hostname" >&2; return 1
      }
      ;;
    *) echo "invalid hostname" >&2; return 1 ;;
  esac
  "$GAWK" -v candidate="$ip" -v lan="$LAN_ADDRESS" '
    function ipnum(value, part, n) {
      n=split(value, part, "."); if (n != 4) return -1
      for (n=1; n<=4; n++) if (part[n] !~ /^[0-9]+$/ || part[n] > 255) return -1
      return (((part[1]*256)+part[2])*256+part[3])*256+part[4]
    }
    BEGIN {
      split(lan, cidr, "/"); router=ipnum(cidr[1]); address=ipnum(candidate); prefix=cidr[2]+0
      if (router < 0 || address < 0 || prefix < 1 || prefix > 30) exit 1
      block=2^(32-prefix); network=int(router/block)*block; broadcast=network+block-1
      if (int(address/block) != int(router/block) || address == router || address == network || address == broadcast) exit 1
    }
  ' || { echo "IPv4 address must be a usable host in the LAN subnet and not the router address" >&2; return 1; }
}

update_static_lease() {
  action="$1"
  mac="$(printf '%s' "$2" | tr 'A-F' 'a-f')"
  ip="${3:-}"
  hostname="${4:-}"
  load_config
  [ -n "${DNSMASQ_HOSTS_FILE:-}" ] || { echo "DNSMASQ_HOSTS_FILE is not configured" >&2; return 1; }
  case "$DNSMASQ_HOSTS_FILE" in /*) ;; *) echo "DNSMASQ_HOSTS_FILE must be absolute" >&2; return 1 ;; esac
  case "$action" in
    set) validate_static_lease "$mac" "$ip" "$hostname" || return 1 ;;
    clear)
      echo "$mac" | "$GAWK" '/^([0-9a-f]{2}:){5}[0-9a-f]{2}$/ { ok=1 } END { exit !ok }' || {
        echo "invalid MAC address" >&2; return 1
      }
      ;;
    *) return 2 ;;
  esac

  mkdir -p "$(dirname "$DNSMASQ_HOSTS_FILE")"
  [ -e "$DNSMASQ_HOSTS_FILE" ] || : > "$DNSMASQ_HOSTS_FILE"
  chmod 600 "$DNSMASQ_HOSTS_FILE"
  if [ "$action" = set ]; then
    for lease_file in $DNSMASQ_LEASE_FILES; do
      [ -r "$lease_file" ] || continue
      if "$GAWK" -v wanted_ip="$ip" -v wanted_mac="$mac" '
        $3 == wanted_ip && tolower($2) != wanted_mac { conflict=1 }
        END { exit !conflict }
      ' "$lease_file"; then
        echo "IPv4 address is currently leased to another client" >&2; return 1
      fi
    done
    if static_lease_list | "$GAWK" -F '\t' -v wanted_ip="$ip" -v wanted_mac="$mac" '
      $2 == wanted_ip && $1 != wanted_mac { conflict=1 } END { exit !conflict }
    '; then
      echo "IPv4 address conflicts with another static lease" >&2; return 1
    fi
  fi

  source_candidate="$DNSMASQ_HOSTS_FILE.new.$$"
  runtime_candidate="$DNSMASQ_STATIC_HOSTS.new.$$"
  validate_conf="$RUNTIME/dnsmasq-static-validate.conf.$$"
  backup="$DNSMASQ_HOSTS_FILE.rollback.$$"
  trap 'rm -f "$source_candidate" "$runtime_candidate" "$validate_conf" "$backup"' EXIT INT TERM
  cp -p "$DNSMASQ_HOSTS_FILE" "$backup"
  "$GAWK" -v action="$action" -v wanted_mac="$mac" -v wanted_ip="$ip" -v wanted_name="$hostname" '
    function emit() {
      printf "dhcp-host=%s,%s", wanted_mac, wanted_ip
      if (wanted_name != "") printf ",%s", wanted_name
      printf "\n"
    }
    /^[[:space:]]*dhcp-host[[:space:]]*=/ {
      line=$0; sub(/^[[:space:]]*dhcp-host[[:space:]]*=[[:space:]]*/, "", line)
      split(line, item, ","); entry_mac=tolower(item[1]); gsub(/[[:space:]]/, "", entry_mac)
      if (entry_mac == wanted_mac) {
        if (action == "set" && !written) { emit(); written=1 }
        next
      }
    }
    { print }
    END { if (action == "set" && !written) emit() }
  ' "$DNSMASQ_HOSTS_FILE" > "$source_candidate"
  chmod 600 "$source_candidate"
  render_static_hosts "$source_candidate" "$runtime_candidate"
  chmod 600 "$runtime_candidate"
  {
    echo 'port=0'
    echo 'no-resolv'
    echo "dhcp-hostsfile=$runtime_candidate"
  } > "$validate_conf"
  "$DNSMASQ" --test --conf-file="$validate_conf" >/dev/null 2>&1 || {
    echo "dnsmasq rejected the static lease candidate" >&2; return 1
  }

  if ! mv -f "$source_candidate" "$DNSMASQ_HOSTS_FILE" || ! mv -f "$runtime_candidate" "$DNSMASQ_STATIC_HOSTS"; then
    cp -p "$backup" "$DNSMASQ_HOSTS_FILE" 2>/dev/null || true
    write_static_hosts_runtime 2>/dev/null || true
    echo "failed to publish static leases; previous configuration restored" >&2
    return 1
  fi
  if managed_dnsmasq_alive && { ! kill -HUP "$(cat "$DNSMASQ_PID")" 2>/dev/null || ! kill -0 "$(cat "$DNSMASQ_PID")" 2>/dev/null; }; then
    cp -p "$backup" "$DNSMASQ_HOSTS_FILE"
    write_static_hosts_runtime
    kill -HUP "$(cat "$DNSMASQ_PID")" 2>/dev/null || true
    echo "dnsmasq refresh failed; previous static leases restored" >&2
    return 1
  fi
  rm -f "$backup" "$validate_conf"
  trap - EXIT INT TERM
  [ "$action" = set ] && printf 'STATIC_LEASE_SET\t%s\t%s\n' "$mac" "$ip" || printf 'STATIC_LEASE_CLEARED\t%s\n' "$mac"
}

write_dnsmasq_config() {
  lan="$1"
  policy="${2:-direct}"
  case "$policy" in mihomo|direct) ;; *) return 2 ;; esac
  lease_dir=/data/data/com.termux/files/home/router-data/dnsmasq
  mkdir -p "$lease_dir"
  touch "$lease_dir/dnsmasq.leases"
  chown 10207:10207 "$lease_dir" "$lease_dir/dnsmasq.leases" 2>/dev/null || true
  write_static_hosts_runtime
  {
    cat <<EOF
interface=$lan
interface=lo
bind-dynamic
port=53
domain-needed
bogus-priv
no-resolv
cache-size=10000
EOF
    if [ "$policy" = mihomo ]; then
      echo 'server=127.0.0.1#1053'
    else
      echo 'server=223.5.5.5'
      echo 'server=119.29.29.29'
    fi
    cat <<EOF
dhcp-authoritative
dhcp-range=192.168.2.100,192.168.2.249,255.255.255.0,12h
dhcp-option=option:router,192.168.2.1
dhcp-option=option:dns-server,192.168.2.1
dhcp-leasefile=$lease_dir/dnsmasq.leases
dhcp-hostsfile=$DNSMASQ_STATIC_HOSTS
log-facility=$DNSMASQ_LOG
user=root
EOF
    if [ -n "${DNSMASQ_HOSTS_FILE:-}" ] && [ -r "$DNSMASQ_HOSTS_FILE" ]; then
      "$GAWK" '!/^[[:space:]]*dhcp-host[[:space:]]*=/' "$DNSMASQ_HOSTS_FILE"
    fi
  } > "$RUNTIME/dnsmasq.conf"
}

start_dnsmasq() {
  lan="$1"
  policy="$2"
  case "$policy" in mihomo|direct) ;; *) return 2 ;; esac
  adopt_dnsmasq && return 0
  write_dnsmasq_config "$lan" "$policy"
  "$DNSMASQ" --test --conf-file="$RUNTIME/dnsmasq.conf" >> "$DNSMASQ_LOG" 2>&1
  log "starting DHCP/DNS on $lan policy=$policy"
  "$DNSMASQ" --keep-in-foreground --conf-file="$RUNTIME/dnsmasq.conf" >> "$DNSMASQ_LOG" 2>&1 &
  candidate=$!
  # A bind failure exits almost immediately.  Never overwrite the pidfile
  # until the candidate has survived startup and still has our exact command.
  sleep 1
  if ! managed_dnsmasq_pid_matches "$candidate"; then
    wait "$candidate" 2>/dev/null || true
    log "DHCP/DNS candidate failed before pidfile publication"
    return 1
  fi
  write_pidfile "$candidate" "$DNSMASQ_PID"
  printf '%s\n' "$candidate" > "$DNSMASQ_STATIC_SCHEMA_PID.new.$$"
  mv -f "$DNSMASQ_STATIC_SCHEMA_PID.new.$$" "$DNSMASQ_STATIC_SCHEMA_PID"
  printf '%s\n' "$policy" > "$DNSMASQ_POLICY"
  touch "$DNSMASQ_UNIFIED_SCHEMA"
}

stop_dnsmasq() {
  adopt_dnsmasq >/dev/null 2>&1 || true
  if managed_dnsmasq_alive; then
    kill "$(cat "$DNSMASQ_PID")" 2>/dev/null || true
    sleep 1
  fi
  rm -f "$DNSMASQ_PID" "$DNSMASQ_POLICY" "$DNSMASQ_STATIC_SCHEMA_PID"
}

ensure_dnsmasq() {
  lan="$1"
  policy="$2"
  case "$policy" in mihomo|direct) ;; *) return 2 ;; esac
  adopt_dnsmasq >/dev/null 2>&1 || true
  current="$(cat "$DNSMASQ_POLICY" 2>/dev/null || true)"
  if managed_dnsmasq_alive && [ "$current" = "$policy" ]; then
    return 0
  fi
  if managed_dnsmasq_alive; then
    log "switching DHCP/DNS policy: ${current:-unknown} -> $policy"
    stop_dnsmasq
  fi
  start_dnsmasq "$lan" "$policy"
}

ensure_jump() {
  table="$1" hook="$2" chain="$3"
  "$IPTABLES" -t "$table" -N "$chain" 2>/dev/null || true
  "$IPTABLES" -t "$table" -C "$hook" -j "$chain" 2>/dev/null || \
    "$IPTABLES" -t "$table" -I "$hook" 1 -j "$chain"
  "$IPTABLES" -t "$table" -F "$chain"
}

remove_jump() {
  table="$1" hook="$2" chain="$3"
  while "$IPTABLES" -t "$table" -C "$hook" -j "$chain" 2>/dev/null; do
    "$IPTABLES" -t "$table" -D "$hook" -j "$chain" 2>/dev/null || break
  done
  "$IPTABLES" -t "$table" -F "$chain" 2>/dev/null || true
  "$IPTABLES" -t "$table" -X "$chain" 2>/dev/null || true
}

apply_ethernet_dhcp_guard() {
  # Both USB Ethernet roles use router-owned static addresses. Install this
  # before interface discovery so Android's IpClient cannot acquire a stale
  # DHCP lease during boot, PPPoE negotiation, or a USB link bounce. The eth+
  # match leaves Wi-Fi DHCP available for rescue access.
  ensure_jump filter OUTPUT OP7_OUTPUT
  "$IPTABLES" -A OP7_OUTPUT -o 'eth+' -p udp --sport 68 --dport 67 -j DROP
  "$IPTABLES" -A OP7_OUTPUT -j RETURN
}

apply_android_dns_guard() {
  # Android's Ethernet service still treats one USB Ethernet interface as the
  # phone's default network. Its cached DHCP DNS may therefore point at the
  # optical modem (which refuses recursive queries) or at an old LAN gateway.
  # Redirect only those two known-bad local resolvers to the router-owned
  # dnsmasq. Public upstream queries from dnsmasq are left untouched.
  ensure_jump nat OUTPUT OP7_DNS_OUTPUT
  for resolver in 192.168.1.1 192.168.3.1; do
    "$IPTABLES" -t nat -A OP7_DNS_OUTPUT -p udp -d "$resolver" --dport 53 \
      -j DNAT --to-destination 127.0.0.1:53
    "$IPTABLES" -t nat -A OP7_DNS_OUTPUT -p tcp -d "$resolver" --dport 53 \
      -j DNAT --to-destination 127.0.0.1:53
  done
  "$IPTABLES" -t nat -A OP7_DNS_OUTPUT -j RETURN
}

sanitize_lan_ipv4() {
  lan="$1"
  android_table="$(awk -v iface="$lan" '$2 == iface { print $1; exit }' /data/misc/net/rt_tables 2>/dev/null || true)"

  # Android can finish a DHCP exchange before the router manager starts. Keep
  # only the router's static LAN address and discard the accompanying default
  # route and per-interface routing table.
  for address in $("$IP" -4 -o addr show dev "$lan" scope global 2>/dev/null | awk '{print $4}'); do
    if [ "$address" != "$LAN_ADDRESS" ]; then
      "$IP" -4 addr del "$address" dev "$lan" 2>/dev/null || true
      log "removed unexpected LAN IPv4 address $address from $lan"
    fi
  done

  while "$IP" -4 route del default dev "$lan" table main 2>/dev/null; do :; done
  if [ -n "$android_table" ]; then
    "$IP" -4 route flush table "$android_table" 2>/dev/null || true
    "$IP" -4 route replace "$LAN_SUBNET" dev "$lan" src "${LAN_ADDRESS%/*}" table "$android_table" 2>/dev/null || true
  fi
}

apply_firewall() {
  wan="$1" lan="$2" ppp="$3"
  ensure_jump filter INPUT OP7_INPUT
  ensure_jump filter FORWARD OP7_FORWARD
  apply_ethernet_dhcp_guard
  apply_android_dns_guard
  ensure_jump nat POSTROUTING OP7_POSTROUTING

  "$IPTABLES" -A OP7_INPUT -i "$lan" -j ACCEPT
  "$IPTABLES" -A OP7_INPUT -i tailscale0 -j ACCEPT 2>/dev/null || true
  "$IPTABLES" -A OP7_INPUT -i "$ppp" -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
  "$IPTABLES" -A OP7_INPUT -i "$ppp" -j DROP
  "$IPTABLES" -A OP7_INPUT -j RETURN

  "$IPTABLES" -A OP7_FORWARD -i "$lan" -o "$ppp" -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --clamp-mss-to-pmtu
  "$IPTABLES" -A OP7_FORWARD -i "$lan" -o "$ppp" -j ACCEPT
  "$IPTABLES" -A OP7_FORWARD -i "$ppp" -o "$lan" -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
  # Mihomo owns the only transparent TUN. Android's tetherctrl_FORWARD chain
  # ends in DROP, so forwarded LAN packets need an explicit path in both
  # directions. If Mihomo is stopped its auto-route disappears and the normal
  # ppp0 rules above provide physical fail-open routing immediately.
  "$IPTABLES" -A OP7_FORWARD -i "$lan" -o "$MIHOMO_TUN" -j ACCEPT
  "$IPTABLES" -A OP7_FORWARD -i "$MIHOMO_TUN" -o "$lan" -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
  "$IPTABLES" -A OP7_FORWARD -i "$lan" -o "$wan" -d 192.168.1.1/32 -j ACCEPT
  "$IPTABLES" -A OP7_FORWARD -i "$ppp" -o "$lan" -j DROP
  "$IPTABLES" -A OP7_FORWARD -i tailscale0 -o "$lan" -j ACCEPT 2>/dev/null || true
  "$IPTABLES" -A OP7_FORWARD -i "$lan" -o tailscale0 -j ACCEPT 2>/dev/null || true
  "$IPTABLES" -A OP7_FORWARD -j RETURN

  "$IPTABLES" -t nat -A OP7_POSTROUTING -s "$LAN_SUBNET" -o "$ppp" -j MASQUERADE
  "$IPTABLES" -t nat -A OP7_POSTROUTING -s "$LAN_SUBNET" -d 192.168.1.1/32 -o "$wan" -j SNAT --to-source 192.168.1.2
}

mihomo_router_listening() {
  ss -lntup 2>/dev/null | grep -q ':17890 '
}

mihomo_router_isolated() {
  # Mihomo owns the router TUN and must run as root so its outbound sockets use
  # the explicit physical-WAN bypass instead of looping back into that TUN.
  # Refuse to enable LAN policy unless that invariant is visible in /proc.
  found=0
  for pid in $(pidof mihomo 2>/dev/null || true); do
    [ -r "/proc/$pid/status" ] || continue
    uid="$(awk '/^Uid:/ { print $2; exit }' "/proc/$pid/status" 2>/dev/null)"
    cmdline="$(read_process_cmdline "$pid" || true)"
    case "$cmdline" in
      *'/router-data/mihomo/config.yaml'*) ;;
      *) continue ;;
    esac
    [ "$uid" = 0 ] || return 1
    found=1
  done
  [ "$found" = 1 ]
}

mihomo_router_available() {
  mihomo_router_listening && mihomo_router_isolated
}

mihomo_tun_ready() {
  [ -e "/sys/class/net/$MIHOMO_TUN" ] &&
    "$IP" rule show 2>/dev/null | grep -q 'lookup 2022' &&
    "$IP" route show table 2022 2>/dev/null | grep -q "dev $MIHOMO_TUN"
}

mihomo_mode() {
  body="$($CURL -fsS --max-time 2 "$MIHOMO_CONTROLLER/configs" 2>/dev/null || true)"
  mode="$(printf '%s' "$body" | sed -n 's/.*"mode":"\([^"]*\)".*/\1/p')"
  case "$mode" in rule|global|direct) echo "$mode" ;; *) echo stopped ;; esac
}

mihomo_policy_ready() {
  mode="$(mihomo_mode)"
  case "$mode" in rule|global) ;; *) return 1 ;; esac
  mihomo_router_available && mihomo_tun_ready
}

mihomo_dns_ready() {
  "$SS" -lntu 2>/dev/null | grep -q '127\.0\.0\.1:1053 '
}

desired_data_plane_state() {
  if mihomo_policy_ready && mihomo_dns_ready; then
    echo mihomo
  else
    echo direct
  fi
}

remove_mihomo_internal_policy() {
  if "$IP" rule show priority "$RULE_MIHOMO_INTERNAL" 2>/dev/null | \
       grep -Eq "^${RULE_MIHOMO_INTERNAL}:.*to ${MIHOMO_INTERNAL_CIDR} lookup ${TABLE_MIHOMO}([[:space:]]|$)"; then
    "$IP" rule del priority "$RULE_MIHOMO_INTERNAL" to "$MIHOMO_INTERNAL_CIDR" lookup "$TABLE_MIHOMO" 2>/dev/null || true
  fi
}

ensure_mihomo_internal_policy() {
  rules="$("$IP" rule show priority "$RULE_MIHOMO_INTERNAL" 2>/dev/null || true)"
  if printf '%s\n' "$rules" | \
       grep -Eq "^${RULE_MIHOMO_INTERNAL}:.*to ${MIHOMO_INTERNAL_CIDR} lookup ${TABLE_MIHOMO}([[:space:]]|$)"; then
    return 0
  fi
  if printf '%s\n' "$rules" | grep -q "^${RULE_MIHOMO_INTERNAL}:"; then
    log "reserved Mihomo internal priority $RULE_MIHOMO_INTERNAL is occupied; leaving external rule unchanged"
    return 1
  fi
  "$IP" rule add priority "$RULE_MIHOMO_INTERNAL" to "$MIHOMO_INTERNAL_CIDR" lookup "$TABLE_MIHOMO" || return 1
  "$IP" rule show priority "$RULE_MIHOMO_INTERNAL" 2>/dev/null | \
    grep -Eq "^${RULE_MIHOMO_INTERNAL}:.*to ${MIHOMO_INTERNAL_CIDR} lookup ${TABLE_MIHOMO}([[:space:]]|$)" || return 1
  log "enabled Mihomo internal return route: $MIHOMO_INTERNAL_CIDR lookup $TABLE_MIHOMO"
}

remove_mihomo_lan_policy() {
  "$IP" rule del priority "$RULE_MIHOMO_LAN" 2>/dev/null || true
}

sync_mihomo_lan_policy() {
  lan="$1"
  policy="$2"
  if [ "$policy" = mihomo ]; then
    if ! "$IP" rule show priority "$RULE_MIHOMO_LAN" 2>/dev/null | \
         grep -Eq "^${RULE_MIHOMO_LAN}:.*from ${LAN_SUBNET} iif ${lan} lookup ${TABLE_MIHOMO}([[:space:]]|$)"; then
      remove_mihomo_lan_policy
      "$IP" rule add priority "$RULE_MIHOMO_LAN" from "$LAN_SUBNET" iif "$lan" lookup "$TABLE_MIHOMO"
      log "enabled LAN Mihomo policy: $LAN_SUBNET iif $lan lookup $TABLE_MIHOMO"
    fi
  else
    if "$IP" rule show priority "$RULE_MIHOMO_LAN" 2>/dev/null | grep -q "^${RULE_MIHOMO_LAN}:"; then
      remove_mihomo_lan_policy
      log "disabled LAN Mihomo policy; traffic falls back to table $TABLE_WAN"
    fi
  fi
}

reconcile_lan_data_plane() {
  lan="$1"
  desired="$(desired_data_plane_state)"

  # Route and DNS are one state machine. Enabling installs the TUN route before
  # clients receive proxy-aware DNS. Disabling withdraws the TUN route first,
  # then restores direct DNS, so a Mihomo failure always fails open to PPPoE.
  # Mixed TUN's root-owned internal TCP endpoint is subject to Mihomo's UID-0
  # auto-route skip rule. Its more-specific return rule must therefore exist
  # before any LAN client is sent to table 2022.
  if [ "$desired" = mihomo ] && ! ensure_mihomo_internal_policy; then
    desired=direct
    log "Mihomo internal return route unavailable; keeping LAN on direct data plane"
  fi
  sync_mihomo_lan_policy "$lan" "$desired"
  [ "$desired" = mihomo ] || remove_mihomo_internal_policy
  ensure_dnsmasq "$lan" "$desired"
  if [ "$(cat "$DATA_PLANE_STATE" 2>/dev/null || true)" != "$desired" ]; then
    printf '%s\n' "$desired" > "$DATA_PLANE_STATE"
  fi
}

write_mihomo_mode() {
  mode="$1"
  printf '%s\n' "$mode" > "$MIHOMO_MODE_FILE.new"
  chmod 600 "$MIHOMO_MODE_FILE.new"
  chown 10207:10207 "$MIHOMO_MODE_FILE.new" 2>/dev/null || true
  mv "$MIHOMO_MODE_FILE.new" "$MIHOMO_MODE_FILE"
}

patch_mihomo_mode() {
  mode="$1"
  case "$mode" in rule|global|direct) ;; *) return 2 ;; esac
  "$CURL" -fsS --max-time 5 -X PATCH -H 'Content-Type: application/json' \
    --data-binary "{\"mode\":\"$mode\"}" "$MIHOMO_CONTROLLER/configs" >/dev/null
  write_mihomo_mode "$mode"
}

legacy_proxy_process_matches() {
  pid="$1"
  process_alive_pid "$pid" || return 1
  cmdline="$(read_process_cmdline "$pid" || true)"
  case "$cmdline" in
    *"sing-box run -c $RUNTIME/sing-box.json"*|\
    *"sing-box run -c $RUNTIME/codex-sing-box.json"*|\
    *"dnsmasq --keep-in-foreground --conf-file=$RUNTIME/codex-dnsmasq.conf"*) return 0 ;;
    *) return 1 ;;
  esac
}

cleanup_legacy_proxy_artifacts() {
  changed=0

  # Before Mihomo owned the router data plane directly, two sing-box TUNs
  # used these private rule priorities and routing tables.  Clean them once
  # when the manager starts; repeating this in the five-second steady-state
  # loop only created process and netlink churn.
  for priority in 8000 8001 8010 8011 8018 8019 8020; do
    while "$IP" rule del priority "$priority" 2>/dev/null; do changed=1; done
  done
  for table in 101 102; do
    if [ -n "$("$IP" route show table "$table" 2>/dev/null || true)" ]; then
      changed=1
    fi
    "$IP" route flush table "$table" 2>/dev/null || true
  done

  legacy_pids=""
  for process_name in sing-box dnsmasq; do
    for pid in $(pidof "$process_name" 2>/dev/null || true); do
      legacy_proxy_process_matches "$pid" || continue
      kill -TERM "$pid" 2>/dev/null || true
      legacy_pids="$legacy_pids $pid"
      changed=1
    done
  done
  elapsed=0
  while [ "$elapsed" -lt 5 ]; do
    live=""
    for pid in $legacy_pids; do
      legacy_proxy_process_matches "$pid" && live="$live $pid"
    done
    [ -n "$live" ] || break
    sleep 1
    elapsed=$((elapsed + 1))
  done
  for pid in $legacy_pids; do
    legacy_proxy_process_matches "$pid" && kill -KILL "$pid" 2>/dev/null || true
  done

  for iface in op7proxy op7codex; do
    if [ -e "/sys/class/net/$iface" ]; then
      changed=1
      log "legacy interface $iface remained after process cleanup"
    fi
  done

  for path in \
    "$BASE/sing-box.pid" "$BASE/codex-sing-box.pid" "$BASE/codex-dnsmasq.pid" \
    "$RUNTIME/sing-box.json" "$RUNTIME/mihomo-endpoint-cidrs" \
    "$RUNTIME/codex-sing-box.json" "$RUNTIME/codex-dnsmasq.conf" \
    "$RUNTIME/dnsmasq-openai-dns-v2" "$RUNTIME/dnsmasq-openai-hosts-v1" \
    "$RUNTIME/openai-addresses" "$RUNTIME/openai-hosts" \
    "$RUNTIME/openai-refresh-at" "$RUNTIME/openai-targets"; do
    [ ! -e "$path" ] || changed=1
    rm -f "$path"
  done
  rm -f "$RUNTIME"/openai-addresses.new.* 2>/dev/null || true
  rm -f "$BASE/proxy.lock/owner" "$BASE/codex-proxy.lock/owner" 2>/dev/null || true
  rmdir "$BASE/proxy.lock" "$BASE/codex-proxy.lock" 2>/dev/null || true

  [ "$changed" = 0 ] || log "removed obsolete sing-box/Codex transition data plane"
}

write_proxy_flag() {
  desired="$1"
  tmp="$CONFIG.proxy.$$"
  if grep -q '^PROXY_ENABLED=' "$CONFIG"; then
    sed "s/^PROXY_ENABLED=.*/PROXY_ENABLED='$desired'/" "$CONFIG" > "$tmp"
  else
    cp "$CONFIG" "$tmp"
    printf "PROXY_ENABLED='%s'\n" "$desired" >> "$tmp"
  fi
  chmod 600 "$tmp"
  chown 0:0 "$tmp" 2>/dev/null || true
  mv "$tmp" "$CONFIG"
}

set_proxy_state() {
  desired="$1"
  case "$desired" in
    0)
      write_proxy_flag 0
      PROXY_ENABLED=0
      if mihomo_router_listening; then
        patch_mihomo_mode direct
      else
        write_mihomo_mode direct
      fi
      echo MIHOMO_MODE_DIRECT
      ;;
    1)
      load_config
      mihomo_router_available || {
        echo 'Mihomo is unavailable; rule mode was not enabled' >&2
        return 1
      }
      patch_mihomo_mode rule
      write_proxy_flag 1
      PROXY_ENABLED=1
      echo MIHOMO_MODE_RULE
      ;;
    *)
      echo 'proxy state must be 0 or 1' >&2
      return 2
      ;;
  esac
}

refresh_proxy_state() {
  load_config
  if [ "${PROXY_ENABLED:-0}" = 1 ]; then
    patch_mihomo_mode rule
    echo MIHOMO_MODE_RULE
  else
    if mihomo_router_listening; then patch_mihomo_mode direct; else write_mihomo_mode direct; fi
    echo MIHOMO_MODE_DIRECT
  fi
}

apply_routes() {
  wan="$1" lan="$2" ppp="$3"
  "$IP" addr replace "$MODEM_ADDRESS" dev "$wan"
  "$IP" route replace "$LAN_SUBNET" dev "$lan" src 192.168.2.1 table "$TABLE_WAN"
  "$IP" route replace "$MODEM_SUBNET" dev "$wan" src 192.168.1.2 table "$TABLE_WAN"
  "$IP" route replace default dev "$ppp" table "$TABLE_WAN"
  "$IP" route replace default dev "$ppp" table main metric 50
  # Avoid deleting and re-adding an already-correct policy rule every five
  # seconds. Besides needless netlink work, that churn makes tailscaled treat
  # the routing table as continuously changing and bloats its log.
  if ! "$IP" rule show priority "$RULE_WAN" 2>/dev/null | \
       grep -Eq "^${RULE_WAN}:.*lookup ${TABLE_WAN}([[:space:]]|$)"; then
    "$IP" rule del priority "$RULE_WAN" 2>/dev/null || true
    "$IP" rule add priority "$RULE_WAN" lookup "$TABLE_WAN"
  fi
}

remove_routes() {
  remove_mihomo_lan_policy
  "$IP" rule del priority "$RULE_WAN" 2>/dev/null || true
  "$IP" route flush table "$TABLE_WAN" 2>/dev/null || true
  "$IP" route del default dev ppp0 table main 2>/dev/null || true
}

publish_interfaces() {
  wan="$1" lan="$2" ppp="$3"
  desired="$wan $lan $ppp"
  if [ "$(cat "$RUNTIME/interfaces" 2>/dev/null || true)" != "$desired" ]; then
    printf '%s\n' "$desired" > "$RUNTIME/interfaces"
  fi
  mkdir -p "$(dirname "$ADMIN_INTERFACES")"
  if [ "$(cat "$ADMIN_INTERFACES" 2>/dev/null || true)" != "$desired" ]; then
    printf '%s\n' "$desired" > "$ADMIN_INTERFACES.new.$$"
    chown 10207:10207 "$ADMIN_INTERFACES.new.$$" 2>/dev/null || true
    chmod 600 "$ADMIN_INTERFACES.new.$$" 2>/dev/null || true
    mv -f "$ADMIN_INTERFACES.new.$$" "$ADMIN_INTERFACES"
  fi
}

activate_router() {
  wan="$1" lan="$2" ppp="$3"
  "$IP" link set "$lan" up
  sanitize_lan_ipv4 "$lan"
  "$IP" addr replace "$LAN_ADDRESS" dev "$lan"
  sysctl -w net.ipv4.ip_forward=1 >/dev/null 2>&1 || true
  sysctl -w net.ipv6.conf.all.forwarding=1 >/dev/null 2>&1 || true
  sysctl -w "net.ipv4.conf.$lan.rp_filter=0" >/dev/null 2>&1 || true
  sysctl -w "net.ipv4.conf.$wan.rp_filter=0" >/dev/null 2>&1 || true
  sysctl -w "net.ipv4.conf.$ppp.rp_filter=0" >/dev/null 2>&1 || true
  apply_ppp_queue "$ppp"
  apply_routes "$wan" "$lan" "$ppp"
  reconcile_lan_data_plane "$lan"
  apply_firewall "$wan" "$lan" "$ppp"
  if [ "${DISABLE_WIFI_ON_ACTIVE:-0}" = "1" ]; then
    # The phone must not associate with the downstream AP while routing it.
    # Doing so creates a layer-2 loop and lets Android's Wi-Fi route compete
    # with the static LAN route. LAN management remains at 192.168.2.1.
    svc wifi disable >/dev/null 2>&1 || true
  fi
  touch "$ACTIVE"
  publish_interfaces "$wan" "$lan" "$ppp"
  log "router active: WAN=$wan LAN=$lan PPP=$ppp"
}

cleanup_runtime() {
  stop_dnsmasq
  stop_ppp
  remove_mihomo_internal_policy
  cleanup_legacy_proxy_artifacts
  remove_jump filter INPUT OP7_INPUT
  remove_jump filter FORWARD OP7_FORWARD
  remove_jump filter OUTPUT OP7_OUTPUT
  remove_jump nat OUTPUT OP7_DNS_OUTPUT
  remove_jump nat POSTROUTING OP7_POSTROUTING
  remove_routes
  if [ -s "$RUNTIME/interfaces" ]; then
    set -- $(cat "$RUNTIME/interfaces")
    [ -n "${1:-}" ] && "$IP" addr del "$MODEM_ADDRESS" dev "$1" 2>/dev/null || true
    [ -n "${2:-}" ] && "$IP" addr del "$LAN_ADDRESS" dev "$2" 2>/dev/null || true
  fi
  rm -f "$ACTIVE" "$RUNTIME/interfaces" "$DATA_PLANE_STATE"
  rm -f "$ADMIN_INTERFACES"
}

validate() {
  load_config
  for binary in "$IP" "$IPTABLES" "$TC" "$DNSMASQ" "$PPPOE" "$PPPD" "$CURL" "$GAWK" "$SS"; do
    [ -x "$binary" ] || { echo "missing $binary" >&2; exit 1; }
  done
  wan="$(resolve_wan_iface)"
  lan="$(resolve_lan_iface "$wan")"
  [ "$wan" != "$lan" ]
  write_dnsmasq_config "$lan" direct
  "$DNSMASQ" --test --conf-file="$RUNTIME/dnsmasq.conf"
  live_pppd_options="$PPPD_OPTIONS"
  PPPD_OPTIONS="$RUNTIME/pppd.options.validate.$$"
  trap 'rm -f "$PPPD_OPTIONS"' EXIT INT TERM
  backend="$(select_pppoe_backend 2>/dev/null || true)"
  [ -n "$backend" ] || { echo "kernel PPPoE plugin unavailable: $PPPOE_PLUGIN" >&2; exit 1; }
  if [ "$backend" = kernel ]; then
    verify_kernel_pppoe_plugin "$wan" || {
      echo "kernel PPPoE plugin preflight failed: $PPPOE_PLUGIN" >&2
      exit 1
    }
  fi
  write_ppp_options "$wan" "$backend"
  rm -f "$PPPD_OPTIONS"
  PPPD_OPTIONS="$live_pppd_options"
  trap - EXIT INT TERM
  echo "VALID WAN=$wan LAN=$lan ENGINE=mihomo MODE=$(mihomo_mode)"
}

status() {
  load_config || true
  wan="$(resolve_wan_iface 2>/dev/null || true)"
  lan="$(resolve_lan_iface "$wan" 2>/dev/null || true)"
  ppp="$(find_ppp 2>/dev/null || true)"
  echo "armed=$([ -f "$ARMED" ] && echo yes || echo no)"
  echo "active=$([ -f "$ACTIVE" ] && echo yes || echo no)"
  echo "wan=${wan:-missing} carrier=$([ -n "$wan" ] && cat "/sys/class/net/$wan/carrier" 2>/dev/null || echo 0)"
  echo "lan=${lan:-missing} carrier=$([ -n "$lan" ] && cat "/sys/class/net/$lan/carrier" 2>/dev/null || echo 0)"
  echo "role_policy=cm418-wan/auto-lan wan_usb=${WAN_USB_PORT:-unknown}"
  echo "ppp=${ppp:-down}"
  echo "pppoe_backend_requested=${PPPOE_BACKEND:-auto}"
  echo "pppoe_backend_active=$(cat "$PPPOE_BACKEND_STATE" 2>/dev/null || echo down)"
  echo "pppoe_kernel_plugin=$([ -s "${PPPOE_PLUGIN:-$PPPOE_PLUGIN_DEFAULT}" ] && echo available || echo missing)"
  echo "pppoe_kernel_session=$(awk 'NR > 1 { found=1 } END { print found ? "active" : "inactive" }' /proc/net/pppoe 2>/dev/null || echo unavailable)"
  echo "tcp_congestion_control=$(cat /proc/sys/net/ipv4/tcp_congestion_control 2>/dev/null || echo unavailable)"
  echo "default_qdisc=$(cat /proc/sys/net/core/default_qdisc 2>/dev/null || echo unavailable)"
  echo "rps_sock_flow_entries=$(cat /proc/sys/net/core/rps_sock_flow_entries 2>/dev/null || echo unavailable)"
  echo "wan_rps_cpus=$([ -n "$wan" ] && cat "$NET_CLASS/$wan/queues/rx-0/rps_cpus" 2>/dev/null || echo unavailable)"
  echo "lan_rps_cpus=$([ -n "$lan" ] && cat "$NET_CLASS/$lan/queues/rx-0/rps_cpus" 2>/dev/null || echo unavailable)"
  if [ -n "$ppp" ]; then
    echo "ppp_txqueuelen=$(cat "$NET_CLASS/$ppp/tx_queue_len" 2>/dev/null || echo unknown)"
    echo "ppp_qdisc=$("$TC" qdisc show dev "$ppp" 2>/dev/null | awk 'NR == 1 { print $2 }')"
  else
    echo "ppp_txqueuelen=unavailable"
    echo "ppp_qdisc=unavailable"
  fi
  echo "dnsmasq=$([ -s "$DNSMASQ_PID" ] && cat "$DNSMASQ_PID" || echo down)"
  echo "dns_policy=$(cat "$DNSMASQ_POLICY" 2>/dev/null || echo unknown)"
  echo "lan_data_plane=$(cat "$DATA_PLANE_STATE" 2>/dev/null || echo direct)"
  mode="$(mihomo_mode)"
  echo "traffic_engine=mihomo"
  echo "mihomo_mode=$mode"
  echo "whole_home_proxy=$([ "$mode" = rule ] || [ "$mode" = global ] && echo enabled || echo disabled)"
  echo "mihomo_router=$(mihomo_router_listening && echo available || echo missing)"
  echo "mihomo_isolated=$(mihomo_router_isolated && echo yes || echo no)"
  echo "mihomo_tun=$(mihomo_tun_ready && echo up || echo down)"
  echo "mihomo_internal_route=$("$IP" rule show priority "$RULE_MIHOMO_INTERNAL" 2>/dev/null | grep -Eq "^${RULE_MIHOMO_INTERNAL}:.*to ${MIHOMO_INTERNAL_CIDR} lookup ${TABLE_MIHOMO}([[:space:]]|$)" && echo active || echo inactive)"
  echo "lan_tun_policy=$([ "$(cat "$DATA_PLANE_STATE" 2>/dev/null || echo direct)" = mihomo ] && "$IP" rule show priority "$RULE_MIHOMO_LAN" 2>/dev/null | grep -q "lookup $TABLE_MIHOMO" && echo active || echo inactive)"
  echo "proxy_tun=$(mihomo_tun_ready && echo up || echo down)"
  echo "local_app_proxy=$(mihomo_tun_ready && echo active || echo inactive)"
  echo "codex_selective_proxy=unified"
  echo "codex_tun=$(mihomo_tun_ready && echo up || echo down)"
  echo "codex_dns=integrated"
  echo "codex_targets=geosite"
  echo "codex_policy=$(mihomo_tun_ready && [ "$mode" = rule ] && echo active || echo inactive)"
}

daemon() {
  if ! acquire_manager_lock; then
    exit 0
  fi
  trap 'release_manager_lock' EXIT
  trap 'exit 0' INT TERM
  trim_runtime_logs
  cleanup_legacy_proxy_artifacts
  adopt_dnsmasq >/dev/null 2>&1 || true
  log "network manager started"
  if managed_dnsmasq_alive && {
       [ ! -s "$DNSMASQ_POLICY" ] ||
       [ "$(cat "$DNSMASQ_STATIC_SCHEMA_PID" 2>/dev/null || true)" != "$(cat "$DNSMASQ_PID")" ];
     }; then
    # A live upgrade has no policy marker or the running dnsmasq predates the
    # hot-reloadable static-hosts schema. Restart once so the process actually
    # opens dhcp-hostsfile; rewriting dnsmasq.conf alone does not reload it.
    stop_dnsmasq
    log "restarting dnsmasq for current DNS/static-hosts schema"
  fi
  maintenance_loops=0
  while :; do
    if [ "$(cat "$MANAGER_LOCK/pid" 2>/dev/null || true)" != "$$" ]; then
      log "manager lock ownership lost; exiting"
      exit 0
    fi
    maintenance_loops=$((maintenance_loops + 1))
    if [ "$maintenance_loops" -ge "$LOG_MAINTENANCE_LOOPS" ]; then
      trim_runtime_logs
      maintenance_loops=0
    fi
    if [ ! -f "$ARMED" ]; then
      sleep 5
      continue
    fi
    if ! load_config; then
      sleep 10
      continue
    fi
    apply_ethernet_dhcp_guard
    apply_android_dns_guard
    wan="$(resolve_wan_iface 2>/dev/null || true)"
    lan="$(resolve_lan_iface "$wan" 2>/dev/null || true)"
    if [ -z "$wan" ] || [ -z "$lan" ]; then
      if [ -f "$ACTIVE" ] || pid_alive "$PPPD_PID" || managed_dnsmasq_alive; then
        cleanup_runtime
        apply_ethernet_dhcp_guard
        log "router inactive: one or both USB Ethernet adapters disappeared"
      fi
      log "waiting for both USB Ethernet adapters"
      sleep 10
      continue
    fi
    "$IP" link set "$wan" up 2>/dev/null || true
    "$IP" link set "$lan" up 2>/dev/null || true
    sanitize_lan_ipv4 "$lan"

    # Keep the LAN management plane available even while PPPoE is negotiating
    # or the optical modem is down. This makes 192.168.2.1 and DHCP reachable
    # from the downstream AP during recovery.
    if has_carrier "$lan"; then
      "$IP" addr replace "$LAN_ADDRESS" dev "$lan"
    else
      stop_dnsmasq
    fi

    if has_carrier "$wan" && has_carrier "$lan"; then
      start_ppp "$wan"
    else
      stop_ppp
    fi

    ppp="$(find_ppp 2>/dev/null || true)"
    if [ -z "$ppp" ]; then
      fallback_unproven_kernel_pppoe "$wan" || true
      ppp="$(find_ppp 2>/dev/null || true)"
    fi
    if [ -n "$ppp" ] && has_carrier "$lan"; then
      current="$(cat "$RUNTIME/interfaces" 2>/dev/null || true)"
      desired="$wan $lan $ppp"
      if [ ! -f "$ACTIVE" ] || [ "$current" != "$desired" ]; then
        activate_router "$wan" "$lan" "$ppp"
      else
        "$IP" addr replace "$LAN_ADDRESS" dev "$lan"
        "$IP" addr replace "$MODEM_ADDRESS" dev "$wan"
        apply_ppp_queue "$ppp"
        apply_routes "$wan" "$lan" "$ppp"
        reconcile_lan_data_plane "$lan"
        publish_interfaces "$wan" "$lan" "$ppp"
        if [ "${DISABLE_WIFI_ON_ACTIVE:-0}" = "1" ]; then
          svc wifi disable >/dev/null 2>&1 || true
        fi
      fi
      if ! record_pppoe_backend_success "$ppp"; then
        if fallback_unproven_kernel_pppoe "$wan"; then
          # The fallback has replaced the failed kernel attempt.  Let the next
          # loop observe the new userspace interface instead of reconciling
          # stale state from the former ppp device.
          sleep 5
          continue
        fi
      fi
    elif [ -f "$ACTIVE" ]; then
      # Keep LAN management alive during a WAN redial; only withdraw the WAN
      # policy route so traffic cannot leak into an unintended Android path.
      remove_routes
      if has_carrier "$lan"; then
        ensure_dnsmasq "$lan" direct
        printf '%s\n' direct > "$DATA_PLANE_STATE"
      fi
      rm -f "$ACTIVE"
      log "router inactive: PPPoE is down; LAN management retained"
    elif has_carrier "$lan"; then
      # The LAN management plane remains available before the first successful
      # PPPoE session, but without an active WAN it must use the direct state.
      remove_mihomo_lan_policy
      ensure_dnsmasq "$lan" direct
      printf '%s\n' direct > "$DATA_PLANE_STATE"
    fi
    sleep 5
  done
}

if [ "${ROUTER_NETWORK_SOURCE_ONLY:-0}" != 1 ]; then
  mkdir -p "$BASE" "$RUNTIME"
  chmod 700 "$BASE" "$RUNTIME"

  case "${1:-status}" in
  validate) validate ;;
  status) status ;;
  proxy-on) set_proxy_state 1 ;;
  proxy-off) set_proxy_state 0 ;;
  proxy-refresh) refresh_proxy_state ;;
  codex-refresh) echo CODEX_POLICY_MANAGED_BY_MIHOMO ;;
  list-static) static_lease_list ;;
  set-static) [ "$#" -ge 3 ] || { echo "usage: $0 set-static MAC IPv4 [hostname]" >&2; exit 2; }; update_static_lease set "$2" "$3" "${4:-}" ;;
  clear-static) [ "$#" -eq 2 ] || { echo "usage: $0 clear-static MAC" >&2; exit 2; }; update_static_lease clear "$2" ;;
  daemon) daemon ;;
  arm) touch "$ARMED"; echo ARMED ;;
  disarm) rm -f "$ARMED"; cleanup_runtime; echo DISARMED ;;
  cleanup) cleanup_runtime; echo CLEANED ;;
  pppoe-retry-kernel)
    load_config
    case "$PPPOE_BACKEND" in
      auto|kernel) ;;
      *)
        echo 'kernel PPPoE retry requires PPPOE_BACKEND=auto or kernel; stable userspace PPPoE was left unchanged' >&2
        exit 1
        ;;
    esac
    : > "$PPPOE_SWITCH_LOCK"
    trap 'rm -f "$PPPOE_SWITCH_LOCK"' EXIT INT TERM
    rm -f "$PPPOE_KERNEL_FAILED" "$PPPOE_KERNEL_PROVEN" "$PPPOE_KERNEL_DOWN_SINCE"
    stop_ppp
    # Avoid racing a still-live concentrator session when changing PPPoE
    # implementations.  LAN management remains up while this lock is held.
    pppoe_switch_holdoff
    rm -f "$PPPOE_SWITCH_LOCK"
    trap - EXIT INT TERM
    echo PPPOE_KERNEL_RETRY_ARMED
    ;;
  pppoe-retry-userspace)
    load_config
    [ "$PPPOE_BACKEND" != kernel ] || {
      echo 'userspace fallback requires PPPOE_BACKEND=auto or userspace' >&2
      exit 1
    }
    : > "$PPPOE_SWITCH_LOCK"
    trap 'rm -f "$PPPOE_SWITCH_LOCK"' EXIT INT TERM
    touch "$PPPOE_KERNEL_FAILED"
    rm -f "$PPPOE_KERNEL_PROVEN" "$PPPOE_KERNEL_DOWN_SINCE"
    stop_ppp
    pppoe_switch_holdoff
    rm -f "$PPPOE_SWITCH_LOCK"
    trap - EXIT INT TERM
    echo PPPOE_USERSPACE_RETRY_ARMED
    ;;
    *) echo "usage: $0 {validate|status|proxy-on|proxy-off|proxy-refresh|codex-refresh|list-static|set-static|clear-static|daemon|arm|disarm|cleanup|pppoe-retry-kernel|pppoe-retry-userspace}" >&2; exit 2 ;;
  esac
fi
