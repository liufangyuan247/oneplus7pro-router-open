#!/data/data/com.termux/files/usr/bin/sh
set -eu
umask 077
export HOME=/data/data/com.termux/files/home PREFIX=/data/data/com.termux/files/usr
export PATH="$PREFIX/bin:/system/bin"
STAGE="$HOME/.stage/adguardhome"
SERVICES="$HOME/router-stack/services"
test -s "$HOME/router-data/adguardhome/AdGuardHome.yaml"
test ! -e "$SERVICES/adguardhome"
test ! -e "$SERVICES/adguardhome-dns"

"$PREFIX/bin/su" -c "mkdir -p /data/adb/router-adguardhome; chmod 700 /data/adb/router-adguardhome; cp '$STAGE/dns-guard.sh' /data/adb/router-adguardhome/dns-guard; cp '$STAGE/dns-health.py' /data/adb/router-adguardhome/dns-health.py; chown -R root:root /data/adb/router-adguardhome; chmod 700 /data/adb/router-adguardhome/dns-guard; chmod 600 /data/adb/router-adguardhome/dns-health.py; /system/bin/sh -n /data/adb/router-adguardhome/dns-guard"

for name in adguardhome adguardhome-dns; do
    dir="$STAGE/$name.service"
    mkdir -p "$dir/log/main"
    cp "$STAGE/$name-service-run" "$dir/run"
    printf '#!%s/bin/sh\nexec %s/bin/svlogd -tt %s/%s/log/main\n' "$PREFIX" "$PREFIX" "$SERVICES" "$name" > "$dir/log/run"
    printf 's1048576\nn4\n' > "$dir/log/main/config"
    touch "$dir/down"
    chmod 0700 "$dir" "$dir/log" "$dir/log/main" "$dir/run" "$dir/log/run"
    chmod 0600 "$dir/down" "$dir/log/main/config"
    mv "$dir" "$SERVICES/$name"
done

rollback() {
    result=$?
    if [ "$result" -ne 0 ]; then
        touch "$SERVICES/adguardhome/down" "$SERVICES/adguardhome-dns/down"
        sv down "$SERVICES/adguardhome-dns" "$SERVICES/adguardhome" >/dev/null 2>&1 || true
        "$PREFIX/bin/su" -c '/data/adb/router-adguardhome/dns-guard stop' || true
        echo 'AdGuard activation failed; interception disabled, existing DNS retained' >&2
    fi
    exit "$result"
}
trap rollback EXIT
rm -f "$SERVICES/adguardhome/down"
sv up "$SERVICES/adguardhome" >/dev/null 2>&1 || true
ready=0
for attempt in $(seq 1 20); do
    if python "$STAGE/dns-health.py"; then ready=1; break; fi
    sleep 1
done
test "$ready" -eq 1
rm -f "$SERVICES/adguardhome-dns/down"
sv up "$SERVICES/adguardhome-dns" >/dev/null 2>&1 || true
sleep 6
"$PREFIX/bin/su" -c 'test "$(cat /data/adb/router-adguardhome/state)" = active'
echo ADGUARD_SERVICES_INSTALLED
