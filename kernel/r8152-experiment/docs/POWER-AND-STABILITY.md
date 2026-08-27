# Router power and stability design

## Baseline image

The first candidate must contain only changes required to boot safely and
recognize the CM418:

```text
CONFIG_USB_RTL8152=y
# CONFIG_MODULE_SIG_FORCE is not set
```

`MODULE_SIG_FORCE` is disabled because a newly built kernel cannot validate
the stock vendor modules with OnePlus's private signing key. Module versioning
remains enabled, so ABI mismatches are still detected.

No frequency, scheduler, thermal or generic suspend code is changed in the
baseline image.

The baseline build replaces only the raw kernel `Image`. It preserves the DTB
and ramdisk extracted from the currently running GM1910 boot_a. The full
upstream `dtbs` target includes a broken Qualcomm RUMI emulation-board DTS and
is neither required nor appropriate for this hardware-only driver change.

## Router-stable image

Changes are admitted only after measurements show they are necessary:

1. Keep RTL8153 and AX88179 devices out of USB autosuspend while link is in use.
2. Preserve CPU idle and `schedutil`; never pin minimum frequencies high.
3. Keep Qualcomm thermal throttling and emergency shutdown intact.
4. Use a partial wake lock only while router mode is enabled.
5. Disable Ethernet EEE at runtime only if link-drop testing proves it faulty.
6. Reapply USB power policy after hotplug by VID/PID, not by unstable bus path.
7. Keep application databases on internal UFS and downloads on external disk.

The current Android 9 build has already retained full CPU maximum frequencies
and all cluster core limits during a short screen-off test. Therefore there is
no evidence supporting a kernel-level screen-off frequency override today.

## Acceptance gates

- Two USB Ethernet adapters survive 1,000 hotplug/link cycles without identity
  reversal.
- No USB reset, filesystem error or battery discharge under simultaneous
  routing and disk I/O.
- Screen-off and screen-on routed throughput differ by no more than 5%.
- Temperature remains within the stock thermal policy during a 24-hour load.
- 72-hour and seven-day unattended runs complete without manual intervention.
