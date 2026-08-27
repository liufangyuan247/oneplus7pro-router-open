#!/system/bin/sh

count="${1:-6}"
index=1
while [ "$index" -le "$count" ]; do
  printf '%s ' "$(date '+%H:%M:%S')"
  printf 'present=%s ' "$(cat /sys/class/power_supply/usb/present 2>/dev/null)"
  printf 'online=%s ' "$(cat /sys/class/power_supply/usb/online 2>/dev/null)"
  printf 'pd=%s ' "$(cat /sys/class/power_supply/usb/pd_active 2>/dev/null)"
  printf 'voltage_mv=%s ' "$(cat /sys/class/power_supply/usb/voltage_now 2>/dev/null)"
  printf 'input_ua=%s ' "$(cat /sys/class/power_supply/usb/input_current_now 2>/dev/null)"
  printf 'battery_ua=%s ' "$(cat /sys/class/power_supply/battery/current_now 2>/dev/null)"
  printf 'disabled=%s\n' "$(cat /sys/class/power_supply/battery/op_disable_charge 2>/dev/null)"
  index=$((index + 1))
  [ "$index" -le "$count" ] && sleep 5
done

echo RECENT_PD_EVENTS
dmesg | grep -i -E 'usbpd.*disconnect|usbin_uv|power_ok|pd.*hard.reset|pd.*connect' | tail -n 20
