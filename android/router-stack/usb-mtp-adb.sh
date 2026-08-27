#!/system/bin/sh
# Keep USB peripheral connections in File Transfer + ADB mode by default.
# This does not force the USB-C role: when Android is USB Host for CM418,
# Type-C role switching remains controlled by the USB HAL/kernel.

settings put global usb_config mtp,adb >/dev/null 2>&1 || true
setprop persist.sys.usb.config mtp,adb

# Keep an authenticated management path available while the USB-C port is in
# host mode for an Ethernet adapter. Android still requires an authorized ADB
# host key before accepting a connection.
setprop persist.adb.tcp.port 5555
setprop service.adb.tcp.port 5555
setprop ctl.restart adbd

exit 0
