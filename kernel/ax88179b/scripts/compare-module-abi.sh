#!/usr/bin/env bash
set -euo pipefail

work_root="${OP7P_WORK_ROOT:-/work}"
symvers="${work_root}/build/ax88179b/Module.symvers"
module_root="/project/01-device-backup/vendor-modules"

[[ -s "${symvers}" ]] || { echo 'Candidate Module.symvers missing' >&2; exit 1; }

tmpdir="$(mktemp -d)"
trap 'rm -rf -- "${tmpdir}"' EXIT
awk '{print $2, tolower($1)}' "${symvers}" > "${tmpdir}/candidate"

status=0
for module in \
  "${module_root}/rmnet_perf.ko" \
  "${module_root}/audio_q6.ko" \
  "${module_root}/qca_cld3_wlan.ko"; do
  [[ -s "${module}" ]] || { echo "Missing module: ${module}" >&2; exit 1; }
  modprobe --dump-modversions "${module}" > "${tmpdir}/expected"
  echo "=== $(basename "${module}") ==="
  if ! awk '
    NR == FNR { candidate[$1] = $2; next }
    {
      expected = tolower($1); symbol = $2; total++
      if (!(symbol in candidate)) {
        missing++; print "MISSING  " symbol " expected=" expected
      } else if (candidate[symbol] != expected) {
        mismatch++
        print "MISMATCH " symbol " expected=" expected " candidate=" candidate[symbol]
      } else {
        matched++
      }
    }
    END {
      printf "SUMMARY total=%d matched=%d mismatch=%d missing=%d\n", \
        total, matched, mismatch, missing
      if (mismatch || missing) exit 1
    }
  ' "${tmpdir}/candidate" "${tmpdir}/expected"; then
    status=1
  fi
done

exit "${status}"
