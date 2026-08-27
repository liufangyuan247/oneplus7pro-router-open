#!/usr/bin/env bash
set -euo pipefail

work_root="${OP7P_WORK_ROOT:-/work}"
artifacts="${OP7P_ARTIFACTS:-/artifacts}"
magiskboot="${work_root}/tools/magiskboot"
prefix="oos11-ax88179b-resetfix-napifix-fqcodel-v4"
final_image="${artifacts}/${prefix}-magisk30.7-minimal-final.img"
candidate_kernel="${artifacts}/${prefix}-candidate-Image"
verify="${work_root}/package/independent-v4-final"

for path in "${magiskboot}" "${final_image}" "${candidate_kernel}"; do
  [[ -s "${path}" ]] || { echo "Missing input: ${path}" >&2; exit 1; }
done

resolved="$(realpath -m "${verify}")"
[[ "${resolved}" == "${work_root}/package/independent-v4-final" ]] || {
  echo "Unsafe verification path: ${resolved}" >&2
  exit 1
}
rm -rf -- "${resolved}"
mkdir -p "${verify}"

cd "${verify}"
"${magiskboot}" unpack "${final_image}" >/dev/null
cp "${candidate_kernel}" expected-kernel
"${magiskboot}" hexpatch expected-kernel \
  736B69705F696E697472616D667300 \
  77616E745F696E697472616D667300 >/dev/null
cmp -s kernel expected-kernel

set +e
"${magiskboot}" cpio ramdisk.cpio test >/dev/null 2>&1
cpio_status=$?
set -e
[[ "${cpio_status}" -eq 1 ]]

ramdisk_hash="$(sha256sum ramdisk.cpio | awk '{print $1}')"
dtb_hash="$(sha256sum dtb | awk '{print $1}')"
[[ -n "${OP7P_EXPECTED_RAMDISK_SHA256:-}" ]] || {
  echo 'Set OP7P_EXPECTED_RAMDISK_SHA256 from your own base boot.' >&2
  exit 1
}
[[ -n "${OP7P_EXPECTED_DTB_SHA256:-}" ]] || {
  echo 'Set OP7P_EXPECTED_DTB_SHA256 from your own base boot.' >&2
  exit 1
}
[[ -n "${OP7P_EXPECTED_BOOT_SIZE:-}" ]] || {
  echo 'Set OP7P_EXPECTED_BOOT_SIZE from your own base boot.' >&2
  exit 1
}
[[ "${ramdisk_hash}" == "${OP7P_EXPECTED_RAMDISK_SHA256}" ]]
[[ "${dtb_hash}" == "${OP7P_EXPECTED_DTB_SHA256}" ]]
[[ "$(stat -c '%s' "${final_image}")" -eq "${OP7P_EXPECTED_BOOT_SIZE}" ]]

echo 'final_kernel_matches_candidate_plus_magisk_4byte_patch=yes'
echo "magisk_cpio_status=${cpio_status}"
sha256sum kernel ramdisk.cpio dtb "${final_image}"
stat -c 'final_size=%s' "${final_image}"
