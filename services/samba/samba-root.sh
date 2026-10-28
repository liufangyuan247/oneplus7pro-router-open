#!/system/bin/sh
set -eu
BASE=/data/adb/router-samba
CONFIG="$BASE/smb.conf"
PIDFILE="$BASE/supervised.pid"
export HOME=/data/data/com.termux/files/home PREFIX=/data/data/com.termux/files/usr
export PATH="$PREFIX/bin:/system/bin" TMPDIR="$PREFIX/tmp"
matches() {
    case "$1" in ''|*[!0-9]*) return 1 ;; esac
    [ -r "/proc/$1/cmdline" ] || return 1
    /system/bin/timeout 2 /system/bin/tr '\000' ' ' < "/proc/$1/cmdline" 2>/dev/null | grep -Fq "$CONFIG"
}
case "${1:-run}" in
    stop)
        pid=$(cat "$PIDFILE" 2>/dev/null || true)
        if matches "$pid"; then kill -TERM "$pid" 2>/dev/null || true; fi
        exit 0
        ;;
    run) ;;
    *) exit 2 ;;
esac
[ -s "$CONFIG" ]
old=$(cat "$PIDFILE" 2>/dev/null || true)
if matches "$old"; then echo 'Samba is already running' >&2; exit 1; fi
umask 077
echo $$ > "$PIDFILE"
exec "$PREFIX/bin/smbd" --foreground --no-process-group --debug-stdout --configfile="$CONFIG"
