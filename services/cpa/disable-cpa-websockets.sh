#!/data/data/com.termux/files/usr/bin/sh
set -eu

HOME=/data/data/com.termux/files/home
PREFIX=/data/data/com.termux/files/usr
DATA="$HOME/router-data/cpa"
BACKUPS="$HOME/router-data/backups/cpa"

auth_file=$(find "$DATA/auth" -maxdepth 1 -type f -name 'codex-*.json' ! -name '*.bak-*' | head -n 1)
[ -n "$auth_file" ] || {
  echo "No Codex auth file found" >&2
  exit 1
}

mkdir -p "$BACKUPS"
stamp=$(date +%Y%m%d-%H%M%S)
cp -p "$auth_file" "$BACKUPS/$(basename "$auth_file").before-disable-websockets-$stamp"

management_key=$(tr -d '\r\n' < "$DATA/management-key")
auth_name=$(basename "$auth_file")
payload=$(printf '{"name":"%s","websockets":false}' "$auth_name")

"$PREFIX/bin/curl" -fsS \
  -X PATCH \
  -H "X-Management-Key: $management_key" \
  -H 'Content-Type: application/json' \
  --data "$payload" \
  http://127.0.0.1:8317/v0/management/auth-files/fields >/dev/null

if ! grep -q '"websockets"[[:space:]]*:[[:space:]]*false' "$auth_file"; then
  echo "CPA did not persist websockets=false" >&2
  exit 1
fi

echo "CPA_CODEX_WEBSOCKETS_DISABLED"
