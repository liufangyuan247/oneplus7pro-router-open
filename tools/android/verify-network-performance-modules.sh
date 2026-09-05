#!/system/bin/sh
set -eu

if [ "$#" -ne 1 ]; then
  echo "usage: $0 <module-directory>" >&2
  exit 2
fi

module_dir="${1%/}"
expected_release='4.14.190-perf+'
loaded_by_test=''

cleanup() {
  for name in $loaded_by_test; do
    /system/bin/rmmod "$name" >/dev/null 2>&1 || true
  done
}
trap cleanup EXIT HUP INT TERM

[ "$(uname -r)" = "$expected_release" ] || {
  echo "unexpected kernel release: $(uname -r)" >&2
  exit 1
}

before_pids="$(pidof pppd pppoe 2>/dev/null || true)"
before_ifindex="$(cat /sys/class/net/ppp0/ifindex 2>/dev/null || true)"
before_qdisc="$(tc qdisc show dev ppp0 2>/dev/null || true)"
before_cc="$(cat /proc/sys/net/ipv4/tcp_congestion_control)"

for name in ifb sch_tbf sch_fq tcp_bbr; do
  path="$module_dir/$name.ko"
  [ -s "$path" ] || { echo "missing module: $path" >&2; exit 1; }
  [ ! -d "/sys/module/$name" ] || {
    echo "refusing to disturb already-loaded module: $name" >&2
    exit 1
  }
  /system/bin/insmod "$path"
  [ -d "/sys/module/$name" ] || {
    echo "module did not load: $name" >&2
    exit 1
  }
  loaded_by_test="$name $loaded_by_test"
done

grep -qw bbr /proc/sys/net/ipv4/tcp_available_congestion_control || {
  echo 'tcp_bbr loaded but bbr is unavailable' >&2
  exit 1
}

cleanup
loaded_by_test=''

[ "$(pidof pppd pppoe 2>/dev/null || true)" = "$before_pids" ] || {
  echo 'PPPoE process identity changed' >&2
  exit 1
}
[ "$(cat /sys/class/net/ppp0/ifindex 2>/dev/null || true)" = "$before_ifindex" ] || {
  echo 'ppp0 identity changed' >&2
  exit 1
}
[ "$(tc qdisc show dev ppp0 2>/dev/null || true)" = "$before_qdisc" ] || {
  echo 'ppp0 qdisc changed' >&2
  exit 1
}
[ "$(cat /proc/sys/net/ipv4/tcp_congestion_control)" = "$before_cc" ] || {
  echo 'default TCP congestion control changed' >&2
  exit 1
}

echo 'network performance module load/unload verification passed'
