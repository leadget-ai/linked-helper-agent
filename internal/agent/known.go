package agent

import (
	"sync"

	"github.com/leadget/lh-agent/internal/client"
)

// known is a thread-safe mirror of the platform's known-state snapshot. The
// agent fills it from bootstrap and replaces it from every successful report
// response — that way a UI-side edit (manual client link, deleted campaign)
// is picked up on the next cycle for free.
//
// Reads happen during payload construction; writes happen on response. Both
// sides are short, so a single mutex is fine — no need for atomic sets.
type known struct {
	mu       sync.RWMutex
	accounts map[int]struct{}
	// Value is the version the platform last accepted for this campaign.
	// Agent re-registers when its current LH version_id differs (covers
	// rename / pause toggle / step edits — knownState would otherwise pin
	// the platform to the first-seen snapshot forever).
	campaigns map[campaignKey]int
}

// campaignKey deduplicates the (accountId, campaignId) tuple — the platform
// stores it serialised as "accountId:campaignId" in external.id but the
// agent only ever needs the pair.
type campaignKey struct {
	AccountID  int
	CampaignID int
}

func newKnown() *known {
	return &known{
		accounts:  make(map[int]struct{}),
		campaigns: make(map[campaignKey]int),
	}
}

// replace swaps in a fresh snapshot. Used after bootstrap and after every
// successful report — never partial updates, so we don't end up with a half
// view if a request was rejected mid-write.
func (k *known) replace(s client.KnownState) {
	accounts := make(map[int]struct{}, len(s.Accounts))
	for _, id := range s.Accounts {
		accounts[id] = struct{}{}
	}
	campaigns := make(map[campaignKey]int, len(s.Campaigns))
	for _, c := range s.Campaigns {
		campaigns[campaignKey{c.AccountID, c.CampaignID}] = c.Version
	}

	k.mu.Lock()
	defer k.mu.Unlock()
	k.accounts = accounts
	k.campaigns = campaigns
}

func (k *known) hasAccount(accountID int) bool {
	k.mu.RLock()
	defer k.mu.RUnlock()
	_, ok := k.accounts[accountID]
	return ok
}

// hasCampaignAtVersion is true only when the platform has this campaign AND
// its accepted version matches what the agent is about to send. Mismatches
// (or unknown campaigns) trigger a full registerCampaign so updates propagate.
func (k *known) hasCampaignAtVersion(accountID, campaignID, version int) bool {
	k.mu.RLock()
	defer k.mu.RUnlock()
	v, ok := k.campaigns[campaignKey{accountID, campaignID}]
	return ok && v == version
}
