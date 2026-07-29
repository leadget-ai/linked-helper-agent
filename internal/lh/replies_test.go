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
	if len(got) != 2 {
		t.Fatalf("len(replies) = %d, want 2 (LH-linked reply + manual follow-up)", len(got))
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

// TestReadCampaignReplies_Manual covers the replies LH never linked to an action
// — the ones a person typed in LinkedIn after the exchange left the workflow.
// They carry the same identity and the same stable LinkedIn message id as linked
// replies, and they arrive in detection order behind them. Our own manual
// answers stay out: they are outbound, and only reach the platform as thread
// messages.
func TestReadCampaignReplies_Manual(t *testing.T) {
	r := NewReader()
	defer r.Close()

	got, err := r.ReadCampaignReplies(context.Background(), fixture("campaign-v205"), 500, "", 500)
	if err != nil {
		t.Fatalf("ReadCampaignReplies: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(replies) = %d, want 2", len(got))
	}

	manual := got[1]
	if manual.ExternalID != "2-manual-theirs-0011" {
		t.Errorf("ExternalID = %q, want the manual reply's LinkedIn message id", manual.ExternalID)
	}
	if manual.CampaignID != 500 || manual.PersonID != 3 {
		t.Errorf("attribution = campaign %d person %d, want 500/3", manual.CampaignID, manual.PersonID)
	}
	if manual.Text != "Perfect, see you Thursday." {
		t.Errorf("Text = %q", manual.Text)
	}
	if manual.MemberID == nil || *manual.MemberID != "555666777" {
		t.Errorf("MemberID = %v, want the replier's member id", manual.MemberID)
	}
	if manual.SentAt != "2026-01-08T08:10:00.000Z" || manual.DetectedAt != "2026-02-01T09:00:00.000Z" {
		t.Errorf("SentAt/DetectedAt = %q/%q", manual.SentAt, manual.DetectedAt)
	}
	if !(got[0].DetectedAt < manual.DetectedAt) {
		t.Errorf("replies not in detection order: %q then %q", got[0].DetectedAt, manual.DetectedAt)
	}
	for _, reply := range got {
		if reply.ExternalID == "2-manual-ours-0010" {
			t.Fatal("our own manual message was reported as an inbound reply")
		}
	}
}

// TestReadCampaignReplies_ManualCursor locks the cursor over the merged batch:
// reading from the linked reply's detection time returns it plus everything
// newer, and reading past the manual reply returns nothing.
func TestReadCampaignReplies_ManualCursor(t *testing.T) {
	r := NewReader()
	defer r.Close()
	ctx := context.Background()

	got, err := r.ReadCampaignReplies(ctx, fixture("campaign-v205"), 500, "2026-01-07T18:30:00.000Z", 500)
	if err != nil {
		t.Fatalf("ReadCampaignReplies: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(replies) = %d at the linked reply's cursor, want 2 (boundary re-read + manual)", len(got))
	}

	got, err = r.ReadCampaignReplies(ctx, fixture("campaign-v205"), 500, "2026-02-01T09:00:00.001Z", 500)
	if err != nil {
		t.Fatalf("ReadCampaignReplies: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("len(replies) = %d past the manual reply, want 0", len(got))
	}
}

// TestReadCampaignReplies_LimitSpansSources caps the merged batch: the oldest
// reply ships now and the rest waits for the next cycle, so the limit is honored
// across both sources rather than per source.
func TestReadCampaignReplies_LimitSpansSources(t *testing.T) {
	r := NewReader()
	defer r.Close()

	got, err := r.ReadCampaignReplies(context.Background(), fixture("campaign-v205"), 500, "", 1)
	if err != nil {
		t.Fatalf("ReadCampaignReplies: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(replies) = %d with limit 1, want 1", len(got))
	}
	if got[0].ExternalID != "2-reply-ext-id-0001" {
		t.Errorf("ExternalID = %q, want the oldest reply", got[0].ExternalID)
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
	if len(first) != 2 {
		t.Fatalf("len(first) = %d, want 2", len(first))
	}
	cursor := first[len(first)-1].DetectedAt

	again, err := r.ReadCampaignReplies(ctx, fixture("campaign-v205"), 500, cursor, 500)
	if err != nil {
		t.Fatalf("cursor read: %v", err)
	}
	if len(again) != 1 {
		t.Fatalf("re-reading at cursor %q returned %d replies, want 1 (>= boundary must re-include)", cursor, len(again))
	}
	if again[0].ExternalID != first[len(first)-1].ExternalID {
		t.Errorf("ExternalID drift: %q vs %q", again[0].ExternalID, first[len(first)-1].ExternalID)
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
