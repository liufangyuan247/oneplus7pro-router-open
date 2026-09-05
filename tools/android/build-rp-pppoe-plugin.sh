#!/system/bin/sh
set -eu

# Build pppd 2.4.7's kernel-mode PPPoE channel plugin with Termux clang.
# This script only creates an artifact. It does not install the plugin or
# touch the active PPPoE session.

if [ "$#" -ne 2 ]; then
  echo "usage: $0 <ppp-2.4.7-source> <output-directory>" >&2
  exit 2
fi

source_root="${1%/}"
output_dir="${2%/}"
plugin_source="$source_root/pppd/plugins/rp-pppoe"
compiler=/data/data/com.termux/files/usr/bin/clang
readelf=/data/data/com.termux/files/usr/bin/readelf

[ -x "$compiler" ] || {
  echo "Termux clang is unavailable: $compiler" >&2
  exit 1
}
[ -f "$source_root/pppd/patchlevel.h" ] || {
  echo "not a ppp source tree: $source_root" >&2
  exit 1
}
grep -q 'VERSION.*"2\.4\.7"' "$source_root/pppd/patchlevel.h" || {
  echo "the plugin must be built from ppp 2.4.7 sources" >&2
  exit 1
}
for file in plugin.c discovery.c if.c common.c pppoe.h; do
  [ -f "$plugin_source/$file" ] || {
    echo "missing source: $plugin_source/$file" >&2
    exit 1
  }
done

mkdir -p "$output_dir/objects"
build_dir="$output_dir/objects"

common_flags="-O2 -fPIC -fstack-protector-strong -D_FORTIFY_SOURCE=2"
common_flags="$common_flags -I$source_root/include -I$source_root"
common_flags="$common_flags -DRP_VERSION=\"3.8p\""

for unit in plugin discovery if common; do
  "$compiler" $common_flags -c "$plugin_source/$unit.c" \
    -o "$build_dir/$unit.o"
done

"$compiler" -shared -Wl,-z,relro,-z,now \
  -Wl,--build-id=sha1 \
  -o "$output_dir/rp-pppoe.so" \
  "$build_dir/plugin.o" "$build_dir/discovery.o" \
  "$build_dir/if.o" "$build_dir/common.o"

[ -s "$output_dir/rp-pppoe.so" ]
"$readelf" -h "$output_dir/rp-pppoe.so" | \
  grep -E 'Class:|Type:|Machine:'
sha256sum "$output_dir/rp-pppoe.so"
