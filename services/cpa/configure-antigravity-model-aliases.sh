#!/data/data/com.termux/files/usr/bin/sh
set -eu

PREFIX=/data/data/com.termux/files/usr
TERMUX_HOME=/data/data/com.termux/files/home
CONFIG="$TERMUX_HOME/router-data/cpa/config.yaml"
KEY_FILE="$TERMUX_HOME/router-data/cpa/api-key"
SERVICE="$TERMUX_HOME/router-stack/services/cpa"
BACKUPS="$TERMUX_HOME/router-data/backups/cpa"
CURL="$PREFIX/bin/curl"
JQ="$PREFIX/bin/jq"
MODEL=gemini-3.8-flash
TARGET=gemini-3.8-flash-high

[ "$(id -u)" = 10207 ] || { echo 'run as the Termux service user' >&2; exit 1; }
[ -s "$CONFIG" ] || { echo "CPA config is missing: $CONFIG" >&2; exit 1; }
[ -s "$KEY_FILE" ] || { echo "CPA API key is missing: $KEY_FILE" >&2; exit 1; }
for binary in "$CURL" "$JQ" "$PREFIX/bin/awk" "$PREFIX/bin/sv"; do
  [ -x "$binary" ] || { echo "required binary is missing: $binary" >&2; exit 1; }
done

STAMP="$(date '+%Y%m%d-%H%M%S')"
BACKUP="$BACKUPS/config.yaml.before-$MODEL-$STAMP"
TMP_CONFIG="$CONFIG.new.$$"
TMP_RESPONSE="$PREFIX/tmp/cpa-$MODEL-probe.$$"
changed=0
committed=0

cleanup() {
  rm -f "$TMP_CONFIG" "$TMP_RESPONSE"
}

rollback() {
  cleanup
  if [ "$changed" = 1 ] && [ "$committed" = 0 ]; then
    cp -p "$BACKUP" "$CONFIG.restore.$$"
    mv -f "$CONFIG.restore.$$" "$CONFIG"
    "$PREFIX/bin/sv" restart "$SERVICE" >/dev/null 2>&1 || true
  fi
}
trap rollback EXIT INT TERM

if ! "$PREFIX/bin/awk" -v model="$MODEL" -v target="$TARGET" '
  $0 == "    - name: " target { in_target = 1; next }
  in_target && $0 == "      alias: " model { found = 1 }
  in_target && $0 ~ /^    - name: / { in_target = 0 }
  END { exit(found ? 0 : 1) }
' "$CONFIG"; then
  if grep -Fq "      alias: $MODEL" "$CONFIG"; then
    echo "refusing to replace an existing alias with an unexpected target: $MODEL" >&2
    exit 1
  fi

  mkdir -p "$BACKUPS"
  chmod 700 "$BACKUPS"
  cp -p "$CONFIG" "$BACKUP"

  if grep -q '^  antigravity:$' "$CONFIG"; then
    "$PREFIX/bin/awk" -v model="$MODEL" -v target="$TARGET" '
      { print }
      !done && $0 == "  antigravity:" {
        print "    - name: " target
        print "      alias: " model
        print "      fork: true"
        print "      force-mapping: true"
        done = 1
      }
      END { if (!done) exit 42 }
    ' "$CONFIG" > "$TMP_CONFIG"
  elif grep -q '^oauth-model-alias:$' "$CONFIG"; then
    "$PREFIX/bin/awk" -v model="$MODEL" -v target="$TARGET" '
      { print }
      !done && $0 == "oauth-model-alias:" {
        print "  antigravity:"
        print "    - name: " target
        print "      alias: " model
        print "      fork: true"
        print "      force-mapping: true"
        done = 1
      }
      END { if (!done) exit 42 }
    ' "$CONFIG" > "$TMP_CONFIG"
  else
    cp "$CONFIG" "$TMP_CONFIG"
    {
      printf '\n%s\n' 'oauth-model-alias:'
      printf '%s\n' '  antigravity:'
      printf '    - name: %s\n' "$TARGET"
      printf '      alias: %s\n' "$MODEL"
      printf '%s\n' '      fork: true'
      printf '%s\n' '      force-mapping: true'
    } >> "$TMP_CONFIG"
  fi

  chmod 600 "$TMP_CONFIG"
  "$PREFIX/bin/awk" -v model="$MODEL" -v target="$TARGET" '
    $0 == "    - name: " target { in_target = 1; next }
    in_target && $0 == "      alias: " model { found = 1 }
    in_target && $0 ~ /^    - name: / { in_target = 0 }
    END { exit(found ? 0 : 1) }
  ' "$TMP_CONFIG"
  mv -f "$TMP_CONFIG" "$CONFIG"
  changed=1
fi

probe_model() {
  key="$(tr -d '\r\n' < "$KEY_FILE")"
  code="$($CURL -sS --connect-timeout 8 --max-time 90 \
    --output "$TMP_RESPONSE" --write-out '%{http_code}' \
    -H "x-goog-api-key: $key" \
    -H 'Content-Type: application/json' \
    --data '{"contents":[{"role":"user","parts":[{"text":"Reply only with OK."}]}],"generationConfig":{"maxOutputTokens":8}}' \
    "http://127.0.0.1:8317/v1beta/models/$MODEL:streamGenerateContent?alt=sse" || true)"
  unset key
  [ "$code" = 200 ] && grep -q '^data:' "$TMP_RESPONSE"
}

reload=unchanged
if [ "$changed" = 1 ]; then
  reload=hot
  sleep 2
fi
if ! probe_model; then
  reload=restart
  "$PREFIX/bin/sv" restart "$SERVICE" >/dev/null
  for delay in 1 2 3 4 5; do
    sleep "$delay"
    "$CURL" -fsS --max-time 3 http://127.0.0.1:8317/ >/dev/null 2>&1 && break
  done
  probe_model || {
    message="$($JQ -r '.error.message // "unknown error"' "$TMP_RESPONSE" 2>/dev/null || printf 'unknown error')"
    echo "CPA model alias probe failed: HTTP ${code:-000}: $message" >&2
    exit 1
  }
fi

committed=1
trap cleanup EXIT INT TERM
printf 'CPA_ANTIGRAVITY_ALIAS_READY alias=%s target=%s http=200 reload=%s' "$MODEL" "$TARGET" "$reload"
if [ "$changed" = 1 ]; then
  printf ' backup=%s' "$BACKUP"
fi
printf '\n'
