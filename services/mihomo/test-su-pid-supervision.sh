#!/data/data/com.termux/files/usr/bin/sh
set -u
PREFIX=/data/data/com.termux/files/usr
PIDFILE=/data/data/com.termux/files/home/.stage/su-pid-test.pid
SU_PID=''
cleanup() {
  pid="$(cat "$PIDFILE" 2>/dev/null || true)"
  case "$pid" in ''|*[!0-9]*) ;; *) "$PREFIX/bin/su" -c "kill -TERM $pid" >/dev/null 2>&1 || true ;; esac
  [ -z "$SU_PID" ] || kill "$SU_PID" >/dev/null 2>&1 || true
  rm -f "$PIDFILE"
}
trap cleanup EXIT INT TERM
"$PREFIX/bin/su" -c "echo \$\$ > '$PIDFILE'; exec /system/bin/sleep 60" &
SU_PID=$!
attempt=0
while [ ! -s "$PIDFILE" ] && [ "$attempt" -lt 10 ]; do sleep 1; attempt=$((attempt + 1)); done
root_pid="$(cat "$PIDFILE" 2>/dev/null || true)"
case "$root_pid" in ''|*[!0-9]*) echo PIDFILE_FAILED; exit 1 ;; esac
cmdline="$($PREFIX/bin/su -c "tr '\\000' ' ' < /proc/$root_pid/cmdline" 2>/dev/null || true)"
echo "PIDFILE_READY root_pid=$root_pid su_pid=$SU_PID cmdline=$cmdline"
