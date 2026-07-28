package lh

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
)

// Direction values distinguish our outbound messages from the person's inbound
// replies in a reconstructed thread; they mirror the platform's wire contract.
const (
	DirectionOutbound = "outbound"
	DirectionInbound  = "inbound"
)

// ThreadMessage is one message exchanged with a person in a campaign, read from
// the action_result_messages link that ties a message to the action result that
// produced it. Direction comes from that link's type ('Sent' => outbound,
// 'Replied' => inbound). ActionID is the history row's action so the caller can
// resolve an outbound message's seq number; inbound messages sit on the
// CheckForReplies step and resolve to no seq. MessageID is LH's local message
// id — unique only within one lh.db, so the caller namespaces it before it goes
// on the wire. OccurredAt is the message time, canonicalized to millisecond
// precision the same way replies/sends are.
type ThreadMessage struct {
	MessageID  int64
	ActionID   int64
	Direction  string
	Body       *string
	OccurredAt string
}

// threadTables are the tables ReadCampaignThread joins. Absent on schema
// generations that predate LH's chat store — the read then yields nothing
// rather than failing the account's sync, exactly like ReadCampaignReplies.
var threadTables = []string{
	"person_in_campaigns_history",
	"action_result_messages",
	"messages",
}

// ReadCampaignThread returns the complete bidirectional thread for one person in
// one campaign — outbound sends and inbound replies alike — ordered ascending by
// message time.
//
// Two sources are merged. The action-linked one walks
// action_result_messages.action_result_id = person_in_campaigns_history.result_id
// and yields the campaign's own steps plus the replies LH recognized. It sees
// only what LH itself sent or classified, so everything typed by hand in
// LinkedIn — our manual answers to a lead and the lead's follow-ups alike — is
// missing from it. The raw-chat source fills that gap by reading the person's
// conversation straight from LH's chat mirror, which stores manual traffic too.
//
// Messages present in both keep their action-linked attribution (the action id
// that resolves an outbound message's seq number); raw-only messages carry no
// action and therefore no seq, which is what they are — messages outside the
// campaign's workflow.
//
// created_at is returned through the same strftime canonicalization used for the
// reply cursor so a thread message's occurredAt compares byte-for-byte with the
// values the platform already stores for that campaign's replies and sends.
func (r *Reader) ReadCampaignThread(ctx context.Context, dbPath string, campaignID, personID int64) ([]ThreadMessage, error) {
	db, err := r.open(dbPath)
	if err != nil {
		return nil, err
	}

	profile := r.profileFor(ctx, dbPath, db)
	linked, err := r.readLinkedThread(ctx, db, profile, campaignID, personID)
	if err != nil {
		return nil, err
	}
	if !hasChatStore(profile) {
		return linked, nil
	}
	raw, err := r.readChatThread(ctx, db, personID)
	if err != nil {
		return nil, err
	}
	return mergeThread(linked, raw), nil
}

// readLinkedThread is the action-linked source: the messages LH tied to this
// person's results in this campaign.
func (r *Reader) readLinkedThread(ctx context.Context, db *sql.DB, profile *DBProfile, campaignID, personID int64) ([]ThreadMessage, error) {
	for _, table := range threadTables {
		if !profile.HasTable(table) {
			return nil, nil
		}
	}

	rows, err := db.QueryContext(ctx, `
		SELECT
			arm.type,
			pich.action_id,
			m.id AS message_id,
			STRFTIME('%Y-%m-%dT%H:%M:%fZ', m.created_at) AS occurred_at,
			m.message_text
		FROM person_in_campaigns_history pich
		JOIN action_result_messages arm ON arm.action_result_id = pich.result_id
		JOIN messages m               ON m.id = arm.message_id
		WHERE pich.campaign_id = ?
		  AND pich.person_id = ?
		ORDER BY STRFTIME('%Y-%m-%dT%H:%M:%fZ', m.created_at)
	`, campaignID, personID)
	if err != nil {
		return nil, fmt.Errorf("select campaign thread: %w", err)
	}
	defer rows.Close()

	var out []ThreadMessage
	for rows.Next() {
		var (
			messageType string
			msg         ThreadMessage
			body        sql.NullString
		)
		if err := rows.Scan(&messageType, &msg.ActionID, &msg.MessageID, &msg.OccurredAt, &body); err != nil {
			return nil, fmt.Errorf("scan thread message: %w", err)
		}
		msg.Direction = threadDirection(messageType)
		if body.Valid {
			msg.Body = &body.String
		}
		out = append(out, msg)
	}
	return out, rows.Err()
}

// readChatThread is the raw-chat source: every message in the conversations this
// person takes part in, whichever side wrote it and whether or not LH linked it
// to an action. Direction comes from the participant a message hangs on — the
// person's own participant row means they wrote it, any other row in that chat
// is the account owner. That rule is inverted deliberately ("not the lead is
// us") so it survives owner-identity gaps a hardcoded self person id would trip
// on, and it agrees with LH's action_result_messages.type on every linked
// message.
func (r *Reader) readChatThread(ctx context.Context, db *sql.DB, personID int64) ([]ThreadMessage, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT
			m.id AS message_id,
			CASE WHEN author.person_id = ? THEN 1 ELSE 0 END AS is_inbound,
			STRFTIME('%Y-%m-%dT%H:%M:%fZ', m.created_at) AS occurred_at,
			m.message_text
		FROM chat_participants member
		JOIN chat_participants author  ON author.chat_id = member.chat_id
		JOIN participant_messages pm   ON pm.chat_participant_id = author.id
		JOIN messages m                ON m.id = pm.message_id
		WHERE member.person_id = ?
		ORDER BY STRFTIME('%Y-%m-%dT%H:%M:%fZ', m.created_at)
	`, personID, personID)
	if err != nil {
		return nil, fmt.Errorf("select chat thread: %w", err)
	}
	defer rows.Close()

	var out []ThreadMessage
	for rows.Next() {
		var (
			msg       ThreadMessage
			isInbound bool
			body      sql.NullString
		)
		if err := rows.Scan(&msg.MessageID, &isInbound, &msg.OccurredAt, &body); err != nil {
			return nil, fmt.Errorf("scan chat thread message: %w", err)
		}
		msg.Direction = DirectionOutbound
		if isInbound {
			msg.Direction = DirectionInbound
		}
		if body.Valid {
			msg.Body = &body.String
		}
		out = append(out, msg)
	}
	return out, rows.Err()
}

// mergeThread folds the raw-chat messages into the action-linked ones, keeping
// the linked attribution where both sources hold the same message, and returns
// the union ordered by occurrence (message id breaking ties so the order is
// stable across cycles).
func mergeThread(linked, raw []ThreadMessage) []ThreadMessage {
	seen := make(map[int64]struct{}, len(linked))
	merged := make([]ThreadMessage, 0, len(linked)+len(raw))
	for _, msg := range linked {
		seen[msg.MessageID] = struct{}{}
		merged = append(merged, msg)
	}
	for _, msg := range raw {
		if _, duplicate := seen[msg.MessageID]; duplicate {
			continue
		}
		merged = append(merged, msg)
	}
	sort.SliceStable(merged, func(i, j int) bool {
		if merged[i].OccurredAt != merged[j].OccurredAt {
			return merged[i].OccurredAt < merged[j].OccurredAt
		}
		return merged[i].MessageID < merged[j].MessageID
	})
	return merged
}

// threadDirection maps an action_result_messages.type to a wire direction. Only
// 'Sent' and 'Replied' reach a thread; the former is our outbound message, and
// anything else is the person answering us.
func threadDirection(messageType string) string {
	if messageType == "Sent" {
		return DirectionOutbound
	}
	return DirectionInbound
}
