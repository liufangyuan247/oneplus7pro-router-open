# OnePlus 7 Pro OxygenOS 11 AX88179B kernel fix

Target: GM1910, OxygenOS 11 `GM1910_21_211210`, Android 11, kernel
`4.14.190-perf+`.

This project replaces the old in-tree AX88179/AX88178A driver for USB ID
`0b95:1790` with ASIX's AX88179B-capable Linux driver v4.1.0. The driver is
built into the kernel because the stock kernel enables forced module
signatures and compiles the incompatible driver in-tree.

The build is isolated in Docker volume `oneplus7pro-oos11-ax88179b-work`.
Project inputs are read-only inside the container and only `06-artifacts/` is
writable for exported images.

The OnePlus source commit is `5c3a4299de5522b86304ae2daaeb1eedd63c6c24`
from official branch `oneplus/SM8150_R_10.0`. It is kernel 4.14.190 and is the
closest publicly released source to the installed OOS 11.0.5.1 build; OnePlus
labels this source release OOS 11.0.6.1.

Nothing in this project flashes the phone automatically. A candidate must be
statically verified first, then flashed only with the rollback image ready.

The boot image approved for the next **temporary boot test** is
`06-artifacts/oos11-ax88179b-resetfix-napifix-fqcodel-v4-magisk30.7-minimal-final.img`.
It preserves the currently running Magisk ramdisk and DTB byte-for-byte and
applies only Magisk's four-byte `skip_initramfs` to `want_initramfs` patch to
the new kernel. See its adjacent `minimal-final-manifest.txt` and checksum
file. This status does not approve a permanent flash: use `fastboot boot`
first and verify boot, root, router services, USB reset/replug and rollback.
The phone currently remains on the v3 image through a temporary `fastboot
boot`; no candidate has been written permanently.

The `candidate` build also emits `sch_fq_codel.ko` with the matching
`4.14.190-perf+` vermagic. It includes a reset-path fix that kills outstanding
ASIX RX/TX URBs before USB reset recovery and removes the vendor driver's
duplicate TX NAPI/tasklet state transition. Candidate artifacts use a distinct
`resetfix-fqcodel-v3-candidate` prefix and do not overwrite the known rollback
images.

The v4 stability candidate additionally clamps the AX driver's TX NAPI poll
return value to the core-provided budget. The vendor code can complete several
skbs from one USB TX descriptor and otherwise return more work than the NAPI
weight, which triggers `WARN_ON_ONCE(work > weight)` in `net_rx_action`.

`fq_codel` is a separate module and is not enabled merely by booting the new
image. The candidate does not enable `CONFIG_IFB`, so it can provide an egress
qdisc but not a complete ingress/downlink SQM setup.

`scripts/container.ps1 -Action candidate-package` repacks that Image into a
separately named candidate boot image. It accepts only the byte-for-byte backup
of the currently running Magisk-patched `boot_a`, verifies the complete kernel
artifact checksum set, rejects unexpected config drift, and confirms that the
ramdisk, DTB and Magisk cpio state survive repacking. The legacy `package`
action is not used for this candidate.

The official kernel release contains one packaging defect in
`coretech/Makefile`: it references both `uxcore/` and `uxcore/core/`, while the
matching official vendor release only ships `uxcore/core/`. The preparation
script removes the redundant nonexistent `uxcore/` target and leaves the real
OPCHAIN core enabled.
