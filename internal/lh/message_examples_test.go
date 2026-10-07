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
