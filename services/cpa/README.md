# CLIProxyAPI on the OnePlus 7 Pro router

The phone runs a device-specific CLIProxyAPI build as the `cpa` runit service. The
binary is built for Android/ARM64 from the matching upstream tag. A small local
replacement for `github.com/wlynxg/anet` avoids its obsolete Go 1.26 Android
`go:linkname` references while retaining the public interface-enumeration API
used by Pion.

## Phone paths

- Binary: `/data/data/com.termux/files/usr/bin/cli-proxy-api`
- Persistent configuration: `~/router-data/cpa/config.yaml`
- Build metadata: `~/router-data/cpa/build-info.json` (custom and upstream base versions)
- OAuth/account files: `~/router-data/cpa/auth`
- Client API key: `~/router-data/cpa/api-key`
- Management key: `~/router-data/cpa/management-key`
- Service: `~/router-stack/services/cpa`
- Rotated logs: `~/router-stack/services/cpa/log/main`
- CPA application and request logs: `~/router-data/cpa/auth/logs` (128 MB cap)
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
- `deploy-cpa-build.sh`: checksum-verifies and atomically deploys a custom build
  plus its build manifest, then health-checks the API and a real model request;
  any failure restores the previous binary and metadata
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
  real streaming request and rolls back on failure. Re-run this verification
  whenever the pinned upstream CPA version changes.
- `sync-model-aliases.py`: previews or batch-adds unambiguous Gemini and Claude aliases
  through CPA's official management API. This replaces the older one-model
  helper for routine alias maintenance; see [automatic aliases](MODEL-ALIASES.md).

Re-running `install-cpa.sh` preserves an existing `config.yaml`, account files
and keys. It only replaces the executable and regenerates credentials when
they are absent or match the rejected weak bootstrap value.

The current verified Android build is `v8.1.2-op7.1`, based on upstream
`v8.1.2` (`85225381d89915595598ed13766cd79890c03a84`). Its public build
manifest is `build-info.v8.1.2-op7.1.json`; the only local compatibility change
remains the Android `anet` replacement. The matching deployed official
Management Center page is `v1.29.1`, SHA256
`30df9503402af03e04affb55920458c13139a07757c8428fc845299876d19c43`.
This update includes upstream Antigravity routing, thinking-level and tool-call
continuation fixes. Native Responses, Messages and Gemini streaming requests
passed after deployment. Existing settings and eight model aliases were
preserved; first startup added the upstream `server.server-id` instance field.

## OpenAI API credits before Codex subscription usage

Use a native `api-keys.codex` group for an official OpenAI API key. Give it a
higher `priority` than the existing file-backed Codex subscription and expose
the same model names. Clients keep one CPA endpoint and their existing client
key; CPA selects the higher-priority credential and fails over before streaming
output when the upstream refuses a request. Neither client-side model aliases
nor separate Codex providers are needed to select the funding source.

This routing applies only to clients explicitly using CPA. Keep local Codex's
normal signed-in provider as the default, and select the existing optional
`cpa` profile when using this gateway. Do not change Codex's global default
provider or its login credentials as part of server-side routing setup.

For example, the following group uses the official Responses API:

```yaml
api-keys:
  codex:
    - name: openai-grant
      base-url: https://api.openai.com/v1
      priority: 100
      request-retry: 0
      models:
        - name: gpt-6.1-sol
        - name: gpt-5.6-sol
      keys:
        - api-key: REPLACE_IN_PRIVATE_CONFIG
          proxy-url: http://127.0.0.1:17891
          disable-codex-cloaking: true
```

Keep session affinity disabled when every eligible request should prefer the
API credential over an already bound subscription. Do not add `disabled` to a
native Codex group: it is not a supported group field. Validate configuration
with a separate loopback-only instance of the actual Android binary before
restarting production. On 2026-11-09, `v8.0.61-op7.1` passed an isolated
`insufficient_quota` test: the first request tried the API credential and
completed through the existing subscription; the next request skipped that
credential while the upstream's `Retry-After` cooldown was active.

The API key does not expose the remaining promotional credit balance. Failover
depends on the upstream returning an error such as insufficient quota; if the
OpenAI project permits paid usage after promotional credits are spent, CPA
cannot distinguish that paid usage from promotional usage.
