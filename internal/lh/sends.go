package lh

import (
	"context"
	"database/sql"
	"fmt"
)

// CampaignSend is one people-level outbound message the account sent, read from
// Linked Helper's person_in_campaigns_history (one row per person×action). Only
// rows LH has actually processed (result_status != -999, so a real dispatch,
// not a queued target) with a timestamp are returned.
//
// ExternalID here is LH's local person_in_campaigns_history.id — unique only
// within one lh.db, so the caller namespaces it (account:campaign:id) before it
// goes on the wire. DetectedAt doubles as SentAt: LH records a single
// result_created_at per action result, canonicalized to millisecond precision
// so the cursor the platform echoes back round-trips byte-for-byte.
type CampaignSend struct {
	CampaignID int64
	PersonID   int64
	ExternalID string
	ActionID   int64
	MemberID   *string
	ProfileURL *string
	FullName   *string
	Headline   *string
	SentAt     string
	DetectedAt string
}

// ReadCampaignSends returns the account's per-person sends for one campaign that
// LH detected at or after `sinceDetectedAt` (empty = from the beginning),
// ordered by detection time and capped at `limit`. The bound is `>=`, not `>`,
// so a send sharing the cursor's millisecond is never skipped at a batch
// boundary — the platform's dedup on the namespaced ExternalID absorbs the
// boundary re-sends. Absent on schema generations without the history table the
// read yields nothing rather than failing the account's sync.
func (r *Reader) ReadCampaignSends(ctx context.Context, dbPath string, campaignID int64, sinceDetectedAt string, limit int) ([]CampaignSend, error) {
	db, err := r.open(dbPath)
	if err != nil {
		return nil, err
	}

	profile := r.profileFor(ctx, dbPath, db)
	if !profile.HasTable("person_in_campaigns_history") {
		return nil, nil
	}

	rows, err := db.QueryContext(ctx, `
		SELECT
			pich.campaign_id,
			pich.person_id,
			pich.id AS send_id,
			pich.action_id,
			STRFTIME('%Y-%m-%dT%H:%M:%fZ', pich.result_created_at) AS detected_at,
			(SELECT pe.external_id FROM person_external_ids pe
				WHERE pe.person_id = pich.person_id AND pe.type_group = 'member' LIMIT 1) AS member_id,
			(SELECT pe.external_id FROM person_external_ids pe
				WHERE pe.person_id = pich.person_id AND pe.type_group = 'public' LIMIT 1) AS public_slug,
			pmp.first_name,
			pmp.last_name,
			pmp.headline
		FROM person_in_campaigns_history pich
		LEFT JOIN person_mini_profile pmp ON pmp.person_id = pich.person_id
		WHERE pich.campaign_id = ?
		  AND pich.result_status != -999
		  AND pich.result_created_at IS NOT NULL
		  AND STRFTIME('%Y-%m-%dT%H:%M:%fZ', pich.result_created_at) >= ?
		ORDER BY STRFTIME('%Y-%m-%dT%H:%M:%fZ', pich.result_created_at)
		LIMIT ?
	`, campaignID, sinceDetectedAt, limit)
	if err != nil {
		return nil, fmt.Errorf("select campaign sends: %w", err)
	}
	defer rows.Close()

	var out []CampaignSend
	for rows.Next() {
		var (
			send                CampaignSend
			sendID              int64
			memberID, publicID  sql.NullString
			firstName, lastName sql.NullString
			headline            sql.NullString
		)
		if err := rows.Scan(
			&send.CampaignID,
			&send.PersonID,
			&sendID,
			&send.ActionID,
			&send.DetectedAt,
			&memberID,
			&publicID,
			&firstName,
			&lastName,
			&headline,
		); err != nil {
			return nil, fmt.Errorf("scan campaign send: %w", err)
		}

		send.ExternalID = fmt.Sprintf("%d", sendID)
		send.SentAt = send.DetectedAt
		if memberID.Valid && memberID.String != "" {
			send.MemberID = &memberID.String
		}
		if publicID.Valid && publicID.String != "" {
			url := "https://www.linkedin.com/in/" + publicID.String
			send.ProfileURL = &url
		}
		if name := fullName(firstName, lastName); name != "" {
			send.FullName = &name
		}
		if headline.Valid && headline.String != "" {
			send.Headline = &headline.String
		}

		out = append(out, send)
	}
	return out, rows.Err()
}
