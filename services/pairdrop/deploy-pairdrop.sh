#!/data/data/com.termux/files/usr/bin/sh
set -eu
umask 077

# Run as the Termux user. Stage the pinned upstream tarball alongside this
# directory's run script, dependency lock, and RTC config under ~/.stage/pairdrop/.
HOME_DIR="${TERMUX_HOME:-/data/data/com.termux/files/home}"
PREFIX="${TERMUX_PREFIX:-/data/data/com.termux/files/usr}"
export HOME="$HOME_DIR" PREFIX PATH="$PREFIX/bin:/system/bin"
STAGE="$HOME/.stage/pairdrop"
APP="$HOME/router-apps/pairdrop"
DATA="$HOME/router-data/pairdrop"
SERVICES="$HOME/router-stack/services"
SERVICE="$SERVICES/pairdrop"
REV=1b0c9c9d903c3cfc706df1303dc75c3b54d04c77
STAMP=$(date '+%Y%m%d-%H%M%S')
RELEASE="$APP/releases/$STAMP-${REV%${REV#????????????}}"
BACKUP="$HOME/router-data/backups/pairdrop/$STAMP"
ACTIVATED=0
DONE=0
HAD_SERVICE=0

fail() { echo "ERROR: $*" >&2; exit 1; }
[ "$#" -eq 1 ] || fail 'expected source archive SHA256'
[ "$(sha256sum "$STAGE/source.tar.gz" | cut -d ' ' -f1)" = "$1" ] || fail 'source checksum mismatch'
export LD_LIBRARY_PATH="$APP/runtime/lib:$PREFIX/lib"
node --version
npm --version
mkdir -p "$RELEASE" "$DATA" "$BACKUP"
tar -xzf "$STAGE/source.tar.gz" -C "$RELEASE"
cp "$STAGE/package-lock.json" "$RELEASE/package-lock.json"
cd "$RELEASE"
npm ci --omit=dev --ignore-scripts --no-fund --no-audit
node --check server/index.js
printf '%s\n' "$REV" > UPSTREAM_COMMIT

[ ! -L "$APP/current" ] || readlink "$APP/current" > "$BACKUP/previous-release"
if [ -d "$SERVICE" ]; then
    HAD_SERVICE=1
    cp -p "$SERVICE/run" "$BACKUP/run"
    [ ! -f "$SERVICE/down" ] || cp -p "$SERVICE/down" "$BACKUP/down"
fi
[ ! -f "$DATA/rtc-config.json" ] || cp -p "$DATA/rtc-config.json" "$BACKUP/rtc-config.json"

rollback() {
    result=$?
    trap - EXIT INT TERM
    if [ "$ACTIVATED" -eq 1 ] && [ "$DONE" -eq 0 ]; then
        sv down "$SERVICE" >/dev/null 2>&1 || true
        if [ -s "$BACKUP/previous-release" ]; then
            ln -s "$(cat "$BACKUP/previous-release")" "$APP/current.rollback.$STAMP"
            mv -Tf "$APP/current.rollback.$STAMP" "$APP/current"
        fi
        if [ "$HAD_SERVICE" -eq 1 ]; then
            cp -p "$BACKUP/run" "$SERVICE/run"
            if [ -f "$BACKUP/rtc-config.json" ]; then cp -p "$BACKUP/rtc-config.json" "$DATA/rtc-config.json"; fi
            if [ -f "$BACKUP/down" ]; then cp -p "$BACKUP/down" "$SERVICE/down"; else sv up "$SERVICE" >/dev/null 2>&1 || true; fi
        else
            touch "$SERVICE/down"
        fi
        echo "PairDrop activation failed; previous service restored or new service disabled: $BACKUP" >&2
    fi
    exit "$result"
}
trap rollback EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

NEW_SERVICE="$STAGE/service.$STAMP"
mkdir -p "$NEW_SERVICE/log/main"
cp "$STAGE/pairdrop-service-run" "$NEW_SERVICE/run"
printf '#!%s/bin/sh\nexec %s/bin/svlogd -tt %s/log/main\n' "$PREFIX" "$PREFIX" "$SERVICE" > "$NEW_SERVICE/log/run"
printf 's1048576\nn4\n' > "$NEW_SERVICE/log/main/config"
touch "$NEW_SERVICE/down"
chmod 0700 "$NEW_SERVICE" "$NEW_SERVICE/run" "$NEW_SERVICE/log" "$NEW_SERVICE/log/run" "$NEW_SERVICE/log/main"
chmod 0600 "$NEW_SERVICE/down" "$NEW_SERVICE/log/main/config"
if [ ! -d "$SERVICE" ]; then mv "$NEW_SERVICE" "$SERVICE"; fi
ACTIVATED=1
cp "$STAGE/pairdrop-service-run" "$SERVICE/run"
chmod 0700 "$SERVICE/run"
[ -f "$DATA/rtc-config.json" ] || cp "$STAGE/rtc-config.json" "$DATA/rtc-config.json"
ln -s "$RELEASE" "$APP/current.new.$STAMP"
mv -Tf "$APP/current.new.$STAMP" "$APP/current"
rm -f "$SERVICE/down"
sv restart "$SERVICE" >/dev/null 2>&1 || true

ready=0
for attempt in $(seq 1 20); do
    if curl -fsS --max-time 2 http://127.0.0.1:3000/config | python -c 'import json,sys; d=json.load(sys.stdin); assert d["signalingServer"] is False' >/dev/null 2>&1; then
        ready=1
        break
    fi
    sleep 1
done
[ "$ready" -eq 1 ] || fail 'PairDrop health check failed'
DONE=1
printf 'PAIRDROP_DEPLOYED release=%s backup=%s\n' "$RELEASE" "$BACKUP"
