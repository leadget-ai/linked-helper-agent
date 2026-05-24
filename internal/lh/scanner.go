// Package lh reads campaign/funnel data straight from a Linked Helper
// desktop installation's per-account SQLite databases.
//
// Layout on disk (one folder per LH login):
//
//	<partitionsDir>/linked-helper-account-<accountId>-main/lh.db
//
// The agent never writes to lh.db — reading is done in WAL+read-only mode so
// it does not conflict with the LH process holding the file.
package lh

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
)

// Account locates one LH login's database on disk.
type Account struct {
	ID     int
	DBPath string
}

// SQLite file name LH writes inside each partition folder. Matches the
// Python importer's default — overridable via env if a future LH build
// changes the layout.
const defaultSQLiteName = "lh.db"

var partitionRe = regexp.MustCompile(`^linked-helper-account-(\d+)-main$`)

// Scan walks `partitionsDir` and returns the accounts whose lh.db is
// readable. Folders that match the pattern but have no db (e.g. a freshly
// added LH login that has never been opened) are silently skipped.
func Scan(partitionsDir string) ([]Account, error) {
	return scanWithFile(partitionsDir, defaultSQLiteName)
}

func scanWithFile(partitionsDir, dbFileName string) ([]Account, error) {
	entries, err := os.ReadDir(partitionsDir)
	if err != nil {
		return nil, err
	}

	accounts := make([]Account, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		m := partitionRe.FindStringSubmatch(entry.Name())
		if m == nil {
			continue
		}
		id, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}

		dbPath := filepath.Join(partitionsDir, entry.Name(), dbFileName)
		if info, err := os.Stat(dbPath); err != nil || info.IsDir() {
			continue
		}
		accounts = append(accounts, Account{ID: id, DBPath: dbPath})
	}
	return accounts, nil
}
