#!/system/bin/sh
set -eu

if [ "$#" -ne 2 ]; then
  echo "usage: $0 <module-directory> <21-router-fq-codel.sh>" >&2
  exit 2
fi

source_modules="${1%/}"
source_service="$2"
base=/data/adb/router-network
module_dir="$base/modules/4.14.190-perf+"
service=/data/adb/service.d/21-router-fq-codel.sh
expected_release='4.14.190-perf+'
expected_version='#2 SMP PREEMPT Tue Aug 25 18:11:57 UTC 2026'
timestamp="$(date '+%Y%m%d-%H%M%S')"
backup="$base/21-router-fq-codel.sh.backup-$timestamp"
before_cc="$(cat /proc/sys/net/ipv4/tcp_congestion_control)"
before_qdisc="$(cat /proc/sys/net/core/default_qdisc)"
before_rps="$(cat /proc/sys/net/core/rps_sock_flow_entries)"
status=''

expected_hash() {
  case "$1" in
    ifb) echo 2d5eb74b27367632997cfef59864dc3558f70ebde74d8af7822c026309bbb777 ;;
    sch_tbf) echo b40e129087c9a4dae9ce2703157e7929391a55d2e2013498c82edf75c7664c97 ;;
    sch_fq) echo 2412daf61f00e2d36a738bfb35bf52ab10efbd255fc427e023cdc0057fdc6c4d ;;
    sch_fq_codel) echo bda616e31b08920bf5c3c926e7ba35df3a92d59134389e8f937d80697f4d03b1 ;;
    tcp_bbr) echo b7480ba6587a1077d9c14014735d361ba08d8018298a1b266d939910cec21eef ;;
  esac
}

restore_setting() {
  path="$1"
  value="$2"
  [ -w "$path" ] && printf '%s\n' "$value" > "$path" || true
}

rollback() {
  if [ -s "$base/performance.pid" ]; then
    pid="$(cat "$base/performance.pid" 2>/dev/null || true)"
    case "$pid" in ''|*[!0-9]*) ;; *) kill "$pid" 2>/dev/null || true ;; esac
  fi
  restore_setting /proc/sys/net/ipv4/tcp_congestion_control "$before_cc"
  restore_setting /proc/sys/net/core/default_qdisc "$before_qdisc"
  restore_setting /proc/sys/net/core/rps_sock_flow_entries "$before_rps"
  for net_path in /sys/class/net/eth* /sys/class/net/ppp*; do
    [ -e "$net_path" ] || continue
    for path in "$net_path"/queues/rx-*/rps_cpus "$net_path"/queues/rx-*/rps_flow_cnt "$net_path"/queues/tx-*/xps_cpus; do
      [ -w "$path" ] && printf '0\n' > "$path" || true
    done
  done
  if [ -s "$backup" ]; then
    cp -p "$backup" "$service.recovering"
    chmod 755 "$service.recovering"
    mv -f "$service.recovering" "$service"
  fi
  echo 'ROLLED_BACK network-performance settings' >&2
}

on_exit() {
  rc="$1"
  [ -z "$status" ] || rm -f "$status"
  [ "$rc" -eq 0 ] || rollback
  exit "$rc"
}

trap 'on_exit $?' EXIT HUP INT TERM

[ "$(uname -r)" = "$expected_release" ]
[ "$(uname -v)" = "$expected_version" ]
/system/bin/sh -n "$source_service"

for name in ifb sch_tbf sch_fq sch_fq_codel tcp_bbr; do
  source="$source_modules/$name.ko"
  [ -s "$source" ] || { echo "missing module: $source" >&2; exit 1; }
  actual="$(sha256sum "$source" | awk '{print $1}')"
  expected="$(expected_hash "$name")"
  [ "$actual" = "$expected" ] || {
    echo "checksum mismatch: $name actual=$actual" >&2
    exit 1
  }
done

mkdir -p "$module_dir"
chmod 700 "$base/modules" "$module_dir"
[ ! -s "$service" ] || cp -p "$service" "$backup"

for name in ifb sch_tbf sch_fq sch_fq_codel tcp_bbr; do
  cp "$source_modules/$name.ko" "$module_dir/$name.ko.new"
  chown root:root "$module_dir/$name.ko.new" 2>/dev/null || true
  chmod 600 "$module_dir/$name.ko.new"
  restorecon "$module_dir/$name.ko.new" >/dev/null 2>&1 || true
  mv -f "$module_dir/$name.ko.new" "$module_dir/$name.ko"
done

cp "$source_service" "$service.new"
chown root:root "$service.new" 2>/dev/null || true
chmod 755 "$service.new"
restorecon "$service.new" >/dev/null 2>&1 || true
mv -f "$service.new" "$service"
sync
/system/bin/sh "$service"

elapsed=0
while [ "$elapsed" -lt 20 ]; do
  [ "$(cat /proc/sys/net/ipv4/tcp_congestion_control)" = bbr ] && \
  [ "$(cat /proc/sys/net/core/default_qdisc)" = fq ] && \
  [ "$(cat /proc/sys/net/core/rps_sock_flow_entries)" = 32768 ] && break
  sleep 1
  elapsed=$((elapsed + 1))
done
[ "$elapsed" -lt 20 ]

for name in ifb sch_tbf sch_fq sch_fq_codel tcp_bbr; do
  [ -d "/sys/module/$name" ] || { echo "module not loaded: $name" >&2; exit 1; }
done
[ "$(cat /sys/class/net/eth0/queues/rx-0/rps_cpus)" = 00000070 ] || \
  [ "$(cat /sys/class/net/eth0/queues/rx-0/rps_cpus)" = 70 ] || \
  [ "$(cat /sys/class/net/eth0/queues/rx-0/rps_cpus)" = 000000f0 ] || \
  [ "$(cat /sys/class/net/eth0/queues/rx-0/rps_cpus)" = f0 ]
[ "$(cat /sys/class/net/eth1/queues/rx-0/rps_cpus)" = 00000070 ] || \
  [ "$(cat /sys/class/net/eth1/queues/rx-0/rps_cpus)" = 70 ] || \
  [ "$(cat /sys/class/net/eth1/queues/rx-0/rps_cpus)" = 000000f0 ] || \
  [ "$(cat /sys/class/net/eth1/queues/rx-0/rps_cpus)" = f0 ]

status="$base/runtime/performance-install-status.$$"
"$base/router-network" status > "$status"
grep -q '^active=yes$' "$status"
grep -q '^ppp=ppp0$' "$status"
grep -q '^mihomo_tun=up$' "$status"
grep -q '^codex_policy=active$' "$status"

prefix=/data/data/com.termux/files/usr
openai_code="$($prefix/bin/curl -sS --connect-timeout 8 --max-time 20 \
  --proxy http://127.0.0.1:17891 --output /dev/null --write-out '%{http_code}' \
  https://api.openai.com/v1/models 2>/dev/null || true)"
case "$openai_code" in 2??|3??|4??) ;; *) exit 1 ;; esac

rm -f "$status"
status=''
trap - EXIT HUP INT TERM
echo "NETWORK_PERFORMANCE_INSTALLED cc=bbr qdisc=fq ppp_qdisc=fq_codel rps=$(cat /sys/class/net/eth0/queues/rx-0/rps_cpus) openai_http=$openai_code"
