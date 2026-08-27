#!/data/data/com.termux/files/usr/bin/sh
set -eu

PREFIX=/data/data/com.termux/files/usr
HOME_DIR=/data/data/com.termux/files/home
GOROOT="$PREFIX/lib/go"
BUILD_DIR="$HOME_DIR/.build/cpa-usage-keeper-v1.14.8"
ARCHIVE="$HOME_DIR/.stage/cpa-usage-keeper-v1.14.8-android-source.tar.gz"

export PREFIX HOME="$HOME_DIR" GOROOT
export PATH="$GOROOT/bin:$PREFIX/bin:/system/bin"
export CGO_ENABLED=1 GOMAXPROCS=2
export GOPROXY=https://goproxy.cn,direct

"$GOROOT/bin/go" version
test -f "$ARCHIVE"

mkdir -p "$BUILD_DIR"
find "$BUILD_DIR" -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +
tar -xzf "$ARCHIVE" -C "$BUILD_DIR"
cd "$BUILD_DIR"

nice -n 15 "$GOROOT/bin/go" build -trimpath \
  -ldflags="-s -w -X cpa-usage-keeper/internal/version.Version=v1.14.8-android" \
  -o cpa-usage-keeper ./cmd/server/main.go

chmod 0755 cpa-usage-keeper
sha256sum cpa-usage-keeper
file cpa-usage-keeper
echo CPA_USAGE_KEEPER_ANDROID_BUILD_OK
