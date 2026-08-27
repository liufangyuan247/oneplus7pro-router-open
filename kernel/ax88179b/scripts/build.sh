#!/usr/bin/env bash
set -euo pipefail

project_root="${OP7P_PROJECT_ROOT:-/project}"
work_root="${OP7P_WORK_ROOT:-/work}"
source_dir="${work_root}/source/oneplus-sm8150"
build_dir="${work_root}/build/ax88179b"
jobs="${OP7P_BUILD_JOBS:-8}"

source "${project_root}/scripts/toolchain-env.sh"
[[ -f "${build_dir}/.config" ]] || { echo 'Run configure first.' >&2; exit 1; }

make -C "${source_dir}" O="${build_dir}" -j"${jobs}" \
  CC="${CC}" HOSTCC="${CC}" HOSTCXX="${clang_dir}/bin/clang++" \
  LOCALVERSION=+ Image

test -s "${build_dir}/arch/arm64/boot/Image"
grep -qx '4.14.190-perf+' "${build_dir}/include/config/kernel.release"
grep -aom1 'Linux version [^[:cntrl:]]*' "${build_dir}/arch/arm64/boot/Image" || true
