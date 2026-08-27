[CmdletBinding()]
param(
    [ValidateSet('image', 'prepare', 'configure', 'build', 'package', 'shell', 'status')]
    [string]$Action = 'status',
    [ValidateRange(1, 12)]
    [int]$Jobs = 8
)

$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$composeFile = Join-Path $projectRoot 'build-env\compose.yaml'
$compose = @('compose', '--project-directory', $projectRoot, '-f', $composeFile)

function Invoke-DockerCompose {
    param([string[]]$Arguments)
    & docker @compose @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "docker compose failed with exit code $LASTEXITCODE"
    }
}

switch ($Action) {
    'image' {
        Invoke-DockerCompose @('build', 'kernel-builder')
    }
    'prepare' {
        Invoke-DockerCompose @('run', '--rm', 'kernel-builder', 'bash', '/project/scripts/prepare-workspace.sh')
    }
    'configure' {
        Invoke-DockerCompose @('run', '--rm', 'kernel-builder', 'bash', '/project/scripts/configure-baseline.sh')
    }
    'build' {
        Invoke-DockerCompose @(
            'run', '--rm', '-e', "OP7P_BUILD_JOBS=$Jobs",
            'kernel-builder', 'bash', '/project/scripts/build-baseline.sh'
        )
    }
    'package' {
        Invoke-DockerCompose @('run', '--rm', 'kernel-builder', 'bash', '/project/scripts/package-baseline.sh')
    }
    'shell' {
        Invoke-DockerCompose @('run', '--rm', 'kernel-builder', 'bash')
    }
    'status' {
        & docker version
        if ($LASTEXITCODE -ne 0) {
            throw 'Docker Desktop Linux engine is not running.'
        }
        & docker volume inspect oneplus7pro-kernel-work
        if ($LASTEXITCODE -ne 0) {
            Write-Host 'The isolated build volume has not been created yet.'
        }
    }
}
