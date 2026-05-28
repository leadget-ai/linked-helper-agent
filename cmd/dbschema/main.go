// Command dbschema dumps the tables, columns, and indexes of a SQLite file.
//
// Usage:
//
//	go run ./cmd/dbschema <path/to/db>            # human-readable
//	go run ./cmd/dbschema -json <path/to/db>      # machine-readable
//	go run ./cmd/dbschema -counts <path/to/db>    # include row counts (SELECT COUNT(*))
//
// Opens the database read-only with immutable=1 so it works on Linked
// Helper's lh.db while LH itself is running and holding write locks.
package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	_ "modernc.org/sqlite"
)

type column struct {
	Name         string  `json:"name"`
	Type         string  `json:"type"`
	NotNull      bool    `json:"notNull"`
	DefaultValue *string `json:"default,omitempty"`
	PrimaryKey   bool    `json:"primaryKey"`
}

type index struct {
	Name    string   `json:"name"`
	Unique  bool     `json:"unique"`
	Columns []string `json:"columns"`
}

type table struct {
	Name     string   `json:"name"`
	Columns  []column `json:"columns"`
	Indexes  []index  `json:"indexes,omitempty"`
	RowCount *int64   `json:"rowCount,omitempty"`
}

func main() {
	asJSON := flag.Bool("json", false, "emit machine-readable JSON instead of a table")
	withCounts := flag.Bool("counts", false, "include SELECT COUNT(*) per table (slow on big tables)")
	flag.Parse()

	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: dbschema [-json] [-counts] <path/to/db>")
		os.Exit(2)
	}
	path := flag.Arg(0)

	abs, err := filepath.Abs(path)
	if err != nil {
		die("resolve path: %v", err)
	}
	if _, err := os.Stat(abs); err != nil {
		die("stat %s: %v", abs, err)
	}

	// immutable=1 lets us bypass shared-cache locking entirely — required
	// when the writer (Linked Helper) is alive. mode=ro is belt-and-suspenders.
	dsn := fmt.Sprintf("file:%s?mode=ro&immutable=1", url.PathEscape(abs))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		die("open %s: %v", abs, err)
	}
	defer db.Close()

	tables, err := loadTables(db)
	if err != nil {
		die("load tables: %v", err)
	}

	if *withCounts {
		for i := range tables {
			n, err := rowCount(db, tables[i].Name)
			if err != nil {
				fmt.Fprintf(os.Stderr, "row count for %q: %v\n", tables[i].Name, err)
				continue
			}
			tables[i].RowCount = &n
		}
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(tables); err != nil {
			die("encode json: %v", err)
		}
		return
	}
	printHuman(tables, *withCounts)
}

func loadTables(db *sql.DB) ([]table, error) {
	// sqlite_master holds the schema. Exclude sqlite_* internal tables and
	// views — caller wanted tables, not the engine's bookkeeping.
	rows, err := db.Query(`
		SELECT name FROM sqlite_master
		WHERE type='table' AND name NOT LIKE 'sqlite_%'
		ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		names = append(names, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]table, 0, len(names))
	for _, name := range names {
		cols, err := loadColumns(db, name)
		if err != nil {
			return nil, fmt.Errorf("columns of %q: %w", name, err)
		}
		idxs, err := loadIndexes(db, name)
		if err != nil {
			return nil, fmt.Errorf("indexes of %q: %w", name, err)
		}
		out = append(out, table{Name: name, Columns: cols, Indexes: idxs})
	}
	return out, nil
}

func loadColumns(db *sql.DB, table string) ([]column, error) {
	// PRAGMA returns: cid, name, type, notnull, dflt_value, pk. Quote the
	// table name with double quotes — some LH tables have hyphens.
	q := fmt.Sprintf(`PRAGMA table_info("%s")`, strings.ReplaceAll(table, `"`, `""`))
	rows, err := db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []column
	for rows.Next() {
		var (
			cid     int
			name    string
			typ     string
			notnull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		c := column{
			Name:       name,
			Type:       typ,
			NotNull:    notnull != 0,
			PrimaryKey: pk != 0,
		}
		if dflt.Valid {
			s := dflt.String
			c.DefaultValue = &s
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func loadIndexes(db *sql.DB, table string) ([]index, error) {
	q := fmt.Sprintf(`PRAGMA index_list("%s")`, strings.ReplaceAll(table, `"`, `""`))
	rows, err := db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type stub struct {
		name   string
		unique bool
	}
	var stubs []stub
	for rows.Next() {
		// seq, name, unique, origin, partial
		var seq int
		var name, origin string
		var unique, partial int
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			return nil, err
		}
		// Skip the implicit indexes SQLite creates for PRIMARY KEY / UNIQUE
		// constraints (origin='pk'/'u') — they're redundant with column flags.
		if origin == "pk" || origin == "u" {
			continue
		}
		stubs = append(stubs, stub{name: name, unique: unique != 0})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]index, 0, len(stubs))
	for _, s := range stubs {
		cols, err := loadIndexColumns(db, s.name)
		if err != nil {
			return nil, err
		}
		out = append(out, index{Name: s.name, Unique: s.unique, Columns: cols})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func loadIndexColumns(db *sql.DB, indexName string) ([]string, error) {
	q := fmt.Sprintf(`PRAGMA index_info("%s")`, strings.ReplaceAll(indexName, `"`, `""`))
	rows, err := db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		// seqno, cid, name
		var seqno, cid int
		var name sql.NullString
		if err := rows.Scan(&seqno, &cid, &name); err != nil {
			return nil, err
		}
		if name.Valid {
			cols = append(cols, name.String)
		}
	}
	return cols, rows.Err()
}

func rowCount(db *sql.DB, table string) (int64, error) {
	q := fmt.Sprintf(`SELECT COUNT(*) FROM "%s"`, strings.ReplaceAll(table, `"`, `""`))
	var n int64
	err := db.QueryRow(q).Scan(&n)
	return n, err
}

func printHuman(tables []table, withCounts bool) {
	if len(tables) == 0 {
		fmt.Println("(no user tables)")
		return
	}
	for i, t := range tables {
		if i > 0 {
			fmt.Println()
		}
		header := t.Name
		if withCounts && t.RowCount != nil {
			header = fmt.Sprintf("%s  (%d rows)", t.Name, *t.RowCount)
		}
		fmt.Println(header)
		fmt.Println(strings.Repeat("=", len(header)))

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "COLUMN\tTYPE\tNULL\tDEFAULT\tPK")
		for _, c := range t.Columns {
			null := "NOT NULL"
			if !c.NotNull {
				null = "NULL"
			}
			dflt := ""
			if c.DefaultValue != nil {
				dflt = *c.DefaultValue
			}
			pk := ""
			if c.PrimaryKey {
				pk = "PK"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", c.Name, c.Type, null, dflt, pk)
		}
		_ = w.Flush()

		if len(t.Indexes) > 0 {
			fmt.Println()
			fmt.Println("  indexes:")
			for _, idx := range t.Indexes {
				kind := "index"
				if idx.Unique {
					kind = "unique"
				}
				fmt.Printf("    %s (%s) on (%s)\n", idx.Name, kind, strings.Join(idx.Columns, ", "))
			}
		}
	}
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
