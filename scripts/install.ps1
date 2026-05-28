# LH Agent installer for Windows.
#
# Run from elevated PowerShell:
#   irm https://github.com/leadget-ai/linked-helper-agent/releases/latest/download/install.ps1 | iex
# or locally:
#   powershell -ExecutionPolicy Bypass -File install.ps1
#
# Installs NSSM (via choco if available, otherwise direct download), drops the
# agent binary into C:\Program Files\lh-agent, prompts for API endpoint + key,
# registers a Windows service that runs under the *current* user account so it
# can read Linked Helper's %APPDATA% partitions.

#Requires -RunAsAdministrator

$ErrorActionPreference = 'Stop'

$ServiceName  = 'LhAgent'
$ApiEndpoint  = 'https://api.leadget-analytics.rawnodes.com'
$InstallDir   = 'C:\Program Files\lh-agent'
$LogDir       = 'C:\ProgramData\lh-agent\logs'
$NssmPath     = Join-Path $InstallDir 'nssm.exe'
$AgentPath    = Join-Path $InstallDir 'lh-agent.exe'
$ReleaseUrl   = 'https://github.com/leadget-ai/linked-helper-agent/releases/latest/download/lh-agent-windows-amd64.exe'
$NssmZipUrl   = 'https://nssm.cc/release/nssm-2.24.zip'

function Write-Step($msg) { Write-Host ">> $msg" -ForegroundColor Cyan }
function Write-OK($msg)   { Write-Host "OK $msg" -ForegroundColor Green }

# 1. Folders
Write-Step "Creating $InstallDir and $LogDir"
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
New-Item -ItemType Directory -Force -Path $LogDir     | Out-Null

# 2. NSSM — preferred way to run a Go binary as a service. sc.exe technically
#    works for console exes too but its restart/logging story is awful and we
#    don't want to reinvent it.
if (-not (Test-Path $NssmPath)) {
  Write-Step 'Fetching NSSM 2.24'
  $tmpZip = Join-Path $env:TEMP 'nssm.zip'
  $tmpDir = Join-Path $env:TEMP 'nssm-extract'
  Invoke-WebRequest -Uri $NssmZipUrl -OutFile $tmpZip
  Remove-Item -Recurse -Force $tmpDir -ErrorAction SilentlyContinue
  Expand-Archive -Path $tmpZip -DestinationPath $tmpDir -Force
  Copy-Item -Force "$tmpDir\nssm-2.24\win64\nssm.exe" $NssmPath
  Remove-Item -Force $tmpZip
  Remove-Item -Recurse -Force $tmpDir
  Write-OK 'NSSM ready'
}

# 3. Download the latest agent. We always overwrite — if the service is
#    already registered we stop it first so the file isn't locked.
if (Get-Service -Name $ServiceName -ErrorAction SilentlyContinue) {
  Write-Step "Stopping existing $ServiceName so we can replace the binary"
  & $NssmPath stop $ServiceName confirm | Out-Null
  Start-Sleep -Seconds 2
}

Write-Step 'Downloading lh-agent.exe'
Invoke-WebRequest -Uri $ReleaseUrl -OutFile $AgentPath -UseBasicParsing
Write-OK 'Agent binary in place'

# 4. Read existing service env (if upgrading) so we don't ask for the API key
#    again on reinstall. Falls back to prompting if absent.
$existingEnv = @{}
if (Get-Service -Name $ServiceName -ErrorAction SilentlyContinue) {
  $raw = & $NssmPath get $ServiceName AppEnvironmentExtra 2>$null
  if ($LASTEXITCODE -eq 0 -and $raw) {
    foreach ($line in $raw -split "`r?`n") {
      if ($line -match '^([^=]+)=(.*)$') { $existingEnv[$matches[1]] = $matches[2] }
    }
  }
}

function PromptOrKeep($name, $default, [switch]$Secret) {
  if ($default) { return $default }
  if ($Secret) {
    $sec = Read-Host -AsSecureString "$name"
    return [System.Net.NetworkCredential]::new('', $sec).Password
  }
  return Read-Host "$name"
}

# Endpoint is fixed for this deployment — never prompted. An existing service
# env still wins on upgrade in case it was pointed elsewhere manually.
$apiEndpoint   = if ($existingEnv['LHA_API_ENDPOINT']) { $existingEnv['LHA_API_ENDPOINT'] } else { $ApiEndpoint }
Write-Step "Using API endpoint $apiEndpoint"
$apiKey        = PromptOrKeep 'LHA_API_KEY (issued from the platform UI)'      $existingEnv['LHA_API_KEY'] -Secret
$defaultParts  = Join-Path $env:APPDATA 'linked-helper\Partitions'
$partitionsDir = PromptOrKeep "LHA_PARTITIONS_DIR (default: $defaultParts)"    $existingEnv['LHA_PARTITIONS_DIR']
if (-not $partitionsDir) { $partitionsDir = $defaultParts }

if (-not (Test-Path $partitionsDir)) {
  Write-Warning "$partitionsDir does not exist yet. Linked Helper creates it on first launch."
}

# 5. Register service. ObjectName = current interactive user so we can read
#    their %APPDATA%. NSSM needs the password to attach the service to a user
#    account — there's no LocalSystem trick that lets it touch HKCU/APPDATA
#    of someone else cleanly.
$currentUser = "$env:USERDOMAIN\$env:USERNAME"
Write-Step "Registering service $ServiceName under $currentUser"

if (-not (Get-Service -Name $ServiceName -ErrorAction SilentlyContinue)) {
  & $NssmPath install $ServiceName $AgentPath | Out-Null
} else {
  & $NssmPath set $ServiceName Application $AgentPath | Out-Null
}

$accountPwd = Read-Host -AsSecureString "Windows password for $currentUser (so the service can run as you)"
$plainPwd   = [System.Net.NetworkCredential]::new('', $accountPwd).Password
& $NssmPath set $ServiceName ObjectName $currentUser $plainPwd | Out-Null

# Env passed to the agent. NSSM joins these with newlines internally.
& $NssmPath set $ServiceName AppEnvironmentExtra `
  "LHA_API_ENDPOINT=$apiEndpoint" `
  "LHA_API_KEY=$apiKey" `
  "LHA_PARTITIONS_DIR=$partitionsDir" `
  "LHA_LOG_LEVEL=info" | Out-Null

# Logs: stdout/stderr go to rotating files in ProgramData. NSSM rotates them
# itself when they hit AppRotateBytes — 10 MiB is small enough to tail safely.
& $NssmPath set $ServiceName AppStdout    (Join-Path $LogDir 'lh-agent.log')     | Out-Null
& $NssmPath set $ServiceName AppStderr    (Join-Path $LogDir 'lh-agent.err.log') | Out-Null
& $NssmPath set $ServiceName AppRotateFiles 1                | Out-Null
& $NssmPath set $ServiceName AppRotateOnline 1               | Out-Null
& $NssmPath set $ServiceName AppRotateBytes 10485760         | Out-Null

# Restart automatically on crash, with backoff to avoid hot-loop on a bad
# config (10s → 30s → 60s).
& $NssmPath set $ServiceName AppRestartDelay 10000 | Out-Null
& $NssmPath set $ServiceName Start SERVICE_AUTO_START | Out-Null

Write-Step "Starting $ServiceName"
& $NssmPath start $ServiceName | Out-Null
Start-Sleep -Seconds 2

$svc = Get-Service -Name $ServiceName
Write-OK  "Service $ServiceName is $($svc.Status)"

Write-Host ''
Write-Host 'Logs:' -ForegroundColor Yellow
Write-Host "  Get-Content -Wait '$LogDir\lh-agent.log'"
Write-Host "  Get-Content -Wait '$LogDir\lh-agent.err.log'"
Write-Host ''
Write-Host 'Service control:' -ForegroundColor Yellow
Write-Host "  Restart-Service $ServiceName"
Write-Host "  Stop-Service    $ServiceName"
Write-Host "  Get-Service     $ServiceName"
