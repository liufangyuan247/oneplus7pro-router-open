#!/data/data/com.termux/files/usr/bin/sh
set -eu

STATE=/data/data/com.termux/files/home/router-data/power-test
PIDS="$STATE/cpu-stress.pids"
PREFIX=/data/data/com.termux/files/usr

mkdir -p "$STATE"
if [ -s "$PIDS" ]; then
  while IFS= read -r pid; do kill "$pid" >/dev/null 2>&1 || true; done < "$PIDS"
fi
: > "$PIDS"

i=0
while [ "$i" -lt 8 ]; do
  "$PREFIX/bin/nohup" "$PREFIX/bin/yes" >/dev/null 2>&1 &
  echo $! >> "$PIDS"
  i=$((i + 1))
done

echo "CPU_STRESS_STARTED $(tr '\n' ' ' < "$PIDS")"
