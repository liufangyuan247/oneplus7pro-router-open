# CLIProxyAPI on the OnePlus 7 Pro router

The phone runs CLIProxyAPI `v7.2.140-op7` as the `cpa` runit service. The
binary is built for Android/ARM64 from the matching upstream tag. A small local
replacement for `github.com/wlynxg/anet` avoids its obsolete Go 1.26 Android
`go:linkname` references while retaining the public interface-enumeration API
used by Pion.

## Phone paths

- Binary: `/data/data/com.termux/files/usr/bin/cli-proxy-api`
- Persistent configuration: `~/router-data/cpa/config.yaml`
- OAuth/account files: `~/router-data/cpa/auth`
- Client API key: `~/router-data/cpa/api-key`
- Management key: `~/router-data/cpa/management-key`
- Service: `~/router-stack/services/cpa`
- Rotated logs: `~/router-stack/services/cpa/log/main`
- CPA application and request logs: `~/router-data/cpa/logs` (128 MB cap)
- Management center: `http://192.168.2.1:8317/management.html`

The management key intentionally reuses the router-admin password so the two
local consoles have one operator credential. The client API key remains
independent. Both key files and the CPA configuration are mode 0600; the auth
directory is mode 0700. Do not commit their contents to this workspace.

The existing router firewall accepts management traffic from the LAN and
Tailscale interfaces, then drops new input arriving from PPPoE/WAN. CPA is also
covered by runit restart, the health monitor and the Magisk low-memory
protection loop.

## Local source and installers

- `source/`: pinned upstream source checkout
- `source/third_party/anet-android/`: Android build compatibility module
- `build/cli-proxy-api-android`: deployed Android/ARM64 binary
- `install-cpa.sh`: idempotent binary, data and runit installer
- `cpa-start.sh`: bounded readiness gate that starts CPA only after configured
  Mihomo-backed model and Antigravity egress are usable; stage it as
  `~/.stage/cpa-start` alongside the CPA binary before installation
- `deploy-cpa-start.sh`: installs the readiness gate on an existing phone with
  automatic run/start backups and health-checked rollback. Root-driven ADB
  deployments restore and verify Termux ownership before restarting the
  service, so a future root-owned `0700` file fails safely before interruption.
- `enable-cpa-health-probe.sh`: adds the CPA HTTP health probe
- `enable-cpa-oom-protection.sh`: adds CPA to the Magisk OOM protection loop
- `enable-cpa-request-logging.sh`: enables persistent application and detailed
  request logs with bounded disk usage and automatic rollback on failed restart
- `configure-antigravity-model-aliases.sh`: idempotently maps the Antigravity
  client model `gemini-3.8-flash` to CPA's provider model
  `gemini-3.8-flash-high`, then verifies the original client model name with a
  real streaming request and rolls back on failure. CPA builds must include
  alias-provider fallback routing when the upstream model is discovered at
  runtime rather than present in the bundled catalog.

Re-running `install-cpa.sh` preserves an existing `config.yaml`, account files
and keys. It only replaces the executable and regenerates credentials when
they are absent or match the rejected weak bootstrap value.
