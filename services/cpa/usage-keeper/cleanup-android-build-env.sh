#!/data/data/com.termux/files/usr/bin/sh
set -eu

PREFIX=/data/data/com.termux/files/usr
HOME_DIR=/data/data/com.termux/files/home
BUILD_DIR="$HOME_DIR/.build/cpa-usage-keeper-v1.14.8"
EXPECTED_BUILD_DIR=/data/data/com.termux/files/home/.build/cpa-usage-keeper-v1.14.8
INSTALLED_BIN="$PREFIX/bin/cpa-usage-keeper"
EXPECTED_SHA256=4cbd3ba408f10b899c35db96134ac56e4b3a8b429d507269bd40bfa765f2a052

export HOME="$HOME_DIR" PREFIX PATH="$PREFIX/bin:/system/bin"

test -x "$INSTALLED_BIN"
ACTUAL_SHA256=$(sha256sum "$INSTALLED_BIN" | awk '{print $1}')
test "$ACTUAL_SHA256" = "$EXPECTED_SHA256"
curl -fsS --max-time 3 http://127.0.0.1:8320/healthz >/dev/null

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
  "$HOME_DIR/.stage/cpa-usage-keeper-v1.14.8-android-source.tar.gz" \
  "$HOME_DIR/.stage/cpa-usage-keeper-v1.14.8-android" \
  "$HOME_DIR/.stage/build-on-android.sh" \
  "$HOME_DIR/.stage/install-cpa-usage-keeper.sh"

curl -fsS --max-time 3 http://127.0.0.1:8320/healthz >/dev/null
echo CPA_USAGE_KEEPER_BUILD_ENV_CLEANED
