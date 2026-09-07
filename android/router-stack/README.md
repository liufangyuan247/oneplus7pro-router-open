# OnePlus 7 Pro router service stack

This stack runs application services as the Termux UID and uses a Magisk
`service.d` keeper only for boot integration, wake-lock handling and process
protection.

## Managed services

| Service | Local listener | Purpose |
|---|---:|---|
| OpenList | `0.0.0.0:5244` | File-list service and LAN WebUI |
| aria2 | `0.0.0.0:6800` | Download RPC (trusted LAN; optional secret) |
| AriaNg/Caddy | `0.0.0.0:6880` | LAN aria2 web interface |
| qBittorrent-nox | `0.0.0.0:8080` | qBittorrent WebUI |
| CLIProxyAPI | `0.0.0.0:8317` | AI subscription API and management center |
| Mihomo | `127.0.0.1:17890` | Generic local port, direct by default |
| Mihomo Codex | `127.0.0.1:17891` | CPA-only Japanese proxy group |
| OpenSSH | `0.0.0.0:8022` | Public-key-only administration |
| Router Admin | `0.0.0.0:9090` | Authenticated All-in-One management UI |
| healthd | none | Health probes and restart after 3 failures |

The qBittorrent peer port is separate from its WebUI and is expected to
listen on network interfaces when torrenting is enabled.

## Paths on the phone

- Stack: `/data/data/com.termux/files/home/router-stack`
- Persistent data: `/data/data/com.termux/files/home/router-data`
- Rotated service logs: `router-stack/services/<name>/log/main`
- Magisk entry: `/data/adb/service.d/20-oneplus-router.sh`
- USB default mode: `/data/adb/service.d/10-usb-mtp-adb.sh`
- Toshiba HDD compatibility/monitor: `/data/adb/service.d/23-router-storage.sh`
- Optional charge guard: `/data/adb/service.d/30-battery-guard.sh`
- Root Tailscale helper: `/data/adb/service.d/25-tailscale-root.sh`

The R2S's 2 TB `ROUTER_DISK` NTFS disk has filesystem UUID
`ROUTER_VOLUME_ID`. Android mounts it at
`/mnt/media_rw/ROUTER_VOLUME_ID`; the storage helper recreates the old R2S
path `/mnt/usb_disk` as a stable symlink and refreshes OpenList on hot-plug.
OpenList storage `/移动硬盘` therefore keeps its imported root path
`/mnt/usb_disk/` unchanged.
- Magisk keeper logs: `/data/adb/router-stack`

The standalone Mihomo core keeps a generic local port on `127.0.0.1:17890`
and a dedicated CPA listener on `127.0.0.1:17891`. The latter is pinned to
the `Codex-日本` group; the generic path stays direct. TUN, LAN listening and
transparent routing remain disabled, so it does not change forwarded
household traffic. FlClash has been removed; Android-local proxying is handled
by Mihomo's explicit system-proxy switch. Port `7890` is no longer active.
Root Tailscale remains independent on `tailscale0`.

Each runit log is limited to five 1 MiB files. OpenList's duplicate built-in
file log is disabled.

The USB helper sets Android's persistent peripheral default to `mtp,adb` and
keeps authenticated wireless ADB on TCP 5555 available during setup. It does
not force the Type-C data role and does not prevent USB Host operation for the
CM418. The Magisk keeper also holds a kernel suspend blocker while the router
stack is enabled and forces attached USB devices out of autosuspend.

The battery guard is enabled by
`/data/adb/router-stack/battery-guard.enabled`. Powered passthrough was tested:
with charging disabled, USB-PD, Host mode and RTL8153 remained active while
battery current reported zero. It defaults to 40%-60% charge hysteresis for
battery swelling prevention, configurable via `/data/adb/router-stack/battery-guard.conf`
or the Web Admin console (20%-95% valid range). To disable the feature for the current boot,
remove the marker; the controller restores charging within 10 seconds. The marker is recreated
automatically on every boot. A readable heartbeat for the dashboard is written to
`router-data/admin/battery-guard.status` every 10 seconds.

The charge controller is deliberately fail-open: invalid battery telemetry,
a failed sysfs write, a controller exit, or a stale heartbeat always restores
charging instead of risking a flat battery. An independent 20-second watchdog
can only enable charging; it never pauses charging. It restarts a dead or stale
controller after first restoring charging, while the controller checks and
revives the watchdog once per minute. Thus a single process failure may
temporarily allow charging toward 100%, but cannot leave charging permanently
disabled.

## Management from Termux

```sh
~/router-stack/bin/routerctl status
~/router-stack/bin/routerctl restart
~/router-stack/bin/routerctl logs openlist
~/router-stack/bin/routerctl stop
~/router-stack/bin/routerctl start
```

`stop` removes the enabled marker. `start` recreates it, and the Magisk keeper
starts the supervisor within 30 seconds.

## Management access

The All-in-One management console is available locally on the phone at
`http://127.0.0.1:9090` and on the current management LAN at
`http://<phone-address>:9090`. Its random initial password is stored with mode
0600 at `~/router-data/admin/password`. It shows live CPU, memory, thermal,
battery/PD, interface, client, service, storage and USB data. Network settings
remain a non-applied draft until both Ethernet adapters are identified.

OpenList is available on the current management LAN at
`http://<phone-address>:5244`. SSH uses the administrator public key and no
password authentication:

CLIProxyAPI is available at `http://<phone-address>:8317`; its management
center is `/management.html`. The management password reuses the router-admin
password, while its independent client API key is stored with mode 0600 in
`~/router-data/cpa/api-key`. The service, account credentials and configuration
survive both service and device restarts.

```sh
ssh -p 8022 <user>@<phone-address>
```

ADB forwards remain useful for services intentionally bound to loopback:

```powershell
adb forward tcp:15244 tcp:5244
adb forward tcp:16800 tcp:6800
adb forward tcp:16880 tcp:6880
adb forward tcp:8080 tcp:8080
```

- OpenList: <http://127.0.0.1:15244>
- AriaNg: <http://127.0.0.1:16880>
- qBittorrent: <http://127.0.0.1:8080>

AriaNg should use `http://127.0.0.1:16800/jsonrpc`. RPC authentication is
disabled when `~/router-data/aria2/aria2.conf` has no `rpc-secret` entry; an
existing non-empty secret is still detected and used by stack components.

AriaNg is also the authority for aria2 concurrency and other tunable global
options. `aria2-config-sync` watches for RPC changes and writes only changed
options back to the persistent `aria2.conf`; it does not impose a fixed
concurrency value. OpenList, qBittorrent and aria2 data/configuration all live
under `~/router-data` and installers preserve existing application settings.

## USB Ethernet state

The current OxygenOS 11 stock kernel has `CONFIG_USB_RTL8152=y`. The CM418
enumerates as Realtek RTL8153 (`0bda:8153`), binds to `r8152` v1.09.9 and
creates `eth0` over USB SuperSpeed. The phone can remain a USB-PD power sink
while acting as USB data host. No custom kernel is required for this adapter.

For a stable Type-C role, connect the PD charger to the CM418 first and then
connect the hub to the phone. Running the whole hub from phone power caused
rapid Type-C role changes and eventually an xHCI controller reset during
testing; powered operation remained stable.

The deployed role policy reserves the CM418 built-in RTL8153 (USB path
`2-1.4`) for WAN and automatically selects the one remaining USB Ethernet
adapter as LAN. At the 2026-08-26 snapshot this resolves to `eth0` WAN,
AX88179B `eth1` LAN, `ppp0` Internet and `192.168.2.1/24` management LAN. The
policy does not rely on the replaceable LAN adapter's `ethX` name.

## Unified Mihomo routing

Mihomo now owns the only proxy TUN (`op7tun`). The router manager no longer
starts the former `op7proxy` or `op7codex` sing-box TUNs, a second dnsmasq, or
tables 101/102. `proxy-on` switches Mihomo to rule mode and installs the LAN
policy through table 2022; `proxy-off` withdraws that policy and returns LAN
traffic and DNS to direct PPPoE. Route and DNS changes are reconciled as one
state transition so failure falls open to the direct WAN.

CPA/Codex integrations use Mihomo's dedicated local listeners and geosite
rules. The `codex_*` status keys remain compatibility aliases for existing
health and link scripts; they do not represent a separate TUN or resolver.

Useful checks:

```sh
/data/adb/router-network/router-network status
/data/adb/router-network/router-network proxy-on
ip rule show priority 5000
ip -4 route show table 2022
/data/adb/router-network/router-network proxy-off
```

The expected rule-mode state is `whole_home_proxy=enabled`, `mihomo_tun=up`,
`lan_tun_policy=active`, and `codex_policy=active`. In direct mode the two
policy fields are inactive while Mihomo and its local CPA listeners remain
available.

## Current stability state

- The active `boot_a` SHA-256 matches the verified v4 final Magisk image
  recorded in the private recovery inventory, so a normal reboot keeps the
  exact kernel ABI used by the pinned performance modules.
- `ppp0` uses `txqueuelen=1024` and `fq_codel`. USB autosuspend is disabled and
  the router keeper holds the `oneplus7-router` suspend blocker.
- Rule/global mode sends all LAN TCP and UDP to Mihomo's managed TPROXY
  listener. Phone-local applications retain Mixed TUN, while LAN connections
  avoid the extra forwarded-packet pass through `op7tun`. Private/LAN control
  destinations remain on the kernel path, and a missing listener fails back
  to the existing TUN/direct state machine.
- On the exact verified `4.14.190-perf+` build, the performance service loads
  checksum-pinned `fq`, BBR, IFB and TBF modules, selects BBR for
  router-originated TCP, and applies RPS/RFS/XPS to recreated Ethernet/PPP
  queues. It skips all module changes on any other kernel build.
- Kernel PPPoE is selected with `PPPOE_BACKEND=auto`. A trial is not confirmed
  by an IP address alone: the kernel session, WAN route, Mihomo TUN and the
  Codex/OpenAI proxy path must all pass. Initial failure falls back after 45
  seconds; an established kernel session that remains down falls back after
  120 seconds. The userspace backend remains available as an emergency path.
- The Tailscale service entry validates `/proc/<pid>/exe` instead of trusting a
  reusable PID, and has a single-instance supervisor. Its pre-fix backup is
  `/data/adb/service.d/25-tailscale-root.sh.backup-pre-pidfix-20260826`.
- The network manager and supervisor use atomic single-instance locks, adopt a
  verified existing dnsmasq during live updates, and leave an already-correct
  priority-9000 policy rule in place.
- Double-tap and wake gestures are disabled to avoid the observed OnePlus
  infrared regulator imbalance warning during display wake.
- The boot rollback guard is installed. Manager and supervisor candidates must
  pass three health checks before their pending/trial markers are cleared.
  Live-update manifests record the pre-trial pppd PID, so rolling back only the
  control script preserves an adopted PPPoE session instead of redialing it.

## Benchmarks

See [`BENCHMARKS.md`](BENCHMARKS.md) for the virtual WAN/LAN NAT test, current
SSD filesystem throughput, Wi-Fi terminal transfer result, limitations and
safe repeat-test scripts.
