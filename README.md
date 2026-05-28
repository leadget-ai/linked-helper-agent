# lh-agent

Reads campaign and funnel data from a local [Linked Helper 2](https://www.linkedhelper.com/) desktop installation and pushes it to the leadget platform every ~10 minutes.

The agent only reads `lh.db` (SQLite, WAL, read-only) — it never writes to it. Linked Helper can be running at the same time without interference.

## Configuration

Environment variables:

| Var | Required | Purpose |
| --- | --- | --- |
| `LHA_API_ENDPOINT` | yes | Platform base URL, e.g. `https://api.leadget.ai` |
| `LHA_API_KEY` | yes | Agent bearer token issued from the platform UI |
| `LHA_PARTITIONS_DIR` | yes | Path to the Linked Helper `Partitions` folder |
| `LHA_LOG_LEVEL` | no | `debug` / `info` (default) / `warn` / `error` |
| `LHA_DISABLE_KEEP_ALIVE` | no | `true` to force a fresh TCP conn per request |
| `LHA_PPROF` | no | `true` to expose `localhost:6060/debug/pprof/` |

Default `LHA_PARTITIONS_DIR` per OS:

- **Windows**: `%APPDATA%\linked-helper\Partitions`
- **Linux**: `~/.config/linked-helper-partitions`
- **macOS**: `~/Library/Application Support/linked-helper/Partitions`

## Install on Windows

Open **PowerShell as Administrator** and run:

```powershell
irm https://github.com/leadget-ai/linked-helper-agent/releases/latest/download/install.ps1 | iex
```

The installer will:

1. Drop the agent binary into `C:\Program Files\lh-agent\`.
2. Install [NSSM](https://nssm.cc) (downloaded automatically).
3. Ask for `LHA_API_ENDPOINT`, `LHA_API_KEY`, `LHA_PARTITIONS_DIR` (default points at `%APPDATA%\linked-helper\Partitions`).
4. Ask for your **Windows password** so the service can run as your user — needed to read `%APPDATA%`.
5. Register `LhAgent` as a Windows service (auto-start on boot) and start it.

Re-run the same command at any time to **upgrade** to the latest version. The installer keeps your saved settings — it'll only re-ask for the Windows password.

### View logs

```powershell
Get-Content -Wait 'C:\ProgramData\lh-agent\logs\lh-agent.log'
Get-Content -Wait 'C:\ProgramData\lh-agent\logs\lh-agent.err.log'
```

### Service control

```powershell
Restart-Service LhAgent
Stop-Service    LhAgent
Get-Service     LhAgent
```

### Uninstall

```powershell
irm https://github.com/leadget-ai/linked-helper-agent/releases/latest/download/uninstall.ps1 | iex
```

## Build from source

```sh
go build -o lh-agent ./cmd/agent
```
