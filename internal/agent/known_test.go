package agent

import (
	"testing"

	"github.com/leadget/lh-agent/internal/client"
)

func TestKnown_ReplaceAndLookup(t *testing.T) {
	k := newKnown()
	k.replace(client.KnownState{
		Accounts: []int{1, 7},
		Campaigns: []client.KnownCampaign{
			{AccountID: 7, CampaignID: 500, Version: 7, HasMessages: true},
		},
	})

	if !k.hasAccount(1) || !k.hasAccount(7) {
		t.Errorf("hasAccount missing seeded accounts")
	}
	if k.hasAccount(99) {
		t.Errorf("hasAccount(99) = true, want false")
	}

	kc, ok := k.lookup(7, 500)
	if !ok || kc.Version != 7 || !kc.HasMessages {
		t.Errorf("lookup(7,500) = (%+v,%v), want version7/hasMessages/true", kc, ok)
	}
	if _, ok := k.lookup(7, 999); ok {
		t.Errorf("lookup(7,999) ok = true, want false")
	}

	// replace is wholesale: a fresh snapshot drops the previous state.
	k.replace(client.KnownState{Accounts: []int{2}})
	if k.hasAccount(7) {
		t.Errorf("replace did not drop old accounts")
	}
	if !k.hasAccount(2) {
		t.Errorf("replace did not apply new accounts")
	}
}

func TestKnown_NeedsRegister(t *testing.T) {
	k := newKnown()
	k.replace(client.KnownState{
		Campaigns: []client.KnownCampaign{
			{AccountID: 7, CampaignID: 500, Version: 7, HasMessages: true},
			{AccountID: 7, CampaignID: 501, Version: 7, HasMessages: false},
		},
	})

	cases := []struct {
		name                   string
		account, campaign, ver int
		want                   bool
	}{
		{"unknown campaign", 7, 999, 7, true},
		{"version drift", 7, 500, 8, true},
		{"hasMessages=false at matching version", 7, 501, 7, true},
		{"known, matching version, hasMessages", 7, 500, 7, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := k.needsRegister(tc.account, tc.campaign, tc.ver); got != tc.want {
				t.Errorf("needsRegister(%d,%d,%d) = %v, want %v", tc.account, tc.campaign, tc.ver, got, tc.want)
			}
		})
	}
}
