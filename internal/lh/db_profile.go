package lh

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

// DBProfile is a capability snapshot of one LH partition database. The lh.db
// schema drifts between LH builds (and where fields are populated drifts even
// within one schema), so readers branch on observed capabilities — which
// tables and columns exist — never on the version number. SchemaVersion is
// for logs and diagnostics only.
type DBProfile struct {
	SchemaVersion int
	tables        map[string]bool
	columns       map[string]bool
}

// profiledTables is the closed set of tables whose columns owner sources may
// probe. Listing them up front keeps BuildDBProfile to a handful of cheap
// pragma calls instead of walking the whole schema.
var profiledTables = []string{
	"li_accounts",
	"person_member_distance",
	"person_external_ids",
	"person_mini_profile",
	"person_email",
	"li_account_ssi_snapshot_store",
}

func (p *DBProfile) HasTable(name string) bool {
	return p.tables[name]
}

func (p *DBProfile) HasColumn(table, column string) bool {
	return p.columns[table+"."+column]
}

// Describe renders the profile as "table(col, col, …); table; …" for
// diagnostics (inspectdb, log lines on unknown schemas).
func (p *DBProfile) Describe() string {
	parts := make([]string, 0, len(profiledTables))
	for _, table := range profiledTables {
		if !p.tables[table] {
			continue
		}
		cols := []string{}
		for key := range p.columns {
			if len(key) > len(table) && key[:len(table)+1] == table+"." {
				cols = append(cols, key[len(table)+1:])
			}
		}
		sort.Strings(cols)
		parts = append(parts, fmt.Sprintf("%s(%s)", table, strings.Join(cols, ", ")))
	}
	return strings.Join(parts, "; ")
}

// BuildDBProfile inspects sqlite_master and pragma_table_info for the tables
// owner sources care about. Errors on individual probes degrade to "absent"
// — a partition we cannot introspect behaves like one with no optional
// capabilities, and the sources skip themselves.
func BuildDBProfile(ctx context.Context, db *sql.DB) *DBProfile {
	profile := &DBProfile{
		tables:  make(map[string]bool),
		columns: make(map[string]bool),
	}

	row := db.QueryRowContext(ctx, `SELECT version FROM version LIMIT 1`)
	var version sql.NullInt64
	if err := row.Scan(&version); err == nil && version.Valid {
		profile.SchemaVersion = int(version.Int64)
	}

	for _, table := range profiledTables {
		cols, err := tableColumns(ctx, db, table)
		if err != nil || len(cols) == 0 {
			continue
		}
		profile.tables[table] = true
		for _, col := range cols {
			profile.columns[table+"."+col] = true
		}
	}

	return profile
}

func tableColumns(ctx context.Context, db *sql.DB, table string) ([]string, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_info(%q)`, table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var (
			cid          int
			name, ctype  string
			notNull, pk  int
			defaultValue sql.NullString
		)
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		columns = append(columns, name)
	}
	return columns, rows.Err()
}
