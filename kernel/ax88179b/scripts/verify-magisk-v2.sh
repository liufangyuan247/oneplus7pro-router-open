#!/usr/bin/env bash
set -euo pipefail

work_root="${OP7P_WORK_ROOT:-/work}"
artifacts="${OP7P_ARTIFACTS:-/artifacts}"
magiskboot="${work_root}/tools/magiskboot"
image="${artifacts}/oos11-ax88179b-v2-magisk30.7.img"
kernel="${work_root}/build/ax88179b/arch/arm64/boot/Image"
reference_dtb="${work_root}/package/ax88179b-unrooted-v2/dtb"
verify="${work_root}/package/verify-magisk-v2-final"

for path in "${magiskboot}" "${image}" "${kernel}" "${reference_dtb}"; do
  [[ -s "${path}" ]] || { echo "Missing input: ${path}" >&2; exit 1; }
done

resolved="$(realpath -m "${verify}")"
[[ "${resolved}" == "${work_root}/package/verify-magisk-v2-final" ]] || {
  echo "Unsafe verification path: ${resolved}" >&2; exit 1;
}
rm -rf -- "${resolved}"
mkdir -p "${resolved}"

(
  cd "${resolved}"
  "${magiskboot}" unpack "${image}"
  cmp -s dtb "${reference_dtb}"

  [[ $(stat -c '%s' kernel) -eq $(stat -c '%s' "${kernel}") ]]
  # Magisk 30.7 intentionally applies a tiny kernel-side patch in addition to
  # changing the ramdisk. Require a small, bounded delta from our verified
  # ASIX kernel instead of incorrectly requiring byte identity.
  changed_bytes="$(cmp -l kernel "${kernel}" | wc -l || true)"
  [[ ${changed_bytes} -ge 1 && ${changed_bytes} -le 64 ]] || {
    echo "Unexpected Magisk kernel delta: ${changed_bytes} bytes" >&2
    exit 1
  }
  grep -aFq 'Linux version 4.14.190-perf+' kernel
  grep -aFq 'ax_usb_nic' kernel

  set +e
  "${magiskboot}" cpio ramdisk.cpio test
  ramdisk_status=$?
  set -e
  [[ ${ramdisk_status} -eq 1 ]] || {
    echo "Expected Magisk-patched ramdisk status 1, got ${ramdisk_status}" >&2
    exit 1
  }
  echo "Magisk kernel delta: ${changed_bytes} bytes"
)

grep -qx '4.14.190-perf+' \
  "${work_root}/build/ax88179b/include/config/kernel.release"
sha256sum "${image}"
echo 'Magisk v2 image verification passed.'
