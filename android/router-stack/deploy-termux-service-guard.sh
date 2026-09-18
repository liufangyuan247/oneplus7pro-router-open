#!/system/bin/sh
set -eu

PREFIX=/data/data/com.termux/files/usr
HOME_DIR=/data/data/com.termux/files/home
STACK="$HOME_DIR/router-stack"
GUARD="$STACK/bin/termux-service-guard"
HEALTHD="$STACK/bin/healthd"
ROUTERCTL="$STACK/bin/routerctl"
KEEPER=/data/adb/service.d/20-oneplus-router.sh
GUARD_SOURCE="${1:-$HOME_DIR/.stage/termux-service-guard}"
KEEPER_SOURCE="${2:-$HOME_DIR/.stage/20-oneplus-router.sh}"
BACKUPS="$HOME_DIR/router-data/backups/service-guard"

[ "$(id -u)" -eq 0 ] || { echo 'deployment requires Magisk root' >&2; exit 1; }
[ -s "$GUARD_SOURCE" ] || { echo "missing guard source: $GUARD_SOURCE" >&2; exit 1; }
[ -s "$KEEPER_SOURCE" ] || { echo "missing keeper source: $KEEPER_SOURCE" >&2; exit 1; }
[ -s "$HEALTHD" ] && [ -s "$ROUTERCTL" ] && [ -s "$KEEPER" ]
"$PREFIX/bin/sh" -n "$GUARD_SOURCE"
/system/bin/sh -n "$KEEPER_SOURCE"

termux_uid="$("$PREFIX/bin/stat" -c '%u' "$HOME_DIR")"
termux_gid="$("$PREFIX/bin/stat" -c '%g' "$HOME_DIR")"
stamp="$(date '+%Y%m%d-%H%M%S')"
backup="$BACKUPS/$stamp"
mkdir -p "$backup"
cp -p "$HEALTHD" "$backup/healthd"
cp -p "$ROUTERCTL" "$backup/routerctl"
cp -p "$KEEPER" "$backup/20-oneplus-router.sh"
[ ! -f "$GUARD" ] || cp -p "$GUARD" "$backup/termux-service-guard"

rollback_required=1
rollback() {
    status=$?
    trap - 0 1 2 15
    [ "$status" -ne 0 ] || exit 0
    [ "$rollback_required" -eq 1 ] || exit "$status"
    set +e
    cp -p "$backup/healthd" "$HEALTHD"
    cp -p "$backup/routerctl" "$ROUTERCTL"
    cp -p "$backup/20-oneplus-router.sh" "$KEEPER"
    if [ -f "$backup/termux-service-guard" ]; then
        cp -p "$backup/termux-service-guard" "$GUARD"
    else
        rm -f "$GUARD"
    fi
    echo "Termux service guard deployment failed; restored $backup" >&2
    exit "$status"
}
trap rollback 0
trap 'exit 1' 1 2 15

cp "$GUARD_SOURCE" "$GUARD.new"
chmod 700 "$GUARD.new"
chown "$termux_uid:$termux_gid" "$GUARD.new"
mv "$GUARD.new" "$GUARD"

if grep -Fq 'refusing to restart $name because its service files are unsafe' "$HEALTHD"; then
    cp -p "$HEALTHD" "$HEALTHD.new"
else
    "$PREFIX/bin/awk" -v guard="$GUARD" '
    /echo "healthcheck: restarting \$name after \$count failed probes"/ && !done {
        print "    if ! \047" guard "\047 audit \"$name\"; then"
        print "      echo \"healthcheck: refusing to restart $name because its service files are unsafe\""
        print "      return"
        print "    fi"
        done=1
    }
    { print }
    END { if (!done) exit 42 }
    ' "$HEALTHD" > "$HEALTHD.new"
fi

if grep -Fq 'Refusing to restart services with unsafe ownership or permissions.' "$ROUTERCTL"; then
    cp -p "$ROUTERCTL" "$ROUTERCTL.new"
else
    "$PREFIX/bin/awk" -v guard="$GUARD" '
    $0 == "  restart)" && !done {
        print
        print "    \047" guard "\047 audit || {"
        print "      echo \047Refusing to restart services with unsafe ownership or permissions.\047 >&2"
        print "      exit 1"
        print "    }"
        done=1
        next
    }
    { print }
    END { if (!done) exit 42 }
    ' "$ROUTERCTL" > "$ROUTERCTL.new"
fi

"$PREFIX/bin/sh" -n "$HEALTHD.new"
"$PREFIX/bin/sh" -n "$ROUTERCTL.new"
chmod 700 "$HEALTHD.new" "$ROUTERCTL.new"
chown "$termux_uid:$termux_gid" "$HEALTHD.new" "$ROUTERCTL.new"
mv "$HEALTHD.new" "$HEALTHD"
mv "$ROUTERCTL.new" "$ROUTERCTL"

cp "$KEEPER_SOURCE" "$KEEPER.new"
/system/bin/sh -n "$KEEPER.new"
chown root:root "$KEEPER.new"
chmod 755 "$KEEPER.new"
mv "$KEEPER.new" "$KEEPER"

mkdir -p "$HOME_DIR/.stage"
cp "$GUARD_SOURCE" "$HOME_DIR/.stage/termux-service-guard.new"
chmod 700 "$HOME_DIR/.stage/termux-service-guard.new"
chown "$termux_uid:$termux_gid" "$HOME_DIR/.stage/termux-service-guard.new"
mv "$HOME_DIR/.stage/termux-service-guard.new" "$HOME_DIR/.stage/termux-service-guard"

"$GUARD" repair
chown -R "$termux_uid:$termux_gid" "$backup"
printf '%s\n' "$backup" > "$HOME_DIR/router-data/last-service-guard-backup.new"
chmod 600 "$HOME_DIR/router-data/last-service-guard-backup.new"
chown "$termux_uid:$termux_gid" "$HOME_DIR/router-data/last-service-guard-backup.new"
mv "$HOME_DIR/router-data/last-service-guard-backup.new" "$HOME_DIR/router-data/last-service-guard-backup"

rollback_required=0
echo "TERMUX_SERVICE_GUARD_DEPLOYED backup=$backup"
