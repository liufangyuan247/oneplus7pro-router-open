#!/usr/bin/env bash

work_root="${OP7P_WORK_ROOT:-/work}"
clang_dir="${work_root}/toolchains/clang-r377782c"
gcc64_dir="${work_root}/toolchains/aarch64-linux-android-4.9"
gcc32_dir="${work_root}/toolchains/arm-linux-androideabi-4.9"

export PATH="${clang_dir}/bin:${gcc64_dir}/bin:${gcc32_dir}/bin:${PATH}"
export ARCH=arm64
export SUBARCH=arm64
export CROSS_COMPILE="${gcc64_dir}/bin/aarch64-linux-android-"
export CROSS_COMPILE_ARM32="${gcc32_dir}/bin/arm-linux-androideabi-"
export CLANG_TRIPLE=aarch64-linux-gnu-
export CC="${clang_dir}/bin/clang"
export HOSTCC=clang
export HOSTCXX=clang++
