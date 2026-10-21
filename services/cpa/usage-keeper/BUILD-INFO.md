# CPA Usage Keeper Android build

- Upstream tag: `v2.0.0`
- Upstream commit: `fc8cbde97fe6baa35189e51115716865ce377ec2`
- Target: Android ARM64 (`android/arm64`, API 24+)
- Build: native Go/CGO with Termux clang; frontend embedded in the binary
- Binary: `build/cpa-usage-keeper-v2.0.0-android`
- Build manifest: `cpa-usage-keeper.build.json` beside the binary; stage it as
  `~/.stage/cpa-usage-keeper-v2.0.0-android.build.json`
- SHA-256: `bf20f01c5ac2704a792fe53e8ca815b073a24d387f0c3acf722d1361bba8b4e2`
- Phone service: `~/router-stack/services/cpa-usage-keeper`
- Persistent data: `~/router-data/cpa-usage-keeper`
- Web address: `http://192.168.2.1:8320`

The installer verifies the checksum, version and CPA v8.0.12+ compatibility
before replacing files. Stage `backup-database.py` alongside the installer.
For upgrades, the installer preserves the existing environment and run scripts,
pauses healthd, CPA and the Keeper watchdog, stops Keeper, and creates a
standalone SQLite backup verified with `PRAGMA integrity_check` and SHA-256.
It waits for `/readyz` and the embedded HTML page before resuming services.
Migration can take several minutes; `/healthz` alone does not indicate readiness.
Failures restore both the old binary and the pre-upgrade database while retaining
the failed v2 database for diagnosis. Never open a migrated v2 database with v1.
The installed manifest is stored at `~/router-data/cpa-usage-keeper/build-info.json`.

Before staging the Android source archive, run `prepare-android-source.ps1`
against a clean checkout of the recorded upstream commit. It builds the Vite
frontend and packages the resulting `web/dist` with the Go source. The Android
build refuses archives without `web/dist/index.html`, and installation verifies
both `/healthz` and the embedded dashboard page.

Deployment on 2026-10-19 migrated all 616,503 existing usage events without
missing IDs; authenticated overview, pricing, CPA subscription and the embedded
JavaScript/CSS checks passed. CPA was upgraded to `v8.0.33-op7.1` first.
The pre-upgrade database is stored on the phone under
`~/router-data/cpa-usage-keeper/install-backups/20261019-152906/app.db`, with a
second verified copy in the workspace at
`backups/service-upgrade-20261019/keeper-v1/app.db`.

Upstream v2 removes local/community ranking and persists request costs. It
rebuilds historical summaries from retained request details; changing current
pricing no longer changes historical costs automatically.
