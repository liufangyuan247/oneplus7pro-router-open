#!/system/bin/sh
set -u

for supply in /sys/class/power_supply/*; do
  [ -d "$supply" ] || continue
  printf '\n[%s]\n' "${supply##*/}"
  for field in \
    type present online status health capacity temp \
    usb_type real_type typec_mode typec_power_role pd_active \
    voltage_now voltage_avg voltage_max voltage_min voltage_max_design \
    pd_voltage_max pd_voltage_min \
    current_now current_avg current_max input_current_now input_current_limit \
    settled_input_current pd_current_max \
    power_now power_avg constant_charge_current constant_charge_current_max \
    charge_enabled charging_enabled input_suspend op_disable_charge
  do
    path="$supply/$field"
    if [ -r "$path" ]; then
      value="$(cat "$path" 2>/dev/null)"
      printf '%s=%s\n' "$field" "$value"
    fi
  done
done
