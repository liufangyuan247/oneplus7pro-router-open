$ErrorActionPreference = 'Stop'

$handler = Join-Path $PSScriptRoot 'router-web-launcher.ps1'
$powershell = "$env:SystemRoot\System32\WindowsPowerShell\v1.0\powershell.exe"

if (-not (Test-Path -LiteralPath $handler)) {
    throw "Launcher handler not found: $handler"
}

function Install-UrlProtocol {
    param(
        [Parameter(Mandatory = $true)] [string]$Scheme,
        [Parameter(Mandatory = $true)] [string]$Description
    )

    $root = "HKCU:\Software\Classes\$Scheme"
    New-Item -Path $root -Force | Out-Null
    Set-Item -Path $root -Value "URL:$Description"
    New-ItemProperty -Path $root -Name 'URL Protocol' -Value '' -PropertyType String -Force | Out-Null
    $commandKey = New-Item -Path "$root\shell\open\command" -Force
    $command = ('"{0}" -NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File "{1}" "%1"' -f $powershell, $handler)
    Set-Item -Path $commandKey.PSPath -Value $command
}

Install-UrlProtocol -Scheme 'op7router-scrcpy' -Description 'OnePlus Router scrcpy'
Install-UrlProtocol -Scheme 'op7router-ssh' -Description 'OnePlus Router SSH'

Write-Output 'OnePlus Router web launchers installed for the current Windows user.'
