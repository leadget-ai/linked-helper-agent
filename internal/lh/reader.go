package lh

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"sync"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver, no CGO required
)

// Campaign is a single row from the LH `campaigns` table, with the integer
// fields normalised to Go types. Boolean columns are stored as INTEGER in
// SQLite — we widen them once here so the rest of the codebase doesn't have
// to remember the convention.
type Campaign struct {
	ID          int64
	Name        string
	Description *string
	Type        int
	IsPaused    bool
	IsArchived  bool
	IsValid     *bool
	CreatedAt   string // raw ISO string as LH stores it
	// Latest version_id from campaign_last_versions (LH bumps this on any
	// edit — rename, pause, step changes). 0 if the campaign has no
	// versions yet, which shouldn't happen for a live campaign.
	Version int
}

// AccountOwner is the LinkedIn identity behind one LH login, used so the
// platform can later automatch the LH account to an existing Client.
//
// ExternalID is LinkedIn's internal numeric member ID — stable across name
// changes / URL rewrites and our best key for matching. Email and FullName
// are softer matchers used as fallbacks when the platform doesn't yet have
// the LinkedIn id of a known client.
type AccountOwner struct {
	ExternalID *int64
	Email      *string
	FullName   *string
	Avatar     *string
}

// Reader owns one open SQLite connection pool per LH account database. Pools
// are created lazily and kept open across cycles — opening a fresh handle
// every minute would defeat WAL caching and is the kind of thing that leaks
// file descriptors on long-running agents.
type Reader struct {
	mu      sync.Mutex
	handles map[string]*sql.DB
}

func NewReader() *Reader {
	return &Reader{handles: make(map[string]*sql.DB)}
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

// open returns a *sql.DB for `dbPath`, creating it on first access. Read-only
// + WAL means we never block the LH desktop process that holds the file.
func (r *Reader) open(dbPath string) (*sql.DB, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db, ok := r.handles[dbPath]; ok {
		return db, nil
	}

	// modernc.org/sqlite uses the URI form for pragmas. Stay strictly
	// read-only and avoid every PRAGMA that mutates the database file:
	//   - `journal_mode` rewrites the header, so setting WAL here fails
	//     with "attempt to write a readonly database" on any file not
	//     already in WAL. LH itself opens lh.db in WAL when it runs;
	//     our handle inherits that, so we don't need to set it.
	//   - `foreign_keys` is per-connection but, on modernc's driver, is
	//     still routed through the write path on open.
	//   `busy_timeout` is connection-local and safe.
	dsn := fmt.Sprintf(
		"file:%s?mode=ro&_pragma=busy_timeout(5000)",
		url.PathEscape(dbPath),
	)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// One connection per LH account is plenty — we run queries serially per
	// account and any concurrency lives at the account level.
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

// ReadCampaigns returns campaigns created after `since` (use the empty string
// on the first sync to fetch everything). `since` is compared as raw text —
// LH stores ISO-8601 timestamps which collate correctly that way.
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

// CampaignActionRow is one workflow step joined out of the LH schema. The
// step "type" lives on action_configs.actionType and the template/wait live
// on action_configs.actionSettings. Body/Example are populated only for
// messaging actions where the template renderer found content.
type CampaignActionRow struct {
	Type    string
	Body    *string
	Example *string
	// Subject/ExampleSubject hold an InMail step's subject line (LH stores it as
	// actionSettings.subjectTemplate). Regular LinkedIn messages have no subject,
	// so these stay nil for everything except InMail.
	Subject        *string
	ExampleSubject *string
	// WaitMs is how long LH holds a person at this step before advancing them
	// to the next workflow action. For CheckForReplies steps it's
	// actionSettings.moveToSuccessfulAfterMs (e.g. "wait 4 days for a reply
	// before sending the next message"). This — NOT action_configs.coolDown,
	// which is a fixed per-dispatch throttle — is the real inter-message
	// interval. The caller folds it onto the delay of the *next* messaging
	// action, since LH encodes inter-message waits as their own steps. Nil
	// when the step imposes no wait.
	WaitMs *int64
}

// ReadCampaignActions walks campaign → last_version → version_actions →
// actions → action_versions → action_configs and returns every step in
// workflow order. Non-messaging actions come back with Body=Example=nil so
// the caller can keep their position when assigning seq numbers but skip
// them when writing CampaignMessage rows.
func (r *Reader) ReadCampaignActions(ctx context.Context, dbPath string, campaignID int64) ([]CampaignActionRow, error) {
	db, err := r.open(dbPath)
	if err != nil {
		return nil, err
	}

	// We join through action_versions to find the latest config per action.
	// One action may have multiple versions; the highest id wins, mirroring
	// how LH itself resolves "current settings".
	//
	// Order by campaign_version_actions.id, NOT actions.startAt: startAt is a
	// mutable runtime timestamp (the step's last scheduled execution, which
	// drifts as the campaign runs and postpones), so ordering by it scrambles
	// the workflow into execution order. cva rows are inserted in design order
	// when the current version is built, so cva.id ascending is the true step
	// sequence.
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

// StepStat is one workflow step's engagement, in workflow (cva.id) order.
// Sent counts people the step actually processed; Replied counts people who
// replied. LH records a reply on the CheckForReplies step that FOLLOWS a
// message, not on the message itself — the caller folds each CheckForReplies'
// Replied back onto the preceding messaging step (mirror of how WaitMs folds
// forward).
type StepStat struct {
	Type    string
	Sent    int
	Replied int
}

// ReadCampaignStepStats returns per-action sent/replied counts for a campaign
// in workflow order. One GROUP BY over person_in_campaigns_history joined to
// the campaign's current-version actions — used both to classify the campaign
// and to build the per-step funnel every cycle.
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

// LinkedinKind values mirror the platform's ELinkedinCampaignKind. Scraper
// campaigns are classified so the agent can drop them before sending.
const (
	KindInMail  = "inmail"
	KindRegular = "regular"
	KindScraper = "scraper"
)

// ClassifyLinkedinKind derives a campaign's kind from its action types:
// an InMail step makes it inmail; an Invite/Message step makes it regular;
// anything else (only extract/visit/scrape/webhook steps) is a scraper.
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

// DailyLimits holds the per-day send caps the LH account is configured with.
// General is daily_limits.max_limit (the global per-day ceiling). Invite is
// the Invite limit_type cap; Message is the general cap (LH has no plain
// MessageToPerson per-action cap row in normal installs, so message-only
// campaigns are bound by General). Zero means "no cap configured / unknown".
type DailyLimits struct {
	General int
	Invite  int
}

// ReadDailyLimits pulls the per-day caps for this LH login. We hit two
// tables: daily_limits.max_limit (global) and limit_type_period_max_credits
// joined to limit_types.type='Invite' (per-action). Both queries are tiny
// and run once per cycle per account, so the cost is negligible.
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

// MessagesPerDayFor picks the right per-day cap for a campaign based on its
// first messaging action. Connection campaigns (InvitePerson) are throttled
// by the Invite limit; everything else is throttled by the global daily cap.
// Returns nil when we can't pick a meaningful number — caller serialises that
// as a null on the wire so the platform won't render a bogus forecast.
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

// ReadAccountOwner pulls the LH login's LinkedIn identity from `li_accounts`,
// best-effort. `li_accounts` is keyed by `id`; the user-facing login is row 1
// in every LH partition we've seen.
//
// Schema observed in LH 2 (May 2026): id, external_id, full_name, avatar,
// email, last_login_at, created_at, updated_at. Older LH builds carried
// `name` / `public_identifier` / `profile_url` instead — we don't read those
// any more because the new schema doesn't have them and even the legacy
// fallback never gave us a numeric LinkedIn member id, which is the most
// useful matcher.
//
// Returns (nil, nil) when the row is missing or the query fails — the agent
// treats it as "owner unknown" and ships the account anyway. We never fail
// the whole sync over the owner field.
func (r *Reader) ReadAccountOwner(ctx context.Context, dbPath string) (*AccountOwner, error) {
	db, err := r.open(dbPath)
	if err != nil {
		return nil, err
	}

	row := db.QueryRowContext(ctx, `
		SELECT external_id, full_name, email, avatar
		FROM li_accounts
		WHERE id = 1
	`)

	var (
		externalID sql.NullInt64
		fullName   sql.NullString
		email      sql.NullString
		avatar     sql.NullString
	)
	if err := row.Scan(&externalID, &fullName, &email, &avatar); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, nil //nolint:nilerr
	}

	owner := &AccountOwner{}
	if externalID.Valid {
		v := externalID.Int64
		owner.ExternalID = &v
	}
	if fullName.Valid && fullName.String != "" {
		s := fullName.String
		owner.FullName = &s
	}
	if email.Valid && email.String != "" {
		s := email.String
		owner.Email = &s
	}
	if avatar.Valid && avatar.String != "" {
		s := avatar.String
		owner.Avatar = &s
	}
	return owner, nil
}
