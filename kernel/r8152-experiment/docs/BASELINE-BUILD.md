# Baseline RTL8153 build

Status: built and structurally verified; non-persistent boot test attempted.

The GM1910 bootloader accepted the 96 MiB image transfer but returned
`remote: unknown command` for `fastboot boot`. It therefore does not support
the non-persistent boot command in its current firmware. The phone was returned
with `fastboot reboot` and verified back on the untouched `4.14.83-perf+`
kernel, slot `_a`, with Magisk root. No partition was written during this test.
Any further live test requires explicit approval for a temporary
`fastboot flash boot_a` followed by restoration from the verified backup.

## Authorized temporary flash result

With explicit approval, `baseline-r8152.img` was flashed only to `boot_a`.
The phone did not return ADB within 180 seconds, so no userspace, Magisk, or
CM418 test was possible. Bootloader remained reachable on slot `a`; the
verified original `boot_a-current-magisk.img` was immediately restored to
`boot_a`. The phone then reached Android with the original
`4.14.83-perf+` kernel and root, and the router service processes were present.

## Authorized retry with USB File Transfer mode enabled

The USB default was set to `mtp,adb` through Magisk service configuration and
the same candidate was retried, again only on `boot_a`. The image transferred
and flashed successfully, but the phone still did not return ADB within 45
seconds. It was restored from `boot_a-current-magisk.img` and verified back on
the original `4.14.83-perf+` kernel with Magisk root. This confirms that the
failure is not caused by the Android USB MTP/ADB function selection; that
setting affects the USB gadget after Android boots, not kernel compatibility.

Do not flash this candidate again. The next candidate should be built from a
kernel source/toolchain combination matched more closely to the phone's April
2019 firmware, then structurally compared before another authorized test.

## Candidate

- Boot image: `06-artifacts/baseline-r8152.img`
- Kernel release: `4.14.83-perf`
- Source commit: `1dd473abda05a72f6978c47b2a7d80828db6b426`
- Compiler: Android Clang 6.0.2, build 4691093
- Boot header: version 1, Android 9.0.0, patch level 2019-03
- Partition-sized output: 100663296 bytes

The candidate was unpacked after repacking. Its kernel matches the newly built
`Image`; its Magisk ramdisk and 1865733-byte device DTB match the current
boot_a components byte for byte.

## Semantic configuration changes

```text
-# CONFIG_USB_RTL8152 is not set
+CONFIG_USB_RTL8152=y
-CONFIG_MODULE_SIG_FORCE=y
+# CONFIG_MODULE_SIG_FORCE is not set
```

`olddefconfig` also emits `# CONFIG_WB_KERNEL_LOG is not set`; this is an
explicit record of an already-disabled option, not a feature change. Full
machine-readable output is in `06-artifacts/baseline-config.diff`.

## Test policy for a future candidate

This baseline candidate is rejected for live testing. The bootloader does not
support `fastboot boot`; a future candidate must only be tested after explicit
approval with a reversible `fastboot flash boot_a`, keeping the verified
backup ready for immediate restoration. Verify:

1. Android reaches ADB and Magisk root remains available.
2. `/sys/bus/usb/drivers/r8152` exists.
3. UGREEN CM418 (`0bda:8153`) binds to r8152 and creates an Ethernet interface.
4. Rebooting normally returns to the untouched boot_a image.

## NetHunter 9 candidate

The next candidate is built from Re4son's `nethunter-9.0` branch at commit
`3eef5cdf6e62a4b36f1bfad734ffd9e4b04773c1`. It remains on kernel 4.14.83 and
uses `nethunter_defconfig`, which has `CONFIG_USB_RTL8152=y` and no forced
module-signature requirement. The image is packaged with the current Magisk
ramdisk and device DTB preserved byte-for-byte.

- Boot image: `06-artifacts/nethunter9-r8152.img`
- Size: 100663296 bytes (boot partition-sized)
- SHA-256: `1f0cd01034917a8bb8adf2eebb71916c620564289834f180cbd53c1d4252dfec`
- Kernel SHA-256: `1666487c869013c7213ff173edeba87637180139b6a4ad11f7c3a995a7cc570f`

## NetHunter 9 live-test result

With explicit approval, `nethunter9-r8152.img` was flashed only to `boot_a`.
The bootloader accepted the image, but Android did not return to ADB within 60
seconds. The exact original `boot_a-current-magisk.img` was restored
immediately. Android then completed boot on slot `_a` with the original
`4.14.83-perf+` kernel, Magisk root, and the `aria2c` and `openlist` processes
running. This candidate is rejected and must not be flashed again.
