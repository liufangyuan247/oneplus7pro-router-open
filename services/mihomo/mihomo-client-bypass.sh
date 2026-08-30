#!/data/data/com.termux/files/usr/bin/sh
set -u

# Route selected LAN clients around Mihomo's native TUN. Lower Linux policy
# rule numbers have higher precedence, so 4990 is deliberately ahead of the
# live priority-5000 rule while remaining in a small, documented namespace.
PREFIX=${PREFIX:-/data/data/com.termux/files/usr}
# Magisk root commonly exports HOME=/; never let that redirect persistent
# policy data outside the Termux router workspace.
HOME_DIR=${MIHOMO_CLIENT_BYPASS_HOME:-/data/data/com.termux/files/home}
DATA=${MIHOMO_CLIENT_BYPASS_DATA:-$HOME_DIR/router-data/mihomo}
CONFIG=${MIHOMO_CLIENT_BYPASS_CONFIG:-$DATA/client-bypass-ipv4.txt}
IFACE_CONFIG=${MIHOMO_CLIENT_BYPASS_IFACE_CONFIG:-$DATA/client-bypass-lan-iface}
IP_BIN=${MIHOMO_CLIENT_BYPASS_IP_BIN:-/system/bin/ip}
RULE_PRIORITY=${MIHOMO_CLIENT_BYPASS_PRIORITY:-4990}
WAN_TABLE=${MIHOMO_CLIENT_BYPASS_WAN_TABLE:-100}
MIHOMO_TABLE=${MIHOMO_CLIENT_BYPASS_MIHOMO_TABLE:-2022}
LIVE_PRIORITY=5000

log_error() { echo "mihomo-client-bypass: $*" >&2; }
run_ip() { "$IP_BIN" "$@"; }

validate_constants() {
    case "$RULE_PRIORITY:$WAN_TABLE:$MIHOMO_TABLE" in
        *[!0-9:]*) log_error 'priority and table identifiers must be numeric'; return 1 ;;
    esac
    [ "$RULE_PRIORITY" -lt "$LIVE_PRIORITY" ] || {
        log_error "priority $RULE_PRIORITY must be higher than live priority $LIVE_PRIORITY (a smaller number)"
        return 1
    }
    [ "$WAN_TABLE" = 100 ] || { log_error 'the physical WAN table must be 100'; return 1; }
    [ "$MIHOMO_TABLE" = 2022 ] || { log_error 'the Mihomo TUN table must be 2022'; return 1; }
    [ "$WAN_TABLE" != "$MIHOMO_TABLE" ] || { log_error 'WAN and Mihomo tables must differ'; return 1; }
}

valid_ipv4() {
    vi_address="$1"
    case "$vi_address" in ''|*[!0-9.]*) return 1 ;; esac
    echo "$vi_address" | awk -F. '
        NF != 4 { exit 1 }
        {
            for (i = 1; i <= 4; i++) {
                if ($i == "" || $i !~ /^[0-9]+$/ || $i + 0 > 255) exit 1
            }
            if ($1 + 0 == 0 || $1 + 0 == 127 || $1 + 0 >= 224) exit 1
        }
    ' >/dev/null 2>&1
}

valid_iface() {
    vif_iface="$1"
    [ -n "$vif_iface" ] && [ "${#vif_iface}" -le 15 ] || return 1
    case "$vif_iface" in *[!A-Za-z0-9_.:-]*) return 1 ;; esac
}

load_addresses() {
    la_output="$1"
    [ -f "$CONFIG" ] || { log_error "configuration is missing: $CONFIG"; return 1; }
    awk '{ sub(/#.*/, ""); gsub(/^[[:space:]]+|[[:space:]]+$/, ""); if ($0 != "") print }' \
        "$CONFIG" | sort -u > "$la_output" || return 1
    while IFS= read -r la_address; do
        valid_ipv4 "$la_address" || {
            log_error "invalid client IPv4 address in $CONFIG: $la_address"
            return 1
        }
    done < "$la_output"
}

load_iface() {
    li_output="$1"
    [ -f "$IFACE_CONFIG" ] || {
        log_error "LAN interface configuration is missing: $IFACE_CONFIG"
        return 1
    }
    awk '{ sub(/#.*/, ""); gsub(/^[[:space:]]+|[[:space:]]+$/, ""); if ($0 != "") print }' \
        "$IFACE_CONFIG" | sort -u > "$li_output" || return 1
    [ "$(wc -l < "$li_output" | tr -d ' ')" = 1 ] || {
        log_error "$IFACE_CONFIG must contain exactly one LAN interface"
        return 1
    }
    li_iface="$(sed -n '1p' "$li_output")"
    valid_iface "$li_iface" || {
        log_error "invalid LAN interface in $IFACE_CONFIG: $li_iface"
        return 1
    }
}

build_desired() {
    bd_output="$1"
    bd_addresses="$2"
    bd_iface_file="$3"
    load_addresses "$bd_addresses" || return 1
    : > "$bd_output"
    [ -s "$bd_addresses" ] || return 0
    load_iface "$bd_iface_file" || return 1
    bd_iface="$(sed -n '1p' "$bd_iface_file")"
    awk -v iface="$bd_iface" '{ print iface, $0 }' "$bd_addresses" > "$bd_output"
}

wan_table_ready() {
    run_ip -4 route show table "$WAN_TABLE" 2>/dev/null | grep -Eq '^default([[:space:]]|$)'
}

lan_iface_ready() {
    lir_iface="$1"
    run_ip link show dev "$lir_iface" >/dev/null 2>&1
}

# Every rule with the reserved priority, physical table, input interface and
# host source belongs to this helper. Output is normalized as "IFACE IPv4".
managed_rules() {
    run_ip -4 rule show priority "$RULE_PRIORITY" 2>/dev/null | awk \
        -v expected_priority="$RULE_PRIORITY" -v expected_table="$WAN_TABLE" '
        {
            priority = $1
            sub(/:$/, "", priority)
            source = $3; iface = $5; table = $7
            sub(/\/32$/, "", source)
            if (NF == 7 && priority == expected_priority && $2 == "from" &&
                $4 == "iif" && $6 == "lookup" && table == expected_table &&
                iface != "" && source ~ /^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$/) print iface, source
        }
    '
}

foreign_rules() {
    run_ip -4 rule show priority "$RULE_PRIORITY" 2>/dev/null | awk \
        -v expected_priority="$RULE_PRIORITY" -v expected_table="$WAN_TABLE" '
        {
            priority = $1
            sub(/:$/, "", priority)
            source = $3; iface = $5; table = $7
            sub(/\/32$/, "", source)
            managed = NF == 7 && priority == expected_priority && $2 == "from" &&
                $4 == "iif" && $6 == "lookup" && table == expected_table &&
                iface != "" && source ~ /^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$/
            if (!managed && NF > 0) print
        }
    '
}

reserved_priority_clean() {
    rpc_foreign="$(foreign_rules)" || return 1
    [ -z "$rpc_foreign" ] || {
        log_error "priority $RULE_PRIORITY is occupied by a foreign policy rule: $rpc_foreign"
        return 1
    }
}

remove_rule() {
    rr_entry="$1"
    rr_iface=${rr_entry%% *}
    rr_address=${rr_entry#* }
    rr_attempts=0
    while managed_rules | grep -Fxq "$rr_entry"; do
        [ "$rr_attempts" -lt 16 ] || { log_error "too many duplicate rules for $rr_iface/$rr_address"; return 1; }
        run_ip -4 rule del priority "$RULE_PRIORITY" iif "$rr_iface" \
            from "$rr_address/32" lookup "$WAN_TABLE" >/dev/null 2>&1 || return 1
        rr_attempts=$((rr_attempts + 1))
    done
}

clear_managed() {
    cm_snapshot="$1"
    managed_rules | sort -u > "$cm_snapshot" || return 1
    while IFS= read -r cm_entry; do
        [ -z "$cm_entry" ] || remove_rule "$cm_entry" || return 1
    done < "$cm_snapshot"
}

add_set() {
    as_source_file="$1"
    while IFS=' ' read -r as_iface as_address; do
        [ -z "$as_iface" ] || add_rule "$as_iface" "$as_address" || return 1
    done < "$as_source_file"
}

add_rule() {
    ar_iface="$1"
    ar_address="$2"
    run_ip -4 rule add priority "$RULE_PRIORITY" iif "$ar_iface" \
        from "$ar_address/32" lookup "$WAN_TABLE" >/dev/null 2>&1
}

restore_set() {
    rs_source_file="$1"
    rs_scratch="$2"
    while IFS=' ' read -r rs_iface rs_address; do
        [ -z "$rs_iface" ] && continue
        rs_entry="$rs_iface $rs_address"
        managed_rules | grep -Fxq "$rs_entry" || add_rule "$rs_iface" "$rs_address" || return 1
    done < "$rs_source_file"
    managed_rules | sort -u > "$rs_scratch" || return 1
    while IFS= read -r rs_entry; do
        [ -z "$rs_entry" ] && continue
        grep -Fxq "$rs_entry" "$rs_source_file" || remove_rule "$rs_entry" || return 1
    done < "$rs_scratch"
    managed_rules | sort -u > "$rs_scratch"
    cmp -s "$rs_source_file" "$rs_scratch"
}

new_workdir() {
    nw_tmp_root=${TMPDIR:-$PREFIX/tmp}
    mkdir -p "$nw_tmp_root" || return 1
    mktemp -d "$nw_tmp_root/mihomo-client-bypass.XXXXXX"
}

apply_rules() {
    workdir="$(new_workdir)" || { log_error 'cannot create temporary workspace'; return 1; }
    desired="$workdir/desired"; addresses="$workdir/addresses"; iface_file="$workdir/iface"
    previous_raw="$workdir/previous.raw"; previous="$workdir/previous"
    current="$workdir/current"; scratch="$workdir/scratch"
    if ! build_desired "$desired" "$addresses" "$iface_file"; then rm -rf "$workdir"; return 1; fi
    reserved_priority_clean || { rm -rf "$workdir"; return 1; }
    if [ -s "$desired" ]; then
        desired_iface="$(sed -n '1s/ .*//p' "$desired")"
        lan_iface_ready "$desired_iface" || {
            log_error "configured LAN interface is unavailable: $desired_iface"
            rm -rf "$workdir"
            return 1
        }
    fi
    managed_rules > "$previous_raw" || {
        log_error 'cannot read current policy rules'; rm -rf "$workdir"; return 1
    }
    sort -u "$previous_raw" > "$previous" || { rm -rf "$workdir"; return 1; }

    previous_count="$(wc -l < "$previous" | tr -d ' ')"
    previous_raw_count="$(wc -l < "$previous_raw" | tr -d ' ')"
    if cmp -s "$desired" "$previous" && [ "$previous_count" = "$previous_raw_count" ]; then
        if [ -s "$desired" ] && ! wan_table_ready; then
            log_error "table $WAN_TABLE has no default route"; rm -rf "$workdir"; return 1
        fi
        count="$(wc -l < "$desired" | tr -d ' ')"
        echo "CLIENT_BYPASS_APPLIED count=$count priority=$RULE_PRIORITY table=$WAN_TABLE"
        rm -rf "$workdir"
        return 0
    fi
    if [ -s "$desired" ] && ! wan_table_ready; then
        log_error "table $WAN_TABLE has no default route; existing rules were not changed"
        rm -rf "$workdir"
        return 1
    fi

    changed=1
    clear_managed "$current" || changed=0
    [ "$changed" = 0 ] || add_set "$desired" || changed=0
    managed_rules | sort -u > "$current" || changed=0
    [ "$changed" = 0 ] || cmp -s "$desired" "$current" || changed=0
    [ "$changed" = 0 ] || { [ ! -s "$desired" ] || wan_table_ready; } || changed=0
    [ "$changed" = 0 ] || reserved_priority_clean || changed=0
    if [ "$changed" = 0 ]; then
        log_error 'failed to apply the requested rules; restoring the previous rule set'
        restore_set "$previous" "$scratch" || log_error 'CRITICAL: previous policy rule set could not be restored'
        rm -rf "$workdir"
        return 1
    fi
    count="$(wc -l < "$desired" | tr -d ' ')"
    echo "CLIENT_BYPASS_APPLIED count=$count priority=$RULE_PRIORITY table=$WAN_TABLE"
    rm -rf "$workdir"
}

clear_rules() {
    workdir="$(new_workdir)" || { log_error 'cannot create temporary workspace'; return 1; }
    snapshot="$workdir/snapshot"; scratch="$workdir/scratch"
    reserved_priority_clean || { rm -rf "$workdir"; return 1; }
    if ! clear_managed "$snapshot"; then
        log_error 'failed to clear all managed policy rules; restoring the previous rule set'
        restore_set "$snapshot" "$scratch" || log_error 'CRITICAL: previous policy rule set could not be restored'
        rm -rf "$workdir"
        return 1
    fi
    [ -z "$(managed_rules)" ] || {
        log_error 'managed policy rules remain after cleanup'; rm -rf "$workdir"; return 1
    }
    echo "CLIENT_BYPASS_CLEARED priority=$RULE_PRIORITY table=$WAN_TABLE"
    rm -rf "$workdir"
}

show_status() {
    workdir="$(new_workdir)" || { log_error 'cannot create temporary workspace'; return 1; }
    desired="$workdir/desired"; addresses="$workdir/addresses"; iface_file="$workdir/iface"
    current_raw="$workdir/current.raw"; current="$workdir/current"
    build_desired "$desired" "$addresses" "$iface_file" || { rm -rf "$workdir"; return 1; }
    if ! reserved_priority_clean; then
        echo "priority=$RULE_PRIORITY"
        echo "wan_table=$WAN_TABLE"
        echo "mihomo_table=$MIHOMO_TABLE"
        echo 'state=conflict'
        rm -rf "$workdir"
        return 1
    fi
    managed_rules > "$current_raw" || { rm -rf "$workdir"; return 1; }
    sort -u "$current_raw" > "$current" || { rm -rf "$workdir"; return 1; }
    configured="$(wc -l < "$desired" | tr -d ' ')"; applied="$(wc -l < "$current" | tr -d ' ')"
    applied_rules="$(wc -l < "$current_raw" | tr -d ' ')"
    echo "priority=$RULE_PRIORITY"
    echo "wan_table=$WAN_TABLE"
    echo "mihomo_table=$MIHOMO_TABLE"
    [ ! -s "$iface_file" ] || echo "lan_iif=$(sed -n '1p' "$iface_file")"
    echo "configured=$configured"
    echo "applied=$applied"
    echo "applied_rules=$applied_rules"

    state=drift; result=1
    if cmp -s "$desired" "$current" && [ "$applied" = "$applied_rules" ]; then
        if [ ! -s "$desired" ]; then state=inactive; result=0
        elif ! lan_iface_ready "$(sed -n '1s/ .*//p' "$desired")"; then state=unsafe
        elif wan_table_ready; then state=active; result=0
        else state=unsafe
        fi
    fi
    while IFS=' ' read -r iface address; do
        if grep -Fxq "$iface $address" "$current"; then
            echo "client=$address iif=$iface state=applied"
        else
            echo "client=$address iif=$iface state=missing"
        fi
    done < "$desired"
    echo "state=$state"
    rm -rf "$workdir"
    return "$result"
}

main() {
    validate_constants || return 1
    if [ "${MIHOMO_CLIENT_BYPASS_SKIP_ROOT_CHECK:-0}" != 1 ] && [ "$(id -u)" != 0 ]; then
        log_error 'this command must run through Magisk root'
        return 1
    fi
    case "${1:-status}" in
        apply) apply_rules ;;
        clear) clear_rules ;;
        status) show_status ;;
        *) echo "usage: ${0##*/} {apply|clear|status}" >&2; return 2 ;;
    esac
}

if [ "${MIHOMO_CLIENT_BYPASS_SOURCE_ONLY:-0}" != 1 ]; then main "$@"; fi
