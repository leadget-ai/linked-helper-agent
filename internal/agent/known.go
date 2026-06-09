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
	// Value is the platform's last-accepted state for this campaign. Agent
	// re-registers when its current LH version_id differs (covers rename /
	// pause toggle / step edits — knownState would otherwise pin the platform
	// to the first-seen snapshot forever) OR when the platform has no messages
	// for the campaign yet (backfill path — version alone never changes for an
	// untouched campaign, so message-less campaigns would never recover).
	campaigns map[campaignKey]knownCampaign
}

// knownCampaign is the platform's accepted state for one campaign, mirrored
// from knownState so the agent can decide whether to re-send it.
type knownCampaign struct {
	Version     int
	HasMessages bool
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
		campaigns: make(map[campaignKey]knownCampaign),
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
	campaigns := make(map[campaignKey]knownCampaign, len(s.Campaigns))
	for _, c := range s.Campaigns {
		campaigns[campaignKey{c.AccountID, c.CampaignID}] = knownCampaign{
			Version:     c.Version,
			HasMessages: c.HasMessages,
		}
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

// needsRegister reports whether the agent should (re)send a campaign's full
// registerCampaign payload this cycle. True when the platform doesn't know the
// campaign, when its accepted version differs from the live LH version (rename
// / pause toggle / step edit), or when the platform has it but holds no
// messages for it yet (backfill — an untouched campaign's version never moves,
// so message-less campaigns would otherwise never recover).
func (k *known) needsRegister(accountID, campaignID, version int) bool {
	k.mu.RLock()
	defer k.mu.RUnlock()
	c, ok := k.campaigns[campaignKey{accountID, campaignID}]
	if !ok {
		return true
	}
	return c.Version != version || !c.HasMessages
}

// lookup returns the platform's accepted state for a campaign and whether it's
// known at all. Callers use it to tell apart the two reasons needsRegister
// fires (version change vs missing messages), since the missing-messages path
// must additionally check the agent actually has messages to send.
func (k *known) lookup(accountID, campaignID int) (knownCampaign, bool) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	c, ok := k.campaigns[campaignKey{accountID, campaignID}]
	return c, ok
}
