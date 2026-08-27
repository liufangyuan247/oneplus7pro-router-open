param(
    [Parameter(Mandatory = $true)]
    [string]$Uri
)

$ErrorActionPreference = 'Stop'
$powershell = "$env:SystemRoot\System32\WindowsPowerShell\v1.0\powershell.exe"
$scheme = ([Uri]$Uri).Scheme.ToLowerInvariant()

switch ($scheme) {
    'op7router-scrcpy' {
        $script = Join-Path $PSScriptRoot 'start-oneplus-router.ps1'
        Start-Process -FilePath $powershell -WindowStyle Hidden -ArgumentList @(
            '-NoProfile',
            '-ExecutionPolicy', 'Bypass',
            '-File', ('"{0}"' -f $script),
            '-DeviceAddress', '192.168.2.1:5555'
        )
    }
    'op7router-ssh' {
        Start-Process -FilePath $powershell -ArgumentList @(
            '-NoExit',
            '-Command',
            'ssh -p 8022 192.168.2.1'
        )
    }
    default {
        throw "Unsupported router launcher URI: $Uri"
    }
}
