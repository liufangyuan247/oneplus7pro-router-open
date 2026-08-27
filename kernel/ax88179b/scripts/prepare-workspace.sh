#!/usr/bin/env bash
set -euo pipefail

project_root="${OP7P_PROJECT_ROOT:-/project}"
work_root="${OP7P_WORK_ROOT:-/work}"
source_commit='5c3a4299de5522b86304ae2daaeb1eedd63c6c24'

required=(
  "${project_root}/01-device-backup/proc-config-oos11.gz"
  "${project_root}/01-device-backup/rollback-magisk-oos11.img"
  "${project_root}/02-source/oneplus-sm8150-SM8150_R_10.0.tar.gz"
  "${project_root}/02-source/oneplus-opensource-kernel-SM8150_R_10.0.tar.gz"
  "${project_root}/02-source/ASIX_USB_NIC_Linux_Driver_Source_v4.1.0.tar.bz2"
  "${project_root}/03-toolchains/clang-r377782c-android11.tar.gz"
  "${project_root}/03-toolchains/aarch64-linux-android-4.9.tar.gz"
  "${project_root}/03-toolchains/arm-linux-androideabi-4.9.tar.gz"
  "${project_root}/04-tools/magiskboot"
)

for path in "${required[@]}"; do
  [[ -s "${path}" ]] || { echo "Missing input: ${path}" >&2; exit 1; }
done

mkdir -p "${work_root}/source" "${work_root}/toolchains" \
  "${work_root}/vendor" "${work_root}/build/ax88179b" \
  "${work_root}/package" "${work_root}/tools"

source_dir="${work_root}/source/oneplus-sm8150"
if [[ ! -f "${source_dir}/Makefile" ]]; then
  case "$(realpath -m "${source_dir}")" in
    "${work_root}/source/oneplus-sm8150") rm -rf -- "${source_dir}" ;;
    *) echo 'Unsafe source path' >&2; exit 1 ;;
  esac
  extract_dir="${work_root}/source/extract-oos11"
  rm -rf -- "${extract_dir}"
  mkdir -p "${extract_dir}"
  tar -xzf "${project_root}/02-source/oneplus-sm8150-SM8150_R_10.0.tar.gz" \
    -C "${extract_dir}"
  extracted="$(find "${extract_dir}" -mindepth 1 -maxdepth 1 -type d | head -n 1)"
  test -f "${extracted}/Makefile"
  mv "${extracted}" "${source_dir}"
  rm -rf -- "${extract_dir}"
fi
printf '%s\n' "${source_commit}" > "${source_dir}/.source-commit"

extract_once() {
  local archive="$1"
  local target="$2"
  local marker="${target}/.op7p-extracted"
  if [[ ! -f "${marker}" ]]; then
    mkdir -p "${target}"
    tar -xf "${archive}" -C "${target}"
    touch "${marker}"
  fi
}

extract_once "${project_root}/03-toolchains/clang-r377782c-android11.tar.gz" \
  "${work_root}/toolchains/clang-r377782c"
extract_once "${project_root}/03-toolchains/aarch64-linux-android-4.9.tar.gz" \
  "${work_root}/toolchains/aarch64-linux-android-4.9"
extract_once "${project_root}/03-toolchains/arm-linux-androideabi-4.9.tar.gz" \
  "${work_root}/toolchains/arm-linux-androideabi-4.9"
extract_once "${project_root}/02-source/ASIX_USB_NIC_Linux_Driver_Source_v4.1.0.tar.bz2" \
  "${work_root}/vendor/asix-v4.1.0"
extract_once "${project_root}/02-source/oneplus-opensource-kernel-SM8150_R_10.0.tar.gz" \
  "${work_root}/vendor/oneplus-overlay"

overlay_root="$(find "${work_root}/vendor/oneplus-overlay" -type d -path '*/msm-4.14' | head -n 1)"
test -d "${overlay_root}/drivers"
mkdir -p "${work_root}/source/oneplus"
cp -a "${overlay_root}/." "${work_root}/source/oneplus/"
# The released kernel Makefile copies this overlay to the parent of the
# kernel checkout, which only works with OnePlus's original repo layout.
# Merge the same official overlay into this isolated standalone checkout.
cp -a "${overlay_root}/." "${source_dir}/"

# The public kernel archive contains uxcore/opchain_helper.c and references
# uxcore/, but omits uxcore/Makefile. The matching vendor overlay supplies
# uxcore/core/. Restore the missing Kbuild glue so both the helper API and the
# implementation are linked, matching the enabled stock CONFIG_OPCHAIN=y.
cat > "${source_dir}/coretech/uxcore/Makefile" <<'EOF'
obj-$(CONFIG_OPCHAIN) += opchain_helper.o
EOF
grep -q '^obj-$(CONFIG_OPCHAIN) += uxcore/$' \
  "${source_dir}/coretech/Makefile" || \
  printf '%s\n' 'obj-$(CONFIG_OPCHAIN) += uxcore/' >> \
    "${source_dir}/coretech/Makefile"

install -m 0755 "${project_root}/04-tools/magiskboot" \
  "${work_root}/tools/magiskboot"

printf 'Source: %s\n' "$(cat "${source_dir}/.source-commit")"
"${work_root}/toolchains/clang-r377782c/bin/clang" --version | head -n 1
"${work_root}/tools/magiskboot" 2>&1 | head -n 1 || true
