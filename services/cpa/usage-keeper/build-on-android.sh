#!/data/data/com.termux/files/usr/bin/sh
set -eu

PREFIX=/data/data/com.termux/files/usr
HOME_DIR=/data/data/com.termux/files/home
GOROOT="$PREFIX/lib/go"
BUILD_DIR="$HOME_DIR/.build/cpa-usage-keeper-v2.0.2"
ARCHIVE="$HOME_DIR/.stage/cpa-usage-keeper-v2.0.2-android-source.tar.gz"
UPSTREAM_VERSION=v2.0.2
UPSTREAM_COMMIT=46fe6278db0c47a5460e882fa1742f15ab49bb14
CUSTOM_VERSION=v2.0.2-android
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
test "$(realpath "$BUILD_DIR")" = "$HOME_DIR/.build/cpa-usage-keeper-v2.0.2"
find "$BUILD_DIR" -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +
tar -xzf "$ARCHIVE" -C "$BUILD_DIR"
cd "$BUILD_DIR"

test -s web/dist/index.html || {
  echo "prepared source archive has no built web/dist/index.html" >&2
  echo "run prepare-android-source.ps1 before staging the archive" >&2
  exit 1
}

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
