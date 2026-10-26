# OnePlus Router Admin

Local-only All-in-One management console for the rooted OnePlus 7 Pro router
appliance. The Go backend runs as the Termux application UID under the existing
runit supervisor and serves the statically exported dashboard on TCP 9090.

The console provides authenticated live monitoring for CPU, memory, thermal
zones, battery/PD, interfaces, clients, storage, USB and runit services. Service
actions use a fixed allowlist. Network settings are saved as a non-applied draft
until both Ethernet adapters have been identified by stable hardware identity.
Interface throughput is sampled once per second by the backend and reported as
a five-second sliding average, so multiple dashboard clients cannot disturb the
measurement window or amplify short proxy and storage-buffer bursts.

The **日志** page is a read-only OpenWrt-style diagnostic viewer. It combines
kernel, removable-storage, network, aria2, OpenList and Router Admin logs, marks
I/O errors and USB disconnects by severity, supports source/level/search filters
and refreshes every five seconds. The aggregated event view also detects stale
block-device mount records left behind after a USB disk reconnect. URL query
strings and common secret fields are redacted before log entries reach the
browser. Kernel history starts again after each device reboot.

CLIProxyAPI (CPA) is managed as the `cpa` runit service. Its service card opens
the bundled management center on TCP 8317 and exposes the same start, stop,
restart and log controls as the other application services.

CPA Usage Keeper is managed as the `cpa-usage-keeper` runit service. Its card
shows the live PID and uptime, opens the standalone dashboard on TCP 8320, and
uses the common log, start, stop and restart controls.

The **代理** page manages the standalone Mihomo core, multiple subscriptions,
proxy groups, node delay tests, connections, persistent mode/listener settings,
and whole-home transparent routing. The whole-home switch covers
both LAN clients and local Android/Termux application traffic; there is no separate
Android system-proxy switch. MetaCubeXD is reached through an authenticated reverse
proxy, while Mihomo's controller remains bound to loopback.

Each subscription card also offers **订阅中转** for Clash / Mihomo clients.
Enable it to get a persistent URL and a locally generated QR code. Public
`GET /sub/<id>/<random-token>/<subscription-name>.yaml` requests read only that subscription's local
node cache. They never download the provider URL, including when the provider
link has expired or the cache is unavailable. Changing the source URL makes an
old unmatched cache unavailable until the owner explicitly refreshes it.
The response body is the original cached subscription document, byte for byte,
including all nodes, groups, rules, DNS, comments and provider definitions. No
client routing configuration is regenerated. The profile title and UTF-8 download
filename use the router subscription name with `（中转）` appended. Newly copied
URLs also end in that readable filename; previously imported token-only URLs
remain valid and serve the same original document.

Relay management uses the authenticated
`/api/mihomo/subscriptions/relay` endpoint. POST actions `enable`, `disable` and
`rotate` operate independently for each subscription and preserve its upstream
URL, active state and automatic-update policy. A 256-bit token is persisted in
the existing mode-0600 subscription store; disabling or rotating invalidates the
old URL. QR images are generated on the router without third-party services.
Clients must be able to reach the address used to open the management page;
remote users can open it through their existing Tailscale connection.

`deploy-subscription-relay.sh <binary-sha256> <web-archive-sha256>` installs
`~/.stage/router-admin-relay` and `~/.stage/router-admin-relay-web.tar.gz`, keeps
the existing service scripts, backs up the backend and frontend, verifies that
subscription cache checksums remain unchanged, and rolls back an unhealthy
release. It does not refresh subscriptions or restart the Mihomo core.

The **服务** page also contains a protected Tailscale control panel. It reports
the node state, Tailscale IP and `192.168.2.0/24` subnet-route status, and offers
start, restart, guarded stop, login and reauthorization actions. These actions
operate the existing root supervisor and do not migrate Tailscale into runit.

Runtime paths:

- Binary: `/data/data/com.termux/files/usr/bin/router-admin`
- UI: `/data/data/com.termux/files/home/router-admin/web`
- Password: `/data/data/com.termux/files/home/router-data/admin/password`
- Remembered sessions: `/data/data/com.termux/files/home/router-data/admin/sessions.json`
  (hashed session identifiers only, mode `0600`; explicit logout revokes them)
- Network draft: `/data/data/com.termux/files/home/router-data/admin/network-draft.json`
- Service: `/data/data/com.termux/files/home/router-stack/services/router-admin`
- Browser terminal: `/api/terminal` (authenticated WebSocket, at most two sessions)
- Browser screen control: `/api/screen` (one on-demand scrcpy session)
- Deterministic wake/unlock and lock actions: `/api/device/power` (authenticated POST)
- Battery guard configuration: `/api/power/battery-guard` (authenticated GET / POST)
- Read-only system log viewer: `/api/system-logs` (authenticated GET)
- scrcpy server: `/data/data/com.termux/files/home/router-data/remote/scrcpy-server-v3.3.1`

The dashboard's Remote page provides an in-browser Termux/root terminal and
an in-browser Android screen with mouse, touch, wheel and keyboard control.
Screen encoding is limited to 1280 pixels, 30 FPS and 3 Mbit/s, starts only
while the page is connected, and is terminated when the page closes.
The encoder uses H.264 Baseline / Level 4 so the software decoder also works
when the dashboard is opened over plain LAN HTTP and WebCodecs is unavailable.
It also starts with `power_on=false`, so opening the web screen does not wake
the physical OLED panel or trigger OnePlus display/infrared power handling.
The screen toolbar can explicitly wake and dismiss an unsecured swipe lock, or
put the display back to sleep. A configured PIN, pattern, or password is never
bypassed.

Text clipboard sharing is bidirectional. With the screen focused, `Ctrl+V`
copies the browser clipboard to Android and pastes it into the focused field.
Android clipboard changes are returned to the dashboard; HTTPS pages try to
write them to the browser clipboard automatically, while the LAN HTTP page
offers a **复制手机剪切板** button because browsers forbid silent clipboard
access in an insecure context. Clipboard payloads sent by the browser are
limited to 240 KiB so they remain below the screen WebSocket read limit.

Every dashboard deployment stores the previous binary, static site and scrcpy
server under `router-data/backups/router-admin/<timestamp>`. The installer
checks both the health API and the static home page after restarting; if either
check fails, it restores the previous release before returning an error.

## Reversible Android router profile

`debloat-router.sh` disables 70 nonessential Google/OnePlus applications for
Android user 0 without deleting system APKs. Core UI, Settings, WebView,
keyboard, networking/tethering, USB, media storage, DocumentsUI, file manager,
Termux and Magisk remain enabled.

Rollback data is stored on the phone at
`/sdcard/OnePlusRouter/debloat-disabled-packages.txt`; run
`su -c 'sh /sdcard/OnePlusRouter/restore-debloat.sh'` to re-enable only the
packages disabled by this profile. `com.oneplus.opbackup`, which was already
disabled by the OTA blocker, is intentionally excluded from the rollback list.

## OxygenOS Chrome compatibility

The stock OxygenOS 11 browser is Chrome 87. Next.js 16 emits class static
blocks that require a newer browser, which previously left the phone on the
server-rendered “connecting to router core” screen. `npm run export` now runs
`scripts/compat-chrome87.mjs` after the static export and transpiles every
JavaScript chunk with an esbuild `chrome87` target before deployment.
