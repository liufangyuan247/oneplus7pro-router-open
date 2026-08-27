#!/data/data/com.termux/files/usr/bin/sh
set -eu

PIDS=/data/data/com.termux/files/home/router-data/power-test/cpu-stress.pids

if [ -s "$PIDS" ]; then
  while IFS= read -r pid; do kill "$pid" >/dev/null 2>&1 || true; done < "$PIDS"
  : > "$PIDS"
fi

pkill -x yes >/dev/null 2>&1 || true
echo CPU_STRESS_STOPPED
