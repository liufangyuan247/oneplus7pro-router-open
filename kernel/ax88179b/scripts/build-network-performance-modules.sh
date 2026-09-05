#!/usr/bin/env bash
set -euo pipefail

project_root="${OP7P_PROJECT_ROOT:-/project}"
work_root="${OP7P_WORK_ROOT:-/work}"
artifacts="${OP7P_ARTIFACTS:-/artifacts}"
source_dir="${work_root}/source/oneplus-sm8150"
build_dir="${work_root}/build/ax88179b"
jobs="${OP7P_BUILD_JOBS:-8}"
prefix="oos11-v4-network-performance-modules"

source "${project_root}/scripts/toolchain-env.sh"
[[ -f "${build_dir}/.config" ]] || { echo 'Run the v4 candidate build first.' >&2; exit 1; }
[[ -s "${build_dir}/Module.symvers" ]] || { echo 'Module.symvers is missing.' >&2; exit 1; }

# These are optional capabilities. Loading a module alone does not install a
# qdisc, redirect ingress traffic, or change the default congestion control.
"${source_dir}/scripts/config" --file "${build_dir}/.config" \
  --module IFB \
  --module NET_SCH_TBF \
  --module NET_SCH_FQ \
  --enable TCP_CONG_ADVANCED \
  --module TCP_CONG_BBR

make -C "${source_dir}" O="${build_dir}" ARCH=arm64 \
  CC="${CC}" HOSTCC="${CC}" HOSTCXX="${clang_dir}/bin/clang++" olddefconfig

grep -qx 'CONFIG_IFB=m' "${build_dir}/.config"
grep -qx 'CONFIG_NET_SCH_TBF=m' "${build_dir}/.config"
grep -qx 'CONFIG_NET_SCH_FQ=m' "${build_dir}/.config"
grep -qx 'CONFIG_TCP_CONG_ADVANCED=y' "${build_dir}/.config"
grep -qx 'CONFIG_TCP_CONG_BBR=m' "${build_dir}/.config"
grep -qx 'CONFIG_NET_SCH_FQ_CODEL=m' "${build_dir}/.config"

# Linux 4.14's Kbuild races on shared *.mod files when unrelated explicit
# module targets run in parallel. Build one directory at a time instead.
for module_dir in drivers/net net/sched net/ipv4; do
  make -C "${source_dir}" O="${build_dir}" -j"${jobs}" \
    CC="${CC}" HOSTCC="${CC}" HOSTCXX="${clang_dir}/bin/clang++" \
    LOCALVERSION=+ M="${module_dir}" modules
done

release="$(cat "${build_dir}/include/config/kernel.release")"
[[ "${release}" == '4.14.190-perf+' ]]

modules=(
  "drivers/net/ifb.ko"
  "net/sched/sch_tbf.ko"
  "net/sched/sch_fq.ko"
  "net/sched/sch_fq_codel.ko"
  "net/ipv4/tcp_bbr.ko"
)

mkdir -p "${artifacts}"
for relative in "${modules[@]}"; do
  module="${build_dir}/${relative}"
  [[ -s "${module}" ]] || { echo "Missing module: ${module}" >&2; exit 1; }
  vermagic="$(modinfo -F vermagic "${module}")"
  case "${vermagic}" in
    "${release}"*) ;;
    *) echo "Unexpected vermagic for ${relative}: ${vermagic}" >&2; exit 1 ;;
  esac
  install -m 0644 "${module}" "${artifacts}/${prefix}-$(basename "${module}")"
done

install -m 0644 "${build_dir}/.config" "${artifacts}/${prefix}.config"

manifest="${artifacts}/${prefix}-manifest.txt"
{
  echo "source_commit=$(cat "${source_dir}/.source-commit")"
  echo "kernel_release=${release}"
  echo "compiler=$(${CC} --version | sed -n '1p')"
  echo 'boot_image_changed=no'
  echo 'default_qdisc_changed=no'
  echo 'default_tcp_congestion_control_changed=no'
  for relative in "${modules[@]}"; do
    module="${build_dir}/${relative}"
    echo "module=$(basename "${module}")"
    modinfo "${module}" | grep -E '^(filename|license|description|name|depends|vermagic):'
  done
} > "${manifest}"

(
  cd "${artifacts}"
  sha256sum \
    "${prefix}-ifb.ko" \
    "${prefix}-sch_tbf.ko" \
    "${prefix}-sch_fq.ko" \
    "${prefix}-sch_fq_codel.ko" \
    "${prefix}-tcp_bbr.ko" \
    "${prefix}.config" \
    "${prefix}-manifest.txt" \
    > "${prefix}-SHA256SUMS.txt"
)

cat "${manifest}"
cat "${artifacts}/${prefix}-SHA256SUMS.txt"
