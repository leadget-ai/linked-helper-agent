package lh

import (
	"context"
	"testing"
)

// TestReadCampaignThread_Bidirectional walks person 3's full conversation: an
// outbound Invite followed by the inbound reply, ordered ascending by message
// time. Direction is derived from the action_result_messages.type, and the
// bodies are the real personalized message text.
func TestReadCampaignThread_Bidirectional(t *testing.T) {
	r := NewReader()
	defer r.Close()

	got, err := r.ReadCampaignThread(context.Background(), fixture("campaign-v205"), 500, 3)
	if err != nil {
		t.Fatalf("ReadCampaignThread: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(thread) = %d, want 2 (Invite send + reply)", len(got))
	}

	send := got[0]
	if send.Direction != DirectionOutbound {
		t.Errorf("first Direction = %q, want %q", send.Direction, DirectionOutbound)
	}
	if send.ActionID != 101 {
		t.Errorf("first ActionID = %d, want 101 (InvitePerson)", send.ActionID)
	}
	if send.Body == nil || *send.Body != "Hi Jane, let's connect!" {
		t.Errorf("first Body = %v", send.Body)
	}
	if send.OccurredAt != "2026-01-05T12:00:00.000Z" {
		t.Errorf("first OccurredAt = %q", send.OccurredAt)
	}

	reply := got[1]
	if reply.Direction != DirectionInbound {
		t.Errorf("second Direction = %q, want %q", reply.Direction, DirectionInbound)
	}
	if reply.Body == nil || *reply.Body != "Thanks for reaching out — happy to chat next week." {
		t.Errorf("second Body = %v", reply.Body)
	}
	if reply.OccurredAt != "2026-01-07T18:30:00.000Z" {
		t.Errorf("second OccurredAt = %q", reply.OccurredAt)
	}
	if !(got[0].OccurredAt < got[1].OccurredAt) {
		t.Errorf("thread not ascending by occurredAt: %q then %q", got[0].OccurredAt, got[1].OccurredAt)
	}
}

// TestReadCampaignThread_MultipleOutbound covers a person we only messaged
// (person 2): two outbound steps in workflow order, each on its own messaging
// action so the caller resolves distinct seq numbers.
func TestReadCampaignThread_MultipleOutbound(t *testing.T) {
	r := NewReader()
	defer r.Close()

	got, err := r.ReadCampaignThread(context.Background(), fixture("campaign-v205"), 500, 2)
	if err != nil {
		t.Fatalf("ReadCampaignThread: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(thread) = %d, want 2 (Invite + Message sends)", len(got))
	}
	for i, m := range got {
		if m.Direction != DirectionOutbound {
			t.Errorf("message %d Direction = %q, want outbound", i, m.Direction)
		}
	}
	if got[0].ActionID != 101 || got[1].ActionID != 103 {
		t.Errorf("ActionIDs = %d,%d, want 101 (Invite) then 103 (Message)", got[0].ActionID, got[1].ActionID)
	}
}

func TestReadCampaignThread_UnknownPerson(t *testing.T) {
	r := NewReader()
	defer r.Close()

	got, err := r.ReadCampaignThread(context.Background(), fixture("campaign-v205"), 500, 9999)
	if err != nil {
		t.Fatalf("ReadCampaignThread: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("len(thread) = %d, want 0 for a person with no messages", len(got))
	}
}
