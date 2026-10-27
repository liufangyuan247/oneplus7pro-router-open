#!/data/data/com.termux/files/usr/bin/sh
set -eu

PREFIX=/data/data/com.termux/files/usr
HOME_DIR=/data/data/com.termux/files/home
BUILD_DIR="$HOME_DIR/.build/cpa-usage-keeper-v2.0.2"
EXPECTED_BUILD_DIR=/data/data/com.termux/files/home/.build/cpa-usage-keeper-v2.0.2
INSTALLED_BIN="$PREFIX/bin/cpa-usage-keeper"
BUILD_INFO="$HOME_DIR/router-data/cpa-usage-keeper/build-info.json"

export HOME="$HOME_DIR" PREFIX PATH="$PREFIX/bin:/system/bin"

test -x "$INSTALLED_BIN"
test -s "$BUILD_INFO"
EXPECTED_SHA256=$("$PREFIX/bin/jq" -r '.sha256 // empty' "$BUILD_INFO")
test -n "$EXPECTED_SHA256"
ACTUAL_SHA256=$(sha256sum "$INSTALLED_BIN" | awk '{print $1}')
test "$ACTUAL_SHA256" = "$EXPECTED_SHA256"
curl -fsS --max-time 3 http://127.0.0.1:8320/healthz >/dev/null
curl -fsS --max-time 3 http://127.0.0.1:8320/readyz >/dev/null
curl -fsS --max-time 3 http://127.0.0.1:8320/ | grep -qi '<!doctype html'

if test -d "$BUILD_DIR"; then
  RESOLVED_BUILD_DIR=$(realpath "$BUILD_DIR")
  test "$RESOLVED_BUILD_DIR" = "$EXPECTED_BUILD_DIR"
fi

if test -x "$PREFIX/lib/go/bin/go"; then
  env -u LD_PRELOAD -u TERMUX_EXEC__PROC_SELF_EXE /system/bin/sh -c \
    "HOME='$HOME_DIR' GOROOT='$PREFIX/lib/go' PATH='$PREFIX/lib/go/bin:$PREFIX/bin:/system/bin' '$PREFIX/lib/go/bin/go' clean -cache -modcache"
fi

pkg uninstall -y golang

if test -d "$BUILD_DIR"; then
  find "$BUILD_DIR" -depth -delete
fi
rm -f \
  "$HOME_DIR/.stage/cpa-usage-keeper-v2.0.2-android-source.tar.gz" \
  "$HOME_DIR/.stage/cpa-usage-keeper-v2.0.2-android" \
  "$HOME_DIR/.stage/cpa-usage-keeper-v2.0.2-android.build.json" \
  "$HOME_DIR/.stage/build-on-android.sh" \
  "$HOME_DIR/.stage/install-cpa-usage-keeper.sh"

curl -fsS --max-time 3 http://127.0.0.1:8320/healthz >/dev/null
curl -fsS --max-time 3 http://127.0.0.1:8320/ | grep -qi '<!doctype html'
echo CPA_USAGE_KEEPER_BUILD_ENV_CLEANED
