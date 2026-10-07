package lh

import (
	"context"
	"database/sql"
	"fmt"
)

type CampaignMessageExample struct {
	Body    string
	Subject *string
}

func (r *Reader) ReadCampaignMessageExamples(ctx context.Context, dbPath string, campaignID int64) (map[int64]CampaignMessageExample, bool, error) {
	db, err := r.open(dbPath)
	if err != nil {
		return nil, false, err
	}
	profile := r.profileFor(ctx, dbPath, db)
	for _, table := range []string{"person_in_campaigns_history", "action_result_messages", "messages", "campaign_last_versions", "campaign_version_actions"} {
		if !profile.HasTable(table) {
			return nil, false, nil
		}
	}
	joins, preference := campaignExamplePreference(profile)
	subject := "NULL"
	if profile.HasColumn("messages", "subject") {
		subject = "m.subject"
	}
	query := `
  WITH ranked AS (
   SELECT pich.action_id, m.message_text, ` + subject + ` AS subject,
    ROW_NUMBER() OVER (PARTITION BY pich.action_id ORDER BY ` + preference + `, m.send_at, m.id) AS rank
   FROM person_in_campaigns_history pich
   JOIN campaign_last_versions clv ON clv.campaign_id = pich.campaign_id
   JOIN campaign_version_actions cva ON cva.version_id = clv.version_id AND cva.action_id = pich.action_id
   JOIN action_result_messages arm ON arm.action_result_id = pich.result_id AND arm.type = 'Sent'
   JOIN messages m ON m.id = arm.message_id
   ` + joins + `
   WHERE pich.campaign_id = ? AND TRIM(m.message_text) <> ''
  )
  SELECT action_id, message_text, subject FROM ranked WHERE rank = 1
 `
	rows, err := db.QueryContext(ctx, query, campaignID)
	if err != nil {
		return nil, false, fmt.Errorf("select campaign message examples: %w", err)
	}
	defer rows.Close()
	out := make(map[int64]CampaignMessageExample)
	for rows.Next() {
		var actionID int64
		var body string
		var subject sql.NullString
		if err := rows.Scan(&actionID, &body, &subject); err != nil {
			return nil, false, fmt.Errorf("scan campaign message example: %w", err)
		}
		example := CampaignMessageExample{Body: body}
		if subject.Valid {
			example.Subject = &subject.String
		}
		out[actionID] = example
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	return out, true, nil
}

func campaignExamplePreference(profile *DBProfile) (string, string) {
	if !profile.HasColumn("action_versions", "id") || !profile.HasColumn("action_versions", "action_id") || !profile.HasColumn("action_versions", "config_id") {
		return "", "0"
	}
	joins := ""
	versionID := "NULL"
	if profile.HasColumn("action_results", "id") && profile.HasColumn("action_results", "action_version_id") {
		joins = "LEFT JOIN action_results ar ON ar.id = pich.result_id "
		versionID = "ar.action_version_id"
	}
	if profile.HasColumn("person_in_campaigns_history", "result_action_version_id") {
		versionID = "COALESCE(pich.result_action_version_id, " + versionID + ")"
	}
	joins += "LEFT JOIN action_versions sent_av ON sent_av.id = " + versionID + `
 LEFT JOIN action_versions current_av ON current_av.id = (
  SELECT MAX(av.id) FROM action_versions av WHERE av.action_id = pich.action_id
 )`
	return joins, "CASE WHEN sent_av.action_id = pich.action_id AND sent_av.config_id = current_av.config_id THEN 0 ELSE 1 END"
}
