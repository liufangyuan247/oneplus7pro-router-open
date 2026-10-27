# CPA Usage Keeper Android build

- Upstream tag: `v2.0.2`
- Upstream commit: `46fe6278db0c47a5460e882fa1742f15ab49bb14`
- Target: Android ARM64 (`android/arm64`, API 24+)
- Build: native Go 1.27.3/CGO with Termux clang; frontend embedded in the binary
- Binary: `build/cpa-usage-keeper-v2.0.2-android`
- Build manifest: `cpa-usage-keeper.build.json` beside the binary; stage it as
  `~/.stage/cpa-usage-keeper-v2.0.2-android.build.json`
- SHA-256: `8ecd3cb6fb5f52528f572b17a688b9ed407c24d04288c9fd5af9100f10b7863b`
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

Deployment on 2026-10-27 updated v2.0.0 to v2.0.2 with the Vite dashboard
embedded. All 811,414 pre-upgrade usage-event IDs were retained; the snapshot
passed SQLite integrity checking. Authenticated overview, pricing, version,
readiness, and desktop/mobile dashboard checks passed. Frontend typechecking
and 1,571 tests passed, along with native token-processor and helper tests.
CPA was first updated to `v8.0.44-op7.1`. The verified pre-upgrade database is
at `~/router-data/cpa-usage-keeper/install-backups/20261027-122607/app.db`.

Upstream v2 removes local/community ranking and persists request costs. It
rebuilds historical summaries from retained request details; changing current
pricing no longer changes historical costs automatically.
