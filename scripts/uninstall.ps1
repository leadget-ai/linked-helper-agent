$ErrorActionPreference = 'Continue'

$principal = New-Object Security.Principal.WindowsPrincipal(
  [Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltinRole]::Administrator)) {
  throw 'This uninstaller must run in an elevated PowerShell (Run as Administrator).'
}

$ServiceName = 'LhAgent'
$InstallDir  = 'C:\Program Files\lh-agent'
$DataDir     = 'C:\ProgramData\lh-agent'

if (Get-Service -Name $ServiceName -ErrorAction SilentlyContinue) {
  Stop-Service -Name $ServiceName -Force -ErrorAction SilentlyContinue
  Start-Sleep -Seconds 2
  sc.exe delete $ServiceName | Out-Null
}

Remove-Item -Recurse -Force $InstallDir -ErrorAction SilentlyContinue
Remove-Item -Recurse -Force $DataDir    -ErrorAction SilentlyContinue
Write-Host "Uninstalled $ServiceName"
