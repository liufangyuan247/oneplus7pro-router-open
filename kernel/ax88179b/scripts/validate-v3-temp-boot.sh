#!/system/bin/sh
set +e

echo '=== IDENTITY ==='
uname -a
id
magisk -v

echo '=== CONFIG ==='
zcat /proc/config.gz 2>/dev/null | grep -E 'CONFIG_(ASIX_AX_USB_NIC|NET_SCH_FQ_CODEL|IFB)'

echo '=== BUILTIN DRIVER ==='
ls -ld /sys/module/ax_usb_nic 2>/dev/null

echo '=== SERVICES ==='
ps -A | grep -E 'openlist|aria2c|caddy|tailscaled|sshd|vlmcsd'

echo '=== MAGISK SERVICE SCRIPTS ==='
ls -1 /data/adb/service.d 2>/dev/null

echo '=== KERNEL ALERTS ==='
dmesg | grep -Ei 'Kernel panic|Unable to handle|BUG:|Call trace|Unknown symbol|module verification failed|ax_usb_nic.*fail|ax_usb_nic.*error' | tail -80

echo '=== LOAD FQ_CODEL ==='
if grep -q '^sch_fq_codel ' /proc/modules; then
  echo 'sch_fq_codel already loaded'
else
  insmod /data/local/tmp/sch_fq_codel-v3.ko
  echo "INSMOD_RC=$?"
fi
grep -E '^sch_fq_codel |^Module' /proc/modules

echo '=== QDISC PROBE ==='
for tc_bin in /system/bin/tc /data/data/com.termux/files/usr/bin/tc; do
  if [ -x "$tc_bin" ]; then
    echo "TC=$tc_bin"
    "$tc_bin" qdisc show
    break
  fi
done

echo '=== DMESG TAIL ==='
dmesg | tail -40
