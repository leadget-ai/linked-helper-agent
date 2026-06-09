package lh

import (
	"context"
	"database/sql"
	"fmt"
)

// Funnel is the per-campaign aggregate the agent ships to the platform. We
// compute it inside the agent so the wire payload stays O(campaigns) instead
// of O(people * actions).
type Funnel struct {
	// Distinct people the campaign messaged at least once
	// (result_status != -999 in LH's person_in_campaigns_history).
	Messaged int
	// Distinct people who replied. LH marks both via result_status = 2 and
	// the recipient_replied flag — we OR them.
	Replied int
	// Total distinct people the campaign ever queued, including those still
	// awaiting processing (status == -999). Maps onto Campaign.maxRecipients.
	Target int
	// LastActivityAt is the most recent action-result timestamp across the
	// campaign (MAX(result_created_at)). For a finished campaign this is
	// effectively when it last sent — the platform stamps it as the real
	// end date instead of "detected today". Nil when nothing has run yet.
	LastActivityAt *string
}

// ReadFunnels returns one Funnel per campaign id present in
// `person_in_campaigns_history` for the database. The map key is the LH
// campaign id (int64 to match the campaigns table primary key).
//
// We don't filter by `since` here: funnel counters are absolute totals, not
// deltas. A returning person doesn't get double-counted because the query
// uses COUNT(DISTINCT person_id). One sweep, three counters per row.
func (r *Reader) ReadFunnels(ctx context.Context, dbPath string) (map[int64]Funnel, error) {
	db, err := r.open(dbPath)
	if err != nil {
		return nil, err
	}

	rows, err := db.QueryContext(ctx, `
		SELECT
			campaign_id,
			COUNT(DISTINCT person_id) AS target,
			COUNT(DISTINCT CASE WHEN result_status != -999 THEN person_id END) AS messaged,
			COUNT(DISTINCT CASE
				WHEN result_status = 2 OR result_flag_recipient_replied = 1
				THEN person_id
			END) AS replied,
			MAX(result_created_at) AS last_activity_at
		FROM person_in_campaigns_history
		GROUP BY campaign_id
	`)
	if err != nil {
		return nil, fmt.Errorf("select funnels: %w", err)
	}
	defer rows.Close()

	out := make(map[int64]Funnel)
	for rows.Next() {
		var campaignID int64
		var target, messaged, replied int
		var lastActivityAt sql.NullString
		if err := rows.Scan(&campaignID, &target, &messaged, &replied, &lastActivityAt); err != nil {
			return nil, fmt.Errorf("scan funnel: %w", err)
		}
		f := Funnel{Messaged: messaged, Replied: replied, Target: target}
		if lastActivityAt.Valid && lastActivityAt.String != "" {
			s := lastActivityAt.String
			f.LastActivityAt = &s
		}
		out[campaignID] = f
	}
	return out, rows.Err()
}
