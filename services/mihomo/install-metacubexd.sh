#!/data/data/com.termux/files/usr/bin/sh
set -eu

PREFIX=/data/data/com.termux/files/usr
HOME=/data/data/com.termux/files/home
DATA="$HOME/router-data/mihomo"
SERVICE="$HOME/router-stack/services/mihomo"
WORK="$DATA/.metacubexd-install"
UI_NEW="$DATA/ui.new"
ARCHIVE=/data/local/tmp/metacubexd-gh-pages.zip
BASE_CONFIG=/data/local/tmp/mihomo-config.base.yaml
UI_CONFIG=/data/local/tmp/metacubexd-config.js

case "$WORK:$UI_NEW" in
  "$DATA"/*:"$DATA"/*) ;;
  *) echo 'unsafe install paths' >&2; exit 1 ;;
esac

[ -f "$ARCHIVE" ] || { echo 'missing MetaCubeXD archive' >&2; exit 1; }
[ -f "$BASE_CONFIG" ] || { echo 'missing base config' >&2; exit 1; }
[ -f "$UI_CONFIG" ] || { echo 'missing MetaCubeXD runtime config' >&2; exit 1; }

mkdir -p "$DATA"
chmod 700 "$DATA"
rm -rf "$WORK" "$UI_NEW"
mkdir -p "$WORK" "$UI_NEW"
"$PREFIX/bin/unzip" -q "$ARCHIVE" -d "$WORK"

SOURCE=$(find "$WORK" -mindepth 1 -maxdepth 1 -type d -name 'metacubexd-*' | head -n 1)
[ -n "$SOURCE" ] && [ -f "$SOURCE/index.html" ] || { echo 'invalid MetaCubeXD archive layout' >&2; exit 1; }
cp -R "$SOURCE"/. "$UI_NEW"/
[ -f "$UI_NEW/index.html" ] || { echo 'MetaCubeXD index missing' >&2; exit 1; }
cp "$UI_CONFIG" "$UI_NEW/config.js"

rm -rf "$DATA/ui.prev"
if [ -d "$DATA/ui" ]; then mv "$DATA/ui" "$DATA/ui.prev"; fi
mv "$UI_NEW" "$DATA/ui"

if [ ! -s "$DATA/config.yaml" ]; then
  cp "$BASE_CONFIG" "$DATA/config.yaml"
  chmod 600 "$DATA/config.yaml"
fi

"$PREFIX/bin/mihomo" -t -d "$DATA" -f "$DATA/config.yaml"
rm -f "$SERVICE/down"
"$PREFIX/bin/sv" up "$SERVICE"
sleep 2
"$PREFIX/bin/sv" status "$SERVICE"

rm -rf "$WORK"
echo METACUBEXD_INSTALLED
