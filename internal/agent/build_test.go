package agent

import (
	"testing"

	"github.com/leadget/lh-agent/internal/client"
	"github.com/leadget/lh-agent/internal/lh"
)

func sp(s string) *string { return &s }
func i64(v int64) *int64  { return &v }

// msgRow / waitRow / plainRow build lh.CampaignActionRow fixtures for the
// builder tests without repeating the struct literal everywhere.
func msgRow(typ, body string) lh.CampaignActionRow {
	return lh.CampaignActionRow{Type: typ, Body: sp(body), Example: sp(body)}
}
func waitRow(typ string, ms int64) lh.CampaignActionRow {
	return lh.CampaignActionRow{Type: typ, WaitMs: i64(ms)}
}
func plainRow(typ string) lh.CampaignActionRow {
	return lh.CampaignActionRow{Type: typ}
}

const (
	dayMs  = int64(24 * 60 * 60_000)
	hourMs = int64(60 * 60_000)
)

func TestWaitToDelay(t *testing.T) {
	cases := []struct {
		name      string
		ms        int64
		wantValue int
		wantUnit  string
	}{
		{"exact 4 days", 4 * dayMs, 4, "DAYS"},
		{"exact 1 day", dayMs, 1, "DAYS"},
		{"exact 2 hours", 2 * hourMs, 2, "HOURS"},
		{"25 hours (not day-divisible)", 25 * hourMs, 25, "HOURS"},
		{"90 minutes (not hour-divisible)", 90 * 60_000, 90, "MINUTES"},
		{"1ms rounds up to 1 minute", 1, 1, "MINUTES"},
		{"sub-minute rounds up", 60_001, 2, "MINUTES"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v, u := waitToDelay(tc.ms)
			if v != tc.wantValue || u != tc.wantUnit {
				t.Errorf("waitToDelay(%d) = (%d, %q), want (%d, %q)", tc.ms, v, u, tc.wantValue, tc.wantUnit)
			}
		})
	}
}

// TestBuildActions checks that a CheckForReplies wait lands as the delay BEFORE
// the next messaging action, that waits accumulate across consecutive
// non-message steps, and that a trailing wait with no following message is
// discarded. coolDown is never an input here, proving it is ignored.
func TestBuildActions(t *testing.T) {
	rows := []lh.CampaignActionRow{
		msgRow("InvitePerson", "hi"),        // 0: no preceding wait → no delay
		waitRow("CheckForReplies", 4*dayMs), // accumulates 4 days
		waitRow("VisitProfile", dayMs),      // accumulates +1 day = 5 days
		msgRow("MessageToPerson", "yo"),     // 3: delay = 5 days
		waitRow("CheckForReplies", dayMs),   // trailing wait, no message after → discarded
	}
	got := buildActions(rows)
	if len(got) != len(rows) {
		t.Fatalf("len = %d, want %d (every row emitted, including non-message)", len(got), len(rows))
	}
	if got[0].DelayValue != nil {
		t.Errorf("action[0].DelayValue = %v, want nil", got[0].DelayValue)
	}
	if got[3].DelayValue == nil || *got[3].DelayValue != 5 || got[3].DelayUnit == nil || *got[3].DelayUnit != "DAYS" {
		t.Errorf("action[3] delay = (%v,%v), want (5,DAYS)", got[3].DelayValue, got[3].DelayUnit)
	}
	// Trailing CheckForReplies emitted but carries no delay of its own.
	if got[4].DelayValue != nil {
		t.Errorf("action[4].DelayValue = %v, want nil", got[4].DelayValue)
	}
}

// TestBuildFunnelSteps verifies replies recorded on the non-message step that
// follows a message fold back onto that message, and that leading non-message
// steps are ignored (no message to attribute them to yet).
func TestBuildFunnelSteps(t *testing.T) {
	steps := []lh.StepStat{
		{Type: "VisitProfile", Sent: 9, Replied: 5},    // leading non-message → ignored
		{Type: "InvitePerson", Sent: 3, Replied: 0},    // seq 1
		{Type: "CheckForReplies", Sent: 2, Replied: 2}, // folds +2 onto seq 1
		{Type: "MessageToPerson", Sent: 1, Replied: 0}, // seq 2
		{Type: "VisitProfile", Sent: 1, Replied: 1},    // folds +1 onto seq 2
	}
	got := buildFunnelSteps(steps)
	want := []client.FunnelStep{
		{SeqNumber: 1, Sent: 3, Replied: 2},
		{SeqNumber: 2, Sent: 1, Replied: 1},
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("step[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestHasMessageActions(t *testing.T) {
	cases := []struct {
		name string
		rows []lh.CampaignActionRow
		want bool
	}{
		{"invite with body", []lh.CampaignActionRow{msgRow("InvitePerson", "hi")}, true},
		{"invite without body", []lh.CampaignActionRow{plainRow("InvitePerson")}, false},
		{"message type with body", []lh.CampaignActionRow{msgRow("MessageToPerson", "x")}, true},
		{"non-message type with body", []lh.CampaignActionRow{msgRow("CheckForReplies", "x")}, false},
		{"empty", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasMessageActions(tc.rows); got != tc.want {
				t.Errorf("hasMessageActions = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBuildRegisterAccount(t *testing.T) {
	t.Run("member id formatted, owner block from slug+url", func(t *testing.T) {
		ssi := 42
		owner := &lh.AccountOwner{
			ExternalID:  i64(100200300),
			Email:       sp("owner@example.test"),
			FullName:    sp("Test Owner"),
			Avatar:      sp("https://example.test/avatar.jpg"),
			ProfileURL:  sp("https://www.linkedin.com/in/test-owner"),
			PublicSlug:  sp("test-owner"),
			LastLoginAt: sp("2026-01-01T00:00:00Z"),
			SSI:         &ssi,
		}
		got := buildRegisterAccount(owner)
		if got.ExternalID == nil || *got.ExternalID != "100200300" {
			t.Errorf("ExternalID = %v, want \"100200300\"", got.ExternalID)
		}
		if got.Owner == nil || got.Owner.ProfileURL == nil || got.Owner.PublicID == nil {
			t.Fatalf("Owner = %+v, want populated", got.Owner)
		}
		if *got.Owner.PublicID != "test-owner" {
			t.Errorf("Owner.PublicID = %q", *got.Owner.PublicID)
		}
		if got.Email == nil || got.FullName == nil || got.Avatar == nil || got.LastLoginAt == nil || got.SSI == nil {
			t.Errorf("identity fields not passed through: %+v", got)
		}
	})

	t.Run("no member id, no owner block", func(t *testing.T) {
		owner := &lh.AccountOwner{FullName: sp("Test Owner")}
		got := buildRegisterAccount(owner)
		if got.ExternalID != nil {
			t.Errorf("ExternalID = %v, want nil", got.ExternalID)
		}
		if got.Owner != nil {
			t.Errorf("Owner = %+v, want nil (no profileURL/publicSlug)", got.Owner)
		}
	})

	t.Run("owner block from url alone", func(t *testing.T) {
		owner := &lh.AccountOwner{ProfileURL: sp("https://www.linkedin.com/in/ACoAA")}
		got := buildRegisterAccount(owner)
		if got.Owner == nil || got.Owner.ProfileURL == nil || got.Owner.PublicID != nil {
			t.Errorf("Owner = %+v, want url-only block", got.Owner)
		}
	})
}
