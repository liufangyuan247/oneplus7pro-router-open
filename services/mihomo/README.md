# Mihomo management stack

This directory contains the standalone Mihomo service used by the OnePlus 7 Pro router.

## Current deployment

- Core: Mihomo `v1.19.30`
- Generic local listener: `127.0.0.1:17890` (`MATCH,DIRECT` in rule mode)
- OpenAI selective listener: `127.0.0.1:17891` -> `Codex-日本`
- Controller: `127.0.0.1:9091`
- Mode: `rule`
- Android system proxy: disabled by default
- LAN listener, TUN, redir and TProxy: disabled by default
- CPA outbound: direct (`proxy-url: ""`), captured by router table 102
- Subscription refresh: every six hours

FlClash is no longer installed and port `7890` is intentionally unused. The
standalone core remains isolated on `17890` and `17891`.

`Codex-日本` is regenerated on every subscription refresh. It contains a
selectable Japanese-node group and a `Codex-日本自动` latency-tested subgroup.
The automatic subgroup probes `chatgpt.com` every 30 seconds, excludes the
known-bad `日本 | 04` node, requires two failed checks before declaring a node
unhealthy, and uses a 1000 ms tolerance to suppress latency-jitter switching.
Probes do not change the egress IP by themselves. `Codex-日本自动` is the first
choice in the parent group, while every Japanese node remains available for
manual selection in `Codex-日本`.
The OpenAI-only `op7codex` TUN uses the dedicated listener and pins every
selected OpenAI connection to that group. CPA has no private proxy setting;
its normal OpenAI sockets and LAN client sockets are selected by the same
trusted DNS and table-102 policy. The independent whole-home TUN remains off,
so unrelated household traffic stays direct.

When Android system proxy is explicitly enabled from Router Admin, it points
to the same isolated Japanese listener on `127.0.0.1:17891`. Stopping Mihomo
through any Router Admin service action clears the Android proxy first. The
offline API returns stable empty/default state and the Dashboard button is
disabled, rather than surfacing controller errors.

## Persistent paths on the phone

- Config and selection cache: `~/router-data/mihomo`
- Subscription list: `~/router-data/mihomo/subscriptions.json`
- Active subscription URL: `~/router-data/mihomo/subscription.url`
- Last known-good config: `~/router-data/mihomo/config.last-good.yaml`
- Listener overrides: `~/router-data/mihomo/runtime-settings.yaml`
- Persistent mode: `~/router-data/mihomo/runtime-mode`
- Service: `~/router-stack/services/mihomo`
- Update service: `~/router-stack/services/mihomo-updater`

Subscription URLs are returned to the web UI only as masked origins. Each update is downloaded to a temporary directory, sanitized, validated with `mihomo -t`, and then atomically activated. A failed start restores the last known-good configuration.

Stopping Mihomo through Router Admin creates `~/router-data/mihomo/disabled`. The health monitor and subscription updater respect this marker, so an intentional stop survives service supervision and reboot. Starting or restarting the service removes it.

## Management

Router Admin exposes the complete management page at `http://192.168.2.1:9090` under **代理**. It includes service lifecycle, mode, node selection and delay tests, listener settings, Android system proxy, active connections, multiple subscriptions, logs, CPA status, and an authenticated reverse proxy for MetaCubeXD.

The MetaCubeXD controller remains loopback-only. Router Admin proxies its UI and API through the authenticated session instead of exposing port `9091` to the LAN.

## Verification

Run on the phone as the Termux user:

```sh
~/router-stack/bin/mihomo-update-subscription
~/.stage/verify-management.sh
```

Local source verification:

```sh
bash -n mihomo/*.sh
cd router-admin && go test ./...
cd ../router-admin-ui && npm run export
```
