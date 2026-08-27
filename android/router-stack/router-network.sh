#!/system/bin/sh
set -eu

BASE=/data/adb/router-network
CONFIG="$BASE/config"
ARMED="$BASE/armed"
ACTIVE="$BASE/active"
PIDFILE="$BASE/manager.pid"
DNSMASQ_PID="$BASE/dnsmasq.pid"
PPPD_PID="$BASE/pppd.pid"
SINGBOX_PID="$BASE/sing-box.pid"
PROXY_LOCK="$BASE/proxy.lock"
LOG="$BASE/router-network.log"
PPPD_LOG="$BASE/pppd.log"
DNSMASQ_LOG="$BASE/dnsmasq.log"
RUNTIME="$BASE/runtime"
ADMIN_INTERFACES=/data/data/com.termux/files/home/router-data/admin/active-interfaces

PREFIX=/data/data/com.termux/files/usr
IP="$PREFIX/bin/ip"
IPTABLES="$PREFIX/bin/iptables"
TC="$PREFIX/bin/tc"
DNSMASQ="$PREFIX/bin/dnsmasq"
PPPOE="$PREFIX/bin/pppoe"
SINGBOX="$PREFIX/bin/sing-box"
DRILL="$PREFIX/bin/drill"
GAWK="$PREFIX/bin/awk"
PPPD=/system/bin/pppd

TABLE_WAN=100
TABLE_TPROXY=101
RULE_TPROXY=8000
RULE_TPROXY_UDP=8001
RULE_TPROXY_LOCAL=8010
RULE_TPROXY_LOCAL_UDP=8011
RULE_WAN=9000
ANDROID_UID_MIN=10000
ANDROID_UID_MAX=99999
LAN_ADDRESS=192.168.2.1/24
LAN_SUBNET=192.168.2.0/24
MODEM_ADDRESS=192.168.1.2/24
MODEM_SUBNET=192.168.1.0/24
NET_CLASS=/sys/class/net

log() {
  printf '%s %s\n' "$(date '+%F %T')" "$*" >> "$LOG"
}

load_config() {
  [ -r "$CONFIG" ] || { log "missing config"; return 1; }
  # shellcheck disable=SC1090
  . "$CONFIG"
  LAN_MODE="${LAN_MODE:-auto}"
  PPP_TXQUEUELEN="${PPP_TXQUEUELEN:-1024}"
  PPP_QDISC="${PPP_QDISC:-auto}"
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
  [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null
}

write_ppp_options() {
  wan="$1"
  umask 077
  cat > "$RUNTIME/pppd.options" <<EOF
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
}

start_ppp() {
  wan="$1"
  pid_alive "$PPPD_PID" && return 0
  write_ppp_options "$wan"
  log "starting PPPoE on $wan"
  "$PPPD" file "$RUNTIME/pppd.options" >> "$PPPD_LOG" 2>&1 &
  echo $! > "$PPPD_PID"
}

stop_ppp() {
  if pid_alive "$PPPD_PID"; then
    kill "$(cat "$PPPD_PID")" 2>/dev/null || true
    sleep 1
  fi
  rm -f "$PPPD_PID"
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

write_dnsmasq_config() {
  lan="$1"
  lease_dir=/data/data/com.termux/files/home/router-data/dnsmasq
  mkdir -p "$lease_dir"
  touch "$lease_dir/dnsmasq.leases"
  chown 10207:10207 "$lease_dir" "$lease_dir/dnsmasq.leases" 2>/dev/null || true
  cat > "$RUNTIME/dnsmasq.conf" <<EOF
interface=$lan
interface=lo
bind-dynamic
port=53
domain-needed
bogus-priv
no-resolv
server=223.5.5.5
server=119.29.29.29
cache-size=10000
dhcp-authoritative
dhcp-range=192.168.2.100,192.168.2.249,255.255.255.0,12h
dhcp-option=option:router,192.168.2.1
dhcp-option=option:dns-server,192.168.2.1
dhcp-leasefile=$lease_dir/dnsmasq.leases
log-facility=$DNSMASQ_LOG
user=root
EOF
  if [ -n "${DNSMASQ_HOSTS_FILE:-}" ] && [ -r "$DNSMASQ_HOSTS_FILE" ]; then
    cat "$DNSMASQ_HOSTS_FILE" >> "$RUNTIME/dnsmasq.conf"
  fi
}

start_dnsmasq() {
  lan="$1"
  pid_alive "$DNSMASQ_PID" && return 0
  write_dnsmasq_config "$lan"
  "$DNSMASQ" --test --conf-file="$RUNTIME/dnsmasq.conf" >> "$DNSMASQ_LOG" 2>&1
  log "starting DHCP/DNS on $lan"
  "$DNSMASQ" --keep-in-foreground --conf-file="$RUNTIME/dnsmasq.conf" >> "$DNSMASQ_LOG" 2>&1 &
  echo $! > "$DNSMASQ_PID"
}

stop_dnsmasq() {
  if pid_alive "$DNSMASQ_PID"; then
    kill "$(cat "$DNSMASQ_PID")" 2>/dev/null || true
    sleep 1
  fi
  rm -f "$DNSMASQ_PID"
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
  # Whole-home proxy traffic leaves the kernel through the userspace TUN
  # instead of ppp0. Android's tetherctrl_FORWARD chain ends in DROP, so the
  # TUN path must be accepted here just like the direct PPP path. Without
  # these two rules the policy route is installed successfully but every LAN
  # packet is discarded before sing-box or Mihomo can see it.
  "$IPTABLES" -A OP7_FORWARD -i "$lan" -o op7proxy -j ACCEPT
  "$IPTABLES" -A OP7_FORWARD -i op7proxy -o "$lan" -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
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
  # Android applications and the Termux service UID are transparently routed
  # through op7proxy. Mihomo itself must therefore run as root, outside the
  # Android application UID range, or its own outbound sockets would loop back
  # into the TUN forever. Refuse to enable the proxy unless that invariant is
  # visible in /proc.
  found=0
  for pid in $(pidof mihomo 2>/dev/null || true); do
    [ -r "/proc/$pid/status" ] || continue
    uid="$(awk '/^Uid:/ { print $2; exit }' "/proc/$pid/status" 2>/dev/null)"
    cmdline="$(tr '\000' ' ' < "/proc/$pid/cmdline" 2>/dev/null)"
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

write_mihomo_endpoint_cidrs() {
  config=/data/data/com.termux/files/home/router-data/mihomo/config.yaml
  output="$RUNTIME/mihomo-endpoint-cidrs"
  candidate="$output.$$"
  [ -s "$config" ] || { : > "$candidate"; mv "$candidate" "$output"; return 0; }

  # A LAN client may run its own Clash/Mihomo. Its connections to subscription
  # servers must go direct instead of being sent through the router's Mihomo a
  # second time. Subscription YAML often stores proxies inline, so extract all
  # server keys regardless of indentation.
  "$GAWK" '
  {
    line=$0
    while (match(line, /(^|[, {])server:[[:space:]]*([^, }]+)/, item)) {
      value=item[2]
      gsub(/^['"'"']|['"'"']$/, "", value)
      print value
      line=substr(line, RSTART+RLENGTH)
    }
  }
  ' "$config" | sort -u | while IFS= read -r host; do
    case "$host" in
      *:*)
        { printf '%s\n' "$host" | grep -Eq '^[0-9A-Fa-f:]+$' && printf '%s/128\n' "$host"; } || true
        ;;
      *[!0-9.]*)
        "$DRILL" "$host" A 2>/dev/null |
          "$GAWK" '$4 == "A" && $5 ~ /^[0-9.]+$/ { print $5 "/32" }'
        ;;
      *)
        { printf '%s\n' "$host" | grep -Eq '^([0-9]{1,3}\.){3}[0-9]{1,3}$' && printf '%s/32\n' "$host"; } || true
        ;;
    esac
  done | sort -u > "$candidate"

  if [ -s "$candidate" ]; then
    mv "$candidate" "$output"
  elif [ ! -s "$output" ]; then
    mv "$candidate" "$output"
  else
    # A transient DNS outage must not remove a known-good bypass list.
    rm -f "$candidate"
  fi
}

write_singbox_config() {
  write_mihomo_endpoint_cidrs
  endpoint_cidrs="$("$GAWK" 'BEGIN { first=1 } { if (!first) printf ", "; printf "\"%s\"", $0; first=0 }' "$RUNTIME/mihomo-endpoint-cidrs" 2>/dev/null)"
  endpoint_rule=""
  [ -z "$endpoint_cidrs" ] || endpoint_rule="      { \"ip_cidr\": [$endpoint_cidrs], \"action\": \"route\", \"outbound\": \"direct\" },"
  singbox_candidate="$RUNTIME/sing-box.json.new.$$"
  cat > "$singbox_candidate" <<EOF
{
  "log": { "level": "warn", "timestamp": true },
  "inbounds": [
    {
      "type": "tun",
      "tag": "lan-tun",
      "interface_name": "op7proxy",
      "address": ["172.31.255.1/30"],
      "mtu": 1500,
      "auto_route": false,
      "strict_route": false,
      "stack": "gvisor"
    }
  ],
  "outbounds": [
    {
      "type": "socks",
      "tag": "mihomo",
      "server": "127.0.0.1",
      "server_port": 17890,
      "version": "5"
    },
    {
      "type": "direct",
      "tag": "direct"
    }
  ],
  "route": {
    "rules": [
      { "network": "icmp", "action": "route", "outbound": "direct" },
$endpoint_rule
      { "ip_is_private": true, "action": "route", "outbound": "direct" },
      { "ip_cidr": ["100.64.0.0/10"], "action": "route", "outbound": "direct" }
    ],
    "final": "mihomo",
    "auto_detect_interface": false
  }
}
EOF
  "$SINGBOX" check -c "$singbox_candidate"
  chmod 600 "$singbox_candidate"
  mv "$singbox_candidate" "$RUNTIME/sing-box.json"
}

proxy_lock_acquire() {
  attempt=0
  while ! mkdir "$PROXY_LOCK" 2>/dev/null; do
    owner="$(cat "$PROXY_LOCK/owner" 2>/dev/null || true)"
    if [ -z "$owner" ] || ! kill -0 "$owner" 2>/dev/null; then
      rm -f "$PROXY_LOCK/owner" 2>/dev/null || true
      rmdir "$PROXY_LOCK" 2>/dev/null || true
      continue
    fi
    attempt=$((attempt + 1))
    [ "$attempt" -lt 10 ] || return 1
    sleep 1
  done
  echo $$ > "$PROXY_LOCK/owner"
}

proxy_lock_release() {
  [ "$(cat "$PROXY_LOCK/owner" 2>/dev/null || true)" = "$$" ] || return 0
  rm -f "$PROXY_LOCK/owner" 2>/dev/null || true
  rmdir "$PROXY_LOCK" 2>/dev/null || true
}

proxy_requested() {
  grep -Eq "^PROXY_ENABLED=['\"]?1['\"]?$" "$CONFIG" 2>/dev/null
}

stop_proxy_unlocked() {
  "$IP" rule del priority "$RULE_TPROXY" 2>/dev/null || true
  "$IP" rule del priority "$RULE_TPROXY_UDP" 2>/dev/null || true
  "$IP" rule del priority "$RULE_TPROXY_LOCAL" 2>/dev/null || true
  "$IP" rule del priority "$RULE_TPROXY_LOCAL_UDP" 2>/dev/null || true
  "$IP" route flush table "$TABLE_TPROXY" 2>/dev/null || true
  if pid_alive "$SINGBOX_PID"; then
    kill "$(cat "$SINGBOX_PID")" 2>/dev/null || true
    sleep 1
  fi
  # A simultaneous daemon iteration used to be able to start a replacement
  # after proxy-off killed the recorded PID, then have its PID file removed
  # by the first stop call. Clean up such an orphan by its exact config path.
  pkill -f "sing-box run -c $RUNTIME/sing-box.json" 2>/dev/null || true
  rm -f "$SINGBOX_PID"
  count=0
  while [ -e /sys/class/net/op7proxy ] && [ "$count" -lt 5 ]; do
    sleep 1
    count=$((count + 1))
  done
  if [ -e /sys/class/net/op7proxy ]; then
    pkill -9 -f "sing-box run -c $RUNTIME/sing-box.json" 2>/dev/null || true
    sleep 1
  fi
}

stop_proxy() {
  if ! proxy_lock_acquire; then
    log "proxy lock timeout while stopping; direct policy routes removed"
    "$IP" rule del priority "$RULE_TPROXY" 2>/dev/null || true
    "$IP" rule del priority "$RULE_TPROXY_UDP" 2>/dev/null || true
    "$IP" rule del priority "$RULE_TPROXY_LOCAL" 2>/dev/null || true
    "$IP" rule del priority "$RULE_TPROXY_LOCAL_UDP" 2>/dev/null || true
    "$IP" route flush table "$TABLE_TPROXY" 2>/dev/null || true
    return 0
  fi
  stop_proxy_unlocked
  proxy_lock_release
}

proxy_ready() {
  lan="$1"
  [ -e /sys/class/net/op7proxy ] &&
    pid_alive "$SINGBOX_PID" &&
    [ "$("$IP" rule show priority "$RULE_TPROXY" 2>/dev/null | grep -c "from $LAN_SUBNET.*lookup $TABLE_TPROXY")" = 1 ] &&
    [ "$("$IP" rule show priority "$RULE_TPROXY_LOCAL" 2>/dev/null | grep -c "uidrange $ANDROID_UID_MIN-$ANDROID_UID_MAX.*lookup $TABLE_TPROXY")" = 1 ] &&
    "$IP" route show table "$TABLE_TPROXY" 2>/dev/null | grep -q '^default dev op7proxy' &&
    "$IP" route show table "$TABLE_TPROXY" 2>/dev/null | grep -q "^$LAN_SUBNET dev $lan" &&
    "$IPTABLES" -C OP7_FORWARD -i "$lan" -o op7proxy -j ACCEPT 2>/dev/null &&
    "$IPTABLES" -C OP7_FORWARD -i op7proxy -o "$lan" -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT 2>/dev/null
}

apply_proxy_unlocked() {
  wan="$1" lan="$2" ppp="$3"
  proxy_requested || { stop_proxy_unlocked; return 0; }
  mihomo_router_listening || { log "Mihomo mixed port 17890 is unavailable; direct routing retained"; stop_proxy_unlocked; return 0; }
  mihomo_router_isolated || { log "Mihomo is not isolated from Android app routing; direct routing retained"; stop_proxy_unlocked; return 0; }
  # The manager runs every five seconds. Keep a healthy policy route intact;
  # rebuilding it on every pass creates forwarding gaps and route-change spam.
  proxy_ready "$lan" && return 0
  if ! pid_alive "$SINGBOX_PID"; then
    stop_proxy_unlocked
    proxy_requested || return 0
    write_singbox_config
    "$SINGBOX" check -c "$RUNTIME/sing-box.json" >> "$LOG" 2>&1 || {
      log "sing-box config validation failed; direct routing retained"
      stop_proxy_unlocked
      return 0
    }
    "$SINGBOX" run -c "$RUNTIME/sing-box.json" >> "$LOG" 2>&1 &
    echo $! > "$SINGBOX_PID"
    count=0
    while [ ! -e /sys/class/net/op7proxy ] && [ "$count" -lt 10 ]; do
      sleep 1
      count=$((count + 1))
    done
  fi
  [ -e /sys/class/net/op7proxy ] || { log "proxy TUN did not appear; direct routing retained"; stop_proxy_unlocked; return 0; }
  proxy_requested || { stop_proxy_unlocked; return 0; }

  # Treat the policy route as one transaction. A partial route/rule setup is
  # worse than no proxy because it can black-hole the whole LAN. stop_proxy
  # removes every policy artifact and therefore fails open to direct PPPoE.
  "$IP" rule del priority "$RULE_TPROXY" 2>/dev/null || true
  "$IP" rule del priority "$RULE_TPROXY_UDP" 2>/dev/null || true
  "$IP" rule del priority "$RULE_TPROXY_LOCAL" 2>/dev/null || true
  "$IP" rule del priority "$RULE_TPROXY_LOCAL_UDP" 2>/dev/null || true
  if ! "$IP" route replace "$LAN_SUBNET" dev "$lan" src 192.168.2.1 table "$TABLE_TPROXY" ||
     ! "$IP" route replace "$MODEM_SUBNET" dev "$wan" src 192.168.1.2 table "$TABLE_TPROXY" ||
     ! "$IP" route replace default dev op7proxy table "$TABLE_TPROXY" ||
     ! "$IP" rule add priority "$RULE_TPROXY" from "$LAN_SUBNET" lookup "$TABLE_TPROXY" ||
     ! "$IP" rule add priority "$RULE_TPROXY_LOCAL" uidrange "$ANDROID_UID_MIN-$ANDROID_UID_MAX" lookup "$TABLE_TPROXY"; then
      log "proxy policy route failed; direct routing restored"
      stop_proxy_unlocked
      return 0
  fi

  if ! proxy_ready "$lan"; then
    log "proxy readiness check failed; direct routing restored"
    stop_proxy_unlocked
    return 0
  fi
  proxy_requested || stop_proxy_unlocked
}

apply_proxy() {
  if ! proxy_lock_acquire; then
    log "proxy lock timeout while applying; direct routing retained"
    return 0
  fi
  apply_proxy_unlocked "$@"
  proxy_lock_release
}

write_proxy_flag() {
  desired="$1"
  tmp="$CONFIG.proxy.$$"
  cp -p "$CONFIG" "$BASE/config.before-proxy-toggle"
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
      stop_proxy
      echo WHOLE_HOME_PROXY_DISABLED
      ;;
    1)
      load_config
      mihomo_router_available || {
        echo 'Mihomo mixed port 17890 is unavailable; full-house proxy remains disabled' >&2
        return 1
      }
      write_singbox_config
      "$SINGBOX" check -c "$RUNTIME/sing-box.json" >/dev/null
      write_proxy_flag 1
      PROXY_ENABLED=1
      if [ ! -f "$ACTIVE" ]; then
        echo WHOLE_HOME_PROXY_QUEUED
        return 0
      fi
      attempt=0
      while [ "$attempt" -lt 15 ]; do
        if [ -s "$RUNTIME/interfaces" ]; then
          set -- $(cat "$RUNTIME/interfaces")
          lan="${2:-}"
        else
          lan=""
        fi
        [ -n "$lan" ] && proxy_ready "$lan" && {
          echo WHOLE_HOME_PROXY_ENABLED
          return 0
        }
        attempt=$((attempt + 1))
        sleep 1
      done
      write_proxy_flag 0
      PROXY_ENABLED=0
      stop_proxy
      echo 'full-house proxy failed to become ready; direct routing restored' >&2
      return 1
      ;;
    *)
      echo 'proxy state must be 0 or 1' >&2
      return 2
      ;;
  esac
}

refresh_proxy_state() {
  load_config
  proxy_requested || { echo WHOLE_HOME_PROXY_DISABLED; return 0; }
  write_singbox_config
  [ -s "$RUNTIME/interfaces" ] || { echo WHOLE_HOME_PROXY_QUEUED; return 0; }
  set -- $(cat "$RUNTIME/interfaces")
  wan="${1:-}" lan="${2:-}" ppp="${3:-}"
  [ -n "$wan" ] && [ -n "$lan" ] && [ -n "$ppp" ] || return 1
  proxy_lock_acquire || { echo 'proxy refresh lock timeout' >&2; return 1; }
  stop_proxy_unlocked
  apply_proxy_unlocked "$wan" "$lan" "$ppp"
  if proxy_ready "$lan"; then
    proxy_lock_release
    echo WHOLE_HOME_PROXY_REFRESHED
    return 0
  fi
  proxy_lock_release
  echo 'full-house proxy refresh failed; daemon will retry safely' >&2
  return 1
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
  "$IP" rule del priority "$RULE_WAN" 2>/dev/null || true
  "$IP" route flush table "$TABLE_WAN" 2>/dev/null || true
  "$IP" route del default dev ppp0 table main 2>/dev/null || true
}

publish_interfaces() {
  wan="$1" lan="$2" ppp="$3"
  printf '%s %s %s\n' "$wan" "$lan" "$ppp" > "$RUNTIME/interfaces"
  mkdir -p "$(dirname "$ADMIN_INTERFACES")"
  printf '%s %s %s\n' "$wan" "$lan" "$ppp" > "$ADMIN_INTERFACES"
  chown 10207:10207 "$ADMIN_INTERFACES" 2>/dev/null || true
  chmod 600 "$ADMIN_INTERFACES" 2>/dev/null || true
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
  start_dnsmasq "$lan"
  apply_ppp_queue "$ppp"
  apply_routes "$wan" "$lan" "$ppp"
  apply_firewall "$wan" "$lan" "$ppp"
  apply_proxy "$wan" "$lan" "$ppp"
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
  stop_proxy
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
  rm -f "$ACTIVE" "$RUNTIME/interfaces"
  rm -f "$ADMIN_INTERFACES"
}

validate() {
  load_config
  for binary in "$IP" "$IPTABLES" "$TC" "$DNSMASQ" "$PPPOE" "$PPPD" "$SINGBOX"; do
    [ -x "$binary" ] || { echo "missing $binary" >&2; exit 1; }
  done
  wan="$(resolve_wan_iface)"
  lan="$(resolve_lan_iface "$wan")"
  [ "$wan" != "$lan" ]
  write_dnsmasq_config "$lan"
  "$DNSMASQ" --test --conf-file="$RUNTIME/dnsmasq.conf"
  write_ppp_options "$wan"
  write_singbox_config
  "$SINGBOX" check -c "$RUNTIME/sing-box.json"
  echo "VALID WAN=$wan LAN=$lan PROXY=${PROXY_ENABLED:-0}"
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
  if [ -n "$ppp" ]; then
    echo "ppp_txqueuelen=$(cat "$NET_CLASS/$ppp/tx_queue_len" 2>/dev/null || echo unknown)"
    echo "ppp_qdisc=$("$TC" qdisc show dev "$ppp" 2>/dev/null | awk 'NR == 1 { print $2 }')"
  else
    echo "ppp_txqueuelen=unavailable"
    echo "ppp_qdisc=unavailable"
  fi
  echo "dnsmasq=$([ -s "$DNSMASQ_PID" ] && cat "$DNSMASQ_PID" || echo down)"
  echo "whole_home_proxy=$([ "${PROXY_ENABLED:-0}" = 1 ] && echo enabled || echo disabled)"
  echo "mihomo_router=$(mihomo_router_listening && echo available || echo missing)"
  echo "mihomo_isolated=$(mihomo_router_isolated && echo yes || echo no)"
  echo "proxy_tun=$([ -e /sys/class/net/op7proxy ] && echo up || echo down)"
  echo "local_app_proxy=$([ -e /sys/class/net/op7proxy ] && "$IP" rule show priority "$RULE_TPROXY_LOCAL" 2>/dev/null | grep -q "lookup $TABLE_TPROXY" && echo active || echo inactive)"
}

daemon() {
  if [ -s "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
    exit 0
  fi
  echo $$ > "$PIDFILE"
  trap 'rm -f "$PIDFILE"' EXIT
  trap 'exit 0' INT TERM
  log "network manager started"
  while :; do
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
      if [ -f "$ACTIVE" ] || pid_alive "$PPPD_PID" || pid_alive "$DNSMASQ_PID"; then
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
      pid_alive "$DNSMASQ_PID" || start_dnsmasq "$lan"
    else
      stop_dnsmasq
    fi

    if has_carrier "$wan" && has_carrier "$lan"; then
      start_ppp "$wan"
    else
      stop_ppp
    fi

    ppp="$(find_ppp 2>/dev/null || true)"
    if [ -n "$ppp" ] && has_carrier "$lan"; then
      current="$(cat "$RUNTIME/interfaces" 2>/dev/null || true)"
      desired="$wan $lan $ppp"
      if [ ! -f "$ACTIVE" ] || [ "$current" != "$desired" ]; then
        activate_router "$wan" "$lan" "$ppp"
      else
        "$IP" addr replace "$LAN_ADDRESS" dev "$lan"
        "$IP" addr replace "$MODEM_ADDRESS" dev "$wan"
        pid_alive "$DNSMASQ_PID" || start_dnsmasq "$lan"
        apply_ppp_queue "$ppp"
        apply_routes "$wan" "$lan" "$ppp"
        apply_proxy "$wan" "$lan" "$ppp"
        publish_interfaces "$wan" "$lan" "$ppp"
        if [ "${DISABLE_WIFI_ON_ACTIVE:-0}" = "1" ]; then
          svc wifi disable >/dev/null 2>&1 || true
        fi
      fi
    elif [ -f "$ACTIVE" ]; then
      # Keep LAN management alive during a WAN redial; only withdraw the WAN
      # policy route so traffic cannot leak into an unintended Android path.
      remove_routes
      stop_proxy
      if has_carrier "$lan"; then
        pid_alive "$DNSMASQ_PID" || start_dnsmasq "$lan"
      fi
      rm -f "$ACTIVE"
      log "router inactive: PPPoE is down; LAN management retained"
    fi
    sleep 5
  done
}

mkdir -p "$BASE" "$RUNTIME"
chmod 700 "$BASE" "$RUNTIME"

case "${1:-status}" in
  validate) validate ;;
  status) status ;;
  proxy-on) set_proxy_state 1 ;;
  proxy-off) set_proxy_state 0 ;;
  proxy-refresh) refresh_proxy_state ;;
  daemon) daemon ;;
  arm) touch "$ARMED"; echo ARMED ;;
  disarm) rm -f "$ARMED"; cleanup_runtime; echo DISARMED ;;
  cleanup) cleanup_runtime; echo CLEANED ;;
  *) echo "usage: $0 {validate|status|proxy-on|proxy-off|proxy-refresh|daemon|arm|disarm|cleanup}" >&2; exit 2 ;;
esac
