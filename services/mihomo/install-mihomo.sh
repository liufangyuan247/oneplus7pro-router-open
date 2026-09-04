#!/data/data/com.termux/files/usr/bin/sh
set -eu

PREFIX=/data/data/com.termux/files/usr
HOME=/data/data/com.termux/files/home
STACK="$HOME/router-stack"
DATA="$HOME/router-data/mihomo"
SERVICE="$STACK/services/mihomo"
UPDATER_SERVICE="$STACK/services/mihomo-updater"
STAGE="$HOME/.stage"
BACKUPS="$HOME/router-data/backups/mihomo"

for file in mihomo mihomo-config.base.yaml mihomo-update-subscription mihomo-updater-run mihomo-run-root mihomo-health-probe mihomo-client-bypass ai-proxy-domains.txt antigravity-proxy-domains.txt selective-proxy-domains.txt metacubexd-ui.tar.gz metacubexd-config.js; do
    [ -s "$STAGE/$file" ] || { echo "missing staged $file" >&2; exit 1; }
done

STAMP=$(date '+%Y%m%d-%H%M%S')
BACKUP="$BACKUPS/$STAMP"
mkdir -p "$BACKUP" "$DATA" "$SERVICE/log/main" "$UPDATER_SERVICE/log/main"
chmod 700 "$DATA" "$BACKUP"

[ -f "$PREFIX/bin/mihomo" ] && cp -p "$PREFIX/bin/mihomo" "$BACKUP/mihomo"
[ -f "$DATA/config.yaml" ] && cp -p "$DATA/config.yaml" "$BACKUP/config.yaml"
[ -f "$DATA/runtime-settings.yaml" ] && cp -p "$DATA/runtime-settings.yaml" "$BACKUP/runtime-settings.yaml"
[ -f "$DATA/runtime-mode" ] && cp -p "$DATA/runtime-mode" "$BACKUP/runtime-mode"
[ -f "$DATA/routing-policy" ] && cp -p "$DATA/routing-policy" "$BACKUP/routing-policy"
[ -f "$DATA/subscription.url" ] && cp -p "$DATA/subscription.url" "$BACKUP/subscription.url"
[ -f "$DATA/ai-proxy-domains.txt" ] && cp -p "$DATA/ai-proxy-domains.txt" "$BACKUP/ai-proxy-domains.txt"
[ -f "$DATA/custom-ai-proxy-domains.txt" ] && cp -p "$DATA/custom-ai-proxy-domains.txt" "$BACKUP/custom-ai-proxy-domains.txt"
[ -f "$DATA/antigravity-proxy-domains.txt" ] && cp -p "$DATA/antigravity-proxy-domains.txt" "$BACKUP/antigravity-proxy-domains.txt"
[ -f "$DATA/custom-antigravity-proxy-domains.txt" ] && cp -p "$DATA/custom-antigravity-proxy-domains.txt" "$BACKUP/custom-antigravity-proxy-domains.txt"
[ -f "$DATA/selective-proxy-domains.txt" ] && cp -p "$DATA/selective-proxy-domains.txt" "$BACKUP/selective-proxy-domains.txt"
[ -f "$DATA/custom-proxy-domains.txt" ] && cp -p "$DATA/custom-proxy-domains.txt" "$BACKUP/custom-proxy-domains.txt"
[ -f "$DATA/client-bypass-ipv4.txt" ] && cp -p "$DATA/client-bypass-ipv4.txt" "$BACKUP/client-bypass-ipv4.txt"
[ -f "$DATA/client-bypass-lan-iface" ] && cp -p "$DATA/client-bypass-lan-iface" "$BACKUP/client-bypass-lan-iface"
[ -f "$STACK/bin/mihomo-client-bypass" ] && cp -p "$STACK/bin/mihomo-client-bypass" "$BACKUP/mihomo-client-bypass"
[ -f "$HOME/router-data/cpa/config.yaml" ] && cp -p "$HOME/router-data/cpa/config.yaml" "$BACKUP/cpa-config.yaml"

cp "$STAGE/mihomo" "$PREFIX/bin/mihomo.new"
chmod 700 "$PREFIX/bin/mihomo.new"
mv "$PREFIX/bin/mihomo.new" "$PREFIX/bin/mihomo"

if [ ! -s "$DATA/config.yaml" ]; then
    cp "$STAGE/mihomo-config.base.yaml" "$DATA/config.yaml"
fi
if [ ! -s "$DATA/runtime-settings.yaml" ]; then
    cat > "$DATA/runtime-settings.yaml" <<'EOF'
mixed-port: 17890
port: 0
socks-port: 0
redir-port: 0
tproxy-port: 0
allow-lan: false
bind-address: 127.0.0.1
EOF
fi
# A fresh install starts fail-open. The user can switch to the locally managed
# RULE allowlist after the subscription has produced a valid candidate.
[ -s "$DATA/runtime-mode" ] || printf 'direct\n' > "$DATA/runtime-mode"
[ -s "$DATA/routing-policy" ] || printf 'allowlist\n' > "$DATA/routing-policy"
cp "$STAGE/ai-proxy-domains.txt" "$DATA/ai-proxy-domains.txt"
cp "$STAGE/antigravity-proxy-domains.txt" "$DATA/antigravity-proxy-domains.txt"
cp "$STAGE/selective-proxy-domains.txt" "$DATA/selective-proxy-domains.txt"
[ -f "$DATA/custom-ai-proxy-domains.txt" ] || : > "$DATA/custom-ai-proxy-domains.txt"
[ -f "$DATA/custom-antigravity-proxy-domains.txt" ] || : > "$DATA/custom-antigravity-proxy-domains.txt"
[ -f "$DATA/custom-proxy-domains.txt" ] || : > "$DATA/custom-proxy-domains.txt"
[ -f "$DATA/client-bypass-ipv4.txt" ] || : > "$DATA/client-bypass-ipv4.txt"
[ -f "$DATA/client-bypass-lan-iface" ] || : > "$DATA/client-bypass-lan-iface"
if [ -s "$STAGE/mihomo-subscription.url" ] && [ ! -s "$DATA/subscription.url" ]; then
    cp "$STAGE/mihomo-subscription.url" "$DATA/subscription.url"
fi

for file in GeoSite.dat geoip.metadb; do
    if [ -s "$STAGE/$file" ] && [ ! -s "$DATA/$file" ]; then
        cp "$STAGE/$file" "$DATA/$file"
    fi
done

UI_NEW="$DATA/ui.new"
case "$UI_NEW" in "$DATA"/*) ;; *) echo 'unsafe UI path' >&2; exit 1 ;; esac
rm -rf "$UI_NEW"
mkdir -p "$UI_NEW"
tar -xzf "$STAGE/metacubexd-ui.tar.gz" -C "$UI_NEW"
[ -s "$UI_NEW/index.html" ] || { echo 'invalid MetaCubeXD archive' >&2; exit 1; }
cp "$STAGE/metacubexd-config.js" "$UI_NEW/config.js"
rm -rf "$DATA/ui.prev"
[ -d "$DATA/ui" ] && mv "$DATA/ui" "$DATA/ui.prev"
mv "$UI_NEW" "$DATA/ui"

cp "$STAGE/mihomo-update-subscription" "$STACK/bin/mihomo-update-subscription.new"
chmod 700 "$STACK/bin/mihomo-update-subscription.new"
mv "$STACK/bin/mihomo-update-subscription.new" "$STACK/bin/mihomo-update-subscription"

cp "$STAGE/mihomo-health-probe" "$STACK/bin/mihomo-health-probe.new"
chmod 700 "$STACK/bin/mihomo-health-probe.new"
mv "$STACK/bin/mihomo-health-probe.new" "$STACK/bin/mihomo-health-probe"

cp "$STAGE/mihomo-client-bypass" "$STACK/bin/mihomo-client-bypass.new"
chmod 700 "$STACK/bin/mihomo-client-bypass.new"
mv "$STACK/bin/mihomo-client-bypass.new" "$STACK/bin/mihomo-client-bypass"

cp "$STAGE/mihomo-run-root" "$SERVICE/run"

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

cp "$STAGE/mihomo-updater-run" "$UPDATER_SERVICE/run"
cat > "$UPDATER_SERVICE/log/run" <<EOF
#!/data/data/com.termux/files/usr/bin/sh
exec '$PREFIX/bin/svlogd' -tt '$UPDATER_SERVICE/log/main'
EOF
cat > "$UPDATER_SERVICE/log/main/config" <<'EOF'
s1048576
n3
EOF
cat > "$UPDATER_SERVICE/finish" <<'EOF'
#!/data/data/com.termux/files/usr/bin/sh
sleep 10
EOF

chmod 600 "$DATA/config.yaml" "$DATA/runtime-settings.yaml" "$DATA/runtime-mode" "$DATA/routing-policy" \
    "$DATA/ai-proxy-domains.txt" "$DATA/custom-ai-proxy-domains.txt" \
    "$DATA/antigravity-proxy-domains.txt" "$DATA/custom-antigravity-proxy-domains.txt" \
    "$DATA/selective-proxy-domains.txt" "$DATA/custom-proxy-domains.txt"
chmod 600 "$DATA/client-bypass-ipv4.txt"
chmod 600 "$DATA/client-bypass-lan-iface"
[ ! -f "$DATA/subscription.url" ] || chmod 600 "$DATA/subscription.url"
chmod 700 "$SERVICE/run" "$SERVICE/finish" "$SERVICE/log/run"
chmod 700 "$UPDATER_SERVICE/run" "$UPDATER_SERVICE/finish" "$UPDATER_SERVICE/log/run"
rm -f "$SERVICE/down" "$UPDATER_SERVICE/down"

"$PREFIX/bin/mihomo" -t -d "$DATA" -f "$DATA/config.yaml"
if [ -s "$DATA/subscription.url" ]; then
    "$STACK/bin/mihomo-update-subscription"
else
    "$PREFIX/bin/sv" up "$SERVICE"
fi
"$PREFIX/bin/sv" up "$UPDATER_SERVICE"

ready=0
attempt=0
while [ "$attempt" -lt 20 ]; do
    if "$PREFIX/bin/curl" -fsS --max-time 2 http://127.0.0.1:9091/version >/dev/null 2>&1; then
        ready=1
        break
    fi
    attempt=$((attempt + 1))
    sleep 1
done
[ "$ready" -eq 1 ] || { echo 'Mihomo failed its local health check' >&2; exit 1; }

HEALTHD="$STACK/bin/healthd"
if [ -f "$HEALTHD" ]; then
    cp -p "$HEALTHD" "$BACKUP/healthd"
    "$PREFIX/bin/awk" '
    /^[[:space:]]*probe mihomo / {
        print "    probe mihomo /data/data/com.termux/files/home/router-stack/bin/mihomo-health-probe"
        done=1
        next
    }
    /^  sleep 60$/ && !done {
        print "  if [ -d \"$SERVICES/mihomo\" ] && [ ! -f \"/data/data/com.termux/files/home/router-data/mihomo/disabled\" ]; then"
        print "    probe mihomo /data/data/com.termux/files/home/router-stack/bin/mihomo-health-probe"
        print "  fi"
        done=1
    }
    { print }
    ' "$HEALTHD" > "$HEALTHD.new"
    "$PREFIX/bin/sh" -n "$HEALTHD.new"
    chmod 700 "$HEALTHD.new"
    mv "$HEALTHD.new" "$HEALTHD"
    "$PREFIX/bin/sv" restart "$STACK/services/healthd" >/dev/null 2>&1 || true
fi

CPA_CONFIG="$HOME/router-data/cpa/config.yaml"
EGRESS_CODE=$("$PREFIX/bin/curl" -sS --connect-timeout 8 --max-time 20 \
    --proxy http://127.0.0.1:17891 \
    --output /dev/null --write-out '%{http_code}' \
    https://chatgpt.com/cdn-cgi/trace 2>/dev/null || true)
case "$EGRESS_CODE" in
    2??|3??|4??) EGRESS_READY=1 ;;
    *) EGRESS_READY=0 ;;
esac

if [ -s "$CPA_CONFIG" ] && [ "$EGRESS_READY" -eq 1 ]; then
    "$PREFIX/bin/awk" '
    /^usage-statistics-enabled:[[:space:]]*/ { print "usage-statistics-enabled: true"; usage=1; next }
    /^proxy-url:[[:space:]]*/ { print "proxy-url: \"http://127.0.0.1:17891\""; proxy=1; next }
    { print }
    END {
        if (!usage) print "usage-statistics-enabled: true"
        if (!proxy) print "proxy-url: \"http://127.0.0.1:17891\""
    }
    ' "$CPA_CONFIG" > "$CPA_CONFIG.new"
    chmod 600 "$CPA_CONFIG.new"
    mv "$CPA_CONFIG.new" "$CPA_CONFIG"
    "$PREFIX/bin/sv" restart "$STACK/services/cpa" >/dev/null 2>&1 || true
    cpa_ready=0
    attempt=0
    while [ "$attempt" -lt 15 ]; do
        if "$PREFIX/bin/curl" -fsS --max-time 2 http://127.0.0.1:8317/ >/dev/null 2>&1; then
            cpa_ready=1
            break
        fi
        attempt=$((attempt + 1))
        sleep 1
    done
    if [ "$cpa_ready" -ne 1 ]; then
        cp -p "$BACKUP/cpa-config.yaml" "$CPA_CONFIG"
        "$PREFIX/bin/sv" restart "$STACK/services/cpa" >/dev/null 2>&1 || true
        echo 'CPA rejected the proxy configuration; restored previous config' >&2
        exit 1
    fi
elif [ -s "$CPA_CONFIG" ]; then
    echo 'Mihomo CPA listener is not ready; CPA configuration was left unchanged' >&2
fi

rm -f "$STAGE/mihomo" "$STAGE/mihomo-config.base.yaml" \
    "$STAGE/mihomo-update-subscription" "$STAGE/mihomo-updater-run" \
    "$STAGE/mihomo-health-probe" "$STAGE/mihomo-client-bypass" \
    "$STAGE/ai-proxy-domains.txt" "$STAGE/antigravity-proxy-domains.txt" \
    "$STAGE/selective-proxy-domains.txt" \
    "$STAGE/metacubexd-ui.tar.gz" "$STAGE/metacubexd-config.js" \
    "$STAGE/mihomo-subscription.url" "$STAGE/GeoSite.dat" "$STAGE/geoip.metadb"

printf '%s\n' "$BACKUP" > "$DATA/last-install-backup"
echo MIHOMO_INSTALLED
