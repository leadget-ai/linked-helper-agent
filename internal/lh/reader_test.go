package lh

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestReadCampaigns(t *testing.T) {
	r := NewReader()
	defer r.Close()
	got, err := r.ReadCampaigns(context.Background(), fixture("campaign-v205"), "")
	if err != nil {
		t.Fatalf("ReadCampaigns: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(campaigns) = %d, want 1", len(got))
	}
	c := got[0]
	if c.ID != 500 {
		t.Errorf("ID = %d, want 500", c.ID)
	}
	if c.Name != "Test Campaign" {
		t.Errorf("Name = %q, want %q", c.Name, "Test Campaign")
	}
	if c.Version != 7 {
		t.Errorf("Version = %d, want 7 (from campaign_last_versions view)", c.Version)
	}
	if c.IsPaused || c.IsArchived {
		t.Errorf("IsPaused/IsArchived = %v/%v, want false/false", c.IsPaused, c.IsArchived)
	}
	if c.IsValid == nil || !*c.IsValid {
		t.Errorf("IsValid = %v, want true", c.IsValid)
	}
	// modernc normalizes the DATETIME column on read (drops zero millis).
	if c.CreatedAt != "2026-01-02T00:00:00Z" {
		t.Errorf("CreatedAt = %q", c.CreatedAt)
	}
}

func TestReadCampaigns_SinceFilter(t *testing.T) {
	r := NewReader()
	defer r.Close()
	// Campaign created_at is strictly > since, so an equal cursor excludes it.
	got, err := r.ReadCampaigns(context.Background(), fixture("campaign-v205"), "2026-01-02T00:00:00.000Z")
	if err != nil {
		t.Fatalf("ReadCampaigns: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("len(campaigns) = %d, want 0 for since==created_at", len(got))
	}
}

func TestReadCampaignActions(t *testing.T) {
	r := NewReader()
	defer r.Close()
	got, err := r.ReadCampaignActions(context.Background(), fixture("campaign-v205"), 500)
	if err != nil {
		t.Fatalf("ReadCampaignActions: %v", err)
	}
	wantTypes := []string{"InvitePerson", "CheckForReplies", "MessageToPerson", "VisitProfile"}
	if len(got) != len(wantTypes) {
		t.Fatalf("len(actions) = %d, want %d", len(got), len(wantTypes))
	}
	for i, want := range wantTypes {
		if got[i].Type != want {
			t.Errorf("action[%d].Type = %q, want %q (workflow order by cva.id)", i, got[i].Type, want)
		}
	}

	invite := got[0]
	if strv(invite.Body) != "{firstName}, let's connect!" {
		t.Errorf("Invite Body = %q", strv(invite.Body))
	}
	if strv(invite.Example) != "John, let's connect!" {
		t.Errorf("Invite Example = %q", strv(invite.Example))
	}

	check := got[1]
	if check.Body != nil {
		t.Errorf("CheckForReplies Body = %q, want nil (non-messaging)", strv(check.Body))
	}
	if check.WaitMs == nil || *check.WaitMs != 345600000 {
		t.Errorf("CheckForReplies WaitMs = %v, want 345600000", check.WaitMs)
	}

	msg := got[2]
	if strv(msg.Body) != "Thanks for connecting, {firstName}!" {
		t.Errorf("Message Body = %q", strv(msg.Body))
	}

	visit := got[3]
	if visit.Body != nil || visit.WaitMs != nil {
		t.Errorf("VisitProfile Body/WaitMs = %q/%v, want nil/nil", strv(visit.Body), visit.WaitMs)
	}
}

func TestReadCampaignStepStats(t *testing.T) {
	r := NewReader()
	defer r.Close()
	got, err := r.ReadCampaignStepStats(context.Background(), fixture("campaign-v205"), 500)
	if err != nil {
		t.Fatalf("ReadCampaignStepStats: %v", err)
	}
	want := []StepStat{
		{ActionID: 101, Type: "InvitePerson", Sent: 3, Replied: 0},    // p2,p3,p4 sent; status -999 (p1) excluded
		{ActionID: 102, Type: "CheckForReplies", Sent: 2, Replied: 2}, // p3 via status=2, p4 via reply flag
		{ActionID: 103, Type: "MessageToPerson", Sent: 1, Replied: 0}, // p2
		{ActionID: 104, Type: "VisitProfile", Sent: 0, Replied: 0},    // no history rows
	}
	if len(got) != len(want) {
		t.Fatalf("len(stats) = %d, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("stat[%d] = %+v, want %+v", i, got[i], w)
		}
	}
}

func TestReadFunnels(t *testing.T) {
	r := NewReader()
	defer r.Close()
	got, err := r.ReadFunnels(context.Background(), fixture("campaign-v205"))
	if err != nil {
		t.Fatalf("ReadFunnels: %v", err)
	}
	f, ok := got[500]
	if !ok {
		t.Fatalf("no funnel for campaign 500")
	}
	if f.Target != 4 {
		t.Errorf("Target = %d, want 4 (p1..p4 distinct)", f.Target)
	}
	if f.Messaged != 3 {
		t.Errorf("Messaged = %d, want 3 (p1 is queued-only)", f.Messaged)
	}
	if f.Replied != 2 {
		t.Errorf("Replied = %d, want 2 (status OR flag)", f.Replied)
	}
	if f.LastActivityAt == nil || *f.LastActivityAt != "2026-01-10T12:00:00.000Z" {
		t.Errorf("LastActivityAt = %v, want 2026-01-10T12:00:00.000Z", f.LastActivityAt)
	}
}

func TestReadDailyLimits(t *testing.T) {
	r := NewReader()
	defer r.Close()
	got, err := r.ReadDailyLimits(context.Background(), fixture("campaign-v205"))
	if err != nil {
		t.Fatalf("ReadDailyLimits: %v", err)
	}
	if got.General != 90 {
		t.Errorf("General = %d, want 90", got.General)
	}
	if got.Invite != 25 {
		t.Errorf("Invite = %d, want 25", got.Invite)
	}
}

// TestClassify_Scraper proves a steps-only-extract/visit/webhook campaign
// classifies as scraper (so the agent drops it).
func TestClassify_Scraper(t *testing.T) {
	r := NewReader()
	defer r.Close()
	stats, err := r.ReadCampaignStepStats(context.Background(), fixture("campaign-scraper-v205"), 600)
	if err != nil {
		t.Fatalf("ReadCampaignStepStats: %v", err)
	}
	types := make([]string, len(stats))
	for i, s := range stats {
		types[i] = s.Type
	}
	if kind := ClassifyLinkedinKind(types); kind != KindScraper {
		t.Errorf("ClassifyLinkedinKind(%v) = %q, want %q", types, kind, KindScraper)
	}
}

// TestReader_OpenErrors confirms a missing or non-SQLite file surfaces an error
// from the read methods rather than panicking or returning empty success.
func TestReader_OpenErrors(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		r := NewReader()
		defer r.Close()
		if _, err := r.ReadCampaigns(context.Background(), filepath.Join(t.TempDir(), "nope.db"), ""); err == nil {
			t.Error("ReadCampaigns on missing db: err = nil, want error")
		}
	})

	t.Run("garbage file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "garbage.db")
		if err := os.WriteFile(path, []byte("this is not sqlite"), 0o644); err != nil {
			t.Fatal(err)
		}
		r := NewReader()
		defer r.Close()
		if _, err := r.ReadFunnels(context.Background(), path); err == nil {
			t.Error("ReadFunnels on garbage db: err = nil, want error")
		}
	})
}

func TestScan(t *testing.T) {
	dir := t.TempDir()
	// Valid partitions (the dbFile only needs to exist; Scan never opens it).
	for _, name := range []string{"linked-helper-account-1-main", "linked-helper-account-42-main"} {
		mkPartition(t, dir, name, true)
	}
	// Garbage / incomplete folders that must be skipped.
	mkPartition(t, dir, "linked-helper-account-7-main", false) // matches but no lh.db
	mkPartition(t, dir, "linked-helper-account-foo-main", true)
	mkPartition(t, dir, "random-folder", true)

	got, err := Scan(dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	ids := map[int]bool{}
	for _, a := range got {
		ids[a.ID] = true
	}
	if len(got) != 2 || !ids[1] || !ids[42] {
		t.Fatalf("Scan returned %+v, want exactly accounts 1 and 42", got)
	}
}

func mkPartition(t *testing.T, dir, name string, withDB bool) {
	t.Helper()
	full := filepath.Join(dir, name)
	if err := os.MkdirAll(full, 0o755); err != nil {
		t.Fatal(err)
	}
	if withDB {
		if err := os.WriteFile(filepath.Join(full, defaultSQLiteName), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReleaseHandlesLeavesReaderUsable(t *testing.T) {
	r := NewReader()
	defer r.Close()

	if _, err := r.ReadCampaigns(context.Background(), fixture("campaign-v205"), ""); err != nil {
		t.Fatalf("first read: %v", err)
	}
	if len(r.handles) != 1 {
		t.Fatalf("handles after read = %d, want 1", len(r.handles))
	}

	if err := r.ReleaseHandles(); err != nil {
		t.Fatalf("ReleaseHandles: %v", err)
	}
	if len(r.handles) != 0 {
		t.Fatalf("handles after release = %d, want 0 — the file stays ours between cycles", len(r.handles))
	}

	got, err := r.ReadCampaigns(context.Background(), fixture("campaign-v205"), "")
	if err != nil {
		t.Fatalf("read after release: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(campaigns) after release = %d, want 1", len(got))
	}
}
