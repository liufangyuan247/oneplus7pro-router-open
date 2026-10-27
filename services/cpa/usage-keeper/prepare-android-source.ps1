param(
    [Parameter(Mandatory = $true)]
    [string]$SourceDirectory,
    [string]$OutputArchive = ''
)

$ErrorActionPreference = 'Stop'
if ([string]::IsNullOrWhiteSpace($OutputArchive)) {
    $OutputArchive = Join-Path $PSScriptRoot 'build/cpa-usage-keeper-v2.0.2-android-source.tar.gz'
}
$expectedCommit = '46fe6278db0c47a5460e882fa1742f15ab49bb14'
$source = [System.IO.Path]::GetFullPath($SourceDirectory)
$archive = [System.IO.Path]::GetFullPath($OutputArchive)

if (-not (Test-Path -LiteralPath (Join-Path $source '.git'))) {
    throw "SourceDirectory must be a Git checkout: $source"
}

$actualCommit = (git -C $source rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0 -or $actualCommit -ne $expectedCommit) {
    throw "Expected upstream commit $expectedCommit, got $actualCommit"
}
if (git -C $source status --porcelain --untracked-files=no) {
    throw 'SourceDirectory has modified tracked files'
}

Push-Location (Join-Path $source 'web')
try {
    npm ci
    if ($LASTEXITCODE -ne 0) { throw 'npm ci failed' }
    npm run build
    if ($LASTEXITCODE -ne 0) { throw 'frontend build failed' }
} finally {
    Pop-Location
}

$index = Join-Path $source 'web/dist/index.html'
if (-not (Test-Path -LiteralPath $index -PathType Leaf) -or (Get-Item -LiteralPath $index).Length -eq 0) {
    throw 'frontend build did not produce web/dist/index.html'
}

$archiveParent = Split-Path -Parent $archive
New-Item -ItemType Directory -Force -Path $archiveParent | Out-Null
if (Test-Path -LiteralPath $archive) {
    Remove-Item -LiteralPath $archive -Force
}

tar -czf $archive --exclude=.git --exclude=web/node_modules -C $source .
if ($LASTEXITCODE -ne 0) { throw 'source archive creation failed' }

Write-Output "SOURCE_ARCHIVE=$archive"
Write-Output "SOURCE_COMMIT=$actualCommit"
Write-Output "WEB_INDEX_BYTES=$((Get-Item -LiteralPath $index).Length)"
