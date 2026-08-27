#!/system/bin/sh

echo '=== kernel network features ==='
if [ -r /proc/config.gz ]; then
  zcat /proc/config.gz | grep -E '^(CONFIG_USB_RTL8152|CONFIG_USB_NET_AX88179_178A|CONFIG_USB_USBNET|CONFIG_TUN|CONFIG_BRIDGE|CONFIG_NETFILTER|CONFIG_NF_NAT|CONFIG_NF_CONNTRACK|CONFIG_IP_NF_IPTABLES)='
else
  echo '/proc/config.gz unavailable'
fi

echo '=== currently loaded related drivers ==='
ls /sys/module 2>/dev/null | grep -E '^(r8152|ax88179_178a|usbnet|tun|bridge)$' || true

echo '=== network interfaces ==='
ip -br link 2>/dev/null || ip link

echo '=== USB tree ==='
for dev in /sys/bus/usb/devices/*; do
  [ -f "$dev/idVendor" ] || continue
  printf '%s %s:%s ' "${dev##*/}" "$(cat "$dev/idVendor")" "$(cat "$dev/idProduct")"
  cat "$dev/product" 2>/dev/null || echo
done
