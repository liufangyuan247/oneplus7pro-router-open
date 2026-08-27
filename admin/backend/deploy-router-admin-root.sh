#!/system/bin/sh
set -eu

PREFIX=/data/data/com.termux/files/usr
HOME=/data/data/com.termux/files/home
STAGE="$HOME/.stage"
TERMUX_UID=10207
TERMUX_GIDS=1004,1007,1011,1015,1028,1078,1079,3001,3002,3003,3006,3009,3011,9997,50207

mkdir -p "$STAGE"
cp /data/local/tmp/router-admin "$STAGE/router-admin"
cp /data/local/tmp/router-admin-web.tar.gz "$STAGE/router-admin-web.tar.gz"
cp /data/local/tmp/scrcpy-server-v3.3.1 "$STAGE/scrcpy-server-v3.3.1"
chown "$TERMUX_UID:$TERMUX_UID" \
  "$STAGE/router-admin" \
  "$STAGE/router-admin-web.tar.gz" \
  "$STAGE/scrcpy-server-v3.3.1"
chmod 700 "$STAGE/router-admin"
chmod 600 "$STAGE/router-admin-web.tar.gz" "$STAGE/scrcpy-server-v3.3.1"

"$PREFIX/bin/setpriv" \
  --reuid "$TERMUX_UID" --regid "$TERMUX_UID" --groups "$TERMUX_GIDS" \
  "$PREFIX/bin/sh" /data/local/tmp/install-router-admin.sh

cp /data/local/tmp/20-oneplus-router.sh /data/adb/service.d/20-oneplus-router.sh
chmod 755 /data/adb/service.d/20-oneplus-router.sh

rm -f /data/local/tmp/router-admin \
  /data/local/tmp/router-admin-web.tar.gz \
  /data/local/tmp/scrcpy-server-v3.3.1 \
  /data/local/tmp/install-router-admin.sh \
  /data/local/tmp/20-oneplus-router.sh

echo ROUTER_ADMIN_DEPLOYED
