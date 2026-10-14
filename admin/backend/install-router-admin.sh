#!/data/data/com.termux/files/usr/bin/sh
set -eu

PREFIX=/data/data/com.termux/files/usr
HOME=/data/data/com.termux/files/home
STACK="$HOME/router-stack"
DATA="$HOME/router-data"
ADMIN="$HOME/router-admin"
REMOTE="$DATA/remote"
SERVICE="$STACK/services/router-admin"
STAGE="$HOME/.stage"
BACKUPS="$DATA/backups/router-admin"

[ -f "$STAGE/router-admin" ] || { echo 'missing staged router-admin binary' >&2; exit 1; }
[ -f "$STAGE/router-admin-web.tar.gz" ] || { echo 'missing staged web archive' >&2; exit 1; }
[ -f "$STAGE/scrcpy-server-v3.3.1" ] || [ -f "$REMOTE/scrcpy-server-v3.3.1" ] || {
  echo 'scrcpy server is neither staged nor already installed' >&2
  exit 1
}

STAMP="$(date '+%Y%m%d-%H%M%S')"
BACKUP="$BACKUPS/$STAMP"
mkdir -p "$BACKUP" "$REMOTE"
[ -f "$PREFIX/bin/router-admin" ] && cp -p "$PREFIX/bin/router-admin" "$BACKUP/router-admin"
[ -d "$ADMIN/web" ] && tar -czf "$BACKUP/router-admin-web.tar.gz" -C "$ADMIN" web
[ -f "$REMOTE/scrcpy-server-v3.3.1" ] && \
  cp -p "$REMOTE/scrcpy-server-v3.3.1" "$BACKUP/scrcpy-server-v3.3.1"

mkdir -p "$ADMIN/web.new" "$DATA/admin" "$SERVICE/log/main"
rm -rf "$ADMIN/web.new"
mkdir -p "$ADMIN/web.new"
tar -xzf "$STAGE/router-admin-web.tar.gz" -C "$ADMIN/web.new"

rm -rf "$ADMIN/web.prev"
if [ -d "$ADMIN/web" ]; then mv "$ADMIN/web" "$ADMIN/web.prev"; fi
mv "$ADMIN/web.new" "$ADMIN/web"

cp "$STAGE/router-admin" "$PREFIX/bin/router-admin.new"
chmod 700 "$PREFIX/bin/router-admin.new"
mv "$PREFIX/bin/router-admin.new" "$PREFIX/bin/router-admin"

if [ -f "$STAGE/scrcpy-server-v3.3.1" ]; then
  cp "$STAGE/scrcpy-server-v3.3.1" "$REMOTE/scrcpy-server-v3.3.1.new"
  chmod 600 "$REMOTE/scrcpy-server-v3.3.1.new"
  mv "$REMOTE/scrcpy-server-v3.3.1.new" "$REMOTE/scrcpy-server-v3.3.1"
fi

cat > "$SERVICE/run" <<EOF
#!/data/data/com.termux/files/usr/bin/sh
exec 2>&1
export HOME='$HOME' PREFIX='$PREFIX' PATH='$PREFIX/bin'
export TMPDIR='$PREFIX/tmp'
export ROUTER_HOME='$HOME'
export ROUTER_PREFIX='$PREFIX'
export ROUTER_ADMIN_WEB='$ADMIN/web'
export ROUTER_ADMIN_LISTEN='0.0.0.0:9090'
export ROUTER_SCRCPY_SERVER='$REMOTE/scrcpy-server-v3.3.1'
exec '$PREFIX/bin/router-admin'
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

"$PREFIX/bin/sv" restart "$SERVICE" >/dev/null 2>&1 || true

healthy=0
attempt=0
while [ "$attempt" -lt 10 ]; do
  if "$PREFIX/bin/curl" -fsS --max-time 2 http://127.0.0.1:9090/api/health >/dev/null 2>&1 && \
     "$PREFIX/bin/curl" -fsS --max-time 2 http://127.0.0.1:9090/ >/dev/null 2>&1; then
    healthy=1
    break
  fi
  attempt=$((attempt + 1))
  sleep 1
done

if [ "$healthy" -ne 1 ]; then
  echo 'router-admin health check failed; restoring previous release' >&2
  [ -f "$BACKUP/router-admin" ] && cp -p "$BACKUP/router-admin" "$PREFIX/bin/router-admin"
  if [ -f "$BACKUP/router-admin-web.tar.gz" ]; then
    rm -rf "$ADMIN/web.failed"
    [ -d "$ADMIN/web" ] && mv "$ADMIN/web" "$ADMIN/web.failed"
    tar -xzf "$BACKUP/router-admin-web.tar.gz" -C "$ADMIN"
  fi
  if [ -f "$BACKUP/scrcpy-server-v3.3.1" ]; then
    cp -p "$BACKUP/scrcpy-server-v3.3.1" "$REMOTE/scrcpy-server-v3.3.1"
  fi
  "$PREFIX/bin/sv" restart "$SERVICE" >/dev/null 2>&1 || true
  exit 1
fi

printf '%s\n' "$BACKUP" > "$DATA/admin/last-router-admin-backup"
echo ROUTER_ADMIN_INSTALLED
