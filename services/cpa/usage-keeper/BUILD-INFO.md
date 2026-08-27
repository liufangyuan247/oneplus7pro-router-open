# CPA Usage Keeper Android build

- Upstream tag: `v1.14.8`
- Upstream commit: `c0129050def1f3e980ed68492014326d35710917`
- Target: Android ARM64 (`android/arm64`, API 24+)
- Build: native Go/CGO with Termux clang; frontend embedded in the binary
- Binary: `build/cpa-usage-keeper-v1.14.8-android`
- SHA-256: `4cbd3ba408f10b899c35db96134ac56e4b3a8b429d507269bd40bfa765f2a052`
- Phone service: `~/router-stack/services/cpa-usage-keeper`
- Persistent data: `~/router-data/cpa-usage-keeper`
- Web address: `http://192.168.2.1:8320`

The installer verifies the checksum and version before replacing files, backs up an existing installation, and restores it if the new service fails its health check.
