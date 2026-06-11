package lh

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	_ "modernc.org/sqlite" // pure-Go driver, no CGO required
)

// Campaign is one row from the LH `campaigns` table with SQLite's
// INTEGER-as-boolean columns widened to Go types.
type Campaign struct {
	ID          int64
	Name        string
	Description *string
	Type        int
	IsPaused    bool
	IsArchived  bool
	IsValid     *bool
	CreatedAt   string // raw ISO string as LH stores it
	// campaign_last_versions.version_id — LH bumps it on any edit (rename,
	// pause, step changes). 0 = no versions yet.
	Version int
}

// AccountOwner is the LH login's LinkedIn identity, assembled by the
// ownerSources chain. ExternalID is the REAL LinkedIn member id (from
// person_external_ids type_group='member'), not Linked Helper's internal
// account id. ProfileURL is built from the public vanity slug when the owner
// has one, falling back to the ACoAA… hash form.
type AccountOwner struct {
	ExternalID  *int64
	Email       *string
	FullName    *string
	Avatar      *string
	ProfileURL  *string
	PublicSlug  *string
	LastLoginAt *string
	SSI         *int
}

func (o *AccountOwner) isEmpty() bool {
	return o.ExternalID == nil && o.Email == nil && o.FullName == nil &&
		o.Avatar == nil && o.ProfileURL == nil && o.PublicSlug == nil &&
		o.LastLoginAt == nil && o.SSI == nil
}

// Reader owns one lazily-opened SQLite handle (and capability profile) per LH
// partition, kept across cycles — reopening every cycle would defeat WAL
// caching and leak file descriptors on long-running agents.
type Reader struct {
	mu       sync.Mutex
	handles  map[string]*sql.DB
	profiles map[string]*DBProfile
}

func NewReader() *Reader {
	return &Reader{
		handles:  make(map[string]*sql.DB),
		profiles: make(map[string]*DBProfile),
	}
}

// profileFor builds the capability profile on first access. The schema of an
// open lh.db only changes across LH upgrades, which restart the desktop app
// (and this agent's handle with it), so caching for the Reader's lifetime is
// safe.
func (r *Reader) profileFor(ctx context.Context, dbPath string, db *sql.DB) *DBProfile {
	r.mu.Lock()
	if profile, ok := r.profiles[dbPath]; ok {
		r.mu.Unlock()
		return profile
	}
	r.mu.Unlock()

	profile := BuildDBProfile(ctx, db)

	r.mu.Lock()
	r.profiles[dbPath] = profile
	r.mu.Unlock()
	return profile
}

// Close releases every cached connection. Call from agent shutdown.
func (r *Reader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var firstErr error
	for path, db := range r.handles {
		if err := db.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("close %s: %w", path, err)
		}
		delete(r.handles, path)
	}
	return firstErr
}

// open returns the cached *sql.DB for `dbPath`, creating it on first access.
// Strictly read-only so we never block the LH desktop process holding the
// file. busy_timeout is the only pragma that is safe here: journal_mode and
// (on modernc's driver) foreign_keys go through the write path and fail on a
// mode=ro handle — WAL is inherited from LH's own connection anyway.
func (r *Reader) open(dbPath string) (*sql.DB, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db, ok := r.handles[dbPath]; ok {
		return db, nil
	}

	dsn := fmt.Sprintf(
		"file:%s?mode=ro&_pragma=busy_timeout(5000)",
		url.PathEscape(dbPath),
	)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Queries run serially per account; concurrency lives at the account level.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxIdleTime(30 * time.Minute)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	r.handles[dbPath] = db
	return db, nil
}

// ReadCampaigns returns campaigns created after `since` (empty string =
// everything). Compared as raw text — LH's ISO-8601 timestamps collate
// correctly that way.
func (r *Reader) ReadCampaigns(ctx context.Context, dbPath, since string) ([]Campaign, error) {
	db, err := r.open(dbPath)
	if err != nil {
		return nil, err
	}

	rows, err := db.QueryContext(ctx, `
		SELECT
			c.id, c.name, c.description, c.type,
			c.is_paused, c.is_archived, c.is_valid, c.created_at,
			COALESCE(clv.version_id, 0) AS version
		FROM campaigns c
		LEFT JOIN campaign_last_versions clv ON clv.campaign_id = c.id
		WHERE c.created_at > ?
		ORDER BY c.created_at
	`, since)
	if err != nil {
		return nil, fmt.Errorf("select campaigns: %w", err)
	}
	defer rows.Close()

	var out []Campaign
	for rows.Next() {
		var c Campaign
		var desc sql.NullString
		var isPaused, isArchived int
		var isValid sql.NullInt64
		if err := rows.Scan(&c.ID, &c.Name, &desc, &c.Type, &isPaused, &isArchived, &isValid, &c.CreatedAt, &c.Version); err != nil {
			return nil, fmt.Errorf("scan campaign: %w", err)
		}
		if desc.Valid {
			c.Description = &desc.String
		}
		c.IsPaused = isPaused != 0
		c.IsArchived = isArchived != 0
		if isValid.Valid {
			v := isValid.Int64 != 0
			c.IsValid = &v
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CampaignActionRow is one workflow step. Body/Example are populated only
// for messaging actions where the template renderer found content;
// Subject/ExampleSubject only for InMail steps.
type CampaignActionRow struct {
	Type           string
	Body           *string
	Example        *string
	Subject        *string
	ExampleSubject *string
	// WaitMs is actionSettings.moveToSuccessfulAfterMs — how long LH holds a
	// person at this step (e.g. a CheckForReplies' "wait 4 days for a reply").
	// This, NOT action_configs.coolDown (a fixed per-dispatch throttle), is
	// the real inter-message interval; the caller folds it onto the delay of
	// the next messaging action.
	WaitMs *int64
}

// ReadCampaignActions returns the current version's steps in workflow order.
// Non-messaging actions come back with Body=Example=nil so the caller keeps
// their position when assigning seq numbers.
//
// Ordered by campaign_version_actions.id, NOT actions.startAt: startAt is a
// mutable runtime timestamp that drifts as the campaign executes, while cva
// rows are inserted in design order when the version is built.
func (r *Reader) ReadCampaignActions(ctx context.Context, dbPath string, campaignID int64) ([]CampaignActionRow, error) {
	db, err := r.open(dbPath)
	if err != nil {
		return nil, err
	}

	rows, err := db.QueryContext(ctx, `
		WITH latest_av AS (
			SELECT av.action_id, MAX(av.id) AS max_id
			FROM action_versions av
			GROUP BY av.action_id
		)
		SELECT
			a.id,
			ac."actionType",
			ac."actionSettings"
		FROM campaign_last_versions clv
		JOIN campaign_version_actions cva ON cva.version_id = clv.version_id
		JOIN actions a ON a.id = cva.action_id
		JOIN latest_av lav ON lav.action_id = a.id
		JOIN action_versions av ON av.id = lav.max_id
		JOIN action_configs ac ON ac.id = av.config_id
		WHERE clv.campaign_id = ?
		ORDER BY cva.id
	`, campaignID)
	if err != nil {
		return nil, fmt.Errorf("select campaign actions: %w", err)
	}
	defer rows.Close()

	var out []CampaignActionRow
	for rows.Next() {
		var (
			actionID   int64
			actionType string
			settings   sql.NullString
		)
		if err := rows.Scan(&actionID, &actionType, &settings); err != nil {
			return nil, fmt.Errorf("scan campaign action: %w", err)
		}

		row := CampaignActionRow{Type: actionType}
		if settings.Valid {
			if tpl, ex, ok := RenderMessage(settings.String); ok {
				row.Body = &tpl
				row.Example = &ex
			}
			if tpl, ex, ok := RenderSubject(settings.String); ok {
				row.Subject = &tpl
				row.ExampleSubject = &ex
			}
			if ms, ok := parseWaitMs(settings.String); ok {
				row.WaitMs = &ms
			}
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// StepStat is one workflow step's engagement, in workflow order. LH records
// a reply on the CheckForReplies step that FOLLOWS a message, not on the
// message itself — the caller folds it back onto the preceding messaging
// step.
type StepStat struct {
	Type    string
	Sent    int
	Replied int
}

// ReadCampaignStepStats returns per-action sent/replied counts in workflow
// order — used both to classify the campaign and to build the per-step
// funnel.
func (r *Reader) ReadCampaignStepStats(ctx context.Context, dbPath string, campaignID int64) ([]StepStat, error) {
	db, err := r.open(dbPath)
	if err != nil {
		return nil, err
	}

	rows, err := db.QueryContext(ctx, `
		SELECT
			ac."actionType",
			COUNT(DISTINCT CASE WHEN pich.result_status != -999 THEN pich.person_id END) AS sent,
			COUNT(DISTINCT CASE
				WHEN pich.result_status = 2 OR pich.result_flag_recipient_replied = 1
				THEN pich.person_id
			END) AS replied
		FROM campaign_last_versions clv
		JOIN campaign_version_actions cva ON cva.version_id = clv.version_id
		JOIN actions a ON a.id = cva.action_id
		JOIN action_configs ac ON ac.id = (
			SELECT av.config_id FROM action_versions av
			WHERE av.action_id = a.id ORDER BY av.id DESC LIMIT 1
		)
		LEFT JOIN person_in_campaigns_history pich
			ON pich.action_id = a.id AND pich.campaign_id = clv.campaign_id
		WHERE clv.campaign_id = ?
		GROUP BY cva.id, ac."actionType"
		ORDER BY cva.id
	`, campaignID)
	if err != nil {
		return nil, fmt.Errorf("select campaign step stats: %w", err)
	}
	defer rows.Close()

	var out []StepStat
	for rows.Next() {
		var s StepStat
		if err := rows.Scan(&s.Type, &s.Sent, &s.Replied); err != nil {
			return nil, fmt.Errorf("scan step stat: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// LinkedinKind values mirror the platform's ELinkedinCampaignKind.
const (
	KindInMail  = "inmail"
	KindRegular = "regular"
	KindScraper = "scraper"
)

// ClassifyLinkedinKind derives a campaign's kind from its action types: an
// InMail step makes it inmail, an Invite/Message step regular, anything else
// (only extract/visit/webhook steps) a scraper the agent drops.
func ClassifyLinkedinKind(actionTypes []string) string {
	hasInMail, hasMessaging := false, false
	for _, t := range actionTypes {
		switch t {
		case "InMail":
			hasInMail = true
		case "InvitePerson", "MessageToPerson":
			hasMessaging = true
		}
	}
	if hasInMail {
		return KindInMail
	}
	if hasMessaging {
		return KindRegular
	}
	return KindScraper
}

// DailyLimits holds the per-day send caps: General is daily_limits.max_limit
// (the global ceiling), Invite the Invite limit_type cap. LH has no plain
// MessageToPerson per-action cap, so message campaigns are bound by General.
// Zero means no cap configured / unknown.
type DailyLimits struct {
	General int
	Invite  int
}

func (r *Reader) ReadDailyLimits(ctx context.Context, dbPath string) (DailyLimits, error) {
	db, err := r.open(dbPath)
	if err != nil {
		return DailyLimits{}, err
	}

	var out DailyLimits

	row := db.QueryRowContext(ctx, `SELECT max_limit FROM daily_limits WHERE li_account_id = 1`)
	var general sql.NullInt64
	if err := row.Scan(&general); err == nil && general.Valid {
		out.General = int(general.Int64)
	}

	row = db.QueryRowContext(ctx, `
		SELECT max_credits
		FROM limit_type_period_max_credits lp
		JOIN limit_types lt ON lt.id = lp.limit_type_id
		WHERE lp.li_account_id = 1
		  AND lp.is_deleted = 0
		  AND lp.period = 86400
		  AND lt.type = 'Invite'
		ORDER BY lp.id DESC
		LIMIT 1
	`)
	var invite sql.NullInt64
	if err := row.Scan(&invite); err == nil && invite.Valid {
		out.Invite = int(invite.Int64)
	}

	return out, nil
}

// MessagesPerDayFor picks the cap that throttles this campaign, based on its
// first messaging action. Nil when no meaningful cap exists — the platform
// then skips its end-date forecast.
func (d DailyLimits) MessagesPerDayFor(actions []CampaignActionRow) *int {
	for _, a := range actions {
		switch a.Type {
		case "InvitePerson":
			if d.Invite > 0 {
				v := d.Invite
				return &v
			}
		case "MessageToPerson":
			if d.General > 0 {
				v := d.General
				return &v
			}
		}
	}
	if d.General > 0 {
		v := d.General
		return &v
	}
	return nil
}

// InspectProfile exposes the partition's capability profile for diagnostics
// (cmd/inspectdb). Production reads go through ReadAccountOwner, which uses
// the same cached profile internally.
func (r *Reader) InspectProfile(ctx context.Context, dbPath string) (*DBProfile, error) {
	db, err := r.open(dbPath)
	if err != nil {
		return nil, err
	}
	return r.profileFor(ctx, dbPath, db), nil
}

// ReadAccountOwner assembles the LH login's LinkedIn identity by running the
// ownerSources chain against the partition (see owner_sources.go). Each source
// only fills fields the previous ones left empty, and each gates itself on the
// partition's capability profile, so one code path serves every lh.db schema
// generation we know (v195 person-tables-only through v210).
//
// Returns (nil, nil) when nothing could be resolved at all — the agent treats
// it as "owner unknown" and ships the account anyway. We never fail the whole
// sync over the owner field.
func (r *Reader) ReadAccountOwner(ctx context.Context, dbPath string) (*AccountOwner, error) {
	db, err := r.open(dbPath)
	if err != nil {
		return nil, err
	}

	profile := r.profileFor(ctx, dbPath, db)
	owner := &AccountOwner{}
	for _, source := range ownerSources {
		if !source.Supports(profile) {
			continue
		}
		if err := source.Fill(ctx, db, profile, owner); err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"source":        source.Name(),
				"schemaVersion": profile.SchemaVersion,
			}).Warn("owner source failed; continuing with partial owner")
		}
	}

	if profile.HasTable("li_account_ssi_snapshot_store") {
		owner.SSI = r.readAccountSSI(ctx, db)
	}

	if owner.isEmpty() {
		return nil, nil
	}
	return owner, nil
}

// readAccountSSI returns the latest Social Selling Index total for the account,
// or nil when no snapshot exists. Best-effort: any error (missing table on
// older LH builds, malformed JSON) yields nil rather than failing the owner read.
func (r *Reader) readAccountSSI(ctx context.Context, db *sql.DB) *int {
	row := db.QueryRowContext(ctx, `
		SELECT snapshot_data
		FROM li_account_ssi_snapshot_store
		WHERE aggregate_id = 1
		ORDER BY version DESC
		LIMIT 1
	`)

	var snapshot sql.NullString
	if err := row.Scan(&snapshot); err != nil || !snapshot.Valid {
		return nil
	}

	var parsed struct {
		LastScore struct {
			Total *float64 `json:"total"`
		} `json:"lastScore"`
	}
	if err := json.Unmarshal([]byte(snapshot.String), &parsed); err != nil {
		return nil
	}
	if parsed.LastScore.Total == nil {
		return nil
	}
	total := int(*parsed.LastScore.Total)
	return &total
}
