package lh

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"
)

// fixture returns the read-only path to a committed test database. Tests open
// these exactly like production (Reader.open uses mode=ro) and never mutate
// them.
func fixture(name string) string {
	return filepath.Join("testdata", "fixtures", name+".db")
}

func strv(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

func intv(p *int) string {
	if p == nil {
		return "<nil>"
	}
	return strconv.Itoa(*p)
}

func int64v(p *int64) string {
	if p == nil {
		return "<nil>"
	}
	return strconv.FormatInt(*p, 10)
}

// wantOwner is the canonical resolved identity; per-fixture cases override the
// fields a schema/data gap leaves empty.
type wantOwner struct {
	externalID  string
	email       string
	fullName    string
	avatar      string
	profileURL  string
	publicSlug  string
	lastLoginAt string
	ssi         string
}

func canonicalOwner() wantOwner {
	return wantOwner{
		externalID: "100200300",
		email:      "owner@example.test",
		fullName:   "Test Owner",
		avatar:     "https://example.test/avatar.jpg",
		profileURL: "https://www.linkedin.com/in/test-owner",
		publicSlug: "test-owner",
		// modernc normalizes DATETIME columns on read, dropping the zero
		// millis the fixture stores (2026-01-01T00:00:00.000Z).
		lastLoginAt: "2026-01-01T00:00:00Z",
		ssi:         "42",
	}
}

func assertOwner(t *testing.T, got *AccountOwner, want wantOwner) {
	t.Helper()
	if got == nil {
		t.Fatalf("owner = nil, want %+v", want)
	}
	checks := []struct {
		name, got, want string
	}{
		{"externalID", int64v(got.ExternalID), want.externalID},
		{"email", strv(got.Email), want.email},
		{"fullName", strv(got.FullName), want.fullName},
		{"avatar", strv(got.Avatar), want.avatar},
		{"profileURL", strv(got.ProfileURL), want.profileURL},
		{"publicSlug", strv(got.PublicSlug), want.publicSlug},
		{"lastLoginAt", strv(got.LastLoginAt), want.lastLoginAt},
		{"ssi", intv(got.SSI), want.ssi},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}

// TestReadAccountOwner_Golden is the cross-version contract: the same owner
// resolves identically from v195 (identity in person tables only), v205
// (identity in both li_accounts and person tables) and v210 (li_accounts
// identity, mini-profile avatar gap, no person_email). This is exactly what the
// owner-sources architecture exists to guarantee.
func TestReadAccountOwner_Golden(t *testing.T) {
	v210 := canonicalOwner()
	v210.email = "<nil>" // v210 has neither li_accounts.email nor a person_email row

	cases := []struct {
		fixture string
		want    wantOwner
	}{
		{"owner-v195", canonicalOwner()},
		{"owner-v205", canonicalOwner()},
		{"owner-v210", v210},
	}

	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			r := NewReader()
			defer r.Close()
			got, err := r.ReadAccountOwner(context.Background(), fixture(tc.fixture))
			if err != nil {
				t.Fatalf("ReadAccountOwner: %v", err)
			}
			assertOwner(t, got, tc.want)
		})
	}
}

// TestReadAccountOwner_Degradations covers the partitions where identity is
// partial: li_accounts-only (no member id / profile URL), hash-only (profile
// URL falls back to the ACoAA… form) and empty (nil owner).
func TestReadAccountOwner_Degradations(t *testing.T) {
	t.Run("degraded-li-accounts-only", func(t *testing.T) {
		r := NewReader()
		defer r.Close()
		got, err := r.ReadAccountOwner(context.Background(), fixture("owner-degraded-v205"))
		if err != nil {
			t.Fatalf("ReadAccountOwner: %v", err)
		}
		assertOwner(t, got, wantOwner{
			externalID:  "<nil>",
			email:       "owner@example.test",
			fullName:    "Test Owner",
			avatar:      "https://example.test/avatar.jpg",
			profileURL:  "<nil>",
			publicSlug:  "<nil>",
			lastLoginAt: "2026-01-01T00:00:00Z",
			ssi:         "<nil>",
		})
	})

	t.Run("hash-only-profile-url-fallback", func(t *testing.T) {
		r := NewReader()
		defer r.Close()
		got, err := r.ReadAccountOwner(context.Background(), fixture("owner-hashonly-v205"))
		if err != nil {
			t.Fatalf("ReadAccountOwner: %v", err)
		}
		want := canonicalOwner()
		want.publicSlug = "<nil>"
		want.profileURL = "https://www.linkedin.com/in/ACoAAATESTHASH0000000000000000000000000"
		assertOwner(t, got, want)
	})

	t.Run("empty-returns-nil", func(t *testing.T) {
		r := NewReader()
		defer r.Close()
		got, err := r.ReadAccountOwner(context.Background(), fixture("empty-v205"))
		if err != nil {
			t.Fatalf("ReadAccountOwner: %v", err)
		}
		if got != nil {
			t.Fatalf("owner = %+v, want nil", got)
		}
	})
}

func TestBuildDBProfile(t *testing.T) {
	cases := []struct {
		fixture        string
		wantVersion    int
		wantLiFullName bool // li_accounts.full_name column present (v205+ only)
	}{
		{"owner-v195", 195, false},
		{"owner-v205", 205, true},
		{"owner-v210", 210, true},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			r := NewReader()
			defer r.Close()
			p, err := r.InspectProfile(context.Background(), fixture(tc.fixture))
			if err != nil {
				t.Fatalf("InspectProfile: %v", err)
			}
			if p.SchemaVersion != tc.wantVersion {
				t.Errorf("SchemaVersion = %d, want %d", p.SchemaVersion, tc.wantVersion)
			}
			if !p.HasTable("li_accounts") {
				t.Errorf("HasTable(li_accounts) = false")
			}
			if got := p.HasColumn("li_accounts", "full_name"); got != tc.wantLiFullName {
				t.Errorf("HasColumn(li_accounts.full_name) = %v, want %v", got, tc.wantLiFullName)
			}
		})
	}
}
