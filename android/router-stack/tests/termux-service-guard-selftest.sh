#!/data/data/com.termux/files/usr/bin/sh
set -eu

PREFIX=/data/data/com.termux/files/usr
SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
GUARD="$SCRIPT_DIR/termux-service-guard.sh"
WORK="${TMPDIR:-$PREFIX/tmp}/termux-service-guard-test.$$"
trap 'rm -rf "$WORK"' EXIT INT TERM

mkdir -p "$WORK/home" "$WORK/stack/bin" "$WORK/services/demo/log/main" "$WORK/prefix/bin"
for tool in stat id chown chmod; do
    ln -s "$PREFIX/bin/$tool" "$WORK/prefix/bin/$tool"
done
printf '#!/data/data/com.termux/files/usr/bin/sh\nexit 0\n' > "$WORK/services/demo/run"
printf '#!/data/data/com.termux/files/usr/bin/sh\nexit 0\n' > "$WORK/services/demo/log/run"
printf '#!/data/data/com.termux/files/usr/bin/sh\nexit 0\n' > "$WORK/stack/bin/helper"
printf 'binary\n' > "$WORK/prefix/bin/router-admin"
chmod 777 "$WORK/services/demo" "$WORK/services/demo/log" "$WORK/services/demo/log/main"
chmod 700 "$WORK/services/demo/run" "$WORK/services/demo/log/run" "$WORK/stack/bin/helper" "$WORK/prefix/bin/router-admin"
chown 0:0 "$WORK/services/demo/run" "$WORK/stack/bin/helper"

guard() {
    TERMUX_PREFIX="$WORK/prefix" \
    TERMUX_HOME="$WORK/home" \
    TERMUX_STACK="$WORK/stack" \
    TERMUX_SERVICES="$WORK/services" \
    TERMUX_SERVICE_UID=10207 \
    TERMUX_SERVICE_GID=10207 \
    "$GUARD" "$@"
}

if guard audit >/dev/null 2>&1; then
    echo 'audit unexpectedly accepted unsafe ownership' >&2
    exit 1
fi

guard repair | grep -q '^TERMUX_SERVICE_GUARD_OK '
[ "$("$PREFIX/bin/stat" -c '%u:%g:%a' "$WORK/services/demo/run")" = 10207:10207:700 ]
[ "$("$PREFIX/bin/stat" -c '%u:%g:%a' "$WORK/services/demo")" = 10207:10207:700 ]
[ "$("$PREFIX/bin/stat" -c '%u:%g:%a' "$WORK/stack/bin/helper")" = 10207:10207:700 ]
[ "$("$PREFIX/bin/stat" -c '%u:%g:%a' "$WORK/prefix/bin/router-admin")" = 10207:10207:700 ]
guard audit demo | grep -q '^TERMUX_SERVICE_GUARD_OK services=1 repairs=0$'

echo TERMUX_SERVICE_GUARD_SELFTEST_OK
