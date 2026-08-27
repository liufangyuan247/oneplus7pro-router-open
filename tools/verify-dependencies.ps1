[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$DownloadDirectory,

    [string]$ChecksumFile = (Join-Path $PSScriptRoot '..\checksums\dependencies.sha256')
)

$ErrorActionPreference = 'Stop'
$downloadRoot = (Resolve-Path -LiteralPath $DownloadDirectory).Path
$checksumPath = (Resolve-Path -LiteralPath $ChecksumFile).Path
$missing = 0
$failed = 0

foreach ($line in Get-Content -LiteralPath $checksumPath) {
    if ($line -notmatch '^([0-9a-fA-F]{64})\s{2}(.+)$') {
        continue
    }
    $expected = $Matches[1].ToLowerInvariant()
    $name = $Matches[2]
    $path = Join-Path $downloadRoot $name
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        Write-Host "MISSING  $name"
        $missing++
        continue
    }
    $actual = (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actual -eq $expected) {
        Write-Host "OK       $name"
    } else {
        Write-Host "FAILED   $name"
        $failed++
    }
}

Write-Host "summary: missing=$missing failed=$failed"
if ($failed -gt 0) {
    exit 1
}
