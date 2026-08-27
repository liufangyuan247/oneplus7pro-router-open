#!/usr/bin/env bash
set -euo pipefail

project_root="${OP7P_PROJECT_ROOT:-/project}"
work_root="${OP7P_WORK_ROOT:-/work}"
artifacts="${OP7P_ARTIFACTS:-/artifacts}"
build_dir="${work_root}/build/ax88179b"
kernel_image="${build_dir}/arch/arm64/boot/Image"
rollback_boot="${project_root}/01-device-backup/rollback-magisk-oos11.img"
magiskboot="${work_root}/tools/magiskboot"
stage="${work_root}/package/ax88179b"
verify="${work_root}/package/verify-ax88179b"
output="${artifacts}/oos11-ax88179b-test-v2.img"

for path in "${kernel_image}" "${rollback_boot}" "${magiskboot}"; do
  [[ -s "${path}" ]] || { echo "Missing input: ${path}" >&2; exit 1; }
done

[[ ! -e "${output}" ]] || {
  echo "Refusing to overwrite historical image: ${output}" >&2
  exit 1
}
if grep -qx 'CONFIG_NET_SCH_FQ_CODEL=m' "${build_dir}/.config"; then
  echo 'This is the v3 candidate build; use package-resetfix-fqcodel.sh.' >&2
  exit 1
fi

grep -qx 'CONFIG_ASIX_AX_USB_NIC=y' "${build_dir}/.config"
grep -qx '# CONFIG_USB_NET_AX88179_178A is not set' "${build_dir}/.config"
grep -qx '# CONFIG_MODULE_SIG_FORCE is not set' "${build_dir}/.config"
grep -qx '4.14.190-perf+' "${build_dir}/include/config/kernel.release"
# Do not use grep -q here: with pipefail it exits after the first match and
# makes nm receive SIGPIPE (141), falsely failing an otherwise valid build.
nm "${build_dir}/vmlinux" | grep ' ax_usb_driver$' > /dev/null

for path in "${stage}" "${verify}"; do
  resolved="$(realpath -m "${path}")"
  case "${resolved}" in
    "${work_root}/package/ax88179b"|"${work_root}/package/verify-ax88179b") rm -rf -- "${resolved}" ;;
    *) echo "Unsafe package path: ${resolved}" >&2; exit 1 ;;
  esac
done

mkdir -p "${stage}" "${verify}" "${artifacts}"
cp "${rollback_boot}" "${stage}/boot.img"

(
  cd "${stage}"
  "${magiskboot}" unpack boot.img
  test -s kernel
  test -s ramdisk.cpio
  # Header v2 metadata is retained internally by magiskboot; this image does
  # not unpack a standalone file named "header". Hash the actual preserved
  # payload files and verify them again after repacking below.
  sha256sum ramdisk.cpio dtb > preserved.sha256
  install -m 0644 "${kernel_image}" kernel
  "${magiskboot}" repack boot.img "${output}"
)

[[ $(stat -c '%s' "${output}") -le $(stat -c '%s' "${rollback_boot}") ]]

(
  cd "${verify}"
  "${magiskboot}" unpack "${output}"
  cmp -s kernel "${kernel_image}"
  cmp -s ramdisk.cpio "${stage}/ramdisk.cpio"
  cmp -s dtb "${stage}/dtb"
)

install -m 0644 "${kernel_image}" "${artifacts}/oos11-ax88179b-Image"
install -m 0644 "${build_dir}/.config" "${artifacts}/oos11-ax88179b.config"
diff -U0 \
  <(gzip -dc "${project_root}/01-device-backup/proc-config-oos11.gz" | sed '/^$/d' | sort) \
  <(sed '/^$/d' "${build_dir}/.config" | sort) \
  > "${artifacts}/oos11-ax88179b-config.diff" || [[ $? -eq 1 ]]

sha256sum "${output}" "${artifacts}/oos11-ax88179b-Image" \
  "${artifacts}/oos11-ax88179b.config" \
  "${artifacts}/oos11-ax88179b-config.diff" \
  | sed "s#${artifacts}/##" > "${artifacts}/SHA256SUMS.txt"

cat "${artifacts}/SHA256SUMS.txt"
