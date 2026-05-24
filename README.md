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

## Build

```sh
go build -o lh-agent ./cmd/agent
```
