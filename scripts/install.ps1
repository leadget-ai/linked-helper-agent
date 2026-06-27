# LH Agent installer for Windows.
#
# Run from elevated PowerShell:
#   irm https://github.com/leadget-ai/linked-helper-agent/releases/latest/download/install.ps1 | iex
# or locally:
#   powershell -ExecutionPolicy Bypass -File install.ps1
#
# Registers the agent as a *native* Windows service — the binary speaks the
# Service Control Manager protocol itself (golang.org/x/sys/windows/svc), so we
# need no third-party service wrapper. The service runs as LocalSystem, which
# has read access to every user's profile, so it can read Linked Helper's
# lh.db under %APPDATA% without storing anyone's Windows password.

$ErrorActionPreference = 'Stop'
# Old PowerShell defaults to TLS 1.0/1.1, which GitHub now rejects.
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$ServiceName = 'LhAgent'
$ApiEndpoint = 'https://api.analytics.leadget.ai'
$InstallDir  = 'C:\Program Files\lh-agent'
$LogDir      = 'C:\ProgramData\lh-agent\logs'
$AgentPath   = Join-Path $InstallDir 'lh-agent.exe'
$ReleaseUrl  = 'https://github.com/leadget-ai/linked-helper-agent/releases/latest/download/lh-agent-windows-amd64.exe'
$SvcRegKey   = "HKLM:\SYSTEM\CurrentControlSet\Services\$ServiceName"

function Write-Step($msg) { Write-Host ">> $msg" -ForegroundColor Cyan }
function Write-OK($msg)   { Write-Host "OK $msg" -ForegroundColor Green }

# 0. Elevation check. `#Requires -RunAsAdministrator` is silently ignored when
#    the script is piped through `irm | iex`, so verify explicitly.
$principal = New-Object Security.Principal.WindowsPrincipal(
  [Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltinRole]::Administrator)) {
  throw 'This installer must run in an elevated PowerShell (Run as Administrator).'
}

# 1. Folders
Write-Step "Creating $InstallDir and $LogDir"
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
New-Item -ItemType Directory -Force -Path $LogDir     | Out-Null

# 2. Read existing service env (on upgrade) BEFORE we tear the service down, so
#    we don't re-ask for settings the operator already supplied. We use an
#    explicit [regex] object instead of the -match automatic $matches variable
#    because the latter has had scope-related surprises under `irm | iex` —
#    enumerating Groups directly is bulletproof.
$existingEnv = @{}
if (Test-Path $SvcRegKey) {
  $raw = (Get-ItemProperty -Path $SvcRegKey -Name Environment -ErrorAction SilentlyContinue).Environment
  $envRe = [regex]'^([^=]+)=(.*)$'
  foreach ($line in @($raw)) {
    $m = $envRe.Match([string]$line)
    if ($m.Success) {
      $existingEnv[$m.Groups[1].Value] = $m.Groups[2].Value
    }
  }
}
Write-Step ("Loaded {0} existing env var(s): {1}" -f $existingEnv.Count, ($existingEnv.Keys -join ', '))

# 3. Stop and remove any existing registration so we can drop a fresh binary
#    and re-create the service cleanly (avoids stale binPath / locked file).
if (Get-Service -Name $ServiceName -ErrorAction SilentlyContinue) {
  Write-Step "Stopping and removing existing $ServiceName"
  Stop-Service -Name $ServiceName -Force -ErrorAction SilentlyContinue
  Start-Sleep -Seconds 2
  sc.exe delete $ServiceName | Out-Null
  Start-Sleep -Seconds 2
}

# 4. Download the latest agent binary.
Write-Step 'Downloading lh-agent.exe'
Invoke-WebRequest -Uri $ReleaseUrl -OutFile $AgentPath -UseBasicParsing
Write-OK 'Agent binary in place'

function PromptOrKeep($name, $default, [switch]$Secret) {
  if ($default) { return $default }
  if ($Secret) {
    $sec = Read-Host -AsSecureString "$name"
    return [System.Net.NetworkCredential]::new('', $sec).Password
  }
  return Read-Host "$name"
}

# Endpoint is fixed for this deployment; the configured value always wins so an
# upgrade migrates installs off any stale endpoint baked into a prior install.
$apiEndpoint   = $ApiEndpoint
if ($existingEnv['LHA_API_ENDPOINT'] -and $existingEnv['LHA_API_ENDPOINT'] -ne $apiEndpoint) {
  Write-Step "Updating API endpoint from $($existingEnv['LHA_API_ENDPOINT']) to $apiEndpoint"
} else {
  Write-Step "Using API endpoint $apiEndpoint"
}
$apiKey        = PromptOrKeep 'LHA_API_KEY (issued from the platform UI)' $existingEnv['LHA_API_KEY'] -Secret

# %APPDATA% here is the *installing* admin's Roaming folder. If Linked Helper
# runs under a different account, type that user's path (e.g.
# C:\Users\manager\AppData\Roaming\linked-helper\Partitions). The LocalSystem
# service can read any user's profile, so only the path needs to be correct.
$defaultParts  = Join-Path $env:APPDATA 'linked-helper\Partitions'
$partitionsDir = PromptOrKeep "LHA_PARTITIONS_DIR (default: $defaultParts)" $existingEnv['LHA_PARTITIONS_DIR']
if (-not $partitionsDir) { $partitionsDir = $defaultParts }

if (-not (Test-Path $partitionsDir)) {
  Write-Warning "$partitionsDir does not exist yet. Linked Helper creates it on first launch."
}

# 5. Create the service (runs as LocalSystem by default — no -Credential).
Write-Step "Creating service $ServiceName"
New-Service -Name $ServiceName -BinaryPathName "`"$AgentPath`"" `
  -DisplayName 'LH Agent' -StartupType Automatic | Out-Null

# 6. Per-service environment lives in the service's registry key as REG_MULTI_SZ;
#    the SCM merges it into the process environment at launch. This keeps the API
#    key out of the machine-wide environment.
Write-Step 'Writing service environment'
$envLines = @(
  "LHA_API_ENDPOINT=$apiEndpoint",
  "LHA_API_KEY=$apiKey",
  "LHA_PARTITIONS_DIR=$partitionsDir",
  "LHA_LOG_LEVEL=info"
)
New-ItemProperty -Path $SvcRegKey -Name Environment -PropertyType MultiString -Value $envLines -Force | Out-Null

# 7. Crash recovery: restart with backoff (10s, 30s, 60s); reset the counter
#    after a day of stability. Built into the SCM — no wrapper needed.
Write-Step 'Configuring automatic restart on failure'
sc.exe failure $ServiceName reset= 86400 actions= restart/10000/restart/30000/restart/60000 | Out-Null

# 8. Start it.
Write-Step "Starting $ServiceName"
Start-Service -Name $ServiceName
Start-Sleep -Seconds 2
$svc = Get-Service -Name $ServiceName
Write-OK "Service $ServiceName is $($svc.Status)"

Write-Host ''
Write-Host 'Logs:' -ForegroundColor Yellow
Write-Host "  Get-Content -Wait '$LogDir\lh-agent.log'"
Write-Host ''
Write-Host 'Service control:' -ForegroundColor Yellow
Write-Host "  Restart-Service $ServiceName"
Write-Host "  Stop-Service    $ServiceName"
Write-Host "  Get-Service     $ServiceName"
