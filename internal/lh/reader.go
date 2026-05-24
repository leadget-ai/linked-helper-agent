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
}

// AccountOwner is the LinkedIn identity behind one LH login, used so the
// platform can later automatch the LH account to an existing Client.
type AccountOwner struct {
	ProfileURL *string
	PublicID   *string
	Nickname   *string
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
		SELECT id, name, description, type, is_paused, is_archived, is_valid, created_at
		FROM campaigns
		WHERE created_at > ?
		ORDER BY created_at
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
		if err := rows.Scan(&c.ID, &c.Name, &desc, &c.Type, &isPaused, &isArchived, &isValid, &c.CreatedAt); err != nil {
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

// ReadAccountOwner pulls the LH login's LinkedIn identity from `li_accounts`,
// best-effort. Older LH databases may not have all columns — missing ones are
// returned as nil pointers and the agent moves on.
func (r *Reader) ReadAccountOwner(ctx context.Context, dbPath string) (*AccountOwner, error) {
	db, err := r.open(dbPath)
	if err != nil {
		return nil, err
	}

	// `li_accounts` is keyed by `id`; the user-facing login is row 1 in every
	// LH partition we've seen. The other columns are optional and queried
	// defensively because LH renames them between major versions.
	row := db.QueryRowContext(ctx, `
		SELECT
			COALESCE(name, '') as nickname,
			COALESCE(public_identifier, '') as public_id,
			COALESCE(profile_url, '') as profile_url
		FROM li_accounts
		WHERE id = 1
	`)

	var nickname, publicID, profileURL string
	if err := row.Scan(&nickname, &publicID, &profileURL); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		// Older LH builds may lack profile_url / public_identifier columns —
		// don't fail the whole sync over the owner field.
		return nil, nil //nolint:nilerr
	}

	owner := &AccountOwner{}
	if nickname != "" {
		owner.Nickname = &nickname
	}
	if publicID != "" {
		owner.PublicID = &publicID
	}
	if profileURL != "" {
		owner.ProfileURL = &profileURL
	}
	return owner, nil
}
