#!/usr/bin/env bash
set -euo pipefail

project_root="${OP7P_PROJECT_ROOT:-/project}"
work_root="${OP7P_WORK_ROOT:-/work}"
artifacts="${OP7P_ARTIFACTS:-/artifacts}"
source_dir="${work_root}/source/oneplus-sm8150"
build_dir="${work_root}/build/ax88179b"
jobs="${OP7P_BUILD_JOBS:-8}"
prefix="oos11-ax88179b-resetfix-napifix-fqcodel-v4-candidate"
module="${build_dir}/net/sched/sch_fq_codel.ko"

source "${project_root}/scripts/toolchain-env.sh"
[[ -f "${build_dir}/.config" ]] || { echo 'Run configure first.' >&2; exit 1; }

make -C "${source_dir}" O="${build_dir}" -j"${jobs}" \
  CC="${CC}" HOSTCC="${CC}" HOSTCXX="${clang_dir}/bin/clang++" \
  LOCALVERSION=+ Image

make -C "${source_dir}" O="${build_dir}" -j"${jobs}" \
  CC="${CC}" HOSTCC="${CC}" HOSTCXX="${clang_dir}/bin/clang++" \
  LOCALVERSION=+ M=net/sched modules

image="${build_dir}/arch/arm64/boot/Image"
release="$(cat "${build_dir}/include/config/kernel.release")"
test -s "${image}"
test -s "${module}"
test -s "${build_dir}/Module.symvers"
[[ "${release}" == '4.14.190-perf+' ]]
grep -qx 'CONFIG_ASIX_AX_USB_NIC=y' "${build_dir}/.config"
grep -qx 'CONFIG_NET_SCH_FQ_CODEL=m' "${build_dir}/.config"
grep -qx '# CONFIG_MODULE_SIG_FORCE is not set' "${build_dir}/.config"
nm "${build_dir}/vmlinux" | grep ' ax_usb_driver$' > /dev/null

vermagic="$(modinfo -F vermagic "${module}")"
case "${vermagic}" in
  "${release}"*) ;;
  *) echo "Unexpected module vermagic: ${vermagic}" >&2; exit 1 ;;
esac

mkdir -p "${artifacts}"
install -m 0644 "${image}" "${artifacts}/${prefix}-Image"
install -m 0644 "${module}" "${artifacts}/${prefix}-sch_fq_codel.ko"
install -m 0644 "${build_dir}/.config" "${artifacts}/${prefix}.config"
install -m 0644 "${build_dir}/Module.symvers" "${artifacts}/${prefix}-Module.symvers"
install -m 0644 "${build_dir}/System.map" "${artifacts}/${prefix}-System.map"

{
  echo "source_commit=$(cat "${source_dir}/.source-commit")"
  echo "kernel_release=${release}"
  echo "module_vermagic=${vermagic}"
  echo "compiler=$(${CC} --version | sed -n '1p')"
  echo "config_asix=$(grep '^CONFIG_ASIX_AX_USB_NIC=' "${build_dir}/.config")"
  echo "config_fq_codel=$(grep '^CONFIG_NET_SCH_FQ_CODEL=' "${build_dir}/.config")"
  echo 'stability_fix=clamp AX TX NAPI poll return to budget'
  modinfo "${module}"
  file "${image}" "${module}"
} > "${artifacts}/${prefix}-manifest.txt"

(
  cd "${artifacts}"
  sha256sum \
    "${prefix}-Image" \
    "${prefix}-sch_fq_codel.ko" \
    "${prefix}.config" \
    "${prefix}-Module.symvers" \
    "${prefix}-System.map" \
    "${prefix}-manifest.txt" \
    > "${prefix}-SHA256SUMS.txt"
)

cat "${artifacts}/${prefix}-manifest.txt"
cat "${artifacts}/${prefix}-SHA256SUMS.txt"
