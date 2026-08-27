#!/system/bin/sh
set -eu

target=/data/adb/service.d/20-oneplus-router.sh
backup=/data/adb/service.d/20-oneplus-router.sh.before-cpa-20260824

[ -f "$target" ] || {
  echo "router Magisk service not found" >&2
  exit 1
}

if ! grep -q 'cli-proxy-api' "$target"; then
  cp "$target" "$backup"
  sed -i \
    's/for name in runsvdir runsv openlist aria2c qbittorrent-nox caddy sshd router-admin/for name in runsvdir runsv openlist aria2c qbittorrent-nox caddy sshd cli-proxy-api router-admin/' \
    "$target"
fi

chmod 755 "$target"
grep -q 'cli-proxy-api' "$target"

for pid in $(pidof cli-proxy-api 2>/dev/null); do
  echo -500 > "/proc/$pid/oom_score_adj" 2>/dev/null || true
done

echo CPA_OOM_PROTECTION_ENABLED
