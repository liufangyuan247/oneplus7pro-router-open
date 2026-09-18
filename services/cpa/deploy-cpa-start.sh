#!/data/data/com.termux/files/usr/bin/sh
set -eu

PREFIX=/data/data/com.termux/files/usr
HOME_DIR=/data/data/com.termux/files/home
SERVICE="$HOME_DIR/router-stack/services/cpa"
SOURCE="${1:-$HOME_DIR/.stage/cpa-start}"
BACKUPS="$HOME_DIR/router-data/backups/cpa"
BIN="$PREFIX/bin/cli-proxy-api"
READINESS_SECONDS="${CPA_DEPLOY_READINESS_SECONDS:-75}"

[ -f "$SOURCE" ] || { echo "missing CPA start wrapper: $SOURCE" >&2; exit 1; }
[ -f "$SERVICE/run" ] || { echo "CPA run script is missing" >&2; exit 1; }
"$PREFIX/bin/sh" -n "$SOURCE"
case "$READINESS_SECONDS" in
    ''|*[!0-9]*) echo "invalid CPA_DEPLOY_READINESS_SECONDS: $READINESS_SECONDS" >&2; exit 1 ;;
esac

service_uid="$("$PREFIX/bin/stat" -c '%u' "$HOME_DIR")"
service_gid="$("$PREFIX/bin/stat" -c '%g' "$HOME_DIR")"

fix_service_owner() {
    [ "$("$PREFIX/bin/id" -u)" -eq 0 ] || return 0
    "$PREFIX/bin/chown" "$service_uid:$service_gid" "$@"
}

assert_service_executable() {
    path="$1"
    actual="$("$PREFIX/bin/stat" -c '%u:%g:%a' "$path" 2>/dev/null || true)"
    expected="$service_uid:$service_gid:700"
    [ "$actual" = "$expected" ] || {
        echo "unsafe CPA executable ownership: $path has ${actual:-missing}, want $expected" >&2
        return 1
    }
}

stamp="$(date '+%Y%m%d-%H%M%S')"
mkdir -p "$BACKUPS"
fix_service_owner "$BACKUPS"
backup="$BACKUPS/run.before-readiness-$stamp"
start_backup="$BACKUPS/start.before-readiness-$stamp"
cp -p "$SERVICE/run" "$backup"
fix_service_owner "$backup"

had_start=0
if [ -f "$SERVICE/start" ]; then
    cp -p "$SERVICE/start" "$start_backup"
    fix_service_owner "$start_backup"
    had_start=1
fi

rollback_required=1
rollback_on_exit() {
    status=$?
    trap - 0 1 2 15
    [ "$status" -ne 0 ] || exit 0
    [ "$rollback_required" -eq 1 ] || exit "$status"

    set +e
    cp -p "$backup" "$SERVICE/run"
    fix_service_owner "$SERVICE/run"
    if [ "$had_start" -eq 1 ]; then
        cp -p "$start_backup" "$SERVICE/start"
        fix_service_owner "$SERVICE/start"
    else
        rm -f "$SERVICE/start"
    fi
    "$PREFIX/bin/sv" restart "$SERVICE" >/dev/null 2>&1 || true
    echo "CPA readiness deployment failed; restored $backup" >&2
    exit "$status"
}
trap rollback_on_exit 0
trap 'exit 1' 1 2 15

cp "$SOURCE" "$SERVICE/start.new"
chmod 700 "$SERVICE/start.new"
fix_service_owner "$SERVICE/start.new"
mv "$SERVICE/start.new" "$SERVICE/start"

"$PREFIX/bin/awk" -v start="$SERVICE/start" '
BEGIN { expected = "exec \047" start "\047" }
/^exec .*cli-proxy-api.* -config / {
    print expected
    replaced=1
    next
}
$0 == expected {
    print
    replaced=1
    next
}
{ print }
END { if (!replaced) exit 42 }
' "$SERVICE/run" > "$SERVICE/run.new" || {
    rm -f "$SERVICE/run.new"
    echo 'CPA run script did not contain the expected executable command' >&2
    exit 1
}
"$PREFIX/bin/sh" -n "$SERVICE/run.new"
chmod 700 "$SERVICE/run.new"
fix_service_owner "$SERVICE/run.new"
mv "$SERVICE/run.new" "$SERVICE/run"

assert_service_executable "$SERVICE/run"
assert_service_executable "$SERVICE/start"
assert_service_executable "$BIN"

"$PREFIX/bin/sv" restart "$SERVICE" >/dev/null
ready=0
started="$(date +%s)"
while :; do
    if "$PREFIX/bin/curl" -fsS --max-time 2 http://127.0.0.1:8317/ >/dev/null 2>&1; then
        ready=1
        break
    fi
    now="$(date +%s)"
    elapsed=$((now - started))
    [ "$elapsed" -lt "$READINESS_SECONDS" ] || break
    sleep 1
done

if [ "$ready" -ne 1 ]; then
    echo "CPA did not become ready within ${READINESS_SECONDS}s" >&2
    exit 1
fi

status_file="$HOME_DIR/router-data/cpa/last-readiness-backup"
printf '%s\n' "$backup" > "$status_file.new"
chmod 600 "$status_file.new"
fix_service_owner "$status_file.new"
mv "$status_file.new" "$status_file"
rollback_required=0
echo CPA_START_READINESS_DEPLOYED
