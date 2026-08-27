# OnePlus 7 Pro RTL8153 kernel project

Target device: OnePlus 7 Pro GM1910, Android 9, kernel `4.14.83-perf+`.

Goal: make the UGREEN CM418's RTL8153 Ethernet controller usable. The current
stock-derived baseline was rejected after two boot tests. The next candidate
is based on the OnePlus 7/7 Pro NetHunter Android 9 branch, with the in-tree
Realtek driver enabled as `CONFIG_USB_RTL8152=y`.

## Layout

- `00-docs/` — inventory, provenance, checksums and build notes
- `01-device-backup/` — current boot image, kernel config and device metadata
- `02-source/oneplus-sm8150/` — official OnePlus Android 9 kernel source
- `03-toolchains/` — pinned Linux x86-64 Clang and GCC cross toolchains
- `04-tools/` — boot image utilities such as Magiskboot
- `05-build/` — generated kernel build output; safe to regenerate
- `06-artifacts/` — candidate boot images and hashes
- `scripts/` — repeatable backup, configure, build and packaging helpers

## Safety rule

Nothing in this directory should flash the phone automatically. Candidate
images must first be tested using `fastboot boot`. Permanent flashing requires
an explicit separate command and a verified rollback image.

## Planned image set

- `baseline-r8152.img`: rejected stock-derived kernel with RTL8152/RTL8153 support.
- `nethunter9-r8152.img`: NetHunter 9 kernel candidate, preserving the current
  Magisk ramdisk and device DTB.
- `router-stable.img`: validated router power and USB reliability changes.
- `rollback-boot_a.img`: byte-for-byte backup of the currently bootable,
  Magisk-patched boot slot.

The baseline and router-stable variants intentionally remain separate so a
network-driver problem cannot be confused with a power-policy change.

## Confirmed hardware

UGREEN CM418 Ethernet controller:

```text
USB VID:PID: 0bda:8153
Revision:    0x3000
Description: Realtek USB 10/100/1000 LAN
USB class:   vendor-specific (no CDC Ethernet fallback)
```

## Expected resource use

- Downloaded source and toolchain archives: about 380 MiB.
- Expanded source and build workspace: normally 4–8 GiB.
- First build on this 12-thread/16-GB computer: approximately 10–25 minutes.
- Incremental rebuild: commonly 1–5 minutes.

The Windows checkout cannot contain the kernel path ending in `aux.c`, because
`AUX` is a reserved Windows filename. Archives stay in this project while the
working source tree is extracted into a Docker named volume.

## Isolated build environment

The build uses an Ubuntu 18.04 container, matching the Python 2-era OnePlus
kernel scripts. The project input is mounted
read-only, source/build intermediates live in the dedicated
`oneplus7pro-kernel-work` Docker volume, and only `06-artifacts/` is writable
for exported results. No compiler or package dependency needs to be installed
in the normal Ubuntu WSL distribution.

From PowerShell:

```powershell
.\scripts\container.ps1 image
.\scripts\container.ps1 prepare
.\scripts\container.ps1 configure
.\scripts\container.ps1 build -Jobs 8
.\scripts\container.ps1 package
```

`configure` intentionally refuses to run until the complete stock
`01-device-backup/proc-config.gz` has been captured. None of these commands
flash the phone.
