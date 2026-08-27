#!/data/data/com.termux/files/usr/bin/sh
set -eu

HOME=/data/data/com.termux/files/home
PREFIX=/data/data/com.termux/files/usr
TARGET="$HOME/router-stack/bin/healthd"
BACKUP="$TARGET.before-cpa-20260824"

[ -f "$TARGET" ] || {
  echo "healthd not found" >&2
  exit 1
}

if ! grep -q 'probe cpa ' "$TARGET"; then
  cp "$TARGET" "$BACKUP"
  sed -i '/probe caddy curl/a\
  if [ -d "$SERVICES/cpa" ]; then\
    probe cpa curl -fsS --max-time 5 http://127.0.0.1:8317/\
  fi' "$TARGET"
fi

"$PREFIX/bin/sh" -n "$TARGET"
chmod 700 "$TARGET"
"$PREFIX/bin/sv" restart "$HOME/router-stack/services/healthd" >/dev/null
echo CPA_HEALTH_PROBE_ENABLED
