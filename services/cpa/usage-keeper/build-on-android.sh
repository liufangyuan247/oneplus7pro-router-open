#!/data/data/com.termux/files/usr/bin/sh
set -eu

PREFIX=/data/data/com.termux/files/usr
HOME_DIR=/data/data/com.termux/files/home
GOROOT="$PREFIX/lib/go"
BUILD_DIR="$HOME_DIR/.build/cpa-usage-keeper-v1.15.11"
ARCHIVE="$HOME_DIR/.stage/cpa-usage-keeper-v1.15.11-android-source.tar.gz"
UPSTREAM_VERSION=v1.15.11
UPSTREAM_COMMIT=71a41cc2d1cce8d75b42473adf31f2616ad8085e
CUSTOM_VERSION=v1.15.11-android
UPSTREAM_REPOSITORY=Willxup/cpa-usage-keeper

export PREFIX HOME="$HOME_DIR" GOROOT
export PATH="$GOROOT/bin:$PREFIX/bin:/system/bin"
export CGO_ENABLED=1 GOMAXPROCS=2
export GOPROXY=https://goproxy.cn,direct

run_android_binary() {
  env -u LD_PRELOAD -u TERMUX_EXEC__PROC_SELF_EXE /system/bin/sh -c 'exec "$@"' sh "$@"
}

run_android_binary "$GOROOT/bin/go" version
test -f "$ARCHIVE"

mkdir -p "$BUILD_DIR"
find "$BUILD_DIR" -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +
tar -xzf "$ARCHIVE" -C "$BUILD_DIR"
cd "$BUILD_DIR"

run_android_binary "$GOROOT/bin/go" build -trimpath \
  -ldflags="-s -w -X cpa-usage-keeper/internal/version.Version=$CUSTOM_VERSION" \
  -o cpa-usage-keeper ./cmd/server/main.go

chmod 0755 cpa-usage-keeper
artifact_sha="$(sha256sum cpa-usage-keeper | awk '{print $1}')"
build_date="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
cat > cpa-usage-keeper.build.json <<EOF
{
  "schemaVersion": 1,
  "service": "cpa-usage-keeper",
  "product": "CPA Usage Keeper",
  "customVersion": "$CUSTOM_VERSION",
  "upstreamVersion": "$UPSTREAM_VERSION",
  "upstreamCommit": "$UPSTREAM_COMMIT",
  "upstreamRepository": "$UPSTREAM_REPOSITORY",
  "variant": "op7-android",
  "buildDate": "$build_date",
  "platform": "android/arm64",
  "sha256": "$artifact_sha",
  "patches": []
}
EOF
chmod 0644 cpa-usage-keeper.build.json
printf '%s  %s\n' "$artifact_sha" cpa-usage-keeper
file cpa-usage-keeper
echo CPA_USAGE_KEEPER_ANDROID_BUILD_OK
