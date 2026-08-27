$ErrorActionPreference = 'Stop'

$ruleName = 'OnePlus Router ICMPv4'
$existing = Get-NetFirewallRule -DisplayName $ruleName -ErrorAction SilentlyContinue
if ($existing) {
    Set-NetFirewallRule -DisplayName $ruleName -Enabled True -Profile Any -Direction Inbound -Action Allow
    Set-NetFirewallAddressFilter -AssociatedNetFirewallRule $existing -RemoteAddress '192.168.2.0/24'
} else {
    New-NetFirewallRule `
        -DisplayName $ruleName `
        -Description 'Allow ICMPv4 echo requests only from the OnePlus router LAN.' `
        -Direction Inbound `
        -Action Allow `
        -Protocol ICMPv4 `
        -IcmpType 8 `
        -RemoteAddress '192.168.2.0/24' `
        -Profile Any | Out-Null
}

Write-Output 'ICMPv4 echo is allowed from 192.168.2.0/24.'
