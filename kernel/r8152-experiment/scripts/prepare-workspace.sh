#!/usr/bin/env bash
set -euo pipefail

project_root="${OP7P_PROJECT_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
work_root="${OP7P_WORK_ROOT:-${HOME}/op7p-kernel-work}"
source_archive="${project_root}/02-source/oneplus-sm8150-SM8150_P_9.0.tar.gz"
toolchain_downloads="${project_root}/03-toolchains/downloads"

required_files=(
  "${source_archive}"
  "${toolchain_downloads}/clang-4691093-android-9.0.0_r61.tar.gz"
  "${toolchain_downloads}/aarch64-linux-android-4.9-android-9.0.0_r61.tar.gz"
  "${toolchain_downloads}/arm-linux-androideabi-4.9-android-9.0.0_r61.tar.gz"
  "${project_root}/04-tools/lib/x86_64/libmagiskboot.so"
)

for required_file in "${required_files[@]}"; do
  if [[ ! -f "${required_file}" ]]; then
    echo "Missing input: ${required_file}" >&2
    exit 1
  fi
done

(
  cd "${project_root}"
  sha256sum --check 00-docs/SHA256SUMS.txt
)

mkdir -p \
  "${work_root}/source" \
  "${work_root}/toolchains" \
  "${work_root}/build/baseline" \
  "${work_root}/build/router-stable" \
  "${work_root}/tools"

if [[ ! -f "${work_root}/source/oneplus-sm8150/Makefile" ]]; then
  tar -xzf "${source_archive}" -C "${work_root}/source"
fi

extract_once() {
  local archive="$1"
  local target="$2"
  local marker="${target}/.op7p-extracted"
  if [[ ! -f "${marker}" ]]; then
    mkdir -p "${target}"
    tar -xzf "${archive}" -C "${target}"
    touch "${marker}"
  fi
}

extract_once \
  "${toolchain_downloads}/clang-4691093-android-9.0.0_r61.tar.gz" \
  "${work_root}/toolchains/clang-4691093"
extract_once \
  "${toolchain_downloads}/aarch64-linux-android-4.9-android-9.0.0_r61.tar.gz" \
  "${work_root}/toolchains/aarch64-linux-android-4.9"
extract_once \
  "${toolchain_downloads}/arm-linux-androideabi-4.9-android-9.0.0_r61.tar.gz" \
  "${work_root}/toolchains/arm-linux-androideabi-4.9"

install -m 0755 \
  "${project_root}/04-tools/lib/x86_64/libmagiskboot.so" \
  "${work_root}/tools/magiskboot"

clang_version="$("${work_root}/toolchains/clang-4691093/bin/clang" --version | head -n 1)"
magiskboot_version="$("${work_root}/tools/magiskboot" 2>&1 | head -n 1 || true)"

printf 'Isolated workspace ready: %s\n' "${work_root}"
printf 'Compiler: %s\n' "${clang_version}"
printf 'Boot tool: %s\n' "${magiskboot_version}"
