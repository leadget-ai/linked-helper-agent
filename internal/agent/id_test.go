package agent

import (
	"regexp"
	"runtime"
	"testing"
)

var uuidV4Re = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestNewUUIDv4Shape(t *testing.T) {
	id, err := newUUIDv4()
	if err != nil {
		t.Fatalf("newUUIDv4: %v", err)
	}
	if !uuidV4Re.MatchString(id) {
		t.Errorf("id %q is not a well-formed RFC 4122 v4 UUID", id)
	}
}

// TestLoadOrCreateAgentID_Persists redirects the per-OS data dir at a tempdir
// (via HOME) and checks the id is generated once and then read back stably.
func TestLoadOrCreateAgentID_Persists(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("data dir derives from ProgramData on windows, not HOME")
	}
	t.Setenv("HOME", t.TempDir())

	first, err := LoadOrCreateAgentID()
	if err != nil {
		t.Fatalf("first LoadOrCreateAgentID: %v", err)
	}
	if !uuidV4Re.MatchString(first) {
		t.Fatalf("generated id %q not a v4 UUID", first)
	}

	second, err := LoadOrCreateAgentID()
	if err != nil {
		t.Fatalf("second LoadOrCreateAgentID: %v", err)
	}
	if second != first {
		t.Errorf("second call = %q, want persisted %q", second, first)
	}
}
