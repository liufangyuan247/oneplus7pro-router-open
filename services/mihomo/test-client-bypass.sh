#!/usr/bin/env sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
TEST_TMP=$(mktemp -d "${TMPDIR:-/tmp}/mihomo-client-bypass-test.XXXXXX")
case "$TEST_TMP" in
    "${TMPDIR:-/tmp}"/mihomo-client-bypass-test.*) ;;
    *) echo "unsafe test directory: $TEST_TMP" >&2; exit 1 ;;
esac
trap 'rm -rf "$TEST_TMP"' EXIT INT TERM

export MIHOMO_CLIENT_BYPASS_SOURCE_ONLY=1
export MIHOMO_CLIENT_BYPASS_SKIP_ROOT_CHECK=1
export MIHOMO_CLIENT_BYPASS_CONFIG="$TEST_TMP/config"
export MIHOMO_CLIENT_BYPASS_IFACE_CONFIG="$TEST_TMP/iface"
export MIHOMO_CLIENT_BYPASS_DATA="$TEST_TMP"
export TMPDIR="$TEST_TMP"
. "$SCRIPT_DIR/mihomo-client-bypass.sh"

RULES="$TEST_TMP/rules"
CALLS="$TEST_TMP/calls"
FOREIGN_RULES="$TEST_TMP/foreign-rules"
: > "$RULES"
: > "$CALLS"
: > "$FOREIGN_RULES"
ROUTE_OK=1
FAIL_ADD_IP=''
FAIL_DEL_IP=''
DETACHED_IP=''

run_ip() {
    echo "$*" >> "$CALLS"
    case "$*" in
        "-4 route show table 100")
            [ "$ROUTE_OK" = 1 ] && echo 'default dev ppp0 scope link'
            ;;
        "link show dev lan-test0")
            echo '7: lan-test0: <UP> mtu 1500'
            ;;
        "-4 rule show priority 4990")
            while IFS=' ' read -r iface ip; do
                [ -z "$ip" ] && continue
                if [ "$ip" = "$DETACHED_IP" ]; then
                    echo "4990: from $ip iif $iface [detached] lookup 100"
                else
                    echo "4990: from $ip iif $iface lookup 100"
                fi
            done < "$RULES"
            sed -n '/./p' "$FOREIGN_RULES"
            ;;
        "-4 rule add priority 4990 iif "*" from "*"/32 lookup 100")
            iface=$7
            ip=${9%/32}
            [ "$ip" != "$FAIL_ADD_IP" ] || return 1
            echo "$iface $ip" >> "$RULES"
            ;;
        "-4 rule del priority 4990 iif "*" from "*"/32 lookup 100")
            iface=$7
            ip=${9%/32}
            target="$iface $ip"
            [ "$ip" != "$FAIL_DEL_IP" ] || return 1
            awk -v target="$target" 'removed || $0 != target { print } $0 == target { removed=1 }' \
                "$RULES" > "$RULES.new"
            mv "$RULES.new" "$RULES"
            [ "$ip" != "$DETACHED_IP" ] || DETACHED_IP=''
            ;;
        *)
            echo "unexpected ip command: $*" >&2
            return 1
            ;;
    esac
}

assert_rules() {
    expected="$1"
    actual=$(sort -u "$RULES" | tr '\n' ' ' | sed 's/ $//')
    [ "$actual" = "$expected" ] || {
        echo "expected rules [$expected], got [$actual]" >&2
        exit 1
    }
}

printf '%s\n' 'lan-test0' > "$MIHOMO_CLIENT_BYPASS_IFACE_CONFIG"
printf '%s\n' '192.0.2.40' '192.0.2.30 # example client' '192.0.2.40' > "$MIHOMO_CLIENT_BYPASS_CONFIG"
main apply >/dev/null
assert_rules 'lan-test0 192.0.2.30 lan-test0 192.0.2.40'
grep -q 'rule add priority 4990 iif lan-test0 from 192.0.2.30/32 lookup 100' "$CALLS"

# A rule whose interface was absent during boot is still owned by this helper.
# Rebuild it instead of rejecting it as a foreign priority conflict.
DETACHED_IP=192.0.2.30
: > "$CALLS"
main apply >/dev/null
[ -z "$DETACHED_IP" ]
assert_rules 'lan-test0 192.0.2.30 lan-test0 192.0.2.40'
grep -q 'rule del priority 4990 iif lan-test0 from 192.0.2.30/32 lookup 100' "$CALLS"
grep -q 'rule add priority 4990 iif lan-test0 from 192.0.2.30/32 lookup 100' "$CALLS"

# A foreign occupant of the reserved priority blocks every mutation.
echo '4990: from all fwmark 0x1 lookup 101' > "$FOREIGN_RULES"
printf '%s\n' '192.0.2.50' > "$MIHOMO_CLIENT_BYPASS_CONFIG"
: > "$CALLS"
if main apply >/dev/null 2>&1; then
    echo 'apply ignored a foreign priority conflict' >&2
    exit 1
fi
! grep -Eq ' rule (add|del) ' "$CALLS"
assert_rules 'lan-test0 192.0.2.30 lan-test0 192.0.2.40'
main status 2>/dev/null | grep -q '^state=conflict$'
if main clear >/dev/null 2>&1; then
    echo 'clear ignored a foreign priority conflict' >&2
    exit 1
fi
: > "$FOREIGN_RULES"
printf '%s\n' '192.0.2.30' '192.0.2.40' > "$MIHOMO_CLIENT_BYPASS_CONFIG"

# An already-correct apply must not churn the policy rules.
: > "$CALLS"
main apply >/dev/null
[ ! -s "$CALLS" ] || {
    # Reading the route/rule state is expected; only mutations are forbidden.
    ! grep -Eq ' rule (add|del) ' "$CALLS"
}

# Duplicate runtime rules are normalized even when their unique set matches.
echo 'lan-test0 192.0.2.30' >> "$RULES"
main apply >/dev/null
[ "$(grep -c '^lan-test0 192\.0\.2\.30$' "$RULES")" = 1 ]

# A missing WAN default must leave the existing rule set untouched.
printf '%s\n' '192.0.2.50' > "$MIHOMO_CLIENT_BYPASS_CONFIG"
ROUTE_OK=0
if main apply >/dev/null 2>&1; then
    echo 'apply unexpectedly succeeded without table 100 default' >&2
    exit 1
fi
assert_rules 'lan-test0 192.0.2.30 lan-test0 192.0.2.40'
ROUTE_OK=1

# A partial add failure rolls back to the exact previous set.
FAIL_ADD_IP=192.0.2.60
printf '%s\n' '192.0.2.50' '192.0.2.60' > "$MIHOMO_CLIENT_BYPASS_CONFIG"
if main apply >/dev/null 2>&1; then
    echo 'apply unexpectedly succeeded after injected add failure' >&2
    exit 1
fi
assert_rules 'lan-test0 192.0.2.30 lan-test0 192.0.2.40'
FAIL_ADD_IP=''

# Invalid input is rejected before any policy mutation.
printf '%s\n' '192.0.2.70;reboot' > "$MIHOMO_CLIENT_BYPASS_CONFIG"
: > "$CALLS"
if main apply >/dev/null 2>&1; then
    echo 'invalid address was accepted' >&2
    exit 1
fi
! grep -Eq ' rule (add|del) ' "$CALLS"
assert_rules 'lan-test0 192.0.2.30 lan-test0 192.0.2.40'

printf '%s\n' '192.0.2.30' '192.0.2.40' > "$MIHOMO_CLIENT_BYPASS_CONFIG"
main status | grep -q '^state=active$'

# A cleanup failure is transactional as well.
FAIL_DEL_IP=192.0.2.40
if main clear >/dev/null 2>&1; then
    echo 'clear unexpectedly succeeded after injected delete failure' >&2
    exit 1
fi
assert_rules 'lan-test0 192.0.2.30 lan-test0 192.0.2.40'
FAIL_DEL_IP=''
main clear >/dev/null
assert_rules ''
if main status | grep -q '^state=active$'; then
    echo 'cleared rules incorrectly reported active' >&2
    exit 1
fi

echo CLIENT_BYPASS_TEST_OK
