#!/data/data/com.termux/files/usr/bin/sh
set -eu
umask 077
export HOME=/data/data/com.termux/files/home PREFIX=/data/data/com.termux/files/usr
export PATH="$PREFIX/bin:/system/bin"
STAGE="$HOME/.stage/samba"
SERVICE="$HOME/router-stack/services/samba"
[ ! -e "$SERVICE" ]
mkdir -p /storage/emulated/0/RouterShare
su -c "$PREFIX/bin/python '$STAGE/bootstrap.py'"
su -c "cp '$STAGE/samba-root.sh' /data/adb/router-samba/samba-root.sh; chown root:root /data/adb/router-samba/samba-root.sh; chmod 700 /data/adb/router-samba/samba-root.sh; /system/bin/sh -n /data/adb/router-samba/samba-root.sh"
NEW="$STAGE/service"
mkdir -p "$NEW/log/main"
cp "$STAGE/samba-service-run" "$NEW/run"
printf '#!%s/bin/sh\nexec %s/bin/svlogd -tt %s/log/main\n' "$PREFIX" "$PREFIX" "$SERVICE" > "$NEW/log/run"
printf 's1048576\nn4\n' > "$NEW/log/main/config"
touch "$NEW/down"
chmod 0700 "$NEW" "$NEW/log" "$NEW/log/main" "$NEW/run" "$NEW/log/run"
mv "$NEW" "$SERVICE"
trap 'touch "$SERVICE/down"; sv down "$SERVICE" >/dev/null 2>&1 || true' EXIT
rm -f "$SERVICE/down"
sv up "$SERVICE" >/dev/null 2>&1 || true
ready=0
for attempt in $(seq 1 15); do
    if python -c 'import socket; s=socket.create_connection(("127.0.0.1",445),2); s.close()' 2>/dev/null; then ready=1; break; fi
    sleep 1
done
[ "$ready" -eq 1 ]
trap - EXIT
echo SAMBA_SERVICE_INSTALLED
