#!/usr/bin/env bash
set -euo pipefail

project_root="${OP7P_PROJECT_ROOT:-/project}"
work_root="${OP7P_WORK_ROOT:-/work}"
source_dir="${work_root}/source/oneplus-sm8150"
build_dir="${work_root}/build/ax88179b"

source "${project_root}/scripts/toolchain-env.sh"
mkdir -p "${build_dir}"
gzip -dc "${project_root}/01-device-backup/proc-config-oos11.gz" > "${build_dir}/.config"

"${source_dir}/scripts/config" --file "${build_dir}/.config" \
  --disable USB_NET_AX88179_178A \
  --disable MODULE_SIG_FORCE \
  --enable ASIX_AX_USB_NIC \
  --module NET_SCH_FQ_CODEL

make -C "${source_dir}" O="${build_dir}" ARCH=arm64 \
  CC="${CC}" HOSTCC="${CC}" HOSTCXX="${clang_dir}/bin/clang++" olddefconfig

grep -qx '# CONFIG_USB_NET_AX88179_178A is not set' "${build_dir}/.config"
grep -qx '# CONFIG_MODULE_SIG_FORCE is not set' "${build_dir}/.config"
grep -qx 'CONFIG_ASIX_AX_USB_NIC=y' "${build_dir}/.config"
grep -qx 'CONFIG_NET_SCH_FQ_CODEL=m' "${build_dir}/.config"
grep -qx 'CONFIG_LOCALVERSION="-perf"' "${build_dir}/.config"
echo 'OOS11 config prepared.'
