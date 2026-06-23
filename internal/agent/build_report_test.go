package agent

import (
	"testing"

	"github.com/leadget/lh-agent/internal/client"
	"github.com/leadget/lh-agent/internal/lh"
)

func testAgent() *Agent {
	return &Agent{agentID: "test-agent", known: newKnown()}
}

func regularCampaign() lh.Campaign {
	return lh.Campaign{ID: 500, Name: "C", Type: 2, Version: 7, CreatedAt: "2026-01-02T00:00:00Z"}
}

// regularInputs returns the per-campaign maps for a single known/regular
// campaign so report cases only vary what they care about.
func regularInputs() (map[int64]lh.Funnel, map[int64]string, map[int64][]client.FunnelStep) {
	return map[int64]lh.Funnel{500: {Messaged: 1, Replied: 0, Target: 2}},
		map[int64]string{500: lh.KindRegular},
		map[int64][]client.FunnelStep{500: {{SeqNumber: 1, Sent: 1, Replied: 0}}}
}

// TestBuildReport_AccountMatrix is the 4-way owner-present × account-known
// table, including the nil-return for an unknown account with an empty
// partition.
func TestBuildReport_AccountMatrix(t *testing.T) {
	owner := &lh.AccountOwner{FullName: sp("Test Owner"), ExternalID: i64(100200300)}
	funnels, kinds, steps := regularInputs()
	campaigns := []lh.Campaign{regularCampaign()}

	t.Run("owner present + account unknown", func(t *testing.T) {
		a := testAgent()
		req := a.buildReport(7, campaigns, funnels, owner, nil, kinds, steps, lh.DailyLimits{})
		if req == nil || req.RegisterAccount == nil || req.RegisterAccount.ExternalID == nil {
			t.Fatalf("want full RegisterAccount from owner, got %+v", req)
		}
	})

	t.Run("owner present + account known", func(t *testing.T) {
		a := testAgent()
		a.known.replace(client.KnownState{Accounts: []int{7}})
		req := a.buildReport(7, campaigns, funnels, owner, nil, kinds, steps, lh.DailyLimits{})
		if req == nil || req.RegisterAccount == nil || req.RegisterAccount.FullName == nil {
			t.Fatalf("owner present must always register account, got %+v", req)
		}
	})

	t.Run("owner absent + account unknown + empty partition → nil", func(t *testing.T) {
		a := testAgent()
		req := a.buildReport(7, nil, map[int64]lh.Funnel{}, nil, nil, nil, nil, lh.DailyLimits{})
		if req != nil {
			t.Fatalf("want nil for unknown account with no campaigns, got %+v", req)
		}
	})

	t.Run("owner absent + account unknown + campaigns present → placeholder", func(t *testing.T) {
		a := testAgent()
		req := a.buildReport(7, campaigns, funnels, nil, nil, kinds, steps, lh.DailyLimits{})
		if req == nil || req.RegisterAccount == nil {
			t.Fatalf("want empty placeholder RegisterAccount, got %+v", req)
		}
		if req.RegisterAccount.ExternalID != nil || req.RegisterAccount.FullName != nil {
			t.Errorf("placeholder must be empty, got %+v", req.RegisterAccount)
		}
	})

	t.Run("owner absent + account known → no RegisterAccount, funnels still sent", func(t *testing.T) {
		a := testAgent()
		a.known.replace(client.KnownState{Accounts: []int{7}})
		req := a.buildReport(7, campaigns, funnels, nil, nil, kinds, steps, lh.DailyLimits{})
		if req == nil {
			t.Fatal("want non-nil report")
		}
		if req.RegisterAccount != nil {
			t.Errorf("RegisterAccount = %+v, want nil", req.RegisterAccount)
		}
		if len(req.Funnels) != 1 {
			t.Errorf("len(Funnels) = %d, want 1", len(req.Funnels))
		}
	})
}

// TestBuildReport_RegisterVsFunnel covers when a campaign's full metadata is
// (re)sent versus only its funnel: unknown → register; known same-version with
// messages → funnel only; scraper → dropped entirely.
func TestBuildReport_RegisterVsFunnel(t *testing.T) {
	owner := &lh.AccountOwner{FullName: sp("Test Owner")}
	funnels, kinds, steps := regularInputs()
	campaigns := []lh.Campaign{regularCampaign()}

	t.Run("unknown campaign registers and funnels", func(t *testing.T) {
		a := testAgent()
		req := a.buildReport(7, campaigns, funnels, owner, nil, kinds, steps, lh.DailyLimits{})
		if len(req.RegisterCampaigns) != 1 || len(req.Funnels) != 1 {
			t.Fatalf("register=%d funnel=%d, want 1/1", len(req.RegisterCampaigns), len(req.Funnels))
		}
	})

	t.Run("known same-version with messages → funnel only", func(t *testing.T) {
		a := testAgent()
		a.known.replace(client.KnownState{
			Accounts:  []int{7},
			Campaigns: []client.KnownCampaign{{AccountID: 7, CampaignID: 500, Version: 7, HasMessages: true}},
		})
		req := a.buildReport(7, campaigns, funnels, owner, nil, kinds, steps, lh.DailyLimits{})
		if len(req.RegisterCampaigns) != 0 {
			t.Errorf("RegisterCampaigns = %d, want 0", len(req.RegisterCampaigns))
		}
		if len(req.Funnels) != 1 {
			t.Errorf("Funnels = %d, want 1", len(req.Funnels))
		}
	})

	t.Run("scraper campaign dropped entirely", func(t *testing.T) {
		a := testAgent()
		scraperKinds := map[int64]string{500: lh.KindScraper}
		req := a.buildReport(7, campaigns, funnels, owner, nil, scraperKinds, steps, lh.DailyLimits{})
		if len(req.RegisterCampaigns) != 0 || len(req.Funnels) != 0 {
			t.Errorf("scraper produced register=%d funnel=%d, want 0/0", len(req.RegisterCampaigns), len(req.Funnels))
		}
	})
}

// TestBuildReport_Backfill: a known same-version campaign the platform holds no
// messages for re-registers ONLY when the agent has message-bearing actions to
// contribute — otherwise it would re-register every cycle forever.
func TestBuildReport_Backfill(t *testing.T) {
	owner := &lh.AccountOwner{FullName: sp("Test Owner")}
	funnels, kinds, steps := regularInputs()
	campaigns := []lh.Campaign{regularCampaign()}

	seedKnownNoMessages := func(a *Agent) {
		a.known.replace(client.KnownState{
			Accounts:  []int{7},
			Campaigns: []client.KnownCampaign{{AccountID: 7, CampaignID: 500, Version: 7, HasMessages: false}},
		})
	}

	t.Run("with message actions → backfill registers", func(t *testing.T) {
		a := testAgent()
		seedKnownNoMessages(a)
		actions := map[int64][]lh.CampaignActionRow{500: {msgRow("InvitePerson", "hi")}}
		req := a.buildReport(7, campaigns, funnels, owner, actions, kinds, steps, lh.DailyLimits{})
		if len(req.RegisterCampaigns) != 1 {
			t.Errorf("RegisterCampaigns = %d, want 1 (backfill)", len(req.RegisterCampaigns))
		}
	})

	t.Run("without message actions → funnel only", func(t *testing.T) {
		a := testAgent()
		seedKnownNoMessages(a)
		actions := map[int64][]lh.CampaignActionRow{500: {plainRow("InvitePerson")}}
		req := a.buildReport(7, campaigns, funnels, owner, actions, kinds, steps, lh.DailyLimits{})
		if len(req.RegisterCampaigns) != 0 {
			t.Errorf("RegisterCampaigns = %d, want 0 (no messages to contribute)", len(req.RegisterCampaigns))
		}
	})
}
