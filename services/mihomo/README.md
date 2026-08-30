# Mihomo unified routing stack

This directory contains the standalone Mihomo service used by the OnePlus 7 Pro router.

## Current deployment

- Core: Mihomo `v1.19.30`
- Native TUN: `op7tun`, gVisor stack, route table `2022`, rules `8100-8110`
- Physical egress: `ppp0`; root UID 0 bypasses the TUN to prevent proxy loops
- Generic local listener: `127.0.0.1:17890`, follows `rule` / `global` / `direct`
- CPA listener: `127.0.0.1:17891`, pinned to `AI`
- Controller: `127.0.0.1:9091`
- Persistent mode: `direct` on a fresh install; `rule` after the local
  allowlist has been validated
- LAN exposure, Redirect and TProxy listeners: disabled
- Subscription refresh: every six hours
- Health recovery: controller, singleton PID, listeners, TUN route and real
  `17891` ChatGPT/OpenAI egress are checked every minute; healthd restarts
  Mihomo after three consecutive actionable failures

Mihomo now owns routing for both forwarded LAN clients and Android-local
applications. Private, management, multicast and Tailscale networks are
excluded from the TUN. The root network manager only maintains PPPoE, DHCP,
firewalling and the physical fail-open route; it no longer launches the
`op7proxy` / `op7codex` sing-box instances or policy tables 101/102.

`AI-日本自动` latency-tests Japanese nodes and excludes the known-bad
`日本 | 03`, `日本 | 04` and `日本 | 05` entries. The `AI` group exposes only
that automatic choice, `DIRECT`, and healthy Japanese subscription nodes.
`家庭RULE自动` tests every subscription node, while `家庭RULE` exposes that
automatic choice, `DIRECT`, and every individual subscription node.

CPA currently uses `proxy-url: "http://127.0.0.1:17891"` as an explicit,
loopback-only migration bridge. This makes the CPA egress independent of its
process UID while still using the same Mihomo core and the `AI` group. No proxy
listener is exposed to the LAN.

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
- Service: `~/router-stack/services/mihomo`
- Update service: `~/router-stack/services/mihomo-updater`
- Data-plane probe: `~/router-stack/bin/mihomo-health-probe`
- Last probe state: `~/router-data/health/mihomo-data-plane.status`

Subscription updates are downloaded to a temporary directory, adapted,
validated with `mihomo -t`, and atomically activated. Set
`MIHOMO_UPDATE_VALIDATE_ONLY=1` to generate
`config.unified-candidate.yaml` without replacing the live configuration.

The subscription is a node transport only. Its `proxy-groups`,
`rule-providers`, and `rules` sections are discarded. AI policy comes from
`~/router-data/mihomo/ai-proxy-domains.txt`, with optional additions in
`custom-ai-proxy-domains.txt`; those domains select `AI`. The general household
allowlist comes from `selective-proxy-domains.txt`, with optional additions in
`custom-proxy-domains.txt`; those domains select `家庭RULE`. The generated
policy also sends Telegram's MTProto IPv4 ranges to `家庭RULE`, because the
mobile app commonly connects to literal IP addresses instead of hostnames. It
always ends with `MATCH,DIRECT`.

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
pidof mihomo
pidof sing-box
~/router-stack/bin/mihomo-update-subscription
```

Local source verification:

```sh
bash -n mihomo/*.sh router-stack/router-network.sh
cd router-admin && go test ./...
cd ../router-admin-ui && npm run export
```
