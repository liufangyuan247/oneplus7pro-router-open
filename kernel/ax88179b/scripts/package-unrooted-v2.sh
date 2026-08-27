#!/usr/bin/env bash
set -euo pipefail

project_root="${OP7P_PROJECT_ROOT:-/project}"
work_root="${OP7P_WORK_ROOT:-/work}"
artifacts="${OP7P_ARTIFACTS:-/artifacts}"
build_dir="${work_root}/build/ax88179b"
kernel_image="${build_dir}/arch/arm64/boot/Image"
stock_boot="${project_root}/01-device-backup/stock-oos11.img"
magiskboot="${work_root}/tools/magiskboot"
stage="${work_root}/package/ax88179b-unrooted-v2"
verify="${work_root}/package/verify-ax88179b-unrooted-v2"
output="${artifacts}/oos11-ax88179b-v2-unrooted-for-magisk.img"

for path in "${kernel_image}" "${stock_boot}" "${magiskboot}"; do
  [[ -s "${path}" ]] || { echo "Missing input: ${path}" >&2; exit 1; }
done
grep -qx '4.14.190-perf+' "${build_dir}/include/config/kernel.release"
grep -qx 'CONFIG_ASIX_AX_USB_NIC=y' "${build_dir}/.config"
grep -qx '# CONFIG_USB_NET_AX88179_178A is not set' "${build_dir}/.config"
grep -qx '# CONFIG_MODULE_SIG_FORCE is not set' "${build_dir}/.config"

for path in "${stage}" "${verify}"; do
  resolved="$(realpath -m "${path}")"
  case "${resolved}" in
    "${work_root}/package/ax88179b-unrooted-v2"|\
    "${work_root}/package/verify-ax88179b-unrooted-v2") rm -rf -- "${resolved}" ;;
    *) echo "Unsafe package path: ${resolved}" >&2; exit 1 ;;
  esac
done
mkdir -p "${stage}" "${verify}" "${artifacts}"
cp "${stock_boot}" "${stage}/boot.img"

(
  cd "${stage}"
  "${magiskboot}" unpack boot.img
  test -s kernel
  test -s ramdisk.cpio
  test -s dtb
  install -m 0644 "${kernel_image}" kernel
  "${magiskboot}" repack boot.img "${output}"
)

[[ $(stat -c '%s' "${output}") -le $(stat -c '%s' "${stock_boot}") ]]

(
  cd "${verify}"
  "${magiskboot}" unpack "${output}"
  cmp -s kernel "${kernel_image}"
  cmp -s ramdisk.cpio "${stage}/ramdisk.cpio"
  cmp -s dtb "${stage}/dtb"
)

sha256sum "${output}"
