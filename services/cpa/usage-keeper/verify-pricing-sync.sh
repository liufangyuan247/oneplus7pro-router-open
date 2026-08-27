#!/data/data/com.termux/files/usr/bin/sh
set -eu

HOME_DIR=/data/data/com.termux/files/home
PASSWORD_FILE="$HOME_DIR/router-data/admin/password"
TMP_DIR="$HOME_DIR/.stage/keeper-pricing-verify.$$"
COOKIE_FILE="$TMP_DIR/cookies"
LOGIN_BODY="$TMP_DIR/login.json"
PREVIEW_BODY="$TMP_DIR/preview.json"

umask 077
mkdir -p "$TMP_DIR"
trap 'rm -rf "$TMP_DIR"' EXIT HUP INT TERM

PASSWORD=$(tr -d '\r\n' < "$PASSWORD_FILE")
ESCAPED_PASSWORD=$(printf '%s' "$PASSWORD" | sed 's/\\/\\\\/g; s/"/\\"/g')
printf '{"password":"%s"}' "$ESCAPED_PASSWORD" > "$LOGIN_BODY"

LOGIN_CODE=$(curl -sS -o /dev/null -w '%{http_code}' -c "$COOKIE_FILE" \
  -H 'Content-Type: application/json' \
  -H 'X-CPA-Usage-Keeper-Request: fetch' \
  --data-binary "@$LOGIN_BODY" \
  http://127.0.0.1:8320/api/v1/auth/login)
test "$LOGIN_CODE" = 204

PREVIEW_CODE=$(curl -sS -o "$PREVIEW_BODY" -w '%{http_code}' -b "$COOKIE_FILE" \
  http://127.0.0.1:8320/api/v1/pricing/sync/preview)
test "$PREVIEW_CODE" = 200
grep -q '"source":"Models.dev"' "$PREVIEW_BODY"

MATCHES=$(grep -o '"match_type"' "$PREVIEW_BODY" | wc -l | tr -d ' ')
UNMATCHED=$(sed -n 's/.*"unmatched_models":\[\([^]]*\)\].*/\1/p' "$PREVIEW_BODY" | \
  awk -F',' '{if ($0 == "") print 0; else print NF}')
printf 'PRICING_PREVIEW_OK matches=%s unmatched=%s\n' "$MATCHES" "${UNMATCHED:-0}"

curl -sS -o /dev/null -b "$COOKIE_FILE" \
  -H 'X-CPA-Usage-Keeper-Request: fetch' -X POST \
  http://127.0.0.1:8320/api/v1/auth/logout || true
