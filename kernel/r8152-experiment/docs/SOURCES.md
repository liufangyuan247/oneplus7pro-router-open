# Source provenance

## Kernel

- Repository: `https://github.com/OnePlusOSS/android_kernel_oneplus_sm8150.git`
- Branch: `oneplus/SM8150_P_9.0`
- Commit: `1dd473abda05a72f6978c47b2a7d80828db6b426`
- Commit description: synchronize OnePlus 7 Pro OxygenOS 9.5.11.GM31CB code
- Local bare repository: `02-source/oneplus-sm8150.git`
- Portable source archive: `02-source/oneplus-sm8150-SM8150_P_9.0.tar.gz`

The portable archive was generated inside Linux directly from the bare Git
objects. This prevents Windows `core.autocrlf` from changing kernel scripts.
The superseded CRLF archive is retained as
`oneplus-sm8150-SM8150_P_9.0.crlf-invalid.tar.gz` for traceability and must not
be used for builds.

The phone currently runs an earlier Android 9 build (`1904162308`). The newer
official branch is therefore a candidate source base, not assumed ABI-identical
until the stock configuration and vendor modules have been tested with a
temporary `fastboot boot` image.

## NetHunter Android 9 candidate

- Repository: `https://github.com/Re4son/android_kernel_oneplus_sm8150.git`
- Branch: `nethunter-9.0`
- Commit: `3eef5cdf6e62a4b36f1bfad734ffd9e4b04773c1`
- Base kernel: 4.14.83
- Defconfig: `nethunter_defconfig`
- Portable source archive: `02-source/oneplus-sm8150-nethunter-9.0.tar.gz`
- Archive SHA-256: `2a8eee752fd518ddc0a4b45141c0b37dbcb59aa7da69dfbb9a8b0a04b17ba57b`

This branch is specifically listed by Kali NetHunter for OnePlus 7/7 Pro on
OxygenOS 9. Its defconfig has `CONFIG_USB_RTL8152=y` and does not force module
signatures. The candidate boot image uses this kernel while preserving the
phone's current Magisk ramdisk and device DTB.

## Toolchains

All toolchain archives are from Android's official Gitiles repositories at tag
`android-9.0.0_r61`:

- AOSP Clang `clang-4691093` (Android Pie generation)
- AArch64 GCC/binutils `aarch64-linux-android-4.9`
- ARM32 GCC/binutils `arm-linux-androideabi-4.9`

The stock kernel reports `clang version 6.0.9 for Android NDK`. Exact binary
reproducibility is not assumed; the temporary-boot test is mandatory.

## Boot tooling

- Magisk v30.7 official release APK
- x86_64 Magiskboot extracted for containerized packaging
- arm64 Magiskboot retained for on-device diagnostics
