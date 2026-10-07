package lh

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func exampleDatabase(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "examples.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, query := range []string{
		`CREATE TABLE campaign_last_versions (campaign_id INTEGER, version_id INTEGER)`,
		`CREATE TABLE campaign_version_actions (version_id INTEGER, action_id INTEGER)`,
		`INSERT INTO campaign_last_versions VALUES (50,1),(51,2)`,
		`INSERT INTO campaign_version_actions VALUES (1,10),(1,11),(2,10)`,
		`CREATE TABLE person_in_campaigns_history (campaign_id INTEGER, action_id INTEGER, result_id INTEGER, result_action_version_id INTEGER)`,
		`CREATE TABLE action_versions (id INTEGER, action_id INTEGER, config_id INTEGER)`,
		`CREATE TABLE action_results (id INTEGER, action_version_id INTEGER)`,
		`CREATE TABLE action_result_messages (action_result_id INTEGER, message_id INTEGER, type TEXT)`,
		`CREATE TABLE messages (id INTEGER, subject TEXT, message_text TEXT, send_at TEXT)`,
		`INSERT INTO action_versions VALUES (1,10,20),(2,10,30),(4,10,20),(5,11,40)`,
		`INSERT INTO action_results VALUES (1,1),(2,1),(3,2),(4,1),(5,1),(6,5),(7,1)`,
		`INSERT INTO person_in_campaigns_history VALUES (50,10,1,NULL),(50,10,2,1),(50,10,3,2),(51,10,4,1),(50,10,5,1),(50,11,6,5),(50,10,7,1)`,
		`INSERT INTO action_result_messages VALUES (1,1,'Replied'),(2,2,'Sent'),(3,3,'Sent'),(4,4,'Sent'),(5,5,'Sent'),(6,6,'Sent'),(7,7,'Sent')`,
		`INSERT INTO messages VALUES (1,'Inbound','Ignore inbound','2026-01-01'),(2,'Actual subject','Actual personalized body','2026-01-04'),(3,'Old subject','Old template body','2026-01-01'),(4,'Other campaign','Ignore campaign','2026-01-01'),(5,NULL,'   ','2026-01-01'),(6,NULL,'Other action body','2026-01-03'),(7,'Later subject','Later personalized body','2026-01-05')`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func TestReadCampaignMessageExamples_ActualCurrentActionOnly(t *testing.T) {
	reader := NewReader()
	defer reader.Close()
	examples, supported, err := reader.ReadCampaignMessageExamples(context.Background(), exampleDatabase(t), 50)
	if err != nil {
		t.Fatal(err)
	}
	if !supported || len(examples) != 2 {
		t.Fatalf("supported=%v count=%d", supported, len(examples))
	}
	sample := examples[10]
	if sample.Body != "Actual personalized body" || sample.Subject == nil || *sample.Subject != "Actual subject" {
		t.Fatal("expected subject and body from earliest real sent message of current config")
	}
	if examples[11].Body != "Other action body" || examples[11].Subject != nil {
		t.Fatal("action samples must remain separate")
	}
}

func TestReadCampaignMessageExamples_VerifiedNoSamples(t *testing.T) {
	reader := NewReader()
	defer reader.Close()
	examples, supported, err := reader.ReadCampaignMessageExamples(context.Background(), exampleDatabase(t), 99)
	if err != nil || !supported || len(examples) != 0 {
		t.Fatalf("supported=%v count=%d error=%v", supported, len(examples), err)
	}
}

func TestReadCampaignMessageExamples_UnsupportedPreservesExisting(t *testing.T) {
	reader := NewReader()
	defer reader.Close()
	path := filepath.Join(t.TempDir(), "unsupported.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE unrelated (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	examples, supported, err := reader.ReadCampaignMessageExamples(context.Background(), path, 50)
	if err != nil {
		t.Fatal(err)
	}
	if supported || examples != nil {
		t.Fatal("unsupported schema must omit examples")
	}
}

func TestReadCampaignMessageExamples_HistoricalFallback(t *testing.T) {
	cases := []struct {
		name    string
		queries []string
	}{
		{name: "changed template"},
		{name: "missing result row", queries: []string{`DELETE FROM action_results WHERE id = 3`, `UPDATE person_in_campaigns_history SET result_action_version_id = NULL WHERE result_id = 3`}},
		{name: "missing version row", queries: []string{`UPDATE person_in_campaigns_history SET result_action_version_id = 999 WHERE result_id = 3`, `UPDATE action_results SET action_version_id = 999 WHERE id = 3`}},
		{name: "missing result table", queries: []string{`DROP TABLE action_results`}},
		{name: "missing version table", queries: []string{`DROP TABLE action_versions`}},
		{name: "missing both metadata tables", queries: []string{`DROP TABLE action_results`, `DROP TABLE action_versions`}},
		{name: "missing history version column", queries: []string{`ALTER TABLE person_in_campaigns_history DROP COLUMN result_action_version_id`}},
		{name: "missing result version column", queries: []string{`ALTER TABLE action_results DROP COLUMN action_version_id`, `ALTER TABLE person_in_campaigns_history DROP COLUMN result_action_version_id`}},
		{name: "missing version config column", queries: []string{`ALTER TABLE action_versions DROP COLUMN config_id`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := exampleDatabase(t)
			queries := append([]string{`DELETE FROM action_result_messages WHERE message_id IN (2,7)`}, tc.queries...)
			mutateExampleDatabase(t, path, queries...)

			reader := NewReader()
			defer reader.Close()
			examples, supported, err := reader.ReadCampaignMessageExamples(context.Background(), path, 50)
			if err != nil || !supported {
				t.Fatalf("supported=%v error=%v", supported, err)
			}
			sample := examples[10]
			if sample.Body != "Old template body" || sample.Subject == nil || *sample.Subject != "Old subject" {
				t.Fatalf("expected paired historical Sent sample, got %+v", sample)
			}
		})
	}
}

func TestReadCampaignMessageExamples_ExcludesRemovedActionsAndManualMessages(t *testing.T) {
	path := exampleDatabase(t)
	mutateExampleDatabase(t, path,
		`INSERT INTO action_versions VALUES (8,12,60)`,
		`INSERT INTO action_results VALUES (8,8)`,
		`INSERT INTO person_in_campaigns_history VALUES (50,12,8,8)`,
		`INSERT INTO action_result_messages VALUES (8,8,'Sent')`,
		`INSERT INTO messages VALUES (8,'Removed subject','Removed action body','2025-01-01'),(9,'Manual subject','Unlinked manual body','2025-01-01')`,
	)

	reader := NewReader()
	defer reader.Close()
	examples, supported, err := reader.ReadCampaignMessageExamples(context.Background(), path, 50)
	if err != nil || !supported || len(examples) != 2 {
		t.Fatalf("supported=%v count=%d error=%v", supported, len(examples), err)
	}
	if _, exists := examples[12]; exists {
		t.Fatal("removed action must not produce a current campaign sample")
	}
	if examples[10].Body != "Actual personalized body" {
		t.Fatal("inbound, manual and foreign campaign messages must not replace the Sent sample")
	}
}

func TestReadCampaignMessageExamples_WithoutSubjectColumn(t *testing.T) {
	path := exampleDatabase(t)
	mutateExampleDatabase(t, path, `ALTER TABLE messages DROP COLUMN subject`)

	reader := NewReader()
	defer reader.Close()
	examples, supported, err := reader.ReadCampaignMessageExamples(context.Background(), path, 50)
	if err != nil || !supported || examples[10].Body != "Actual personalized body" || examples[10].Subject != nil {
		t.Fatalf("supported=%v example=%+v error=%v", supported, examples[10], err)
	}
}

func mutateExampleDatabase(t *testing.T, path string, queries ...string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, query := range queries {
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
}
