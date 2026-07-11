package agent

import (
	"sync"

	"github.com/leadget/lh-agent/internal/client"
)

// known is a thread-safe mirror of the platform's known-state snapshot,
// seeded by bootstrap and replaced wholesale from every successful report
// response — so UI-side edits (manual client link, deleted campaign) are
// picked up on the next cycle for free.
type known struct {
	mu        sync.RWMutex
	accounts  map[int]struct{}
	campaigns map[campaignKey]knownCampaign
}

type knownCampaign struct {
	Version     int
	HasMessages bool
	// ReplyCursor is the platform's per-campaign reply high-water mark. Empty =
	// registered but no replies stored yet (agent backfills); a value = read
	// incrementally from it. The campaign being present in the map at all is
	// what gates the reply path — an unregistered campaign has no entry.
	ReplyCursor string
	// SendCursor is the platform's per-campaign send high-water mark, gating the
	// per-person send path exactly like ReplyCursor gates replies.
	SendCursor string
}

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
			ReplyCursor: c.ReplyCursor,
			SendCursor:  c.SendCursor,
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

// needsRegister reports whether the campaign's full metadata must be
// (re)sent: unknown to the platform, version drift (rename / pause toggle /
// step edit), or the platform holds no messages yet (an untouched campaign's
// version never moves, so message-less campaigns would otherwise never
// recover).
func (k *known) needsRegister(accountID, campaignID, version int) bool {
	k.mu.RLock()
	defer k.mu.RUnlock()
	c, ok := k.campaigns[campaignKey{accountID, campaignID}]
	if !ok {
		return true
	}
	return c.Version != version || !c.HasMessages
}

// lookup exposes the raw accepted state so callers can tell apart the two
// needsRegister reasons — the missing-messages path must additionally check
// the agent has messages to contribute.
func (k *known) lookup(accountID, campaignID int) (knownCampaign, bool) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	c, ok := k.campaigns[campaignKey{accountID, campaignID}]
	return c, ok
}
