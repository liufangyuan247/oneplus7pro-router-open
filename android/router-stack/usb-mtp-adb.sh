#!/system/bin/sh
# Keep USB peripheral connections in File Transfer + ADB mode by default.
# This does not force the USB-C role: when Android is USB Host for CM418,
# Type-C role switching remains controlled by the USB HAL/kernel.

settings put global usb_config mtp,adb >/dev/null 2>&1 || true
setprop persist.sys.usb.config mtp,adb

# Use key-only SSH for network management; keep ADB available over USB.
setprop persist.adb.tcp.port -1
setprop service.adb.tcp.port -1
setprop ctl.restart adbd

exit 0
