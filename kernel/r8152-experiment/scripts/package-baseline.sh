#!/usr/bin/env bash
set -euo pipefail

project_root="${OP7P_PROJECT_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
work_root="${OP7P_WORK_ROOT:-${HOME}/op7p-kernel-work}"
artifacts="${OP7P_ARTIFACTS:-${project_root}/06-artifacts}"
build_dir="${work_root}/build/baseline"
kernel_image="${build_dir}/arch/arm64/boot/Image"
stock_boot="${project_root}/01-device-backup/boot_a-current-magisk.img"
magiskboot="${work_root}/tools/magiskboot"
stage="${work_root}/package/baseline"
verify_stage="${work_root}/package/verify-baseline"
output="${artifacts}/baseline-r8152.img"

for required_file in "${kernel_image}" "${stock_boot}" "${magiskboot}"; do
  if [[ ! -s "${required_file}" ]]; then
    echo "Missing build input: ${required_file}" >&2
    exit 1
  fi
done

grep -qx 'CONFIG_USB_RTL8152=y' "${build_dir}/.config"
nm "${build_dir}/vmlinux" \
  | awk '/ rtl8152_driver$/ { found = 1 } END { exit(found ? 0 : 1) }'

for target in "${stage}" "${verify_stage}"; do
  resolved="$(realpath -m "${target}")"
  case "${resolved}" in
    "${work_root}/package/baseline"|"${work_root}/package/verify-baseline")
      rm -rf -- "${resolved}"
      ;;
    *)
      echo "Refusing unsafe package path: ${resolved}" >&2
      exit 1
      ;;
  esac
done

mkdir -p "${stage}" "${verify_stage}" "${artifacts}"
cp "${stock_boot}" "${stage}/boot.img"

(
  cd "${stage}"
  "${magiskboot}" unpack boot.img
  test -s kernel
  test -s kernel_dtb
  test -s ramdisk.cpio
  install -m 0644 "${kernel_image}" kernel
  "${magiskboot}" repack boot.img "${output}"
)

if (( $(stat -c '%s' "${output}") > $(stat -c '%s' "${stock_boot}") )); then
  echo 'Candidate image is larger than the boot partition backup.' >&2
  exit 1
fi

(
  cd "${verify_stage}"
  "${magiskboot}" unpack "${output}"
  cmp -s kernel "${kernel_image}"
  cmp -s kernel_dtb "${stage}/kernel_dtb"
  cmp -s ramdisk.cpio "${stage}/ramdisk.cpio"
)

install -m 0644 "${kernel_image}" "${artifacts}/baseline-Image"
install -m 0644 "${build_dir}/.config" "${artifacts}/baseline.config"
diff -U0 \
  <(gzip -dc "${project_root}/01-device-backup/proc-config.gz" | sed '/^$/d' | sort) \
  <(sed '/^$/d' "${build_dir}/.config" | sort) \
  > "${artifacts}/baseline-config.diff" || [[ $? -eq 1 ]]

sha256sum \
  "${output}" \
  "${artifacts}/baseline-Image" \
  "${artifacts}/baseline.config" \
  "${artifacts}/baseline-config.diff" \
  | sed "s#${artifacts}/##" > "${artifacts}/baseline-SHA256SUMS.txt"

printf 'Candidate: %s (%s bytes)\n' "${output}" "$(stat -c '%s' "${output}")"
grep -aom1 'Linux version [^[:cntrl:]]*' "${kernel_image}" || true
cat "${artifacts}/baseline-SHA256SUMS.txt"
