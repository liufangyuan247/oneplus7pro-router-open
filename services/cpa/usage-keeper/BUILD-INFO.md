# CPA Usage Keeper Android build

- Upstream tag: `v1.15.11`
- Upstream commit: `71a41cc2d1cce8d75b42473adf31f2616ad8085e`
- Target: Android ARM64 (`android/arm64`, API 24+)
- Build: native Go/CGO with Termux clang; frontend embedded in the binary
- Binary: `build/cpa-usage-keeper-v1.15.11-android`
- Build manifest: `cpa-usage-keeper.build.json` beside the binary; stage it as
  `~/.stage/cpa-usage-keeper-v1.15.11-android.build.json`
- SHA-256: `33a4bb13756576c8a3666c23ff4358a2328d6504fb25c491fa8a8ed2662992c7`
- Phone service: `~/router-stack/services/cpa-usage-keeper`
- Persistent data: `~/router-data/cpa-usage-keeper`
- Web address: `http://192.168.2.1:8320`

The installer verifies the checksum and version before replacing files, backs up an existing installation, and restores it if the new service fails its health check.
The installed manifest is stored at `~/router-data/cpa-usage-keeper/build-info.json`.
