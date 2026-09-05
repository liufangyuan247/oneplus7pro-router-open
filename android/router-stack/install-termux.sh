#!/data/data/com.termux/files/usr/bin/sh
set -eu

PREFIX=/data/data/com.termux/files/usr
HOME=/data/data/com.termux/files/home
STACK="$HOME/router-stack"
DATA="$HOME/router-data"
SERVICES="$STACK/services"

export PREFIX HOME PATH="$PREFIX/bin:$PREFIX/bin/applets"
export TMPDIR="$PREFIX/tmp"

mkdir -p "$STACK/bin" "$STACK/www/ariang" "$SERVICES" \
  "$DATA/downloads" "$DATA/openlist" "$DATA/aria2" \
  "$DATA/qbittorrent/qBittorrent/config" "$DATA/caddy"

ARIA_CONF="$DATA/aria2/aria2.conf"
ARIA_SECRET=$(sed -n 's/^rpc-secret=//p' "$ARIA_CONF" 2>/dev/null | head -n 1 || true)
ARIA_SECRET_B64=$(printf '%s' "$ARIA_SECRET" | base64 | tr -d '\r\n')

# Google Public DNS is unreliable on this uplink. Keep Termux-native package
# tools and daemons on the same reachable resolvers used by router dnsmasq.
cat > "$PREFIX/etc/resolv.conf" <<'EOF'
nameserver 127.0.0.1
nameserver ::1
EOF

# AriaNg is staged by the desktop deployment script.
if [ -f "$HOME/.stage/AriaNg-1.3.14-AllInOne.zip" ]; then
  unzip -oq "$HOME/.stage/AriaNg-1.3.14-AllInOne.zip" -d "$STACK/www/ariang"
fi

if [ ! -f "$DATA/aria2/aria2.session" ]; then
  : > "$DATA/aria2/aria2.session"
fi

if [ ! -f "$ARIA_CONF" ]; then
  cat > "$ARIA_CONF" <<EOF
dir=$DATA/downloads
continue=true
file-allocation=none
max-connection-per-server=8
min-split-size=20M
split=8
disk-cache=128M
enable-rpc=true
rpc-listen-all=true
rpc-listen-port=6800
rpc-allow-origin-all=true
input-file=$DATA/aria2/aria2.session
save-session=$DATA/aria2/aria2.session
save-session-interval=60
auto-save-interval=60
save-not-found=true
max-tries=0
retry-wait=10
bt-save-metadata=true
bt-enable-lpd=false
enable-dht=true
enable-dht6=false
enable-peer-exchange=true
seed-time=0
log=-
log-level=notice
console-log-level=notice
EOF
fi

# AriaNg changes global aria2 options through RPC. aria2 normally keeps those
# changes only in memory, so mirror changed runtime options back to the
# persistent config without imposing our own values. Connection-critical RPC
# settings remain owned by the stack so the management endpoint cannot be
# accidentally disconnected.
cat > "$STACK/bin/aria2-config-sync" <<'EOF'
#!/data/data/com.termux/files/usr/bin/sh
set -eu

PREFIX=/data/data/com.termux/files/usr
HOME=/data/data/com.termux/files/home
CONF="$HOME/router-data/aria2/aria2.conf"
STATE="$HOME/router-data/aria2/runtime-options.json"
TMP="$HOME/router-data/aria2/.aria2.conf.sync.$$"
PATH="$PREFIX/bin"

cleanup() {
  rm -f "$TMP"
}
trap cleanup EXIT
trap 'exit 0' HUP INT TERM

secret=$(sed -n 's/^rpc-secret=//p' "$CONF" | head -n 1)
auth=
[ -n "$secret" ] && auth="\"token:$secret\""

fetch_options() {
  curl -fsS --max-time 5 \
    -H 'Content-Type: application/json' \
    -d "{\"jsonrpc\":\"2.0\",\"id\":\"persist\",\"method\":\"aria2.getGlobalOption\",\"params\":[$auth]}" \
    http://127.0.0.1:6800/jsonrpc | jq -c '.result // empty'
}

set_config_value() {
  key="$1"
  value="$2"
  awk -v key="$key" -v value="$value" '
    BEGIN { prefix = key "="; found = 0 }
    index($0, prefix) == 1 {
      if (!found) print prefix value
      found = 1
      next
    }
    { print }
    END { if (!found) print prefix value }
  ' "$CONF" > "$TMP"
  chmod 600 "$TMP"
  mv "$TMP" "$CONF"
}

while :; do
  current=$(fetch_options 2>/dev/null || true)
  if [ -z "$current" ]; then
    sleep 15
    continue
  fi

  # The first observation is only a baseline. This is important for defaults:
  # an option absent from aria2.conf stays absent until the user changes it.
  if [ ! -s "$STATE" ]; then
    printf '%s\n' "$current" > "$STATE.new"
    chmod 600 "$STATE.new"
    mv "$STATE.new" "$STATE"
    sleep 15
    continue
  fi

  previous=$(cat "$STATE" 2>/dev/null || printf '{}')
  printf '%s\n' "$current" | jq -r --argjson old "$previous" '
    ["conf-path", "daemon", "enable-rpc", "help", "human-readable",
     "log", "quiet", "rpc-allow-origin-all", "rpc-listen-all",
     "rpc-listen-port", "rpc-max-request-size", "rpc-save-upload-metadata",
     "rpc-secure", "save-session", "show-console-readout", "show-files",
     "stderr", "stop"] as $locked
    | to_entries[]
    | . as $item
    | select(($locked | index($item.key)) == null)
    | select(($old[$item.key] // null) != $item.value)
    | [$item.key, $item.value] | @tsv
  ' | while IFS="$(printf '\t')" read -r key value; do
    [ -n "$key" ] && set_config_value "$key" "$value"
  done

  printf '%s\n' "$current" > "$STATE.new"
  chmod 600 "$STATE.new"
  mv "$STATE.new" "$STATE"
  sleep 15
done
EOF
chmod 700 "$STACK/bin/aria2-config-sync"

QBT_CONF="$DATA/qbittorrent/qBittorrent/config/qBittorrent.conf"
if [ ! -f "$QBT_CONF" ]; then
  cat > "$QBT_CONF" <<EOF
[LegalNotice]
Accepted=true

[Preferences]
Downloads\SavePath=$DATA/downloads/
Downloads\TempPath=$DATA/downloads/.incomplete/
Downloads\TempPathEnabled=true
WebUI\Address=127.0.0.1
WebUI\LocalHostAuth=false
WebUI\Port=8080
WebUI\ServerDomains=localhost
EOF
fi
mkdir -p "$DATA/downloads/.incomplete"

cat > "$DATA/caddy/Caddyfile" <<EOF
{
    admin off
    auto_https off
}

:6880 {
    bind 0.0.0.0

    @root path /
    redir @root /op7p-setup 302
    redir /op7p-setup "/index.html#!/settings/rpc/set?protocol=http&host=192.168.2.1&port=6880&interface=jsonrpc&secret=$ARIA_SECRET_B64" 302

    handle /jsonrpc* {
        reverse_proxy 127.0.0.1:6800
    }

    handle {
        root * $STACK/www/ariang
        file_server
    }

    log {
        output discard
    }
}
EOF

# OpenList's config.json and data.db are persistent application state. The
# installer deliberately does not rewrite them after initialization.

make_service() {
  name="$1"
  mkdir -p "$SERVICES/$name/log/main"
  cat > "$SERVICES/$name/log/run" <<EOF
#!/data/data/com.termux/files/usr/bin/sh
exec $PREFIX/bin/svlogd -tt '$SERVICES/$name/log/main'
EOF
  cat > "$SERVICES/$name/log/main/config" <<EOF
s1048576
n5
EOF
  cat > "$SERVICES/$name/finish" <<'EOF'
#!/data/data/com.termux/files/usr/bin/sh
# Prevent a broken configuration from causing a tight restart loop.
sleep 5
EOF
  chmod 700 "$SERVICES/$name/log/run" "$SERVICES/$name/finish"
}

make_service openlist
cat > "$SERVICES/openlist/run" <<EOF
#!/data/data/com.termux/files/usr/bin/sh
exec 2>&1
export HOME='$HOME' PREFIX='$PREFIX' PATH='$PREFIX/bin'
export TMPDIR='$PREFIX/tmp'
export SSL_CERT_FILE='$PREFIX/etc/tls/cert.pem'

# OpenList initializes the Aria2 offline-download backend only once. Wait for
# aria2 during parallel runit startup so a boot race does not disable it.
aria_conf='$DATA/aria2/aria2.conf'
attempt=0
while [ "\$attempt" -lt 15 ]; do
  aria_secret=\$(sed -n 's/^rpc-secret=//p' "\$aria_conf" 2>/dev/null | head -n 1)
  aria_auth=
  [ -n "\$aria_secret" ] && aria_auth="\"token:\$aria_secret\""
  if curl -fsS --max-time 2 \\
    -H 'Content-Type: application/json' \\
    -d "{\"jsonrpc\":\"2.0\",\"id\":\"openlist-start\",\"method\":\"aria2.getVersion\",\"params\":[\$aria_auth]}" \\
    http://127.0.0.1:6800/jsonrpc >/dev/null 2>&1; then
    break
  fi
  attempt=\$((attempt + 1))
  sleep 2
done

if [ "\$attempt" -ge 15 ]; then
  echo 'OpenList startup: aria2 was not ready after 30 seconds; continuing without it'
fi

exec '$PREFIX/bin/openlist' server --data '$DATA/openlist' --log-std
EOF

make_service aria2
cat > "$SERVICES/aria2/run" <<EOF
#!/data/data/com.termux/files/usr/bin/sh
exec 2>&1
export HOME='$HOME' PREFIX='$PREFIX' PATH='$PREFIX/bin'
export TMPDIR='$PREFIX/tmp'
exec '$PREFIX/bin/aria2c' --conf-path='$DATA/aria2/aria2.conf'
EOF

make_service aria2-config-sync
cat > "$SERVICES/aria2-config-sync/run" <<EOF
#!/data/data/com.termux/files/usr/bin/sh
exec 2>&1
exec '$STACK/bin/aria2-config-sync'
EOF

make_service qbittorrent
cat > "$SERVICES/qbittorrent/run" <<EOF
#!/data/data/com.termux/files/usr/bin/sh
exec 2>&1
export HOME='$HOME' PREFIX='$PREFIX' PATH='$PREFIX/bin'
export TMPDIR='$PREFIX/tmp' QT_QPA_PLATFORM=offscreen
exec '$PREFIX/bin/qbittorrent-nox' --profile='$DATA/qbittorrent' --webui-port=8080 --confirm-legal-notice
EOF

make_service caddy
cat > "$SERVICES/caddy/run" <<EOF
#!/data/data/com.termux/files/usr/bin/sh
exec 2>&1
export HOME='$HOME' PREFIX='$PREFIX' PATH='$PREFIX/bin'
export TMPDIR='$PREFIX/tmp'
exec '$PREFIX/bin/caddy' run --config '$DATA/caddy/Caddyfile' --adapter caddyfile
EOF

mkdir -p "$PREFIX/etc/ssh/sshd_config.d"
cat > "$PREFIX/etc/ssh/sshd_config.d/router.conf" <<'EOF'
Port 8022
ListenAddress 0.0.0.0
PubkeyAuthentication yes
AuthenticationMethods publickey
PasswordAuthentication no
KbdInteractiveAuthentication no
PermitEmptyPasswords no
PermitRootLogin no
MaxAuthTries 3
ClientAliveInterval 60
ClientAliveCountMax 3
TCPKeepAlive yes
UseDNS no
EOF

make_service sshd
cat > "$SERVICES/sshd/run" <<EOF
#!/data/data/com.termux/files/usr/bin/sh
exec 2>&1
export HOME='$HOME' PREFIX='$PREFIX' PATH='$PREFIX/bin'
export TMPDIR='$PREFIX/tmp'
exec '$PREFIX/bin/sshd' -D -e
EOF

cat > "$STACK/bin/healthd" <<EOF
#!/data/data/com.termux/files/usr/bin/sh
PREFIX='$PREFIX'
SERVICES='$SERVICES'
STATE='$DATA/health'
ARIA_SECRET='$ARIA_SECRET'
PATH="\$PREFIX/bin"
mkdir -p "\$STATE"
sleep 25

probe() {
  name="\$1"
  shift
  if "\$@" >/dev/null 2>&1; then
    echo 0 > "\$STATE/\$name.fail"
    return
  fi
  count=0
  [ -f "\$STATE/\$name.fail" ] && count=\$(cat "\$STATE/\$name.fail")
  count=\$((count + 1))
  echo "\$count" > "\$STATE/\$name.fail"
  if [ "\$count" -ge 3 ]; then
    echo "healthcheck: restarting \$name after \$count failed probes"
    "\$PREFIX/bin/sv" restart "\$SERVICES/\$name" || true
    echo 0 > "\$STATE/\$name.fail"
  fi
}

while :; do
  probe openlist curl -fsS --max-time 5 http://127.0.0.1:5244/ping
  aria_auth=
  [ -n "\$ARIA_SECRET" ] && aria_auth="\"token:\$ARIA_SECRET\""
  probe aria2 curl -fsS --max-time 5 -H 'Content-Type: application/json' \
    -d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"aria2.getVersion\",\"params\":[\$aria_auth]}" \
    http://127.0.0.1:6800/jsonrpc
  # A protected qBittorrent WebUI returns HTTP 401 before login, which still
  # proves that the daemon and HTTP listener are healthy. Do not use -f here.
  probe qbittorrent curl -sS --max-time 5 http://127.0.0.1:8080/api/v2/app/version
  probe caddy curl -fsS --max-time 5 http://127.0.0.1:6880/
  if [ -d "\$SERVICES/cpa" ]; then
    probe cpa curl -fsS --max-time 5 http://127.0.0.1:8317/
  fi
  sleep 60
done
EOF
chmod 700 "$STACK/bin/healthd"

make_service healthd
cat > "$SERVICES/healthd/run" <<EOF
#!/data/data/com.termux/files/usr/bin/sh
exec 2>&1
exec '$STACK/bin/healthd'
EOF

for service in openlist aria2 aria2-config-sync qbittorrent caddy sshd healthd; do
  chmod 700 "$SERVICES/$service/run"
done

cat > "$STACK/bin/supervisor" <<EOF
#!/data/data/com.termux/files/usr/bin/sh
export HOME='$HOME' PREFIX='$PREFIX' PATH='$PREFIX/bin'
export TMPDIR='$PREFIX/tmp'
while [ -f '$STACK/enabled' ]; do
  '$PREFIX/bin/runsvdir' -P '$SERVICES'
  sleep 5
done
EOF
chmod 700 "$STACK/bin/supervisor"

cat > "$STACK/bin/routerctl" <<EOF
#!/data/data/com.termux/files/usr/bin/sh
PREFIX='$PREFIX'
STACK='$STACK'
SERVICES='$SERVICES'
PATH="\$PREFIX/bin"
case "\${1:-status}" in
  start)
    : > "\$STACK/enabled"
    echo 'Router stack enabled; the Magisk keeper will start it within 30 seconds.'
    ;;
  stop)
    rm -f "\$STACK/enabled"
    for s in "\$SERVICES"/*; do "\$PREFIX/bin/sv" down "\$s" 2>/dev/null || true; done
    ;;
  restart)
    for s in "\$SERVICES"/*; do "\$PREFIX/bin/sv" restart "\$s" 2>/dev/null || true; done
    ;;
  status)
    for s in "\$SERVICES"/*; do "\$PREFIX/bin/sv" status "\$s" 2>/dev/null || true; done
    echo '--- listeners ---'
    su -c "\$PREFIX/bin/ss -lnt" 2>/dev/null | \
      grep -E ':(5244|6800|6880|8022|8080)[[:space:]]' || true
    ;;
  logs)
    service="\${2:-healthd}"
    tail -n 80 "\$SERVICES/\$service/log/main/current"
    ;;
  *)
    echo 'usage: routerctl {start|stop|restart|status|logs SERVICE}' >&2
    exit 2
    ;;
esac
EOF
chmod 700 "$STACK/bin/routerctl"

: > "$STACK/enabled"
echo "Installed router stack at $STACK"
if [ -n "$ARIA_SECRET" ]; then
  echo 'Aria2 RPC authentication: enabled'
else
  echo 'Aria2 RPC authentication: disabled'
fi
