#!/data/data/com.termux/files/usr/bin/sh
# Sourced by the existing healthd loop; no additional resident process.
cpa_alias_now=$(date +%s)
if [ "$cpa_alias_now" -ge "${CPA_ALIAS_SYNC_NEXT:-0}" ]; then
    CPA_ALIAS_SYNC_NEXT=$((cpa_alias_now + 300))
    if [ -x "$PREFIX/bin/cpa-model-alias-sync" ]; then
        "$PREFIX/bin/python" "$PREFIX/bin/cpa-model-alias-sync" --if-due >/dev/null 2>&1 &
    fi
fi
