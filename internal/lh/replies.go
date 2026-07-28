package lh

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
)

// CampaignReply is one inbound reply to the account's cold outreach, attributed
// to the campaign whose step the person was answering. Two kinds reach this
// path: the messages Linked Helper itself linked as a reply
// (action_result_messages.type = 'Replied'), and the ones a campaign person
// wrote straight in LinkedIn after the conversation left LH's workflow, read
// from the chat mirror. General inbox traffic (recruiters messaging the owner,
// personal chats) still never reaches it — the raw source only admits people the
// campaign actually targeted.
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
	linked, err := r.readLinkedReplies(ctx, db, profile, campaignID, sinceDetectedAt, limit)
	if err != nil {
		return nil, err
	}
	if !hasChatStore(profile) || !profile.HasTable("message_external_ids") || !profile.HasTable("person_in_campaigns_history") {
		return linked, nil
	}
	manual, err := r.readManualReplies(ctx, db, campaignID, sinceDetectedAt, limit)
	if err != nil {
		return nil, err
	}
	return mergeReplies(linked, manual, limit), nil
}

// readLinkedReplies is the action-linked source: replies LH recognized as
// answers to a campaign step.
func (r *Reader) readLinkedReplies(ctx context.Context, db *sql.DB, profile *DBProfile, campaignID int64, sinceDetectedAt string, limit int) ([]CampaignReply, error) {
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

	return scanReplies(rows)
}

// readManualReplies is the raw-chat source: messages a campaign person wrote in
// the LinkedIn conversation that LH mirrored but never linked to an action —
// everything the lead sent once the exchange moved out of the workflow.
//
// A message hangs on its author's chat participant row, so selecting the
// person's own row yields exactly their side of the thread; our manual answers
// sit on the owner's row and stay out of the reply feed (they reach the platform
// as thread messages instead).
//
// A person can be targeted by several campaigns, so the message is attributed to
// one of them deterministically: the campaign whose action last touched that
// person before the message, falling back to their earliest campaign when the
// message predates every result. That keeps a manual reply from being reported
// once per campaign the person belongs to.
func (r *Reader) readManualReplies(ctx context.Context, db *sql.DB, campaignID int64, sinceDetectedAt string, limit int) ([]CampaignReply, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT
			?,
			author.person_id,
			mei.external_id,
			m.subject,
			m.message_text,
			m.send_at,
			STRFTIME('%Y-%m-%dT%H:%M:%fZ', m.created_at) AS detected_at,
			(SELECT pe.external_id FROM person_external_ids pe
				WHERE pe.person_id = author.person_id AND pe.type_group = 'member' LIMIT 1) AS member_id,
			(SELECT pe.external_id FROM person_external_ids pe
				WHERE pe.person_id = author.person_id AND pe.type_group = 'public' LIMIT 1) AS public_slug,
			pmp.first_name,
			pmp.last_name,
			pmp.headline
		FROM chat_participants author
		JOIN participant_messages pm  ON pm.chat_participant_id = author.id
		JOIN messages m               ON m.id = pm.message_id
		JOIN message_external_ids mei ON mei.message_id = m.id
		LEFT JOIN person_mini_profile pmp ON pmp.person_id = author.person_id
		WHERE EXISTS (
			SELECT 1 FROM person_in_campaigns_history target
			WHERE target.person_id = author.person_id AND target.campaign_id = ?
		)
		  AND NOT EXISTS (
			SELECT 1 FROM action_result_messages arm WHERE arm.message_id = m.id
		)
		  AND COALESCE(
			(SELECT last.campaign_id FROM person_in_campaigns_history last
				WHERE last.person_id = author.person_id
				  AND last.campaign_id IS NOT NULL
				  AND last.result_created_at IS NOT NULL
				  AND last.result_created_at <= m.send_at
				ORDER BY last.result_created_at DESC, last.campaign_id
				LIMIT 1),
			(SELECT MIN(earliest.campaign_id) FROM person_in_campaigns_history earliest
				WHERE earliest.person_id = author.person_id AND earliest.campaign_id IS NOT NULL)
		  ) = ?
		  AND STRFTIME('%Y-%m-%dT%H:%M:%fZ', m.created_at) >= ?
		ORDER BY STRFTIME('%Y-%m-%dT%H:%M:%fZ', m.created_at)
		LIMIT ?
	`, campaignID, campaignID, campaignID, sinceDetectedAt, limit)
	if err != nil {
		return nil, fmt.Errorf("select manual campaign replies: %w", err)
	}
	defer rows.Close()

	return scanReplies(rows)
}

// mergeReplies interleaves the two sources back into one detection-ordered batch
// and caps it at the caller's limit; whatever the cap drops is picked up next
// cycle, since the platform advances the cursor only past what it stored.
func mergeReplies(linked, manual []CampaignReply, limit int) []CampaignReply {
	merged := make([]CampaignReply, 0, len(linked)+len(manual))
	merged = append(merged, linked...)
	merged = append(merged, manual...)
	sort.SliceStable(merged, func(i, j int) bool {
		if merged[i].DetectedAt != merged[j].DetectedAt {
			return merged[i].DetectedAt < merged[j].DetectedAt
		}
		return merged[i].ExternalID < merged[j].ExternalID
	})
	if limit > 0 && len(merged) > limit {
		merged = merged[:limit]
	}
	return merged
}

func scanReplies(rows *sql.Rows) ([]CampaignReply, error) {
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
