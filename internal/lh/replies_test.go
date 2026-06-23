package lh

import (
	"context"
	"testing"
)

func TestReadCampaignReplies_All(t *testing.T) {
	r := NewReader()
	defer r.Close()

	got, err := r.ReadCampaignReplies(context.Background(), fixture("campaign-v205"), 500, "", 500)
	if err != nil {
		t.Fatalf("ReadCampaignReplies: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(replies) = %d, want 1", len(got))
	}

	reply := got[0]
	if reply.CampaignID != 500 {
		t.Errorf("CampaignID = %d, want 500", reply.CampaignID)
	}
	if reply.PersonID != 3 {
		t.Errorf("PersonID = %d, want 3", reply.PersonID)
	}
	if reply.ExternalID != "2-reply-ext-id-0001" {
		t.Errorf("ExternalID = %q", reply.ExternalID)
	}
	if reply.Text != "Thanks for reaching out — happy to chat next week." {
		t.Errorf("Text = %q", reply.Text)
	}
	if reply.MemberID == nil || *reply.MemberID != "555666777" {
		t.Errorf("MemberID = %v, want 555666777", reply.MemberID)
	}
	if reply.ProfileURL == nil || *reply.ProfileURL != "https://www.linkedin.com/in/jane-prospect" {
		t.Errorf("ProfileURL = %v", reply.ProfileURL)
	}
	if reply.FullName == nil || *reply.FullName != "Jane Prospect" {
		t.Errorf("FullName = %v", reply.FullName)
	}
	if reply.Headline == nil || *reply.Headline != "Head of Engineering at Acme" {
		t.Errorf("Headline = %v", reply.Headline)
	}
}

// TestReadCampaignReplies_CursorRoundTrip locks the `>=` dedup guarantee end to
// end: feeding a reply's own DetectedAt back as the cursor must re-return that
// reply, never drop it. This catches any datetime-format drift between the value
// the scan emits (and the platform echoes back) and the value the WHERE compares
// against in SQLite.
func TestReadCampaignReplies_CursorRoundTrip(t *testing.T) {
	r := NewReader()
	defer r.Close()
	ctx := context.Background()

	first, err := r.ReadCampaignReplies(ctx, fixture("campaign-v205"), 500, "", 500)
	if err != nil {
		t.Fatalf("initial read: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("len(first) = %d, want 1", len(first))
	}
	cursor := first[0].DetectedAt

	again, err := r.ReadCampaignReplies(ctx, fixture("campaign-v205"), 500, cursor, 500)
	if err != nil {
		t.Fatalf("cursor read: %v", err)
	}
	if len(again) != 1 {
		t.Fatalf("re-reading at cursor %q returned %d replies, want 1 (>= boundary must re-include)", cursor, len(again))
	}
	if again[0].ExternalID != first[0].ExternalID {
		t.Errorf("ExternalID drift: %q vs %q", again[0].ExternalID, first[0].ExternalID)
	}
}

func TestReadCampaignReplies_CursorPastEnd(t *testing.T) {
	r := NewReader()
	defer r.Close()

	got, err := r.ReadCampaignReplies(context.Background(), fixture("campaign-v205"), 500, "2099-01-01T00:00:00Z", 500)
	if err != nil {
		t.Fatalf("ReadCampaignReplies: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("len(replies) = %d, want 0 past the last detection", len(got))
	}
}

func TestReadCampaignReplies_OtherCampaignExcluded(t *testing.T) {
	r := NewReader()
	defer r.Close()

	got, err := r.ReadCampaignReplies(context.Background(), fixture("campaign-v205"), 999, "", 500)
	if err != nil {
		t.Fatalf("ReadCampaignReplies: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("len(replies) = %d, want 0 for a campaign with no replies", len(got))
	}
}
