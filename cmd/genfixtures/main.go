// Command genfixtures builds the committed SQLite test fixtures under
// internal/lh/testdata/fixtures. It is OFFLINE tooling: it is never wired into
// `go test`, `go generate`-on-build or CI. The fixtures it produces are the
// real test inputs; this generator only has to run when a new fixture is
// needed, and its output is reviewed and committed like any other change.
//
// Each fixture is one self-contained spec (schema file + seed function). The
// generator applies the checked-in DDL with CREATE TRIGGER statements stripped
// (the agent only ever reads, so triggers are dead weight — and several LH
// triggers fire now()-stamped writes or reference tables absent from a
// data-free schema, which would break inserts and determinism), seeds fixed
// synthetic rows, then VACUUMs so the binary is minimal and stable.
//
// Determinism: page_size is pinned, every timestamp is a literal (no now()),
// and ids are fixed, so re-running with -force produces byte-identical files.
//
//	go run ./cmd/genfixtures            # generate missing fixtures
//	go run ./cmd/genfixtures -force     # regenerate all (overwrite)
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// spec declares one fixture: its output file name (without .db), the schema
// DDL to apply, and the seed routine that inserts its synthetic rows.
type spec struct {
	name   string
	schema string // schema file stem under -schema, e.g. "v205"
	seed   func(*sql.DB) error
}

var specs = []spec{
	{"owner-v195", "v195", seedOwnerV195},
	{"owner-v205", "v205", seedOwnerV205},
	{"owner-v210", "v210", seedOwnerV210},
	{"owner-degraded-v205", "v205", seedDegraded},
	{"owner-hashonly-v205", "v205", seedHashOnly},
	{"empty-v205", "v205", seedEmpty},
	{"campaign-v205", "v205", seedCampaign},
	{"campaign-scraper-v205", "v205", seedScraper},
}

func main() {
	force := flag.Bool("force", false, "overwrite existing fixtures")
	schemaDir := flag.String("schema", "internal/lh/testdata/schema", "directory holding vN.sql DDL files")
	outDir := flag.String("out", "internal/lh/testdata/fixtures", "directory to write .db fixtures into")
	only := flag.String("only", "", "generate only the named fixture (default: all)")
	flag.Parse()

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fatal("mkdir out: %v", err)
	}

	for _, s := range specs {
		if *only != "" && s.name != *only {
			continue
		}
		outPath := filepath.Join(*outDir, s.name+".db")
		if _, err := os.Stat(outPath); err == nil {
			if !*force {
				fmt.Printf("skip   %s (exists; -force to overwrite)\n", s.name)
				continue
			}
			if err := os.Remove(outPath); err != nil {
				fatal("remove %s: %v", outPath, err)
			}
		}
		if err := generate(s, *schemaDir, outPath); err != nil {
			fatal("generate %s: %v", s.name, err)
		}
		fmt.Printf("wrote  %s\n", outPath)
	}
}

// generate creates one fixture file: pin page_size, apply the DDL, seed, VACUUM.
func generate(s spec, schemaDir, outPath string) error {
	ddl, err := os.ReadFile(filepath.Join(schemaDir, s.schema+".sql"))
	if err != nil {
		return fmt.Errorf("read schema: %w", err)
	}

	db, err := sql.Open("sqlite", outPath)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer db.Close()
	// One connection so PRAGMA page_size and VACUUM act on the same handle and
	// statement order stays deterministic.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec("PRAGMA page_size=4096"); err != nil {
		return fmt.Errorf("set page_size: %w", err)
	}

	for _, stmt := range schemaStatements(string(ddl)) {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("apply ddl %.60q…: %w", stmt, err)
		}
	}

	if err := s.seed(db); err != nil {
		return fmt.Errorf("seed: %w", err)
	}

	if _, err := db.Exec("VACUUM"); err != nil {
		return fmt.Errorf("vacuum: %w", err)
	}
	return nil
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "genfixtures: "+format+"\n", args...)
	os.Exit(1)
}
