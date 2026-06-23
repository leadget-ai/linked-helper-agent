package main

import "database/sql"

// Synthetic identity shared across fixtures so owner goldens are identical per
// scenario regardless of schema version. None of these values are real.
const (
	liAccountID  = 454999                                    // LH internal account id (li_accounts)
	memberID     = "100200300"                               // real LinkedIn member id
	vanitySlug   = "test-owner"                              // public vanity slug
	ownerHash    = "ACoAAATESTHASH0000000000000000000000000" // ACoAA… profile hash
	ownerFirst   = "Test"
	ownerLast    = "Owner"
	ownerName    = "Test Owner"
	ownerEmail   = "owner@example.test"
	ownerAvatar  = "https://example.test/avatar.jpg"
	lastLoginAt  = "2026-01-01T00:00:00.000Z"
	ssiSnapshot  = `{"liAccountId":1,"version":1,"lastScore":{"total":42}}`
	fixedTime    = "2026-01-01T00:00:00.000Z" // every created_at/updated_at literal
	selfPersonID = 1000
)

// ---- low-level helpers ---------------------------------------------------

// exec runs one statement, threading any error back through a saved slot so
// seed functions can chain calls without an `if err` after every line.
type seeder struct {
	db  *sql.DB
	err error
}

func (s *seeder) exec(query string, args ...any) {
	if s.err != nil {
		return
	}
	if _, err := s.db.Exec(query, args...); err != nil {
		s.err = err
	}
}

func seedVersion(s *seeder, version int) {
	s.exec(`INSERT INTO version(id, version) VALUES (1, ?)`, version)
}

// liAccountsV195 is the v195 shape: identity lives only in the person tables;
// li_accounts carries just the internal id and last_login_at (NOT NULL there).
func liAccountsV195(s *seeder) {
	s.exec(`INSERT INTO li_accounts(id, li_account_id, last_login_at, created_at, updated_at)
	        VALUES (1, ?, ?, ?, ?)`, liAccountID, lastLoginAt, fixedTime, fixedTime)
}

// liAccountsFull is the v205/v210 shape. fullName/email/avatar may be nil to
// model the gaps real partitions exhibit (e.g. v210's empty email).
func liAccountsFull(s *seeder, fullName, email, avatar *string) {
	s.exec(`INSERT INTO li_accounts(id, external_id, full_name, avatar, email, last_login_at, created_at, updated_at)
	        VALUES (1, ?, ?, ?, ?, ?, ?, ?)`,
		liAccountID, fullName, avatar, email, lastLoginAt, fixedTime, fixedTime)
}

// personOpts tunes the SELF person fixture to exercise owner-source branches.
type personOpts struct {
	withPublic bool   // emit the 'public' vanity slug (off → ProfileURL falls back to hash)
	withEmail  bool   // emit a person_email row
	miniAvatar string // mini-profile avatar ("" models the v210 gap)
}

// selfPerson seeds the owner's own people entry: the SELF distance row plus the
// member/hash (+optional public) external ids, a mini profile and optional
// email. people itself is never inserted — no source joins it and foreign keys
// are off on the read path.
func selfPerson(s *seeder, o personOpts) {
	s.exec(`INSERT INTO person_member_distance(id, person_id, distance, li_account_id, actual_at, created_at, updated_at)
	        VALUES (1, ?, 'SELF', 1, ?, ?, ?)`, selfPersonID, fixedTime, fixedTime, fixedTime)

	insExt := func(id int, ext, group string, isMember any) {
		s.exec(`INSERT INTO person_external_ids(id, person_id, external_id, external_id_uppercase, type_group, is_member_id, created_at, updated_at)
		        VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			id, selfPersonID, ext, upper(ext), group, isMember, fixedTime, fixedTime)
	}
	insExt(1, memberID, "member", 1)
	insExt(2, ownerHash, "hash", nil)
	if o.withPublic {
		insExt(3, vanitySlug, "public", nil)
	}

	var avatar any
	if o.miniAvatar != "" {
		avatar = o.miniAvatar
	}
	s.exec(`INSERT INTO person_mini_profile(id, person_id, first_name, first_name_uppercase, last_name, last_name_uppercase, avatar, created_at, updated_at)
	        VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?)`,
		selfPersonID, ownerFirst, upper(ownerFirst), ownerLast, upper(ownerLast), avatar, fixedTime, fixedTime)

	if o.withEmail {
		s.exec(`INSERT INTO person_email(id, person_id, email, type, actual_at, created_at, updated_at)
		        VALUES (1, ?, ?, 'personal', ?, ?, ?)`, selfPersonID, ownerEmail, fixedTime, fixedTime, fixedTime)
	}
}

func ssiSnapshotRow(s *seeder) {
	s.exec(`INSERT INTO li_account_ssi_snapshot_store(id, aggregate_id, aggregate_type, version, snapshot_data, created_at, updated_at)
	        VALUES (1, 1, 'ssi', 1, ?, ?, ?)`, ssiSnapshot, fixedTime, fixedTime)
}

func upper(s string) string {
	// ASCII-only fixtures; a simple uppercase keeps the *_uppercase columns
	// populated without pulling in unicode tables.
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}

func ptr(s string) *string { return &s }

// ---- per-fixture seeds ---------------------------------------------------

func seedOwnerV195(db *sql.DB) error {
	s := &seeder{db: db}
	seedVersion(s, 195)
	liAccountsV195(s)
	selfPerson(s, personOpts{withPublic: true, withEmail: true, miniAvatar: ownerAvatar})
	ssiSnapshotRow(s)
	return s.err
}

func seedOwnerV205(db *sql.DB) error {
	s := &seeder{db: db}
	seedVersion(s, 205)
	liAccountsFull(s, ptr(ownerName), ptr(ownerEmail), ptr(ownerAvatar))
	selfPerson(s, personOpts{withPublic: true, withEmail: true, miniAvatar: ownerAvatar})
	ssiSnapshotRow(s)
	return s.err
}

// seedOwnerV210 mirrors a real v210 partition: li_accounts holds name/avatar but
// NO email, the mini-profile avatar is blank, and there is no person_email row —
// so the resolved owner matches v195/v205 except Email is nil.
func seedOwnerV210(db *sql.DB) error {
	s := &seeder{db: db}
	seedVersion(s, 210)
	liAccountsFull(s, ptr(ownerName), nil, ptr(ownerAvatar))
	selfPerson(s, personOpts{withPublic: true, withEmail: false, miniAvatar: ""})
	ssiSnapshotRow(s)
	return s.err
}

// seedDegraded: li_accounts row only, no SELF person — owner resolvable (name,
// email, avatar, last login) without member id or profile URL.
func seedDegraded(db *sql.DB) error {
	s := &seeder{db: db}
	seedVersion(s, 205)
	liAccountsFull(s, ptr(ownerName), ptr(ownerEmail), ptr(ownerAvatar))
	return s.err
}

// seedHashOnly: SELF person has member + hash but no public slug, so ProfileURL
// must fall back to the …/in/<hash> form.
func seedHashOnly(db *sql.DB) error {
	s := &seeder{db: db}
	seedVersion(s, 205)
	liAccountsFull(s, ptr(ownerName), ptr(ownerEmail), ptr(ownerAvatar))
	selfPerson(s, personOpts{withPublic: false, withEmail: true, miniAvatar: ownerAvatar})
	ssiSnapshotRow(s)
	return s.err
}

// seedEmpty: schema only, version row apart — empty partition semantics.
func seedEmpty(db *sql.DB) error {
	s := &seeder{db: db}
	seedVersion(s, 205)
	return s.err
}
