// Package client is the wire contract between the LH agent and the platform.
// The structs here mirror the DTOs in @leadget-ai/models — keep them in sync.
package client

// BootstrapRequest identifies the agent install to the server. Sent once at
// startup; the response carries the cadence the agent should run at AND the
// seed known-state snapshot the agent diffs against on the first cycle.
type BootstrapRequest struct {
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
}

// AccountOwner is the LinkedIn identity behind one LH login, used so the
// platform can later automatch the account to an existing Client.
type AccountOwner struct {
	ProfileURL *string `json:"profileUrl,omitempty"`
	PublicID   *string `json:"publicId,omitempty"`
}

// RegisterAccount is sent only the first time an LH accountId is reported.
// The platform creates a Client + platform-link (PENDING) from this; later
// cycles omit the block entirely.
type RegisterAccount struct {
	Nickname *string       `json:"nickname,omitempty"`
	Owner    *AccountOwner `json:"owner,omitempty"`
}

// CampaignAction is a single workflow step from the lh.db actions table.
type CampaignAction struct {
	Type string  `json:"type"`
	Body *string `json:"body"`
}

// RegisterCampaign is sent once per LH campaign the agent has just
// discovered. Subsequent cycles only ship its CampaignFunnel.
type RegisterCampaign struct {
	CampaignID  int              `json:"campaignId"`
	Name        string           `json:"name"`
	Description *string          `json:"description,omitempty"`
	Type        int              `json:"type"`
	IsPaused    bool             `json:"isPaused"`
	IsArchived  bool             `json:"isArchived"`
	CreatedAt   string           `json:"createdAt"`
	Actions     []CampaignAction `json:"actions"`
}

// CampaignFunnel carries the volatile counters that always overwrite the
// platform-side values.
type CampaignFunnel struct {
	CampaignID int `json:"campaignId"`
	Messaged   int `json:"messaged"`
	Replied    int `json:"replied"`
	Target     int `json:"target"`
}

// AccountReportRequest is the per-account batch sent every cycle. The
// "register" blocks are optional/empty on steady-state cycles; funnels are
// always present.
type AccountReportRequest struct {
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
