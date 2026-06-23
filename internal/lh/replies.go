package lh

import (
	"context"
	"database/sql"
	"fmt"
)

// CampaignReply is one inbound reply to the account's cold outreach, attributed
// to the campaign whose step the person was answering. Only messages Linked
// Helper itself linked as a reply (action_result_messages.type = 'Replied') are
// returned — general inbox traffic (recruiters messaging the owner, personal
// chats) never reaches this path.
//
// ExternalID is the stable LinkedIn message id; the platform dedupes on it, so
// the same reply may be re-sent across cycles without creating duplicates.
// DetectedAt is the agent's sync cursor: it is monotonic in LH's insertion
// order, unlike SentAt (the LinkedIn timestamp), which can arrive out of order
// because LH polls each person's inbox on its own cadence. It is canonicalized
// to millisecond precision so the value the platform echoes back as the next
// cursor compares byte-for-byte against the stored column.
type CampaignReply struct {
	CampaignID int64
	PersonID   int64
	ExternalID string
	MemberID   *string
	ProfileURL *string
	FullName   *string
	Headline   *string
	Subject    *string
	Text       string
	SentAt     string
	DetectedAt string
}

// replyTables are the tables ReadCampaignReplies joins. Absent on schema
// generations that predate LH's chat store — the read then yields nothing
// rather than failing the account's sync.
var replyTables = []string{
	"action_result_messages",
	"messages",
	"message_external_ids",
	"action_results",
	"action_versions",
	"actions",
}

// ReadCampaignReplies returns replies LH detected at or after `sinceDetectedAt`
// (empty string = from the beginning), ordered by detection time and capped at
// `limit`. The bound is `>=`, not `>`, so a reply sharing the cursor's
// millisecond is never skipped at a batch boundary — the platform's dedup on
// ExternalID absorbs the resulting boundary re-sends.
//
// created_at is compared and returned through the same strftime canonicalization
// so the cursor round-trips exactly: modernc drops zero milliseconds when
// scanning a DATETIME column, which would otherwise make the echoed cursor sort
// below the raw stored value and silently drop the boundary reply.
//
// One reply message can be linked to several campaign steps; reply_links folds
// those to a single row (the earliest linked action_result), so each LinkedIn
// reply is emitted once with one campaign attribution.
func (r *Reader) ReadCampaignReplies(ctx context.Context, dbPath string, campaignID int64, sinceDetectedAt string, limit int) ([]CampaignReply, error) {
	db, err := r.open(dbPath)
	if err != nil {
		return nil, err
	}

	profile := r.profileFor(ctx, dbPath, db)
	for _, table := range replyTables {
		if !profile.HasTable(table) {
			return nil, nil
		}
	}

	rows, err := db.QueryContext(ctx, `
		WITH reply_links AS (
			SELECT message_id, MIN(action_result_id) AS action_result_id
			FROM action_result_messages
			WHERE type = 'Replied'
			GROUP BY message_id
		)
		SELECT
			a.campaign_id,
			ar.person_id,
			mei.external_id,
			m.subject,
			m.message_text,
			m.send_at,
			STRFTIME('%Y-%m-%dT%H:%M:%fZ', m.created_at) AS detected_at,
			(SELECT pe.external_id FROM person_external_ids pe
				WHERE pe.person_id = ar.person_id AND pe.type_group = 'member' LIMIT 1) AS member_id,
			(SELECT pe.external_id FROM person_external_ids pe
				WHERE pe.person_id = ar.person_id AND pe.type_group = 'public' LIMIT 1) AS public_slug,
			pmp.first_name,
			pmp.last_name,
			pmp.headline
		FROM reply_links rl
		JOIN messages m               ON m.id = rl.message_id
		JOIN message_external_ids mei ON mei.message_id = m.id
		JOIN action_results ar        ON ar.id = rl.action_result_id
		JOIN action_versions av       ON av.id = ar.action_version_id
		JOIN actions a                ON a.id = av.action_id
		LEFT JOIN person_mini_profile pmp ON pmp.person_id = ar.person_id
		WHERE a.campaign_id = ?
		  AND STRFTIME('%Y-%m-%dT%H:%M:%fZ', m.created_at) >= ?
		ORDER BY STRFTIME('%Y-%m-%dT%H:%M:%fZ', m.created_at)
		LIMIT ?
	`, campaignID, sinceDetectedAt, limit)
	if err != nil {
		return nil, fmt.Errorf("select campaign replies: %w", err)
	}
	defer rows.Close()

	var out []CampaignReply
	for rows.Next() {
		var (
			reply               CampaignReply
			subject             sql.NullString
			memberID, publicID  sql.NullString
			firstName, lastName sql.NullString
			headline            sql.NullString
		)
		if err := rows.Scan(
			&reply.CampaignID,
			&reply.PersonID,
			&reply.ExternalID,
			&subject,
			&reply.Text,
			&reply.SentAt,
			&reply.DetectedAt,
			&memberID,
			&publicID,
			&firstName,
			&lastName,
			&headline,
		); err != nil {
			return nil, fmt.Errorf("scan campaign reply: %w", err)
		}

		if subject.Valid && subject.String != "" {
			reply.Subject = &subject.String
		}
		if memberID.Valid && memberID.String != "" {
			reply.MemberID = &memberID.String
		}
		if publicID.Valid && publicID.String != "" {
			url := "https://www.linkedin.com/in/" + publicID.String
			reply.ProfileURL = &url
		}
		if name := fullName(firstName, lastName); name != "" {
			reply.FullName = &name
		}
		if headline.Valid && headline.String != "" {
			reply.Headline = &headline.String
		}

		out = append(out, reply)
	}
	return out, rows.Err()
}

func fullName(firstName, lastName sql.NullString) string {
	first := ""
	if firstName.Valid {
		first = firstName.String
	}
	last := ""
	if lastName.Valid {
		last = lastName.String
	}
	name := first
	if last != "" {
		if name != "" {
			name += " "
		}
		name += last
	}
	return name
}
