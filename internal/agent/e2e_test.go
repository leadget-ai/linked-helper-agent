package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/leadget/lh-agent/internal/client"
	"github.com/leadget/lh-agent/internal/lh"
)

// TestSyncAccount_Golden locks the whole wire contract: the campaign-v205
// fixture flows through Agent.syncAccount and the decoded AccountReportRequest
// must match a committed golden. syncedAt (wall clock) is normalized before
// comparison; everything else is deterministic. Regenerate with
// UPDATE_GOLDEN=1 go test ./internal/agent -run TestSyncAccount_Golden.
func TestSyncAccount_Golden(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.Write([]byte(`{"reportInterval":600,"knownState":{"accounts":[454999],"campaigns":[]}}`))
	}))
	defer srv.Close()

	a := &Agent{
		cfg:            &Config{},
		client:         client.New(srv.URL, "tok", true),
		reader:         lh.NewReader(),
		known:          newKnown(),
		agentID:        "test-agent",
		reportInterval: defaultReportInterval,
	}
	defer a.reader.Close()

	// Pre-seed known state so the report exercises BOTH paths in one golden:
	// the campaign is known (so its replies ship) but at a stale version (so it
	// still re-registers). An empty ReplyCursor means backfill, so the fixture's
	// single reply is included.
	a.known.replace(client.KnownState{
		Accounts: []int{454999},
		Campaigns: []client.KnownCampaign{
			{AccountID: 454999, CampaignID: 500, Version: 1, HasMessages: false, ReplyCursor: ""},
		},
	})

	acc := lh.Account{ID: 454999, DBPath: filepath.Join("..", "lh", "testdata", "fixtures", "campaign-v205.db")}
	a.syncAccount(context.Background(), acc)

	if body == nil {
		t.Fatal("server never received a report request")
	}

	var req client.AccountReportRequest
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	// Normalize the only non-deterministic field.
	if _, err := time.Parse(time.RFC3339, req.SyncedAt); err != nil {
		t.Errorf("syncedAt %q is not RFC3339: %v", req.SyncedAt, err)
	}
	req.SyncedAt = "NORMALIZED"

	got, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got = append(got, '\n')

	goldenPath := filepath.Join("testdata", "golden", "report-campaign-v205.json")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote golden %s", goldenPath)
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run with UPDATE_GOLDEN=1 to create): %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("report mismatch.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
