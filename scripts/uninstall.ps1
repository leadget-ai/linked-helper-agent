#Requires -RunAsAdministrator
$ErrorActionPreference = 'Continue'

$ServiceName = 'LhAgent'
$InstallDir  = 'C:\Program Files\lh-agent'
$LogDir      = 'C:\ProgramData\lh-agent'
$NssmPath    = Join-Path $InstallDir 'nssm.exe'

if (Get-Service -Name $ServiceName -ErrorAction SilentlyContinue) {
  if (Test-Path $NssmPath) {
    & $NssmPath stop   $ServiceName confirm | Out-Null
    & $NssmPath remove $ServiceName confirm | Out-Null
  } else {
    sc.exe stop   $ServiceName | Out-Null
    sc.exe delete $ServiceName | Out-Null
  }
}

Remove-Item -Recurse -Force $InstallDir -ErrorAction SilentlyContinue
Remove-Item -Recurse -Force $LogDir     -ErrorAction SilentlyContinue
Write-Host "Uninstalled $ServiceName"
