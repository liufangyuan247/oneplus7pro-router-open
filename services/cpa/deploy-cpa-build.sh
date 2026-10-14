#!/data/data/com.termux/files/usr/bin/sh
set -eu

PREFIX=/data/data/com.termux/files/usr
HOME_DIR=/data/data/com.termux/files/home
SERVICE="$HOME_DIR/router-stack/services/cpa"
DATA="$HOME_DIR/router-data/cpa"
STAGE="$HOME_DIR/.stage"
LIVE="$PREFIX/bin/cli-proxy-api"
CANDIDATE="$STAGE/cli-proxy-api"
CANDIDATE_INFO="$STAGE/cli-proxy-api.build.json"
LIVE_INFO="$DATA/build-info.json"
KEY_FILE="$DATA/api-key"
BACKUPS="$HOME_DIR/router-data/backups/cpa"
# Use a known account capability for deployment health. Callers can override
# this when validating a newly granted model, without making a staged rollout
# look like a broken binary when the account has not received that model yet.
MODEL="${CPA_DEPLOY_PROBE_MODEL:-gemini-3.8-flash-high}"

fail() { echo "ERROR: $*" >&2; exit 1; }

[ -s "$CANDIDATE" ] || fail "candidate binary is missing: $CANDIDATE"
[ -s "$CANDIDATE_INFO" ] || fail "candidate build manifest is missing: $CANDIDATE_INFO"
[ -s "$KEY_FILE" ] || fail "CPA API key is missing: $KEY_FILE"

expected_sha="$($PREFIX/bin/jq -r '.sha256 // empty' "$CANDIDATE_INFO")"
custom_version="$($PREFIX/bin/jq -r '.customVersion // empty' "$CANDIDATE_INFO")"
upstream_version="$($PREFIX/bin/jq -r '.upstreamVersion // empty' "$CANDIDATE_INFO")"
[ -n "$expected_sha" ] || fail "build manifest has no sha256"
[ -n "$custom_version" ] || fail "build manifest has no customVersion"
[ -n "$upstream_version" ] || fail "build manifest has no upstreamVersion"

actual_sha="$(sha256sum "$CANDIDATE" | awk '{print $1}')"
[ "$actual_sha" = "$expected_sha" ] || fail "candidate SHA256 mismatch: $actual_sha"

stamp="$(date '+%Y%m%d-%H%M%S')"
backup="$BACKUPS/$stamp-$custom_version"
mkdir -p "$backup"
cp -p "$LIVE" "$backup/cli-proxy-api"
had_info=0
if [ -f "$LIVE_INFO" ]; then
  cp -p "$LIVE_INFO" "$backup/build-info.json"
  had_info=1
fi

live_owner="$(stat -c '%u:%g' "$LIVE")"
committed=0
response="$PREFIX/tmp/cpa-deploy-probe.$$"

rollback() {
  rm -f "$response"
  [ "$committed" = 0 ] || return 0
  cp -p "$backup/cli-proxy-api" "$LIVE.new"
  chown "$live_owner" "$LIVE.new"
  chmod 700 "$LIVE.new"
  mv -f "$LIVE.new" "$LIVE"
  if [ "$had_info" = 1 ]; then
    cp -p "$backup/build-info.json" "$LIVE_INFO.new"
    chown "$live_owner" "$LIVE_INFO.new"
    chmod 600 "$LIVE_INFO.new"
    mv -f "$LIVE_INFO.new" "$LIVE_INFO"
  else
    rm -f "$LIVE_INFO"
  fi
  "$PREFIX/bin/sv" restart "$SERVICE" >/dev/null 2>&1 || true
}
trap rollback EXIT INT TERM

cp "$CANDIDATE" "$LIVE.new"
chown "$live_owner" "$LIVE.new"
chmod 700 "$LIVE.new"
mv -f "$LIVE.new" "$LIVE"
cp "$CANDIDATE_INFO" "$LIVE_INFO.new"
chown "$live_owner" "$LIVE_INFO.new"
chmod 600 "$LIVE_INFO.new"
mv -f "$LIVE_INFO.new" "$LIVE_INFO"
"$PREFIX/bin/sv" restart "$SERVICE" >/dev/null 2>&1 || true

ready=0
attempt=0
while [ "$attempt" -lt 25 ]; do
  if "$PREFIX/bin/curl" -fsS --max-time 2 http://127.0.0.1:8317/ >/dev/null 2>&1; then
    ready=1
    break
  fi
  attempt=$((attempt + 1))
  sleep 1
done
[ "$ready" = 1 ] || fail "CPA failed its HTTP health check; restoring $backup"

version="$($LIVE --version 2>&1 | sed -n '1p')"
case "$version" in
  *"$custom_version"*) ;;
  *) fail "CPA version verification failed: $version" ;;
esac

key="$(tr -d '\r\n' < "$KEY_FILE")"
probe_code=000
attempt=0
while [ "$attempt" -lt 8 ]; do
  probe_code="$($PREFIX/bin/curl -sS --connect-timeout 8 --max-time 90 \
    --output "$response" --write-out '%{http_code}' \
    -H "x-goog-api-key: $key" -H 'Content-Type: application/json' \
    --data '{"contents":[{"role":"user","parts":[{"text":"Reply only with OK."}]}],"generationConfig":{"maxOutputTokens":8}}' \
    "http://127.0.0.1:8317/v1beta/models/$MODEL:streamGenerateContent?alt=sse" || true)"
  if [ "$probe_code" = 200 ] && grep -q '^data:' "$response"; then
    break
  fi
  attempt=$((attempt + 1))
  sleep 2
done
unset key
if [ "$probe_code" != 200 ] || ! grep -q '^data:' "$response"; then
  message="$($PREFIX/bin/jq -r '.error.message // "unknown error"' "$response" 2>/dev/null || printf 'unknown error')"
  fail "$MODEL probe failed: HTTP $probe_code: $message"
fi

committed=1
trap - EXIT INT TERM
rm -f "$response" "$CANDIDATE" "$CANDIDATE_INFO"
pid="$(pidof cli-proxy-api)"
printf 'CPA_BUILD_DEPLOYED pid=%s version=%s upstream=%s sha256=%s probe_model=%s backup=%s\n' \
  "$pid" "$custom_version" "$upstream_version" "$actual_sha" "$MODEL" "$backup"
