#!/usr/bin/env bash
set -euo pipefail

project_root="${OP7P_PROJECT_ROOT:-/project}"
work_root="${OP7P_WORK_ROOT:-/work}"
artifacts="${OP7P_ARTIFACTS:-/artifacts}"
prefix="oos11-ax88179b-resetfix-napifix-fqcodel-v4-candidate"
kernel_image="${artifacts}/${prefix}-Image"
kernel_config="${artifacts}/${prefix}.config"
kernel_sums="${artifacts}/${prefix}-SHA256SUMS.txt"
base_boot="${OP7P_BASE_BOOT:-${project_root}/01-device-backup/current-magisk-boot.img}"
base_boot_sha256="${OP7P_BASE_BOOT_SHA256:-}"
old_config="${artifacts}/oos11-ax88179b.config"
magiskboot="${work_root}/tools/magiskboot"
stage="${work_root}/package/resetfix-napifix-fqcodel-v4"
verify="${work_root}/package/verify-resetfix-napifix-fqcodel-v4"
output="${artifacts}/${prefix}-boot.img"
manifest="${artifacts}/${prefix}-boot-manifest.txt"
sums="${artifacts}/${prefix}-boot-SHA256SUMS.txt"

[[ -n "${base_boot_sha256}" ]] || {
  echo 'Set OP7P_BASE_BOOT_SHA256 to the SHA256 of your own matching Magisk boot.' >&2
  exit 1
}

for path in "${kernel_image}" "${kernel_config}" "${kernel_sums}" \
  "${base_boot}" "${old_config}" "${magiskboot}"; do
  [[ -s "${path}" ]] || { echo "Missing input: ${path}" >&2; exit 1; }
done

actual_base_sha256="$(sha256sum "${base_boot}" | awk '{print $1}')"
[[ "${actual_base_sha256}" == "${base_boot_sha256}" ]] || {
  echo "Current boot backup hash mismatch: ${actual_base_sha256}" >&2
  exit 1
}

(
  cd "${artifacts}"
  sha256sum -c "$(basename "${kernel_sums}")"
) >/dev/null

removed_config="$(comm -23 <(LC_ALL=C sort "${old_config}") <(LC_ALL=C sort "${kernel_config}"))"
added_config="$(comm -13 <(LC_ALL=C sort "${old_config}") <(LC_ALL=C sort "${kernel_config}"))"
[[ "${removed_config}" == '# CONFIG_NET_SCH_FQ_CODEL is not set' ]]
[[ "${added_config}" == 'CONFIG_NET_SCH_FQ_CODEL=m' ]]

for path in "${stage}" "${verify}"; do
  resolved="$(realpath -m "${path}")"
  case "${resolved}" in
    "${work_root}/package/resetfix-napifix-fqcodel-v4"|"${work_root}/package/verify-resetfix-napifix-fqcodel-v4")
      rm -rf -- "${resolved}"
      ;;
    *)
      echo "Unsafe package path: ${resolved}" >&2
      exit 1
      ;;
  esac
done

mkdir -p "${stage}" "${verify}" "${artifacts}"
cp "${base_boot}" "${stage}/boot.img"

(
  cd "${stage}"
  "${magiskboot}" unpack boot.img > unpack-base.log
  test -s kernel
  test -s ramdisk.cpio
  test -s dtb
  sha256sum ramdisk.cpio dtb > preserved.sha256
  set +e
  "${magiskboot}" cpio ramdisk.cpio test >/dev/null 2>&1
  base_cpio_status=$?
  set -e
  printf '%s\n' "${base_cpio_status}" > base-cpio-status
  [[ "${base_cpio_status}" -ne 0 ]] || {
    echo 'The current boot backup ramdisk is not Magisk patched.' >&2
    exit 1
  }
  install -m 0644 "${kernel_image}" kernel
  "${magiskboot}" repack boot.img candidate-boot.img > repack.log
  test -s candidate-boot.img
)

(
  cd "${verify}"
  "${magiskboot}" unpack "${stage}/candidate-boot.img" > unpack-candidate.log
  cmp -s kernel "${kernel_image}"
  cmp -s ramdisk.cpio "${stage}/ramdisk.cpio"
  cmp -s dtb "${stage}/dtb"
  set +e
  "${magiskboot}" cpio ramdisk.cpio test >/dev/null 2>&1
  candidate_cpio_status=$?
  set -e
  [[ "${candidate_cpio_status}" == "$(cat "${stage}/base-cpio-status")" ]]
)

[[ "$(stat -c '%s' "${stage}/candidate-boot.img")" -le "$(stat -c '%s' "${base_boot}")" ]]
install -m 0644 "${stage}/candidate-boot.img" "${output}.new"
mv -f "${output}.new" "${output}"

{
  echo "base_boot=$(basename "${base_boot}")"
  echo "base_boot_sha256=${actual_base_sha256}"
  echo "base_hash_verified=yes"
  echo "kernel_image=$(basename "${kernel_image}")"
  echo "kernel_sha256=$(sha256sum "${kernel_image}" | awk '{print $1}')"
  echo "boot_image=$(basename "${output}")"
  echo "boot_sha256=$(sha256sum "${output}" | awk '{print $1}')"
  echo "boot_size=$(stat -c '%s' "${output}")"
  echo "ramdisk_sha256=$(sha256sum "${stage}/ramdisk.cpio" | awk '{print $1}')"
  echo "dtb_sha256=$(sha256sum "${stage}/dtb" | awk '{print $1}')"
  echo "magisk_cpio_status=$(cat "${stage}/base-cpio-status")"
  echo 'config_delta=CONFIG_NET_SCH_FQ_CODEL:m'
  echo 'stability_fix=clamp AX TX NAPI poll return to budget'
} > "${manifest}.new"
mv -f "${manifest}.new" "${manifest}"

(
  cd "${artifacts}"
  sha256sum "$(basename "${output}")" "$(basename "${manifest}")" > "$(basename "${sums}").new"
  mv -f "$(basename "${sums}").new" "$(basename "${sums}")"
)

cat "${manifest}"
cat "${sums}"
