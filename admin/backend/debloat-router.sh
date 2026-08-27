#!/system/bin/sh
set -u

# Reversible Android package trimming for a dedicated OnePlus 7 Pro router.
# Packages are disabled only for Android user 0. System APK files are untouched.

STATE_DIR=/sdcard/OnePlusRouter
MANIFEST="$STATE_DIR/debloat-disabled-packages.txt"
STAMP="$(date +%Y%m%d-%H%M%S 2>/dev/null || echo unknown)"
BEFORE="$STATE_DIR/packages-before-debloat-$STAMP.txt"

mkdir -p "$STATE_DIR"
pm list packages -e > "$BEFORE"
touch "$MANIFEST"
chmod 600 "$MANIFEST" "$BEFORE"

PACKAGES='com.android.vending
com.google.android.gms
com.google.android.gsf
com.google.android.googlequicksearchbox
com.google.android.youtube
com.google.android.gm
com.google.android.apps.tachyon
com.google.android.music
com.google.android.apps.wellbeing
com.google.android.apps.docs
com.google.android.apps.maps
com.google.android.apps.walletnfcrel
com.google.android.videos
com.google.android.feedback
com.google.android.apps.photos
com.google.android.calendar
com.google.android.projection.gearhead
com.google.android.gms.location.history
com.google.android.apps.restore
com.google.android.as
com.google.ar.core
com.google.android.marvin.talkback
com.google.android.printservice.recommendation
com.google.android.syncadapters.contacts
com.google.android.tts
com.google.android.onetimeinitializer
com.google.android.partnersetup
com.google.android.setupwizard
com.google.mainline.telemetry
com.android.hotwordenrollment.xgoogle
com.android.hotwordenrollment.okgoogle
com.oneplus.account
cn.oneplus.photos
com.oneplus.gallery
com.oneplus.gamespace
com.oneplus.aod
com.oneplus.wallpaper
com.oneplus.brickmode
com.oneplus.deskclock
com.oneplus.applocker
com.oneplus.opbackup
cn.oneplus.nvbackup
com.oneplus.backuprestore.remoteservice
com.oneplus.camera
com.oneplus.camera.service
com.oneplus.faceunlock
com.oneplus.contacts
com.oneplus.dialer
com.oneplus.mms
com.oneplus.simcontacts
com.oneplus.sms.smscplugger
com.oneplus.carrierlocation
com.oneplus.telephonyoptimization
com.oneplus.opbugreportlite
com.oneplus.diagnosemanager
com.oneplus.minidumpoptimization
com.oneplus.factorymode
com.oneplus.factorymode.specialtest
com.oneplus.engmode
com.oneplus.bttestmode
net.oneplus.commonlogtool
net.oneplus.odm
net.oneplus.odm.provider
net.oneplus.push
com.oneplus.appupgrader
com.oneplus.config
com.oneplus.geoiptime
com.oneplus.communication.data
net.oneplus.provider.appcategoryprovider
cn.oneplus.oemtcma
com.qti.ltebc'

disabled=0
skipped=0
failed=0

echo "$PACKAGES" | while IFS= read -r pkg; do
    [ -n "$pkg" ] || continue

    if ! pm path "$pkg" >/dev/null 2>&1; then
        echo "SKIP not-installed $pkg"
        continue
    fi

    if pm list packages -d | grep -qx "package:$pkg"; then
        # This device had only com.oneplus.opbackup disabled before the router
        # profile was first applied. Reconstruct all other entries if needed.
        if [ "$pkg" != "com.oneplus.opbackup" ]; then
            grep -qx "$pkg" "$MANIFEST" || echo "$pkg" >> "$MANIFEST"
            echo "KEEP disabled-and-recorded $pkg"
        else
            echo "SKIP disabled-before-profile $pkg"
        fi
        continue
    fi

    if pm disable-user --user 0 "$pkg" >/dev/null 2>&1 && \
       pm list packages -d | grep -qx "package:$pkg"; then
        grep -qx "$pkg" "$MANIFEST" || echo "$pkg" >> "$MANIFEST"
        am force-stop "$pkg" >/dev/null 2>&1 || true
        echo "OK   disabled $pkg"
    else
        echo "FAIL $pkg"
    fi
done

sort -u "$MANIFEST" -o "$MANIFEST"
chmod 600 "$MANIFEST"

echo "---"
echo "Rollback manifest: $MANIFEST"
echo "Enabled-package snapshot: $BEFORE"
echo "Disabled by router profile: $(wc -l < "$MANIFEST")"
