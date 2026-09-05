#!/system/bin/sh
set -eu

if [ "$#" -ne 1 ]; then
  echo "usage: $0 <verified-rp-pppoe.so>" >&2
  exit 2
fi

source_plugin="$1"
base=/data/adb/router-network
destination_dir="$base/plugins/pppd-2.4.7"
destination="$destination_dir/rp-pppoe.so"
expected_sha256='2c485eca217fb6da88e705da90c34eae0e3de684062f579cc890da7a6e0eb97d'

[ "$(/system/bin/pppd --version 2>&1)" = 'pppd version 2.4.7' ] || {
  echo 'the installed pppd is not version 2.4.7' >&2
  exit 1
}
zcat /proc/config.gz | grep -qx 'CONFIG_PPPOE=y' || {
  echo 'the running kernel does not provide CONFIG_PPPOE=y' >&2
  exit 1
}
[ -s "$source_plugin" ] || { echo "plugin missing: $source_plugin" >&2; exit 1; }
actual_sha256="$(sha256sum "$source_plugin" | awk '{print $1}')"
[ "$actual_sha256" = "$expected_sha256" ] || {
  echo "plugin checksum mismatch: $actual_sha256" >&2
  exit 1
}

# dryrun loads plugin_init and resolves the pppd ABI, but does not perform
# discovery, authentication or any data-plane change.
before_pids="$(pidof pppd pppoe 2>/dev/null || true)"
before_ifindex="$(cat /sys/class/net/ppp0/ifindex 2>/dev/null || true)"
/system/bin/pppd plugin "$source_plugin" eth0 dryrun noauth noipdefault nodetach \
  >/dev/null
[ "$(pidof pppd pppoe 2>/dev/null || true)" = "$before_pids" ]
[ "$(cat /sys/class/net/ppp0/ifindex 2>/dev/null || true)" = "$before_ifindex" ]

mkdir -p "$destination_dir"
chmod 700 "$base/plugins" "$destination_dir"
if [ -s "$destination" ] && [ "$(sha256sum "$destination" | awk '{print $1}')" != "$expected_sha256" ]; then
  backup="$destination.backup-$(date +%Y%m%d-%H%M%S)"
  cp -p "$destination" "$backup"
  chmod 600 "$backup"
fi
cp "$source_plugin" "$destination.new"
chown root:root "$destination.new" 2>/dev/null || true
chmod 600 "$destination.new"
restorecon "$destination.new" >/dev/null 2>&1 || true
mv -f "$destination.new" "$destination"

installed_sha256="$(sha256sum "$destination" | awk '{print $1}')"
[ "$installed_sha256" = "$expected_sha256" ]
echo "INSTALLED path=$destination sha256=$installed_sha256; active PPPoE unchanged"
