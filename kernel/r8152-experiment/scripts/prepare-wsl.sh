#!/usr/bin/env bash
set -euo pipefail

echo 'This compatibility entry point now uses the generic workspace setup.' >&2
echo 'For dependency isolation, run scripts/container.ps1 prepare from Windows.' >&2
exec "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/prepare-workspace.sh" "$@"
