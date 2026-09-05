#!/system/bin/sh
set -eu

# Ask the stock pppd to parse and load a kernel-mode PPPoE plugin without
# starting discovery or changing the active session.

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
  echo "usage: $0 <rp-pppoe.so> [ethernet-interface]" >&2
  exit 2
fi

plugin="$1"
interface="${2:-eth0}"
pppd=/system/bin/pppd

[ -s "$plugin" ] || {
  echo "plugin not found: $plugin" >&2
  exit 1
}
[ -d "/sys/class/net/$interface" ] || {
  echo "interface not found: $interface" >&2
  exit 1
}

before_pids="$(pidof pppd 2>/dev/null || true)"
before_ifindex="$(cat /sys/class/net/ppp0/ifindex 2>/dev/null || true)"

"$pppd" plugin "$plugin" "$interface" dryrun noauth noipdefault nodetach

after_pids="$(pidof pppd 2>/dev/null || true)"
after_ifindex="$(cat /sys/class/net/ppp0/ifindex 2>/dev/null || true)"

[ "$after_pids" = "$before_pids" ] || {
  echo "active pppd PID changed during dry-run" >&2
  exit 1
}
[ "$after_ifindex" = "$before_ifindex" ] || {
  echo "ppp0 identity changed during dry-run" >&2
  exit 1
}

echo "rp-pppoe plugin dry-run passed; active PPPoE session was unchanged"
