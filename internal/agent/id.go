// Package agent — persistent install id.
//
// The platform identifies agent installs by a UUID we generate on first run.
// It lives in a per-OS data dir OUTSIDE the install dir, so it survives
// `sc.exe delete` + `New-Service` on Windows (the install dir + service
// registry get torn down on every upgrade) and analogous reinstalls on
// macOS / Linux. As long as the data dir is intact, the same agent install
// keeps the same id forever.
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

// LoadOrCreateAgentID returns the persistent id from disk, generating and
// writing one on first run. The path is OS-specific so admins can find it
// easily for diagnostics:
//
//	Windows: C:\ProgramData\lh-agent\agent-id
//	macOS:   ~/Library/Application Support/lh-agent/agent-id
//	Linux:   /var/lib/lh-agent/agent-id (root) or ~/.config/lh-agent/agent-id
//
// Returns "" + nil on unrecoverable IO errors (no data dir writable) — the
// agent still works, the platform just can't dedupe by id and falls back
// to hostname matching.
func LoadOrCreateAgentID() (string, error) {
	dir := dataDir()
	if dir == "" {
		return "", nil
	}
	path := filepath.Join(dir, idFileName)

	if raw, err := os.ReadFile(path); err == nil {
		id := strings.TrimSpace(string(raw))
		if id != "" {
			return id, nil
		}
		// File exists but is blank — fall through to regenerate.
	}

	id, err := newUUIDv4()
	if err != nil {
		return "", fmt.Errorf("generate agent id: %w", err)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}
	// 0600 so a non-admin user can't read the id from a shared machine —
	// it's not a secret per se, but no reason to leak it.
	if err := os.WriteFile(path, []byte(id), 0o600); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return id, nil
}

// dataDir picks the directory we'll persist the id (and any future agent-side
// state) into. The Windows path matches what install.ps1 already creates for
// logs, so we know it's writable by the LocalSystem service.
func dataDir() string {
	switch runtime.GOOS {
	case "windows":
		// ProgramData is the canonical machine-wide data dir; install.ps1
		// already uses C:\ProgramData\lh-agent\logs.
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
	default: // linux + everything else POSIX-ish
		// Prefer the system path when running as root (likely the service
		// account); otherwise fall back to the user's XDG config.
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
// 16 bytes of crypto/rand, set the version + variant bits, hex-format with
// hyphens.
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
