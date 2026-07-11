// Package client is the wire contract between the LH agent and the platform.
// The structs mirror the DTOs in @leadget-ai/models — keep them in sync.
package client

// BootstrapRequest identifies the agent install to the server, sent once at
// startup. AgentID is a persistent UUID stored outside the install dir (see
// agent.LoadOrCreateAgentID) so the platform can dedupe agents even when
// hostname or IP change.
type BootstrapRequest struct {
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

// KnownState is the platform's view of what it already has for this
// integration. Returned by bootstrap and refreshed by every report; the agent
// diffs against it to decide what to (re)send.
type KnownState struct {
	Accounts  []int           `json:"accounts"`
	Campaigns []KnownCampaign `json:"campaigns"`
}

type KnownCampaign struct {
	AccountID  int `json:"accountId"`
	CampaignID int `json:"campaignId"`
	// LH version_id the platform last accepted; 0 = pre-versioning row that
	// must be re-registered.
	Version int `json:"version"`
	// Whether the platform stored CampaignMessage rows; false triggers a
	// message backfill even at a matching version.
	HasMessages bool `json:"hasMessages"`
	// Per-campaign reply sync high-water mark (LH messages.created_at, canonical
	// ms). Empty means the campaign is registered but the platform holds no
	// replies yet — the agent backfills all of them; a value makes it
	// incremental from that point (re-read with a `>=` bound, deduped by
	// ExternalID). The agent only ships replies for campaigns present here, so a
	// freshly registered campaign's replies go out on the NEXT cycle.
	ReplyCursor string `json:"replyCursor"`
	// Per-campaign SEND sync high-water mark (LH person_in_campaigns_history
	// result_created_at, canonical ms). Empty = registered but no per-person
	// sends stored yet (agent backfills all); a value = incremental from it
	// (re-read with a `>=` bound, deduped by the send's ExternalID). Gated the
	// same way as ReplyCursor: only campaigns present here ship sends.
	SendCursor string `json:"sendCursor"`
}

// RegisterAccount is the full account snapshot, sent every cycle: identity
// fields seed/refresh the Client and linkedin_accounts row, LastLoginAt and
// SSI feed the LH account health calculator. ExternalID is the real LinkedIn
// member id (stable across renames), not Linked Helper's internal account id.
type RegisterAccount struct {
	ExternalID  *string          `json:"externalId,omitempty"`
	Email       *string          `json:"email,omitempty"`
	FullName    *string          `json:"fullName,omitempty"`
	Avatar      *string          `json:"avatar,omitempty"`
	Owner       *AccountOwnerRef `json:"owner,omitempty"`
	LastLoginAt *string          `json:"lastLoginAt,omitempty"`
	SSI         *int             `json:"ssi,omitempty"`
}

// AccountOwnerRef is the owner's public LinkedIn identity: the canonical
// linkedin.com/in/… URL and the bare vanity slug it was built from.
type AccountOwnerRef struct {
	ProfileURL *string `json:"profileUrl,omitempty"`
	PublicID   *string `json:"publicId,omitempty"`
}

// CampaignAction is one workflow step. Body/Example carry the message
// template with placeholders intact and with sample values substituted;
// both are nil for non-messaging steps. Delay is the wait BEFORE this action
// (folded in from the CheckForReplies steps that precede it).
type CampaignAction struct {
	Type           string  `json:"type"`
	Body           *string `json:"body"`
	Example        *string `json:"example"`
	Subject        *string `json:"subject,omitempty"`
	ExampleSubject *string `json:"exampleSubject,omitempty"`
	DelayValue     *int    `json:"delayValue,omitempty"`
	DelayUnit      *string `json:"delayUnit,omitempty"` // "MINUTES" | "HOURS" | "DAYS"
}

// RegisterCampaign carries a campaign's full metadata. Sent when the platform
// doesn't know the campaign at this version; steady-state cycles ship only
// the funnel.
type RegisterCampaign struct {
	CampaignID  int     `json:"campaignId"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	Type        int     `json:"type"`
	IsPaused    bool    `json:"isPaused"`
	IsArchived  bool    `json:"isArchived"`
	CreatedAt   string  `json:"createdAt"`
	// LH version_id at the moment of send; echoed back in knownState.
	Version int `json:"version"`
	// Per-day send cap from LH's limit tables; nil disables the platform's
	// end-date forecast.
	MessagesPerDay *int `json:"messagesPerDay,omitempty"`
	// "inmail" or "regular" — scraper campaigns are never sent.
	LinkedinKind string           `json:"linkedinKind"`
	Actions      []CampaignAction `json:"actions"`
}

// CampaignFunnel carries the volatile counters, sent every cycle and always
// overwriting platform-side values. IsPaused/IsArchived ride along because LH
// pause/archive don't bump version_id, so the version-gated register path
// would miss those transitions.
type CampaignFunnel struct {
	CampaignID int  `json:"campaignId"`
	Messaged   int  `json:"messaged"`
	Replied    int  `json:"replied"`
	Target     int  `json:"target"`
	IsPaused   bool `json:"isPaused"`
	IsArchived bool `json:"isArchived"`
	// Most recent action-result time; the platform uses it as the end date
	// for terminal campaigns.
	LastActivityAt *string `json:"lastActivityAt,omitempty"`
	// Sent every cycle so the platform backfills campaigns registered before
	// the kind field existed.
	LinkedinKind string `json:"linkedinKind"`
	// Per-message engagement; seq numbering matches the platform's
	// CampaignMessage rows.
	Steps []FunnelStep `json:"steps"`
}

type FunnelStep struct {
	SeqNumber int `json:"seqNumber"`
	Sent      int `json:"sent"`
	Replied   int `json:"replied"`
}

// CampaignReply is one inbound reply to the account's cold outreach. Only
// messages LH itself linked as a reply are sent — general inbox traffic never
// reaches here. ExternalID is the stable LinkedIn message id the platform
// dedupes on, so boundary re-sends are harmless. DetectedAt is LH's insertion
// time and doubles as the sync cursor; SentAt is the LinkedIn timestamp, kept
// for display only because it can arrive out of detection order.
type CampaignReply struct {
	CampaignID int         `json:"campaignId"`
	ExternalID string      `json:"externalId"`
	Person     ReplyPerson `json:"person"`
	Subject    *string     `json:"subject,omitempty"`
	Text       string      `json:"text"`
	SentAt     string      `json:"sentAt"`
	DetectedAt string      `json:"detectedAt"`
}

// ReplyPerson is the replier's LinkedIn identity, used to create or match the
// lead the reply belongs to. ExternalID is the stable LinkedIn member id.
type ReplyPerson struct {
	ExternalID *string `json:"externalId,omitempty"`
	ProfileURL *string `json:"profileUrl,omitempty"`
	FullName   *string `json:"fullName,omitempty"`
	Headline   *string `json:"headline,omitempty"`
}

// CampaignSend is one people-level outbound message the account sent,
// attributed to the campaign step that produced it. The platform stores these
// as dated outbound_messages rows (the event-log complement to the aggregate
// funnel) and never derives counters from them. ExternalID is namespaced
// (accountId:campaignId:pichId) because LH's per-person-history id is only
// unique within one lh.db; the platform dedupes on it. SentAt and DetectedAt
// are the same value — LH records a single result_created_at per send.
type CampaignSend struct {
	CampaignID int         `json:"campaignId"`
	ExternalID string      `json:"externalId"`
	Person     ReplyPerson `json:"person"`
	SeqNumber  int         `json:"seqNumber"`
	SentAt     string      `json:"sentAt"`
	DetectedAt string      `json:"detectedAt"`
}

// AccountReportRequest is the per-account batch sent every cycle. The
// register blocks are empty on steady-state cycles; funnels are always
// present; replies and sends carry only those detected since the account's
// per-campaign cursors.
type AccountReportRequest struct {
	AgentID           string             `json:"agentId,omitempty"`
	SyncedAt          string             `json:"syncedAt"`
	RegisterAccount   *RegisterAccount   `json:"registerAccount,omitempty"`
	RegisterCampaigns []RegisterCampaign `json:"registerCampaigns"`
	Funnels           []CampaignFunnel   `json:"funnels"`
	Replies           []CampaignReply    `json:"replies"`
	Sends             []CampaignSend     `json:"sends"`
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
