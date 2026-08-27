#!/data/data/com.termux/files/usr/bin/sh
set -eu

PREFIX=/data/data/com.termux/files/usr
HOME=/data/data/com.termux/files/home
STACK="$HOME/router-stack"
DATA="$HOME/router-data/cpa"
SERVICE="$STACK/services/cpa"
STAGE="$HOME/.stage"
BIN="$PREFIX/bin/cli-proxy-api"

random_hex() {
  od -An -N "$1" -tx1 /dev/urandom | tr -d ' \r\n'
}

[ -f "$STAGE/cli-proxy-api" ] || {
  echo "missing staged cli-proxy-api binary" >&2
  exit 1
}

mkdir -p "$DATA/auth" "$SERVICE/log/main"
chmod 700 "$DATA" "$DATA/auth"

cp "$STAGE/cli-proxy-api" "$BIN.new"
chmod 700 "$BIN.new"
mv "$BIN.new" "$BIN"

if [ ! -s "$DATA/management-key" ]; then
  if [ -s "$HOME/router-data/admin/password" ]; then
    cp "$HOME/router-data/admin/password" "$DATA/management-key"
  else
    printf 'OP7-CPA-%s\n' "$(random_hex 18)" > "$DATA/management-key"
  fi
  chmod 600 "$DATA/management-key"
fi

if [ ! -s "$DATA/api-key" ] || [ "$(tr -d '\r\n' < "$DATA/api-key")" = "op7-cpa-" ]; then
  printf 'op7-cpa-%s\n' "$(random_hex 24)" > "$DATA/api-key"
  chmod 600 "$DATA/api-key"
fi

if [ ! -s "$DATA/config.yaml" ]; then
  management_key=$(tr -d '\r\n' < "$DATA/management-key")
  api_key=$(tr -d '\r\n' < "$DATA/api-key")
  umask 077
  cat > "$DATA/config.yaml" <<EOF
host: "0.0.0.0"
port: 8317

tls:
  enable: false
  cert: ""
  key: ""

remote-management:
  allow-remote: true
  secret-key: "$management_key"
  disable-control-panel: false
  disable-auto-update-panel: false
  panel-github-repository: "https://github.com/router-for-me/Cli-Proxy-API-Management-Center"

auth-dir: "$DATA/auth"

api-keys:
  - "$api_key"

debug: false
pprof:
  enable: false
  addr: "127.0.0.1:8316"
plugins:
  enabled: false
  dir: "$DATA/plugins"
commercial-mode: false
logging-to-file: true
request-log: true
logs-max-total-size-mb: 128
error-logs-max-files: 20
usage-statistics-enabled: false
proxy-url: ""
EOF
  chmod 600 "$DATA/config.yaml"
elif grep -q '^  - "op7-cpa-"$' "$DATA/config.yaml"; then
  api_key=$(tr -d '\r\n' < "$DATA/api-key")
  sed -i "s|^  - \"op7-cpa-\"$|  - \"$api_key\"|" "$DATA/config.yaml"
fi

cat > "$SERVICE/run" <<EOF
#!/data/data/com.termux/files/usr/bin/sh
exec 2>&1
export HOME='$HOME' PREFIX='$PREFIX' PATH='$PREFIX/bin'
export TMPDIR='$PREFIX/tmp'
cd '$DATA'
exec '$BIN' -config '$DATA/config.yaml'
EOF

cat > "$SERVICE/log/run" <<EOF
#!/data/data/com.termux/files/usr/bin/sh
exec '$PREFIX/bin/svlogd' -tt '$SERVICE/log/main'
EOF

cat > "$SERVICE/log/main/config" <<'EOF'
s1048576
n5
EOF

cat > "$SERVICE/finish" <<'EOF'
#!/data/data/com.termux/files/usr/bin/sh
sleep 3
EOF

chmod 700 "$SERVICE/run" "$SERVICE/finish" "$SERVICE/log/run"
rm -f "$SERVICE/down"
rm -f "$STAGE/cli-proxy-api"

"$PREFIX/bin/sv" restart "$SERVICE" >/dev/null 2>&1 || true
echo CPA_INSTALLED
