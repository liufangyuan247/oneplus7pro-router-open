#!/system/bin/sh
set -u

MANIFEST=/sdcard/OnePlusRouter/debloat-disabled-packages.txt

if [ ! -s "$MANIFEST" ]; then
    echo "No router debloat manifest found: $MANIFEST"
    exit 0
fi

while IFS= read -r pkg; do
    [ -n "$pkg" ] || continue
    if pm enable --user 0 "$pkg" >/dev/null 2>&1; then
        echo "OK   enabled $pkg"
    else
        echo "FAIL $pkg"
    fi
done < "$MANIFEST"

echo "Restore pass complete. The manifest was retained for audit."
