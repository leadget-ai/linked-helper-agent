package lh

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
)

// OwnerSource is one strategy for extracting the owner's identity from an LH
// partition. Sources run in priority order and only fill nil fields; errors
// skip the source, never the sync. Supporting a new LH schema = one more
// source (or a wider Supports) appended to ownerSources. Supports may gate on
// profile.SchemaVersion as a last resort when capabilities can't
// disambiguate.
type OwnerSource interface {
	Name() string
	Supports(p *DBProfile) bool
	Fill(ctx context.Context, db *sql.DB, p *DBProfile, owner *AccountOwner) error
}

var ownerSources = []OwnerSource{
	liAccountsSource{},
	selfPersonSource{},
}

// liAccountsSource reads whatever identity columns the li_accounts row
// carries (all of them on v205+, only last_login_at on v195). The numeric
// external_id column is deliberately NOT read: it is Linked Helper's internal
// account id, not the LinkedIn member id.
type liAccountsSource struct{}

func (liAccountsSource) Name() string { return "li_accounts" }

func (liAccountsSource) Supports(p *DBProfile) bool {
	return p.HasTable("li_accounts")
}

func (liAccountsSource) Fill(ctx context.Context, db *sql.DB, p *DBProfile, owner *AccountOwner) error {
	available := []string{}
	for _, col := range []string{"full_name", "email", "avatar", "last_login_at"} {
		if p.HasColumn("li_accounts", col) {
			available = append(available, col)
		}
	}
	if len(available) == 0 {
		return nil
	}

	row := db.QueryRowContext(ctx,
		`SELECT `+strings.Join(available, ", ")+` FROM li_accounts WHERE id = 1`)

	values := make([]sql.NullString, len(available))
	scan := make([]any, len(available))
	for i := range values {
		scan[i] = &values[i]
	}
	if err := row.Scan(scan...); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}

	for i, col := range available {
		if !values[i].Valid || values[i].String == "" {
			continue
		}
		value := values[i].String
		switch col {
		case "full_name":
			fillString(&owner.FullName, value)
		case "email":
			fillString(&owner.Email, value)
		case "avatar":
			fillString(&owner.Avatar, value)
		case "last_login_at":
			fillString(&owner.LastLoginAt, value)
		}
	}
	return nil
}

// selfPersonSource resolves the owner via their own people entry (the
// person_member_distance row with distance='SELF') — the only place the
// vanity slug, ACoAA… hash and real member id exist. It also backfills name,
// avatar and email on schemas where li_accounts has gaps.
type selfPersonSource struct{}

func (selfPersonSource) Name() string { return "self_person" }

func (selfPersonSource) Supports(p *DBProfile) bool {
	return p.HasTable("person_member_distance") && p.HasTable("person_external_ids")
}

func (selfPersonSource) Fill(ctx context.Context, db *sql.DB, p *DBProfile, owner *AccountOwner) error {
	row := db.QueryRowContext(ctx,
		`SELECT person_id FROM person_member_distance WHERE distance = 'SELF' LIMIT 1`)
	var personID int64
	if err := row.Scan(&personID); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}

	rows, err := db.QueryContext(ctx,
		`SELECT type_group, external_id FROM person_external_ids WHERE person_id = ?`, personID)
	if err != nil {
		return err
	}
	defer rows.Close()

	var hash *string
	for rows.Next() {
		var typeGroup, externalID string
		if err := rows.Scan(&typeGroup, &externalID); err != nil {
			return err
		}
		switch typeGroup {
		case "member":
			if memberID, err := strconv.ParseInt(externalID, 10, 64); err == nil && owner.ExternalID == nil {
				owner.ExternalID = &memberID
			}
		case "public":
			fillString(&owner.PublicSlug, externalID)
		case "hash":
			h := externalID
			hash = &h
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if owner.ProfileURL == nil {
		if owner.PublicSlug != nil {
			fillString(&owner.ProfileURL, "https://www.linkedin.com/in/"+*owner.PublicSlug)
		} else if hash != nil {
			fillString(&owner.ProfileURL, "https://www.linkedin.com/in/"+*hash)
		}
	}

	if (owner.FullName == nil || owner.Avatar == nil) && p.HasTable("person_mini_profile") {
		fillFromMiniProfile(ctx, db, personID, owner)
	}
	if owner.Email == nil && p.HasTable("person_email") {
		fillFromPersonEmail(ctx, db, personID, owner)
	}
	return nil
}

func fillFromMiniProfile(ctx context.Context, db *sql.DB, personID int64, owner *AccountOwner) {
	row := db.QueryRowContext(ctx,
		`SELECT first_name, last_name, avatar FROM person_mini_profile WHERE person_id = ?`, personID)
	var firstName, lastName, avatar sql.NullString
	if err := row.Scan(&firstName, &lastName, &avatar); err != nil {
		return
	}
	name := strings.TrimSpace(strings.TrimSpace(firstName.String) + " " + strings.TrimSpace(lastName.String))
	if name != "" {
		fillString(&owner.FullName, name)
	}
	if avatar.Valid && avatar.String != "" {
		fillString(&owner.Avatar, avatar.String)
	}
}

func fillFromPersonEmail(ctx context.Context, db *sql.DB, personID int64, owner *AccountOwner) {
	row := db.QueryRowContext(ctx, `
		SELECT email FROM person_email
		WHERE person_id = ?
		ORDER BY CASE type WHEN 'personal' THEN 0 ELSE 1 END
		LIMIT 1
	`, personID)
	var email sql.NullString
	if err := row.Scan(&email); err != nil {
		return
	}
	if email.Valid && email.String != "" {
		fillString(&owner.Email, email.String)
	}
}

func fillString(target **string, value string) {
	if *target == nil && value != "" {
		v := value
		*target = &v
	}
}
