#!/data/data/com.termux/files/usr/bin/sh
set -eu
umask 077

# Prepare an isolated, upstream SearXNG release. Does not start any service.
HOME_DIR="${TERMUX_HOME:-/data/data/com.termux/files/home}"
PREFIX="${TERMUX_PREFIX:-/data/data/com.termux/files/usr}"
export HOME="$HOME_DIR" PREFIX PATH="$PREFIX/bin:/system/bin"
STAGE="$HOME/.stage/searxng-install"
APP="$HOME/router-apps/searxng"
REV=ffa88e3ba3ade64185d5754c839cdf5a80708c4a
RELEASE="$APP/releases/$REV"

[ "$#" -eq 1 ]
[ "$(sha256sum "$STAGE/source.tar.gz" | cut -d ' ' -f1)" = "$1" ]
apt-get install -y --no-upgrade libxslt
mkdir -p "$RELEASE"
tar -xzf "$STAGE/source.tar.gz" --strip-components=1 -C "$RELEASE"
python -m venv "$RELEASE/venv"
export MAKEFLAGS=-j1 MAX_JOBS=1 CFLAGS=-O1
export HTTP_PROXY=http://127.0.0.1:17890 HTTPS_PROXY=http://127.0.0.1:17890
export NO_PROXY=127.0.0.1,localhost
"$RELEASE/venv/bin/python" -m pip install --disable-pip-version-check \
  -r "$RELEASE/requirements.txt" -r "$STAGE/requirements.lock.txt"
printf '%s\n' "$REV" > "$RELEASE/UPSTREAM_COMMIT"
printf '%s\n' '2026.10.30' > "$RELEASE/UPSTREAM_VERSION"
# Upstream supports a generated version_frozen module for source archives.
cat > "$RELEASE/searx/version_frozen.py" <<'PY'
VERSION_STRING = '2026.10.30+ffa88e3ba3ad'
VERSION_TAG = VERSION_STRING
DOCKER_TAG = '2026.10.30-ffa88e3ba3ad'
GIT_URL = 'https://github.com/searxng/searxng'
GIT_BRANCH = 'master'
PY
"$RELEASE/venv/bin/python" -m pip check
echo SEARXNG_RUNTIME_READY
