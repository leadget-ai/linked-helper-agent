package agent

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const idFileName = "agent-id"

// LoadOrCreateAgentID returns the persistent install UUID, generating it on
// first run. It lives in a per-OS data dir OUTSIDE the install dir
// (C:\ProgramData\lh-agent, ~/Library/Application Support/lh-agent,
// /var/lib/lh-agent or ~/.config/lh-agent) so it survives service
// reinstalls. Returns "" + nil when no data dir is writable — the agent still
// works, the platform falls back to hostname matching.
func LoadOrCreateAgentID() (string, error) {
	dir := dataDir()
	if dir == "" {
		return "", nil
	}
	path := filepath.Join(dir, idFileName)

	if raw, err := os.ReadFile(path); err == nil {
		if id := strings.TrimSpace(string(raw)); id != "" {
			return id, nil
		}
	}

	id, err := newUUIDv4()
	if err != nil {
		return "", fmt.Errorf("generate agent id: %w", err)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}
	if err := os.WriteFile(path, []byte(id), 0o600); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return id, nil
}

func dataDir() string {
	switch runtime.GOOS {
	case "windows":
		// install.ps1 already creates C:\ProgramData\lh-agent for logs, so it
		// is known-writable by the LocalSystem service.
		base := os.Getenv("ProgramData")
		if base == "" {
			base = `C:\ProgramData`
		}
		return filepath.Join(base, "lh-agent")
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		return filepath.Join(home, "Library", "Application Support", "lh-agent")
	default:
		if os.Geteuid() == 0 {
			return "/var/lib/lh-agent"
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		return filepath.Join(home, ".config", "lh-agent")
	}
}

// newUUIDv4 produces an RFC 4122 v4 string without pulling in a dependency.
func newUUIDv4() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf(
		"%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16],
	), nil
}
