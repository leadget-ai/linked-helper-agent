package lh

import (
	"context"
	"testing"
)

// TestReadCampaignThread_Bidirectional walks person 3's full conversation: an
// outbound Invite followed by the inbound reply, then the two messages that only
// exist in LH's chat mirror, ordered ascending by message time. Direction on the
// linked pair is derived from the action_result_messages.type, and the bodies are
// the real personalized message text.
func TestReadCampaignThread_Bidirectional(t *testing.T) {
	r := NewReader()
	defer r.Close()

	got, err := r.ReadCampaignThread(context.Background(), fixture("campaign-v205"), 500, 3)
	if err != nil {
		t.Fatalf("ReadCampaignThread: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("len(thread) = %d, want 5 (pre-outreach history + Invite send + reply + two manual messages)", len(got))
	}

	send := got[1]
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

	reply := got[2]
	if reply.Direction != DirectionInbound {
		t.Errorf("second Direction = %q, want %q", reply.Direction, DirectionInbound)
	}
	if reply.Body == nil || *reply.Body != "Thanks for reaching out — happy to chat next week." {
		t.Errorf("second Body = %v", reply.Body)
	}
	// The LinkedIn send time (09:55), not LH's detection time (18:30) — the
	// fixture keeps them apart precisely to lock which one reaches the wire.
	if reply.OccurredAt != "2026-01-07T09:55:00.000Z" {
		t.Errorf("second OccurredAt = %q, want the send time, not LH's detection time", reply.OccurredAt)
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].OccurredAt > got[i].OccurredAt {
			t.Errorf("thread not ascending by occurredAt: %q then %q", got[i-1].OccurredAt, got[i].OccurredAt)
		}
	}
}

// TestReadCampaignThread_ManualMessages covers the half of the conversation that
// never touched LH's workflow: our manual answer and the person's manual
// follow-up. Both come from the chat mirror, so neither carries an action id —
// they belong to no campaign step and the caller resolves no seq number for
// them. Direction still splits correctly, which is the whole point: a manual
// answer we typed in LinkedIn is ours, not the lead's.
func TestReadCampaignThread_ManualMessages(t *testing.T) {
	r := NewReader()
	defer r.Close()

	got, err := r.ReadCampaignThread(context.Background(), fixture("campaign-v205"), 500, 3)
	if err != nil {
		t.Fatalf("ReadCampaignThread: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("len(thread) = %d, want 5", len(got))
	}

	ours := got[3]
	if ours.Direction != DirectionOutbound {
		t.Errorf("manual answer Direction = %q, want %q", ours.Direction, DirectionOutbound)
	}
	if ours.ActionID != 0 {
		t.Errorf("manual answer ActionID = %d, want 0 (outside the workflow)", ours.ActionID)
	}
	if ours.Body == nil || *ours.Body != "Great — booked us Thursday 3pm, talk then!" {
		t.Errorf("manual answer Body = %v", ours.Body)
	}

	theirs := got[4]
	if theirs.Direction != DirectionInbound {
		t.Errorf("manual follow-up Direction = %q, want %q", theirs.Direction, DirectionInbound)
	}
	if theirs.ActionID != 0 {
		t.Errorf("manual follow-up ActionID = %d, want 0", theirs.ActionID)
	}
	if theirs.Body == nil || *theirs.Body != "Perfect, see you Thursday." {
		t.Errorf("manual follow-up Body = %v", theirs.Body)
	}
}

// TestReadCampaignThread_SendTimeNotDetectionTime pins the timestamp a thread
// message carries. LH imports an existing chat in one pass, so created_at is the
// insert moment: a conversation spanning weeks collapses into milliseconds, and
// insert order need not match send order at all. The fixture's two manual
// messages are detected in the REVERSE of the order they were sent, so reading
// created_at would both mis-time them and swap them.
func TestReadCampaignThread_SendTimeNotDetectionTime(t *testing.T) {
	r := NewReader()
	defer r.Close()

	got, err := r.ReadCampaignThread(context.Background(), fixture("campaign-v205"), 500, 3)
	if err != nil {
		t.Fatalf("ReadCampaignThread: %v", err)
	}

	want := []struct {
		messageID  int64
		occurredAt string
	}{
		{12, "2023-05-04T11:00:00.000Z"},
		{4, "2026-01-05T12:00:00.000Z"},
		{1, "2026-01-07T09:55:00.000Z"},
		{10, "2026-01-07T19:02:00.000Z"},
		{11, "2026-01-08T08:10:00.000Z"},
	}
	if len(got) != len(want) {
		t.Fatalf("len(thread) = %d, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].MessageID != w.messageID || got[i].OccurredAt != w.occurredAt {
			t.Errorf("message %d = id %d at %q, want id %d at %q",
				i, got[i].MessageID, got[i].OccurredAt, w.messageID, w.occurredAt)
		}
	}
}

// TestReadCampaignThread_NoDuplicates locks the merge: the messages both sources
// hold (the Invite send and the linked reply, mirrored into the chat store)
// appear once, keeping their action attribution.
func TestReadCampaignThread_NoDuplicates(t *testing.T) {
	r := NewReader()
	defer r.Close()

	got, err := r.ReadCampaignThread(context.Background(), fixture("campaign-v205"), 500, 3)
	if err != nil {
		t.Fatalf("ReadCampaignThread: %v", err)
	}

	seen := map[int64]int{}
	for _, msg := range got {
		seen[msg.MessageID]++
	}
	for id, count := range seen {
		if count != 1 {
			t.Errorf("message %d appears %d times, want 1", id, count)
		}
	}
	if got[1].ActionID != 101 {
		t.Errorf("mirrored Invite lost its action attribution: ActionID = %d, want 101", got[1].ActionID)
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
