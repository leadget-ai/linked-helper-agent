package lh

import (
	"context"
	"database/sql"
	"fmt"
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
// one campaign — every message LH linked to that person's action results
// (outbound sends and inbound replies alike) — ordered ascending by message
// time. The join is action_result_messages.action_result_id =
// person_in_campaigns_history.result_id, which covers the whole conversation;
// pich.id would only reach a fraction of it.
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

// threadDirection maps an action_result_messages.type to a wire direction. Only
// 'Sent' and 'Replied' reach a thread; the former is our outbound message, and
// anything else is the person answering us.
func threadDirection(messageType string) string {
	if messageType == "Sent" {
		return DirectionOutbound
	}
	return DirectionInbound
}
