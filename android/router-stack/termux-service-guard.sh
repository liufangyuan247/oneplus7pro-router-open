#!/data/data/com.termux/files/usr/bin/sh
set -u

PREFIX="${TERMUX_PREFIX:-/data/data/com.termux/files/usr}"
HOME_DIR="${TERMUX_HOME:-/data/data/com.termux/files/home}"
STACK="${TERMUX_STACK:-$HOME_DIR/router-stack}"
SERVICES="${TERMUX_SERVICES:-$STACK/services}"
ACTION="${1:-audit}"
[ "$#" -eq 0 ] || shift

case "$ACTION" in
    audit|repair) ;;
    *) echo "usage: termux-service-guard {audit|repair} [SERVICE ...]" >&2; exit 2 ;;
esac

service_uid="${TERMUX_SERVICE_UID:-$("$PREFIX/bin/stat" -c '%u' "$HOME_DIR")}"
service_gid="${TERMUX_SERVICE_GID:-$("$PREFIX/bin/stat" -c '%g' "$HOME_DIR")}"
current_uid="$("$PREFIX/bin/id" -u)"
errors=0
repairs=0
checked_services=0

check_number() {
    case "$1" in ''|*[!0-9]*) return 1 ;; *) return 0 ;; esac
}
check_number "$service_uid" && check_number "$service_gid" || {
    echo 'termux-service-guard: invalid service UID or GID' >&2
    exit 2
}

report_error() {
    echo "termux-service-guard: $*" >&2
    errors=$((errors + 1))
}

ensure_path() {
    path="$1"
    wanted_mode="$2"
    kind="$3"
    [ -e "$path" ] || { report_error "missing $kind: $path"; return; }

    actual="$("$PREFIX/bin/stat" -c '%u:%g:%a' "$path" 2>/dev/null || true)"
    expected="$service_uid:$service_gid:$wanted_mode"
    [ "$actual" = "$expected" ] && return

    if [ "$ACTION" = repair ] && { [ "$current_uid" -eq 0 ] || [ "$current_uid" -eq "$service_uid" ]; }; then
        if [ "$current_uid" -eq 0 ]; then
            "$PREFIX/bin/chown" "$service_uid:$service_gid" "$path" 2>/dev/null || true
        fi
        "$PREFIX/bin/chmod" "$wanted_mode" "$path" 2>/dev/null || true
        actual="$("$PREFIX/bin/stat" -c '%u:%g:%a' "$path" 2>/dev/null || true)"
        if [ "$actual" = "$expected" ]; then
            repairs=$((repairs + 1))
            return
        fi
    fi

    report_error "$kind $path has ${actual:-unreadable}, want $expected"
}

check_optional_file() {
    path="$1"
    [ ! -e "$path" ] || ensure_path "$path" 700 executable
}

check_service() {
    name="$1"
    service="$SERVICES/$name"
    [ -d "$service" ] || { report_error "unknown service: $name"; return; }
    checked_services=$((checked_services + 1))
    ensure_path "$service" 700 'service directory'
    ensure_path "$service/run" 700 executable
    check_optional_file "$service/start"
    check_optional_file "$service/finish"
    if [ -d "$service/log" ]; then
        ensure_path "$service/log" 700 'log directory'
        [ ! -d "$service/log/main" ] || ensure_path "$service/log/main" 700 'log directory'
        check_optional_file "$service/log/run"
    fi
    if [ -e "$service/down" ]; then
        ensure_path "$service/down" 600 'service state file'
    fi
}

if [ "$#" -gt 0 ]; then
    for service_name in "$@"; do
        case "$service_name" in ''|*/*|.*) report_error "invalid service name: $service_name" ;; *) check_service "$service_name" ;; esac
    done
else
    for service in "$SERVICES"/*; do
        [ -d "$service" ] || continue
        check_service "${service##*/}"
    done

    ensure_path "$STACK/bin" 700 'stack bin directory'
    for helper in "$STACK/bin"/*; do
        [ -f "$helper" ] || continue
        if [ -x "$helper" ]; then
            ensure_path "$helper" 700 executable
        else
            ensure_path "$helper" 600 'stack file'
        fi
    done

    for binary in \
        "$PREFIX/bin/cli-proxy-api" \
        "$PREFIX/bin/cpa-usage-keeper" \
        "$PREFIX/bin/mihomo" \
        "$PREFIX/bin/router-admin"
    do
        [ ! -e "$binary" ] || ensure_path "$binary" 700 executable
    done
fi

if [ "$errors" -ne 0 ]; then
    echo "TERMUX_SERVICE_GUARD_FAILED services=$checked_services errors=$errors repairs=$repairs" >&2
    exit 1
fi

echo "TERMUX_SERVICE_GUARD_OK services=$checked_services repairs=$repairs"
