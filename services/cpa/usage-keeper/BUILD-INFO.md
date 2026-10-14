# CPA Usage Keeper Android build

- Upstream tag: `v1.15.11`
- Upstream commit: `71a41cc2d1cce8d75b42473adf31f2616ad8085e`
- Target: Android ARM64 (`android/arm64`, API 24+)
- Build: native Go/CGO with Termux clang; frontend embedded in the binary
- Binary: `build/cpa-usage-keeper-v1.15.11-android`
- Build manifest: `cpa-usage-keeper.build.json` beside the binary; stage it as
  `~/.stage/cpa-usage-keeper-v1.15.11-android.build.json`
- SHA-256: `1ce86bcdf835194285515b9208bd65a1d55d1b3154d935afcc98d72eb850ef5e`
- Phone service: `~/router-stack/services/cpa-usage-keeper`
- Persistent data: `~/router-data/cpa-usage-keeper`
- Web address: `http://192.168.2.1:8320`

The installer verifies the checksum and version before replacing files, backs up an existing installation, and restores it if the new service fails its health check.
The installed manifest is stored at `~/router-data/cpa-usage-keeper/build-info.json`.

Before staging the Android source archive, run `prepare-android-source.ps1`
against a clean checkout of the recorded upstream commit. It builds the Vite
frontend and packages the resulting `web/dist` with the Go source. The Android
build refuses archives without `web/dist/index.html`, and installation verifies
both `/healthz` and the embedded dashboard page.
