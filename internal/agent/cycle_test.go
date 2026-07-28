package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leadget/lh-agent/internal/client"
	"github.com/leadget/lh-agent/internal/lh"
)

// TestCycle_BootstrapRefreshesKnownState locks the cursor-reset recovery path:
// the agent's cached known-state says the campaign's replies are fully shipped
// (cursor far in the future), but the server has since reset the cursor. The
// cycle must pick up the reset via its leading bootstrap and backfill the
// reply — an agent that trusts its cache would ship nothing and its next
// report would re-advance the cursor, silently swallowing the reset.
func TestCycle_BootstrapRefreshesKnownState(t *testing.T) {
	partitionsDir := t.TempDir()
	accountDir := filepath.Join(partitionsDir, "linked-helper-account-454999-main")
	if err := os.MkdirAll(accountDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile(filepath.Join("..", "lh", "testdata", "fixtures", "campaign-v205.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(accountDir, "lh.db"), fixture, 0o644); err != nil {
		t.Fatal(err)
	}

	resetState := client.KnownState{
		Accounts: []int{454999},
		Campaigns: []client.KnownCampaign{
			{AccountID: 454999, CampaignID: 500, Version: 2, HasMessages: true, ReplyCursor: ""},
		},
	}

	var reportBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/bootstrap") {
			json.NewEncoder(w).Encode(client.BootstrapResponse{
				ReportInterval: 600,
				KnownState:     resetState,
			})
			return
		}
		reportBody, _ = io.ReadAll(r.Body)
		json.NewEncoder(w).Encode(client.AccountReportResponse{
			ReportInterval: 600,
			KnownState:     resetState,
		})
	}))
	defer srv.Close()

	a := &Agent{
		cfg:            &Config{PartitionsDir: partitionsDir},
		client:         client.New(srv.URL, "tok", true),
		reader:         lh.NewReader(),
		known:          newKnown(),
		agentID:        "test-agent",
		reportInterval: defaultReportInterval,
	}
	defer a.reader.Close()

	// Stale cache: cursor past every reply, as left by the previous cycle's
	// report response — without the bootstrap refresh nothing would ship.
	a.known.replace(client.KnownState{
		Accounts: []int{454999},
		Campaigns: []client.KnownCampaign{
			{AccountID: 454999, CampaignID: 500, Version: 2, HasMessages: true, ReplyCursor: "9999-01-01T00:00:00.000Z"},
		},
	})

	a.cycle(context.Background())

	if reportBody == nil {
		t.Fatal("server never received a report request")
	}
	var req client.AccountReportRequest
	if err := json.Unmarshal(reportBody, &req); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if len(req.Replies) != 2 {
		t.Fatalf("expected the reset cursor to backfill 2 replies, got %d", len(req.Replies))
	}
}
