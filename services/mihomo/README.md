# Mihomo unified routing stack

This directory contains the standalone Mihomo service used by the OnePlus 7 Pro router.

## Current deployment

- Core: Mihomo `v1.19.30`
- Native TUN: `op7tun`, Mixed stack with GSO, route table `2022`, rules `8100-8110`
- LAN TCP/UDP: Mihomo `tproxy` listener on `17893`; policy table `2023` and
  mark `0x20000000` avoid the forwarded-packet/single-queue TUN bottleneck.
  The local-route rule is restricted to LAN ingress so the mark cannot loop
  Mihomo's own DIRECT sockets back into the listener.
- Physical egress: `ppp0`; root UID 0 bypasses the TUN to prevent proxy loops,
  while priority `8099` returns Mixed's internal `198.18.0.0/30` TCP path to `op7tun`
- Generic local listener: `127.0.0.1:17890`, follows `rule` / `global` / `direct`
- Codex/CPA listener: `127.0.0.1:17891`, pinned to `AI`
- Antigravity listener: `127.0.0.1:17892`, pinned to `Antigravity`
- Node benchmark listener: `127.0.0.1:17894`, pinned to `家庭测速`
- Controller: `127.0.0.1:9091`
- Persistent mode: `direct` on a fresh install; `rule` after the local
  allowlist has been validated
- LAN exposure, Redirect and TProxy listeners: disabled
- Subscription refresh: every six hours
- Health recovery: controller, singleton PID, listeners, TUN route and real
  `17891` ChatGPT/OpenAI egress are checked every minute; subscription
  activation additionally requires both `17891` and `17892`; healthd restarts
  Mihomo after three consecutive actionable failures

Mihomo now owns routing for both forwarded LAN clients and Android-local
applications. Private, management, multicast and Tailscale networks are
excluded from the TUN. The root network manager only maintains PPPoE, DHCP,
firewalling and the physical fail-open route; it no longer launches the
`op7proxy` / `op7codex` sing-box instances or policy tables 101/102.

`AI-日本自动` latency-tests all Japanese subscription nodes and selects the
fastest healthy exit. The `AI` group exposes that automatic choice, `DIRECT`,
and all individual Japanese subscription nodes.
`Antigravity` is a separate selector containing only US subscription nodes. An
update preserves the currently selected Antigravity node by default; set
`ANTIGRAVITY_NODE` to override it. This prevents Google location policy from
sharing the Japanese Codex egress or changing the existing `AI` selection.
`家庭RULE自动` tests every subscription node, while `家庭RULE` exposes that
automatic choice, `DIRECT`, and every individual subscription node.

CPA currently uses `proxy-url: "http://127.0.0.1:17891"` as an explicit,
loopback-only migration bridge for Codex. Each Antigravity auth file overrides
that default with `proxy_url: "http://127.0.0.1:17892"`. Both providers use the
same Mihomo core but independent selectors, and neither listener is exposed to
the LAN.

DNS uses dedicated domestic resolvers for proxy-node hostnames. Overseas DoH
fallbacks are explicitly bound to `ppp0`, avoiding a proxy/DNS bootstrap loop.

## Failure behavior

When Mihomo exits, its auto-route rules and `op7tun` are withdrawn. The
priority-9000 physical route through table 100 remains, so normal router and LAN
traffic fails open to `ppp0`. CPA/Codex traffic intentionally does not bypass
its dedicated proxy listener and is unavailable until Mihomo recovers.

Stopping Mihomo through Router Admin creates
`~/router-data/mihomo/disabled`. The health monitor and subscription updater
respect this marker, so an intentional stop survives service supervision and
reboot.

The Mihomo health probe distinguishes a dead proxy data plane from a physical
WAN outage. A working direct connectivity check plus failed `17891` egress is
actionable; an unavailable WAN is reported as degraded without restarting the
core. After a core PID change, external-egress restarts have a five-minute
cooldown to prevent restart loops, while broken local PID/TUN/listener
invariants remain immediately actionable.

## Persistent paths on the phone

- Config and selection cache: `~/router-data/mihomo`
- Subscription list: `~/router-data/mihomo/subscriptions.json`
- Active subscription URL: `~/router-data/mihomo/subscription.url`
- Last known-good config: `~/router-data/mihomo/config.last-good.yaml`
- Listener overrides: `~/router-data/mihomo/runtime-settings.yaml`
- Persistent mode: `~/router-data/mihomo/runtime-mode`
- Benchmark results: `~/router-data/mihomo/speedtest-results.json`
- Service: `~/router-stack/services/mihomo`
- Update service: `~/router-stack/services/mihomo-updater`
- Data-plane probe: `~/router-stack/bin/mihomo-health-probe`
- Last probe state: `~/router-data/health/mihomo-data-plane.status`
- Client TUN bypass list: `~/router-data/mihomo/client-bypass-ipv4.txt`
- Active LAN input interface: `~/router-data/mihomo/client-bypass-lan-iface`
- Client TUN bypass control: `~/router-stack/bin/mihomo-client-bypass`

## Per-client native TUN bypass

Some cameras and other peer-to-peer appliances rely on stable kernel NAT and
STUN endpoint mappings. A Mihomo `DIRECT` decision still crosses `op7tun` and
the gVisor userspace stack, so such a device can be assigned a source-IP policy
bypass without changing proxy policy for the rest of the home.

Put the current LAN interface name in `client-bypass-lan-iface`, and one IPv4
address per line in `client-bypass-ipv4.txt`; blank lines, comments, and
duplicates are ignored. Both values come from runtime/private deployment data:
real interfaces and client addresses never belong in this repository. Then run
through Magisk root:

```sh
su -c '/data/data/com.termux/files/home/router-stack/bin/mihomo-client-bypass apply'
su -c '/data/data/com.termux/files/home/router-stack/bin/mihomo-client-bypass status'
su -c '/data/data/com.termux/files/home/router-stack/bin/mihomo-client-bypass clear'
```

The script reserves priority `4990` and routes only packets matching the active
LAN input interface and a configured source `/32` through physical WAN table
`100`. This is strictly ahead of the live priority `5000` path and cannot
select Mihomo table `2022`. Apply is idempotent;
an unavailable WAN table, invalid input, partial update, or verification error
leaves or restores the previous rule set. Mihomo startup restores configured
rules before creating `op7tun`, and the data-plane health probe repairs policy
rule drift after boot or an external flush. `clear` removes runtime rules but
does not erase the persistent list. Priority `4990` is reserved exclusively for
this helper: any foreign rule at that priority makes apply, clear, and status
fail without modifying policy state. A managed rule displayed by the kernel
with an `iif ... [detached]` marker is recognized as owned state and is
removed and recreated against the live LAN interface on the next apply.

Subscription updates are downloaded to a temporary directory, adapted,
validated with `mihomo -t`, and atomically activated. Set
`MIHOMO_UPDATE_VALIDATE_ONLY=1` to generate
`config.unified-candidate.yaml` without replacing the live configuration.

The subscription is a node transport only. Its `proxy-groups`,
`rule-providers`, and `rules` sections are discarded. AI policy comes from
`~/router-data/mihomo/ai-proxy-domains.txt`, with optional additions in
`custom-ai-proxy-domains.txt`; those domains select `AI`.
Antigravity policy is managed independently in
`antigravity-proxy-domains.txt`, with optional additions in
`custom-antigravity-proxy-domains.txt`; those domains select the US-only
`Antigravity` group for both transparent routing and policy-bound DNS/DoH.
Together with the per-account `17892` proxy override, this keeps Antigravity
on the US exit even when a request is not made through CPA's explicit proxy.

`~/router-data/mihomo/routing-policy` selects one of two locally managed
policies:

- `allowlist` retains the conservative legacy behavior. Domains from
  `selective-proxy-domains.txt` and `custom-proxy-domains.txt` select
  `家庭RULE`, while the final rule is `MATCH,DIRECT`.
- `cn-direct` keeps those domain files as force-proxy overrides, then applies
  `GEOSITE,CN,DIRECT`, `GEOIP,CN,DIRECT`, and finally `MATCH,家庭RULE`. This
  gives mainland services a direct path while unknown overseas services no
  longer require manual allowlist additions.

Both policies send Telegram's MTProto IPv4 ranges to `家庭RULE`, because the
mobile app commonly connects to literal IP addresses instead of hostnames.
`cn-direct` preserves the deployed selective fake-IP behavior: AI and explicit
force-proxy domains keep fake mappings, while mainland and previously unknown
overseas domains receive real answers. Mainland domains use domestic resolvers;
non-mainland/default queries use trusted DoH through `家庭RULE`. Keeping
unlisted answers real is important for source-IP clients that bypass `op7tun`,
because a fake `198.18/16` answer cannot be translated after the kernel sends
that client directly to physical table 100.

## Management and verification

Router Admin exposes mode selection, TUN state, proxy groups, subscriptions,
CPA status, logs and an authenticated MetaCubeXD reverse proxy at
`http://192.168.2.1:9090` under **代理**. The controller itself remains
loopback-only.

Useful checks on the phone:

```sh
/data/adb/router-network/router-network status
ip rule show
ip route show table 2022
su -c '~/router-stack/bin/mihomo-client-bypass status'
pidof mihomo
~/router-stack/bin/mihomo-update-subscription
```

Local source verification:

```sh
sh -n services/mihomo/*.sh services/mihomo/*-run
sh services/mihomo/test-client-bypass.sh
cd router-admin && go test ./...
cd ../router-admin-ui && npm run export
```
