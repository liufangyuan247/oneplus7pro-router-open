#!/system/bin/sh
set -eu
# Fail closed on the management backends before the application stack starts.
/data/adb/router-security/apply
