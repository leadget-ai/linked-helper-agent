// Package client is the wire contract between the LH agent and the platform.
// The structs here mirror the DTOs in @leadget-ai/models — keep them in sync.
package client

// BootstrapRequest identifies the agent install to the server. Sent once at
// startup; the response carries the cadence the agent should run at AND the
// seed known-state snapshot the agent diffs against on the first cycle.
type BootstrapRequest struct {
	// Persistent UUID generated on first run and stored in the agent's data
	// dir (ProgramData on Windows, ~/Library/Application Support on macOS,
	// ~/.config on Linux). Survives sc.exe delete + reinstall, so the
	// platform can dedupe agents[] entries reliably even when hostname or
	// IP change.
	AgentID         string `json:"agentId,omitempty"`
	AgentVersion    string `json:"agentVersion"`
	Hostname        string `json:"hostname"`
	OS              string `json:"os"`
	PartitionsCount int    `json:"partitionsCount"`
}

type BootstrapResponse struct {
	IntegrationID  string     `json:"integrationId"`
	ReportInterval int        `json:"reportInterval"`
	Enabled        bool       `json:"enabled"`
	KnownState     KnownState `json:"knownState"`
}

// KnownState mirrors ILhAgentKnownState — the (account, campaign) tuples
// the platform already has registered for this integration. Returned by
// bootstrap and refreshed by every report.
type KnownState struct {
	Accounts  []int           `json:"accounts"`
	Campaigns []KnownCampaign `json:"campaigns"`
}

type KnownCampaign struct {
	AccountID  int `json:"accountId"`
	CampaignID int `json:"campaignId"`
	// Version the platform last accepted. The agent skips re-register only
	// when its current LH version_id matches; any bump means re-send so
	// renames / pause toggles / step edits propagate. 0 = pre-versioning row
	// → unconditional re-register on the next cycle.
	Version int `json:"version"`
}

// RegisterAccount is sent only the first time an LH accountId is reported.
// The platform creates a Client + platform-link (PENDING) from this; later
// cycles omit the block entirely.
//
// externalId is LinkedIn's internal numeric member id — stable across name
// changes / vanity-URL rewrites and the strongest matcher we have. Email
// and fullName are softer matchers used as fallbacks when the platform
// doesn't yet know the LinkedIn id of a Client. Shape is flat to match the
// platform DTO (LhAgentRegisterAccountDto).
type RegisterAccount struct {
	ExternalID *string `json:"externalId,omitempty"`
	Email      *string `json:"email,omitempty"`
	FullName   *string `json:"fullName,omitempty"`
	Avatar     *string `json:"avatar,omitempty"`
}

// CampaignAction is one workflow step from lh.db. For messaging actions
// (MessageToPerson / InvitePerson) Body holds the message template with
// {var} placeholders intact and Example holds the same template with
// sample values substituted. Delay (taken from a preceding Waiter step)
// describes the wait that fires this action. Non-messaging actions ship
// with Body=Example=nil and tell the platform the step exists without
// trying to mirror its content.
type CampaignAction struct {
	Type       string  `json:"type"`
	Body       *string `json:"body"`
	Example    *string `json:"example"`
	DelayValue *int    `json:"delayValue,omitempty"`
	DelayUnit  *string `json:"delayUnit,omitempty"` // "MINUTES" | "HOURS" | "DAYS"
}

// RegisterCampaign is sent once per LH campaign the agent has just
// discovered. Subsequent cycles only ship its CampaignFunnel.
type RegisterCampaign struct {
	CampaignID  int     `json:"campaignId"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	Type        int     `json:"type"`
	IsPaused    bool    `json:"isPaused"`
	IsArchived  bool    `json:"isArchived"`
	CreatedAt   string  `json:"createdAt"`
	// LH campaign_last_versions.version_id at the moment of send. Echoed
	// back in knownState so future cycles can short-circuit unchanged
	// campaigns.
	Version int `json:"version"`
	// Per-day send cap for this campaign — picked from LH's limit_types
	// (Invite for connection campaigns) or daily_limits.max_limit (global)
	// based on the first messaging action. Nil when LH had no usable cap;
	// the platform skips the end-date forecast in that case.
	MessagesPerDay *int             `json:"messagesPerDay,omitempty"`
	Actions        []CampaignAction `json:"actions"`
}

// CampaignFunnel carries the volatile counters that always overwrite the
// platform-side values. IsPaused/IsArchived ride along on every cycle so
// the platform can flip status without re-firing registerCampaign — LH
// pause/archive don't bump campaign_last_versions.version_id, so the
// version-gated upsert path would otherwise miss those transitions.
type CampaignFunnel struct {
	CampaignID int  `json:"campaignId"`
	Messaged   int  `json:"messaged"`
	Replied    int  `json:"replied"`
	Target     int  `json:"target"`
	IsPaused   bool `json:"isPaused"`
	IsArchived bool `json:"isArchived"`
}

// AccountReportRequest is the per-account batch sent every cycle. The
// "register" blocks are optional/empty on steady-state cycles; funnels are
// always present.
type AccountReportRequest struct {
	// Same persistent UUID as BootstrapRequest.AgentID — included on every
	// cycle so the platform can refresh agents[].lastSeenAt / lastSeenIp
	// between bootstraps (bootstrap fires on agent restart, reports every
	// ~10 minutes).
	AgentID           string             `json:"agentId,omitempty"`
	SyncedAt          string             `json:"syncedAt"`
	RegisterAccount   *RegisterAccount   `json:"registerAccount,omitempty"`
	RegisterCampaigns []RegisterCampaign `json:"registerCampaigns"`
	Funnels           []CampaignFunnel   `json:"funnels"`
}

type AccountReportResponse struct {
	ReportInterval int                          `json:"reportInterval"`
	Applied        AccountReportResponseApplied `json:"applied"`
	KnownState     KnownState                   `json:"knownState"`
}

type AccountReportResponseApplied struct {
	AccountRegistered   bool `json:"accountRegistered"`
	CampaignsRegistered int  `json:"campaignsRegistered"`
	FunnelsApplied      int  `json:"funnelsApplied"`
	FunnelsSkipped      int  `json:"funnelsSkipped"`
}
