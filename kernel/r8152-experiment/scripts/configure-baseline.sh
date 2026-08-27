#!/usr/bin/env bash
set -euo pipefail

project_root="${OP7P_PROJECT_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
work_root="${OP7P_WORK_ROOT:-${HOME}/op7p-kernel-work}"
source_dir="${work_root}/source/oneplus-sm8150"
build_dir="${work_root}/build/baseline"
stock_config_gz="${project_root}/01-device-backup/proc-config.gz"

if [[ ! -f "${stock_config_gz}" ]]; then
  echo "Missing ${stock_config_gz}; complete the device backup first." >&2
  exit 1
fi

mkdir -p "${build_dir}"
gzip -dc "${stock_config_gz}" > "${build_dir}/.config"

"${source_dir}/scripts/config" --file "${build_dir}/.config" \
  --enable USB_RTL8152 \
  --disable MODULE_SIG_FORCE

make -C "${source_dir}" O="${build_dir}" ARCH=arm64 olddefconfig

grep -E '^(CONFIG_USB_RTL8152=y|# CONFIG_MODULE_SIG_FORCE is not set)$' \
  "${build_dir}/.config"
