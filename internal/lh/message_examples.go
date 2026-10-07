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
	for _, table := range []string{"person_in_campaigns_history", "action_result_messages", "messages", "action_results", "action_versions"} {
		if !profile.HasTable(table) {
			return nil, false, nil
		}
	}
	for _, column := range []struct{ table, name string }{
		{"person_in_campaigns_history", "result_action_version_id"}, {"messages", "subject"},
	} {
		if !profile.HasColumn(column.table, column.name) {
			return nil, false, nil
		}
	}
	rows, err := db.QueryContext(ctx, `
  WITH ranked AS (
   SELECT pich.action_id, m.message_text, m.subject,
    ROW_NUMBER() OVER (PARTITION BY pich.action_id ORDER BY m.send_at, m.id) AS rank
   FROM person_in_campaigns_history pich
   JOIN action_results ar ON ar.id = pich.result_id
   JOIN action_versions sent_av ON sent_av.id = COALESCE(pich.result_action_version_id, ar.action_version_id)
   JOIN action_versions current_av ON current_av.id = (
    SELECT MAX(av.id) FROM action_versions av WHERE av.action_id = pich.action_id
   )
   JOIN action_result_messages arm ON arm.action_result_id = pich.result_id AND arm.type = 'Sent'
   JOIN messages m ON m.id = arm.message_id
   WHERE pich.campaign_id = ? AND sent_av.action_id = pich.action_id
    AND sent_av.config_id = current_av.config_id AND TRIM(m.message_text) <> ''
  )
  SELECT action_id, message_text, subject FROM ranked WHERE rank = 1
 `, campaignID)
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
