#!/system/bin/sh
# Dedicated router appliance: keep OxygenOS system OTA disabled so it cannot
# replace the rooted boot image or change the kernel unexpectedly.

settings put global automatic_system_updates 0 >/dev/null 2>&1 || true
settings put global ota_disable_automatic_update 1 >/dev/null 2>&1 || true
pm disable-user --user 0 com.oneplus.opbackup >/dev/null 2>&1 || true
am force-stop com.oneplus.opbackup >/dev/null 2>&1 || true

exit 0
