param(
    [string]$DeviceAddress = '192.168.2.1:5555'
)

$ErrorActionPreference = 'Stop'
$scrcpyRoot = Join-Path $PSScriptRoot 'scrcpy\v4.1\scrcpy-win64-v4.1'
$scrcpy = Join-Path $scrcpyRoot 'scrcpy.exe'
$adb = Join-Path $scrcpyRoot 'adb.exe'

if (-not (Test-Path -LiteralPath $scrcpy)) {
    throw "scrcpy not found: $scrcpy"
}
if (-not (Test-Path -LiteralPath $adb)) {
    throw "adb not found: $adb"
}

& $adb connect $DeviceAddress | Out-Host

& $scrcpy `
    --serial $DeviceAddress `
    --window-title 'OnePlus 7 Pro Router' `
    --max-size 1600 `
    --video-bit-rate 8M `
    --stay-awake
