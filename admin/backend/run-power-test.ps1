param(
  [Parameter(Mandatory = $true)]
  [string]$Password,
  [string]$RouterUrl = 'http://192.168.2.107:9090',
  [int]$BaselineSeconds = 60,
  [int]$StressSeconds = 180,
  [int]$RecoverySeconds = 60,
  [int]$IntervalSeconds = 5,
  [double]$CpuCutoffC = 80,
  [double]$BatteryCutoffC = 43.5
)

$ErrorActionPreference = 'Stop'
$stamp = Get-Date -Format 'yyyyMMdd-HHmmss'
$output = Join-Path $PSScriptRoot "power-test-$stamp.csv"
$body = @{ password = $Password } | ConvertTo-Json -Compress
Invoke-RestMethod -Uri "$RouterUrl/api/login" -Method Post -ContentType 'application/json' -Body $body -SessionVariable session | Out-Null

function Stop-PhoneStress {
  adb shell run-as com.termux /data/data/com.termux/files/usr/bin/sh /data/local/tmp/stop-cpu-stress.sh | Out-Host
}

function Measure-Phase([string]$Phase, [int]$Seconds, [bool]$EnforceCutoff) {
  $samples = [Math]::Max(1, [Math]::Ceiling($Seconds / $IntervalSeconds))
  for ($i = 0; $i -lt $samples; $i++) {
    try {
      $snapshot = Invoke-RestMethod -Uri "$RouterUrl/api/snapshot" -WebSession $session -TimeoutSec 4
      $power = $snapshot.power
      $cpu = $snapshot.cpu
      $memory = $snapshot.memory
      $row = [pscustomobject]@{
        Time = (Get-Date).ToString('o')
        Phase = $Phase
        BatteryPercent = $power.batteryPercent
        BatteryTempC = $power.BatteryTemp
        BatteryVoltageV = $power.Voltage
        BatteryCurrentA = $power.Current
        BatteryPowerW = $power.BatteryWatts
        ChargeStatus = $power.Status
        USBOnline = $power.usbOnline
        USBType = $power.usbType
        CPUUsagePercent = $cpu.usage
        CPUTempC = $cpu.temperature
        Load1 = $cpu.Load1
        AvgMHz = $cpu.avgMHz
        MemAvailableGB = [Math]::Round($memory.Available / 1GB, 3)
      }
      $row | Export-Csv -Path $output -Append -NoTypeInformation -Encoding UTF8
      Write-Host ("{0:HH:mm:ss} {1,-8} batt={2}% {3,5:N1}C {4,6:N2}W cpu={5,5:N1}% {6,5:N1}C {7,4}MHz mem={8:N2}GB pd={9}" -f (Get-Date), $Phase, $row.BatteryPercent, $row.BatteryTempC, $row.BatteryPowerW, $row.CPUUsagePercent, $row.CPUTempC, $row.AvgMHz, $row.MemAvailableGB, $row.USBOnline)

      if ($EnforceCutoff -and (($row.CPUTempC -ge $CpuCutoffC) -or ($row.BatteryTempC -ge $BatteryCutoffC) -or (-not $row.USBOnline))) {
        Write-Warning "Safety cutoff triggered: CPU=$($row.CPUTempC)C battery=$($row.BatteryTempC)C USB=$($row.USBOnline)"
        Stop-PhoneStress
        return $false
      }
    }
    catch {
      Write-Warning "Sampling failed: $($_.Exception.Message)"
      if ($EnforceCutoff) { Stop-PhoneStress }
      return $false
    }
    Start-Sleep -Seconds $IntervalSeconds
  }
  return $true
}

try {
  Write-Host "POWER_TEST_OUTPUT=$output"
  Measure-Phase 'baseline' $BaselineSeconds $false | Out-Null
  adb shell run-as com.termux /data/data/com.termux/files/usr/bin/sh /data/local/tmp/start-cpu-stress.sh | Out-Host
  Measure-Phase 'stress' $StressSeconds $true | Out-Null
}
finally {
  Stop-PhoneStress
}
Measure-Phase 'recovery' $RecoverySeconds $false | Out-Null
Write-Host "POWER_TEST_COMPLETE=$output"
