#!/system/bin/sh
# Magisk service.d entry for the OnePlus 7 Pro router stack.

HOME=/data/data/com.termux/files/home
PREFIX=/data/data/com.termux/files/usr
STACK="$HOME/router-stack"
SERVICES="$STACK/services"
LOGROOT=/data/adb/router-stack
WAKE_NAME=oneplus7-router
TERMUX_UID=10207
# 1023 is media_rw on AOSP; OxygenOS 11 owns /mnt/media_rw with its
# external_storage group 1077. Include both so root-launched Termux daemons
# can traverse Android's public USB-volume mount.
TERMUX_GIDS=1004,1007,1011,1015,1023,1028,1077,1078,1079,3001,3002,3003,3006,3009,3011,9997,50207

enforce_android_router_profile() {
  # These packages have been observed re-enabling or retaining a process even
  # after the one-time debloat pass. Re-assert only this short volatile list;
  # the full 70-package disabled-user state remains persistent in PackageManager.
  for pkg in \
    com.google.android.gms \
    com.google.android.gsf \
    com.android.vending \
    com.google.android.setupwizard \
    com.oneplus.setupwizard \
    com.oneplus.opbugreportlite \
    com.oneplus.gallery
  do
    pm disable-user --user 0 "$pkg" >/dev/null 2>&1 || true
    am force-stop "$pkg" >/dev/null 2>&1 || true
  done

  svc bluetooth disable >/dev/null 2>&1 || true
  svc nfc disable >/dev/null 2>&1 || true
  cmd location set-location-enabled false --user 0 >/dev/null 2>&1 || true
  cmd appops set com.android.chrome VIBRATE ignore >/dev/null 2>&1 || true
  cmd appops set com.google.android.inputmethod.latin VIBRATE ignore >/dev/null 2>&1 || true
  settings put system haptic_feedback_enabled 0 >/dev/null 2>&1 || true
  settings put system notification_vibration_intensity 0 >/dev/null 2>&1 || true
}

ensure_ax88179b_vendor_config() {
  # AX88179B exposes two USB configurations. OxygenOS may select configuration
  # 2 first, where the generic cdc_ncm driver creates usb0 and prevents the
  # vendor ax_usb_nic driver from binding. Configuration 1 is the native ASIX
  # interface used by the custom kernel driver. Re-assert it after every
  # hotplug so the LAN adapter recovers without ADB or manual sysfs writes.
  for usbdev in /sys/bus/usb/devices/*; do
    [ -r "$usbdev/idVendor" ] || continue
    [ -r "$usbdev/idProduct" ] || continue
    vendor="$(tr 'A-F' 'a-f' < "$usbdev/idVendor" 2>/dev/null)"
    product="$(tr 'A-F' 'a-f' < "$usbdev/idProduct" 2>/dev/null)"
    [ "$vendor:$product" = "0b95:1790" ] || continue
    [ -w "$usbdev/bConfigurationValue" ] || continue
    current="$(cat "$usbdev/bConfigurationValue" 2>/dev/null)"
    if [ "$current" != "1" ]; then
      echo "$(date '+%F %T') switching AX88179B ${usbdev##*/} from USB configuration ${current:-unknown} to 1"
      echo 1 > "$usbdev/bConfigurationValue" 2>/dev/null || true
    fi
  done
}

mkdir -p "$LOGROOT"
chmod 700 "$LOGROOT"

# Keep OxygenOS OTG host support available across unattended boots. The
# Settings value only disables OnePlus' ten-minute timeout; the persistent
# property is what makes OTG available before Android user data is unlocked.
setprop persist.sys.oem.otg_support true

# Bound the two root-side logs across reboots. Service logs are separately
# handled by svlogd (1 MiB x 5 files per service).
for logfile in keeper supervisor; do
  file="$LOGROOT/$logfile.log"
  size=0
  [ -f "$file" ] && size=$(wc -c < "$file" 2>/dev/null)
  if [ "${size:-0}" -gt 1048576 ]; then
    mv -f "$file" "$file.1"
  fi
done

(
  count=0
  while [ "$(getprop sys.boot_completed)" != "1" ] && [ "$count" -lt 120 ]; do
    sleep 2
    count=$((count + 1))
  done

  # On this dedicated router device the swipe-only keyguard is disabled. This
  # call is harmless when already unlocked and lets credential-encrypted app
  # data become available after an unattended reboot.
  wm dismiss-keyguard >/dev/null 2>&1 || true

  settings put global oneplus_otg_auto_disable 0 >/dev/null 2>&1 || true
  settings put global oem_otg_read 1 >/dev/null 2>&1 || true
  enforce_android_router_profile
  ensure_ax88179b_vendor_config

  cmd deviceidle whitelist +com.termux >/dev/null 2>&1 || true
  am set-standby-bucket com.termux active >/dev/null 2>&1 || true
  cmd appops set com.termux RUN_IN_BACKGROUND allow >/dev/null 2>&1 || true
  cmd appops set com.termux RUN_ANY_IN_BACKGROUND allow >/dev/null 2>&1 || true
  cmd appops set com.termux WAKE_LOCK allow >/dev/null 2>&1 || true

  wake_held=0
  while :; do
    ensure_ax88179b_vendor_config

    if [ -f "$STACK/enabled" ] && [ -x "$STACK/bin/supervisor" ]; then
      if [ "$wake_held" -eq 0 ] && [ -w /sys/power/wake_lock ]; then
        echo "$WAKE_NAME" > /sys/power/wake_lock 2>/dev/null || true
        wake_held=1
      fi

      if ! pgrep -f "$STACK/bin/supervisor" >/dev/null 2>&1; then
        "$PREFIX/bin/setpriv" \
          --reuid "$TERMUX_UID" --regid "$TERMUX_UID" --groups "$TERMUX_GIDS" \
          "$STACK/bin/supervisor" >> "$LOGROOT/supervisor.log" 2>&1 &
        sleep 3
        # runsv creates control FIFOs from the Magisk SELinux domain. Restore
        # the Termux app's MCS categories so routerctl can manage them later.
        restorecon -RF "$SERVICES" >/dev/null 2>&1 || true
      fi

      # Protect only the router daemons from Android's low-memory killer.
      for name in runsvdir runsv openlist aria2c qbittorrent-nox caddy sshd mihomo cli-proxy-api router-admin; do
        for pid in $(pidof "$name" 2>/dev/null); do
          echo -500 > "/proc/$pid/oom_score_adj" 2>/dev/null || true
        done
      done

      # A router appliance values USB link stability over peripheral power
      # saving. Re-apply this after every hotplug; it covers the CM418 RTL8153
      # and a future second USB Ethernet adapter without hard-coding its ID.
      for usbdev in /sys/bus/usb/devices/*; do
        [ -f "$usbdev/idVendor" ] || continue
        [ -w "$usbdev/power/control" ] && \
          echo on > "$usbdev/power/control" 2>/dev/null || true
        [ -w "$usbdev/power/autosuspend_delay_ms" ] && \
          echo -1 > "$usbdev/power/autosuspend_delay_ms" 2>/dev/null || true
      done
    else
      if [ "$wake_held" -eq 1 ] && [ -w /sys/power/wake_unlock ]; then
        echo "$WAKE_NAME" > /sys/power/wake_unlock 2>/dev/null || true
        wake_held=0
      fi
    fi

    sleep 30
  done
) >> "$LOGROOT/keeper.log" 2>&1 &

exit 0
