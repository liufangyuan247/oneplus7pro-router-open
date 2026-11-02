#!/system/bin/sh
# Root-owned ingress policy, independent of Android's default ACCEPT policy.
# The IPv4 redirect keeps existing Rclone URLs while its running jobs survive.
set -eu
BASE=/data/adb/router-security
IPT=/system/bin/iptables
IP6T=/system/bin/ip6tables
LAN=${1:-}
if [ -z "$LAN" ]; then
  LAN=$(awk '{print $2}' /data/adb/router-network/runtime/interfaces 2>/dev/null || true)
fi
LAN=${LAN:-eth1}
case "$LAN" in *[!a-zA-Z0-9_.-]*|'') echo 'invalid LAN interface' >&2; exit 1 ;; esac
umask 077
mkdir -p "$BASE"

# Restore commits the complete chain at once; no empty live policy window.
rules="$BASE/.rules.$$"
trap 'rm -f "$rules"' EXIT
trap 'exit 1' HUP INT TERM
for family in 4 6; do
  if [ "$family" = 4 ]; then
    tool=$IPT; restore=/system/bin/iptables-restore; icmp=icmp
  else
    tool=$IP6T; restore=/system/bin/ip6tables-restore; icmp=ipv6-icmp
  fi
  cat > "$rules" <<EOF
*filter
:OP7_SECURITY - [0:0]
-F OP7_SECURITY
-A OP7_SECURITY -i lo -j RETURN
# TPROXY delivery retains the public destination; it is not a local backend.
-A OP7_SECURITY -m mark --mark 0x20000000/0x20000000 -j RETURN
# Do not keep established connections to the old unauthenticated backends.
-A OP7_SECURITY -p tcp -m multiport --dports 1688,5201,5555,5572,6800 -j REJECT --reject-with tcp-reset
# Mihomo's local transparent TUN delivers new connections as INPUT packets.
# Keep that trusted internal path while rejecting physical upstream ingress.
-A OP7_SECURITY -i op7tun -j RETURN
-A OP7_SECURITY -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN
-A OP7_SECURITY -i $LAN -j RETURN
-A OP7_SECURITY -i tailscale0 -p tcp -m multiport --dports 53,445,3000,3001,5244,5573,6880,8022,8080,8317,8320,8888,9090 -j RETURN
-A OP7_SECURITY -i tailscale0 -p udp --dport 53 -j RETURN
-A OP7_SECURITY -p $icmp -j RETURN
-A OP7_SECURITY -i tailscale0 -j DROP
-A OP7_SECURITY -p udp --dport 41641 -j RETURN
# Preserve the phone's DHCP client on a physical upstream interface.
-A OP7_SECURITY -p udp --sport 67 --dport 68 -j RETURN
-A OP7_SECURITY -p udp --sport 547 --dport 546 -j RETURN
-A OP7_SECURITY -j DROP
COMMIT
EOF
  "$restore" -w 5 --noflush < "$rules"
  # Only our jump is moved. Android/Tailscale/router rules remain untouched.
  while "$tool" -w 5 -C INPUT -j OP7_SECURITY 2>/dev/null; do
    "$tool" -w 5 -D INPUT -j OP7_SECURITY
  done
  "$tool" -w 5 -I INPUT 1 -j OP7_SECURITY
done

# IPv6 NAT is unavailable on this kernel. IPv6 users use the authenticated
# proxy on 5573; direct access to 5572 is rejected by both address families.
cat > "$rules" <<EOF
*nat
:OP7_SERVICE_AUTH - [0:0]
-F OP7_SERVICE_AUTH
EOF
# Android's system xtables lacks the addrtype userspace extension. Enumerate
# only addresses owned by this router; forwarded traffic must not be captured.
for address in $(/system/bin/ip -4 -o addr show | awk '{sub(/\/.*/, "", $4); print $4}'); do
  case "$address" in 127.*) continue ;; esac
  for iface in "$LAN" tailscale0; do
    printf '%s\n' "-A OP7_SERVICE_AUTH -i $iface -d $address/32 -p tcp --dport 5572 -j REDIRECT --to-ports 5573" >> "$rules"
  done
done
cat >> "$rules" <<EOF
-A OP7_SERVICE_AUTH -j RETURN
COMMIT
EOF
/system/bin/iptables-restore -w 5 --noflush < "$rules"
"$IPT" -w 5 -t nat -C PREROUTING -j OP7_SERVICE_AUTH 2>/dev/null ||
  "$IPT" -w 5 -t nat -I PREROUTING 1 -j OP7_SERVICE_AUTH
