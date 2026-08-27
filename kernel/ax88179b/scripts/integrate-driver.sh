#!/usr/bin/env bash
set -euo pipefail

work_root="${OP7P_WORK_ROOT:-/work}"
source_dir="${work_root}/source/oneplus-sm8150"
vendor_root="${work_root}/vendor/asix-v4.1.0"
vendor_dir="$(find "${vendor_root}" -mindepth 1 -maxdepth 1 -type d | head -n 1)"
target="${source_dir}/drivers/net/usb/asix_ax88179b"

[[ -f "${source_dir}/Makefile" ]] || { echo 'Kernel source missing' >&2; exit 1; }
[[ -f "${vendor_dir}/ax_main.c" ]] || { echo 'ASIX source missing' >&2; exit 1; }

mkdir -p "${target}"
for file in \
  ax_main.c ax_main.h ax_config.h ax_ioctl.h \
  ax88179_178a.c ax88179_178a.h \
  ax88179a_772d.c ax88179a_772d.h \
  ax_ptp.c ax_ptp.h; do
  install -m 0644 "${vendor_dir}/${file}" "${target}/${file}"
done

# ASIX publishes the archive with CRLF line endings. Normalize the copied
# sources so the compatibility patch and kernel tooling behave consistently.
find "${target}" -maxdepth 1 -type f \( -name '*.c' -o -name '*.h' \) \
  -exec sed -i 's/\r$//' {} +

patch -d "${source_dir}" -p1 < \
  /project/patches/asix-v4.1.0-oneplus-4.14.patch

printf '%s\n' \
  'obj-$(CONFIG_ASIX_AX_USB_NIC) += ax_usb_nic.o' \
  'ax_usb_nic-y := ax_main.o ax88179_178a.o ax88179a_772d.o ax_ptp.o' \
  > "${target}/Makefile"

cat > "${target}/Kconfig" <<'EOF'
config ASIX_AX_USB_NIC
	tristate "ASIX AX88179B/179A/772E/772D USB Ethernet driver"
	depends on USB && NET
	select MII
	help
	  ASIX vendor driver v4.1.0. This driver is required for AX88179B
	  devices whose 0b95:1790 ID is incorrectly claimed by the older
	  in-tree AX88179/AX88178A driver.
EOF

parent_makefile="${source_dir}/drivers/net/usb/Makefile"
parent_kconfig="${source_dir}/drivers/net/usb/Kconfig"
grep -q 'CONFIG_ASIX_AX_USB_NIC' "${parent_makefile}" || \
  printf '%s\n' 'obj-$(CONFIG_ASIX_AX_USB_NIC) += asix_ax88179b/' >> "${parent_makefile}"

if ! grep -q 'asix_ax88179b/Kconfig' "${parent_kconfig}"; then
  sed -i '/^endif.*USB_NET_DRIVERS/i source "drivers/net/usb/asix_ax88179b/Kconfig"' \
    "${parent_kconfig}"
fi

grep -q 'CONFIG_ASIX_AX_USB_NIC' "${parent_makefile}"
grep -q 'asix_ax88179b/Kconfig' "${parent_kconfig}"
echo 'ASIX AX88179B vendor driver integrated.'
