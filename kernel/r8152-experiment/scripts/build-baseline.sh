#!/usr/bin/env bash
set -euo pipefail

work_root="${OP7P_WORK_ROOT:-${HOME}/op7p-kernel-work}"
source_dir="${work_root}/source/oneplus-sm8150"
build_dir="${work_root}/build/baseline"
clang_dir="${work_root}/toolchains/clang-4691093"
gcc64_dir="${work_root}/toolchains/aarch64-linux-android-4.9"
gcc32_dir="${work_root}/toolchains/arm-linux-androideabi-4.9"
jobs="${OP7P_BUILD_JOBS:-$(nproc)}"

if [[ ! -f "${build_dir}/.config" ]]; then
  echo 'Run scripts/configure-baseline.sh first.' >&2
  exit 1
fi

export PATH="${clang_dir}/bin:${gcc64_dir}/bin:${gcc32_dir}/bin:${PATH}"
export ARCH=arm64
export SUBARCH=arm64
export CROSS_COMPILE="${gcc64_dir}/bin/aarch64-linux-android-"
export CROSS_COMPILE_ARM32="${gcc32_dir}/bin/arm-linux-androideabi-"
export CLANG_TRIPLE=aarch64-linux-gnu-
export CC="${clang_dir}/bin/clang"
export HOSTCC=clang
export HOSTCXX=clang++

# OnePlus hard-codes a Python wrapper that rejects every compiler warning.
# Supplying CC on the make command line intentionally takes precedence over
# that Makefile assignment while retaining Kbuild's targeted -Werror checks.
make -C "${source_dir}" O="${build_dir}" -j"${jobs}" \
  CC="${clang_dir}/bin/clang" \
  HOSTCC="${clang_dir}/bin/clang" \
  HOSTCXX="${clang_dir}/bin/clang++" \
  Image

echo "Kernel: ${build_dir}/arch/arm64/boot/Image"
echo 'Baseline packaging preserves the device-matched DTB from boot_a.'
