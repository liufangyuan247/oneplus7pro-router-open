#!/data/data/com.termux/files/usr/bin/sh
set -eu

PREFIX=/data/data/com.termux/files/usr
HOME_DIR=/data/data/com.termux/files/home
DATA="$HOME_DIR/router-data/mihomo"
CANDIDATE="${1:-$DATA/config.unified-candidate.yaml}"
CORE="$PREFIX/bin/mihomo"
CURL="$PREFIX/bin/curl"
DRILL="$PREFIX/bin/drill"
JQ="$PREFIX/bin/jq"
SU="$PREFIX/bin/su"
SHADOW="$PREFIX/tmp/mihomo-cn-direct-shadow.$$"
CONFIG="$SHADOW/config.yaml"
LOG="$SHADOW/mihomo.log"
PID=''
SU_PID=''

root_pid_alive() {
    [ -n "$1" ] && "$SU" -c "kill -0 $1" >/dev/null 2>&1
}

cleanup() {
    trap - EXIT INT TERM
    if root_pid_alive "$PID"; then
        "$SU" -c "kill -TERM $PID" >/dev/null 2>&1 || true
        attempt=0
        while root_pid_alive "$PID" && [ "$attempt" -lt 10 ]; do
            sleep 1
            attempt=$((attempt + 1))
        done
        root_pid_alive "$PID" && "$SU" -c "kill -KILL $PID" >/dev/null 2>&1 || true
    fi
    [ -z "$SU_PID" ] || kill "$SU_PID" >/dev/null 2>&1 || true
    case "$SHADOW" in "$PREFIX/tmp"/mihomo-cn-direct-shadow.*) rm -rf "$SHADOW" ;; esac
}
trap cleanup EXIT INT TERM

[ -s "$CANDIDATE" ] || { echo "candidate is missing: $CANDIDATE" >&2; exit 1; }
for binary in "$CORE" "$CURL" "$DRILL" "$JQ" "$PREFIX/bin/awk" "$SU"; do
    [ -x "$binary" ] || { echo "required binary is missing: $binary" >&2; exit 1; }
done
grep -q '^[[:space:]]*- GEOSITE,CN,DIRECT$' "$CANDIDATE"
grep -q '^[[:space:]]*- GEOIP,CN,DIRECT$' "$CANDIDATE"
grep -q '^[[:space:]]*- MATCH,家庭RULE$' "$CANDIDATE"
grep -q '^[[:space:]]*fake-ip-filter-mode: whitelist$' "$CANDIDATE"
grep -q '^[[:space:]]*store-fake-ip: true$' "$CANDIDATE"
grep -q '^[[:space:]]*stack: mixed$' "$CANDIDATE"
grep -q '^[[:space:]]*gso: true$' "$CANDIDATE"
grep -q '^[[:space:]]*gso-max-size: 65536$' "$CANDIDATE"
grep -q '^[[:space:]]*- DOMAIN-SUFFIX,cloudcode-pa.googleapis.com,Antigravity$' "$CANDIDATE"
grep -q '^[[:space:]]*- DOMAIN-SUFFIX,antigravity-unleash.goog,Antigravity$' "$CANDIDATE"
grep -q '^[[:space:]]*port: 17892$' "$CANDIDATE"
grep -q '^[[:space:]]*proxy: "Antigravity"$' "$CANDIDATE"

[ "$(grep -E -c '^unified-delay:[[:space:]]*true[[:space:]]*$' "$CANDIDATE")" -eq 1 ] || {
    echo "candidate missing or non-unique top-level unified-delay: true" >&2
    exit 1
}
[ "$(grep -E -c '^tcp-concurrent:[[:space:]]*true[[:space:]]*$' "$CANDIDATE")" -eq 1 ] || {
    echo "candidate missing or non-unique top-level tcp-concurrent: true" >&2
    exit 1
}
[ "$(grep -E -c '^[[:space:]]*- name:[[:space:]]*"?家庭测速"?[[:space:]]*$' "$CANDIDATE")" -eq 1 ] || {
    echo "candidate missing or non-unique 家庭测速 group" >&2
    exit 1
}
[ "$(grep -E -c '^[[:space:]]*- name:[[:space:]]*"?node-benchmark"?[[:space:]]*$' "$CANDIDATE")" -eq 1 ] || {
    echo "candidate missing or non-unique node-benchmark listener" >&2
    exit 1
}
"$PREFIX/bin/awk" '
    /^[[:space:]]*- name:[[:space:]]*"?node-benchmark"?[[:space:]]*$/ {
        in_nb = 1
        found_nb++
        next
    }
    in_nb && (/^[[:space:]]*- name:/ || /^[a-zA-Z0-9_-]+:[[:space:]]*/) {
        in_nb = 0
    }
    in_nb && /^[[:space:]]*listen:[[:space:]]*127\.0\.0\.1[[:space:]]*$/ {
        listen_count++
        next
    }
    in_nb && /^[[:space:]]*listen:[[:space:]]*/ {
        bad_listen++
        next
    }
    in_nb && /^[[:space:]]*port:[[:space:]]*17894[[:space:]]*$/ {
        port_count++
        next
    }
    in_nb && /^[[:space:]]*port:[[:space:]]*/ {
        bad_port++
        next
    }
    in_nb && /^[[:space:]]*proxy:[[:space:]]*"?家庭测速"?[[:space:]]*$/ {
        proxy_count++
        next
    }
    in_nb && /^[[:space:]]*proxy:[[:space:]]*/ {
        bad_proxy++
        next
    }
    END {
        if (found_nb != 1 || listen_count != 1 || port_count != 1 || proxy_count != 1 || bad_listen != 0 || bad_port != 0 || bad_proxy != 0) {
            exit 1
        }
    }
' "$CANDIDATE" || {
    echo "candidate node-benchmark listener must be loopback-only 127.0.0.1:17894 targeting 家庭测速" >&2
    exit 1
}

mkdir -p "$SHADOW"
chmod 700 "$SHADOW"
for geodata in GeoSite.dat geoip.metadb Country.mmdb; do
    [ ! -s "$DATA/$geodata" ] || ln -s "$DATA/$geodata" "$SHADOW/$geodata"
done

# Run a second Mihomo with TUN disabled and isolated ports. It exercises the
# candidate's real proxy nodes, DNS policies and rule engine without touching
# op7tun, the live controller or the supervised core.
"$PREFIX/bin/awk" '
    /^mixed-port:[[:space:]]*/ { print "mixed-port: 17990"; next }
    /^external-controller:[[:space:]]*/ { print "external-controller: 127.0.0.1:9191"; next }
    /^  listen: 127\.0\.0\.1:1053[[:space:]]*$/ { print "  listen: 127.0.0.1:1153"; next }
    /^    port: 17891[[:space:]]*$/ { print "    port: 17991"; next }
    /^    port: 17892[[:space:]]*$/ { print "    port: 17992"; next }
    /^    port: 17893[[:space:]]*$/ { print "    port: 17993"; next }
    /^    port: 17894[[:space:]]*$/ { print "    port: 17994"; next }
    /^tun:[[:space:]]*$/ { in_tun=1; print; next }
    in_tun && /^  enable:[[:space:]]*/ { print "  enable: false"; in_tun=0; next }
    { print }
' "$CANDIDATE" > "$CONFIG"
chmod 600 "$CONFIG"
"$CORE" -t -d "$SHADOW" -f "$CONFIG" >/dev/null
"$SU" -c "umask 077; echo \$\$ > '$SHADOW/core.pid'; chown 10207:10207 '$SHADOW/core.pid'; chmod 600 '$SHADOW/core.pid'; exec '$CORE' -d '$SHADOW' -f '$CONFIG'" > "$LOG" 2>&1 &
SU_PID=$!

attempt=0
while [ "$attempt" -lt 10 ] && [ ! -s "$SHADOW/core.pid" ]; do
    sleep 1
    attempt=$((attempt + 1))
done
PID="$(cat "$SHADOW/core.pid" 2>/dev/null || true)"
case "$PID" in ''|*[!0-9]*) tail -n 80 "$LOG" >&2; exit 1 ;; esac

attempt=0
while [ "$attempt" -lt 20 ]; do
    "$CURL" -fsS --max-time 2 http://127.0.0.1:9191/version >/dev/null 2>&1 && break
    root_pid_alive "$PID" || { tail -n 80 "$LOG" >&2; exit 1; }
    sleep 1
    attempt=$((attempt + 1))
done
"$CURL" -fsS --max-time 2 http://127.0.0.1:9191/version >/dev/null
proxy_state="$($CURL -fsS --max-time 3 http://127.0.0.1:9191/proxies)"
household_now="$(printf '%s' "$proxy_state" | "$JQ" -r '.proxies["家庭RULE"].now // empty')"
ai_now="$(printf '%s' "$proxy_state" | "$JQ" -r '.proxies["AI"].now // empty')"
antigravity_now="$(printf '%s' "$proxy_state" | "$JQ" -r '.proxies["Antigravity"].now // empty')"
[ -n "$household_now" ] && [ "$household_now" != DIRECT ] || {
    echo "household shadow group is not using a proxy: ${household_now:-missing}" >&2
    exit 1
}
[ -n "$ai_now" ] && [ "$ai_now" != DIRECT ] || {
    echo "AI shadow group is not using a proxy: ${ai_now:-missing}" >&2
    exit 1
}
[ -n "$antigravity_now" ] && [ "$antigravity_now" != DIRECT ] || {
    echo "Antigravity shadow group is not using a proxy: ${antigravity_now:-missing}" >&2
    exit 1
}

dns_answers() {
    "$DRILL" @127.0.0.1 -p 1153 "$1" A 2>/dev/null |
        "$PREFIX/bin/awk" '$3 == "IN" && $4 == "A" { print $5 }'
}

for domain in www.baidu.com example.com; do
    answers="$(dns_answers "$domain")"
    [ -n "$answers" ] || { echo "shadow DNS returned no IPv4 answer for $domain" >&2; exit 1; }
    if echo "$answers" | grep -q '^198\.18\.'; then
        echo "shadow DNS returned fake IP for bypass-unsafe policy: $domain $answers" >&2
        exit 1
    fi
done

http_ok() {
    case "$1" in 2??|3??|4??) return 0 ;; *) return 1 ;; esac
}

domestic_code="$($CURL -sS --connect-timeout 5 --max-time 15 \
    --proxy http://127.0.0.1:17990 --output /dev/null --write-out '%{http_code}' \
    https://www.baidu.com/ 2>/dev/null || true)"
overseas_code="$($CURL -sS --connect-timeout 5 --max-time 20 \
    --proxy http://127.0.0.1:17990 --output /dev/null --write-out '%{http_code}' \
    https://www.google.com/generate_204 2>/dev/null || true)"
fallback_code="$($CURL -sS --connect-timeout 5 --max-time 20 \
    --proxy http://127.0.0.1:17990 --output /dev/null --write-out '%{http_code}' \
    https://example.com/ 2>/dev/null || true)"
ai_code="$($CURL -sS --connect-timeout 5 --max-time 20 \
    --proxy http://127.0.0.1:17991 --output /dev/null --write-out '%{http_code}' \
    https://chatgpt.com/cdn-cgi/trace 2>/dev/null || true)"
antigravity_code="$($CURL -sS --connect-timeout 5 --max-time 20 \
    --proxy http://127.0.0.1:17992 --output /dev/null --write-out '%{http_code}' \
    https://cloudcode-pa.googleapis.com/ 2>/dev/null || true)"
http_ok "$domestic_code" || { echo "domestic shadow request failed: $domestic_code" >&2; exit 1; }
http_ok "$overseas_code" || { echo "overseas shadow request failed: $overseas_code" >&2; exit 1; }
http_ok "$fallback_code" || { echo "fallback shadow request failed: $fallback_code" >&2; exit 1; }
http_ok "$ai_code" || { echo "AI shadow request failed: $ai_code" >&2; exit 1; }
http_ok "$antigravity_code" || { echo "Antigravity shadow request failed: $antigravity_code" >&2; exit 1; }

echo "CN_DIRECT_CANDIDATE_OK household=$household_now ai_group=$ai_now antigravity_group=$antigravity_now domestic=$domestic_code overseas=$overseas_code fallback=$fallback_code ai=$ai_code antigravity=$antigravity_code"
