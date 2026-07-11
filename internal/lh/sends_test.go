package lh

import (
	"context"
	"testing"
)

// TestReadCampaignSends_All reads every processed per-person send for the
// fixture campaign. person_in_campaigns_history holds 7 rows for campaign 500;
// one is queued (result_status -999, excluded), leaving 6 detected rows across
// all action types (messaging AND CheckForReplies) — the messaging-only filter
// lives in the agent's buildSends, not the reader.
func TestReadCampaignSends_All(t *testing.T) {
	r := NewReader()
	defer r.Close()

	got, err := r.ReadCampaignSends(context.Background(), fixture("campaign-v205"), 500, "", 1000)
	if err != nil {
		t.Fatalf("ReadCampaignSends: %v", err)
	}
	if len(got) != 6 {
		t.Fatalf("len(sends) = %d, want 6 (7 rows minus the queued -999 row)", len(got))
	}

	// Ordered by detection time; the first is person 2's InvitePerson (action 101).
	first := got[0]
	if first.CampaignID != 500 || first.PersonID != 2 || first.ActionID != 101 {
		t.Errorf("first = campaign %d person %d action %d, want 500/2/101", first.CampaignID, first.PersonID, first.ActionID)
	}
	if first.ExternalID != "2" {
		t.Errorf("ExternalID = %q, want raw pich id \"2\" (namespacing happens in the agent)", first.ExternalID)
	}
	if first.SentAt != first.DetectedAt {
		t.Errorf("SentAt %q != DetectedAt %q (LH has a single timestamp)", first.SentAt, first.DetectedAt)
	}

	// Person 3's row carries the resolved LinkedIn identity from the fixture.
	var withIdentity *CampaignSend
	for i := range got {
		if got[i].PersonID == 3 {
			withIdentity = &got[i]
			break
		}
	}
	if withIdentity == nil {
		t.Fatal("no send row for person 3")
	}
	if withIdentity.MemberID == nil || *withIdentity.MemberID != "555666777" {
		t.Errorf("MemberID = %v, want 555666777", withIdentity.MemberID)
	}
	if withIdentity.ProfileURL == nil || *withIdentity.ProfileURL != "https://www.linkedin.com/in/jane-prospect" {
		t.Errorf("ProfileURL = %v", withIdentity.ProfileURL)
	}
}

// TestReadCampaignSends_CursorRoundTrip locks the `>=` dedup guarantee: feeding
// a send's own DetectedAt back as the cursor must re-return that send, so a
// boundary row is never silently dropped (platform dedups on ExternalID).
func TestReadCampaignSends_CursorRoundTrip(t *testing.T) {
	r := NewReader()
	defer r.Close()
	ctx := context.Background()

	all, err := r.ReadCampaignSends(ctx, fixture("campaign-v205"), 500, "", 1000)
	if err != nil {
		t.Fatalf("initial read: %v", err)
	}
	if len(all) == 0 {
		t.Fatal("expected sends in the fixture")
	}
	last := all[len(all)-1]

	again, err := r.ReadCampaignSends(ctx, fixture("campaign-v205"), 500, last.DetectedAt, 1000)
	if err != nil {
		t.Fatalf("cursor read: %v", err)
	}
	if len(again) == 0 || again[len(again)-1].ExternalID != last.ExternalID {
		t.Fatalf("re-reading at cursor %q dropped the boundary send", last.DetectedAt)
	}
}

func TestReadCampaignSends_CursorPastEnd(t *testing.T) {
	r := NewReader()
	defer r.Close()

	got, err := r.ReadCampaignSends(context.Background(), fixture("campaign-v205"), 500, "2099-01-01T00:00:00Z", 1000)
	if err != nil {
		t.Fatalf("ReadCampaignSends: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("len(sends) = %d, want 0 past the last detection", len(got))
	}
}
