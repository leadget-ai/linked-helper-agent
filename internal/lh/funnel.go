package lh

import (
	"context"
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
			END) AS replied
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
		if err := rows.Scan(&campaignID, &target, &messaged, &replied); err != nil {
			return nil, fmt.Errorf("scan funnel: %w", err)
		}
		out[campaignID] = Funnel{Messaged: messaged, Replied: replied, Target: target}
	}
	return out, rows.Err()
}
