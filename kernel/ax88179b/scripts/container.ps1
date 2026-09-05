[CmdletBinding()]
param(
    [ValidateSet('image', 'prepare', 'integrate', 'configure', 'build', 'candidate', 'candidate-package', 'performance-modules', 'package', 'shell', 'status')]
    [string]$Action = 'status',
    [ValidateRange(1, 12)]
    [int]$Jobs = 8
)

$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$composeFile = Join-Path $projectRoot 'build-env\compose.yaml'
$compose = @('compose', '--project-directory', $projectRoot, '-f', $composeFile)

function Invoke-Compose {
    param([string[]]$Arguments)
    & docker @compose @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "docker compose failed with exit code $LASTEXITCODE"
    }
}

switch ($Action) {
    'image'     { Invoke-Compose @('build', 'kernel-builder') }
    'prepare'   { Invoke-Compose @('run', '--rm', 'kernel-builder', 'bash', '/project/scripts/prepare-workspace.sh') }
    'integrate' { Invoke-Compose @('run', '--rm', 'kernel-builder', 'bash', '/project/scripts/integrate-driver.sh') }
    'configure' { Invoke-Compose @('run', '--rm', 'kernel-builder', 'bash', '/project/scripts/configure.sh') }
    'build'     { Invoke-Compose @('run', '--rm', '-e', "OP7P_BUILD_JOBS=$Jobs", 'kernel-builder', 'bash', '/project/scripts/build.sh') }
    'candidate' { Invoke-Compose @('run', '--rm', '-e', "OP7P_BUILD_JOBS=$Jobs", 'kernel-builder', 'bash', '/project/scripts/build-resetfix-fqcodel.sh') }
    'candidate-package' { Invoke-Compose @('run', '--rm', 'kernel-builder', 'bash', '/project/scripts/package-resetfix-fqcodel.sh') }
    'performance-modules' { Invoke-Compose @('run', '--rm', '-e', "OP7P_BUILD_JOBS=$Jobs", 'kernel-builder', 'bash', '/project/scripts/build-network-performance-modules.sh') }
    'package'   { Invoke-Compose @('run', '--rm', 'kernel-builder', 'bash', '/project/scripts/package.sh') }
    'shell'     { Invoke-Compose @('run', '--rm', 'kernel-builder', 'bash') }
    'status'    { & docker volume inspect oneplus7pro-oos11-ax88179b-work }
}
