package main

import "database/sql"

// Message templates as LH stores them in action_configs.actionSettings: a
// variants→variant→group tree with text and {var} nodes.
const (
	inviteSettings = `{"messageTemplate":{"type":"variants","variants":[{"type":"variant","child":{"type":"group","children":` +
		`[{"type":"var","name":"firstName"},{"type":"text","value":", let's connect!"}]}}]}}`
	// CheckForReplies holds a person for 4 days (345600000ms) before advancing.
	checkSettings   = `{"moveToSuccessfulAfterMs":345600000}`
	messageSettings = `{"messageTemplate":{"type":"variants","variants":[{"type":"variant","child":{"type":"group","children":` +
		`[{"type":"text","value":"Thanks for connecting, "},{"type":"var","name":"firstName"},{"type":"text","value":"!"}]}}]}}`
	visitSettings = `{}`
)

// workflowStep is one design-order action: a campaign_version_actions row (cva
// id sets workflow order) tied through actions / action_versions /
// action_configs to its type and settings.
type workflowStep struct {
	actionID   int
	configID   int
	versionID  int // action_versions.id
	cvaID      int
	actionType string
	settings   string
}

// seedWorkflow writes the actions/configs/versions/cva rows for one campaign's
// steps and its campaign_versions row (campaignVersionID becomes the
// campaign_last_versions view's version_id).
func seedWorkflow(s *seeder, campaignID, campaignVersionID int, steps []workflowStep) {
	s.exec(`INSERT INTO campaign_versions(id, campaign_id, created_at, updated_at)
	        VALUES (?, ?, ?, ?)`, campaignVersionID, campaignID, fixedTime, fixedTime)

	for _, st := range steps {
		s.exec(`INSERT INTO actions(id, campaign_id, name, "startAt") VALUES (?, ?, ?, ?)`,
			st.actionID, campaignID, st.actionType, fixedTime)
		s.exec(`INSERT INTO action_configs(id, "actionType", "actionSettings", "coolDown", "maxActionResultsPerIteration", "isDraft")
		        VALUES (?, ?, ?, 60000, 1, 0)`, st.configID, st.actionType, st.settings)
		s.exec(`INSERT INTO action_versions(id, action_id, config_id, created_at, updated_at)
		        VALUES (?, ?, ?, ?, ?)`, st.versionID, st.actionID, st.configID, fixedTime, fixedTime)
		s.exec(`INSERT INTO campaign_version_actions(id, version_id, action_id, created_at, updated_at)
		        VALUES (?, ?, ?, ?, ?)`, st.cvaID, campaignVersionID, st.actionID, fixedTime, fixedTime)
	}
}

func seedCampaignRow(s *seeder, id int, uuid, name, desc string, typ int, paused, archived, valid int) {
	s.exec(`INSERT INTO campaigns(id, uuid, name, description, type, is_paused, is_archived, is_valid, li_account_id, created_at)
	        VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?)`,
		id, uuid, name, desc, typ, paused, archived, valid, "2026-01-02T00:00:00.000Z")
}

// pich seeds one person_in_campaigns_history row. Only the columns readers
// touch carry meaning; the remaining NOT NULL columns get inert placeholders.
// resultID links the row to its action_results row (the thread reader joins
// action_result_messages on it); 0 leaves it NULL, as it is for queued targets.
func pich(s *seeder, id, personID, campaignID, actionID, actionVersionID, resultID, status, repliedFlag int, createdAt string) {
	var result any
	if resultID != 0 {
		result = resultID
	}
	s.exec(`INSERT INTO person_in_campaigns_history(
	            id, action_target_people_id, action_target_action_version_id, person_id,
	            campaign_id, action_id, result_id, result_status, result_flag_recipient_replied,
	            result_created_at, action_add_to_target_state, action_target_li_account_id)
	        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, 1)`,
		id, personID, actionVersionID, personID, campaignID, actionID, result, status, repliedFlag, createdAt)
}

// seedSent writes the rows the thread reader joins for one outbound message: the
// action_results attribution row, the message with its stable external id, and
// the action_result_messages link marked 'Sent'. The caller points a pich row's
// result_id at actionResultID so the message joins into the person's thread.
func seedSent(s *seeder, actionResultID, actionVersionID, personID, messageID int, externalID, messageText, createdAt string) {
	s.exec(`INSERT INTO action_results(id, action_version_id, person_id, result, target_platform, li_account_id, created_at)
	        VALUES (?, ?, ?, 0, 'linkedin', 1, ?)`,
		actionResultID, actionVersionID, personID, createdAt)
	s.exec(`INSERT INTO messages(id, type, message_text, send_at, created_at, updated_at)
	        VALUES (?, 'DEFAULT', ?, ?, ?, ?)`,
		messageID, messageText, createdAt, createdAt, createdAt)
	s.exec(`INSERT INTO message_external_ids(id, message_id, external_id, li_account_id, created_at, updated_at)
	        VALUES (?, ?, ?, 1, ?, ?)`,
		messageID, messageID, externalID, createdAt, createdAt)
	s.exec(`INSERT INTO action_result_messages(id, action_result_id, message_id, type, created_at, updated_at)
	        VALUES (?, ?, ?, 'Sent', ?, ?)`,
		messageID, actionResultID, messageID, createdAt, createdAt)
}

// seedCampaign: owner as in owner-v205 plus one full regular campaign — four
// steps (Invite → CheckForReplies(4d) → Message → VisitProfile), engagement
// history covering queued/messaged/replied(status)/replied(flag), and the
// daily-limit rows (General 90, Invite cap 25).
func seedCampaign(db *sql.DB) error {
	s := &seeder{db: db}
	seedVersion(s, 205)
	liAccountsFull(s, ptr(ownerName), ptr(ownerEmail), ptr(ownerAvatar))
	selfPerson(s, personOpts{withPublic: true, withEmail: true, miniAvatar: ownerAvatar})
	ssiSnapshotRow(s)

	const cID = 500
	seedCampaignRow(s, cID, "camp-uuid-500", "Test Campaign", "A test campaign", 2, 0, 0, 1)
	seedWorkflow(s, cID, 7, []workflowStep{
		{actionID: 101, configID: 201, versionID: 301, cvaID: 1, actionType: "InvitePerson", settings: inviteSettings},
		{actionID: 102, configID: 202, versionID: 302, cvaID: 2, actionType: "CheckForReplies", settings: checkSettings},
		{actionID: 103, configID: 203, versionID: 303, cvaID: 3, actionType: "MessageToPerson", settings: messageSettings},
		{actionID: 104, configID: 204, versionID: 304, cvaID: 4, actionType: "VisitProfile", settings: visitSettings},
	})

	// Engagement: p1 queued-only, p2 messaged (Invite+Message), p3 replied via
	// status=2 on CheckForReplies, p4 replied via flag on CheckForReplies. The
	// resultID column links a processed row to its action_results row so the
	// thread reader can walk the person's messages (see seedSent below).
	pich(s, 1, 1, cID, 101, 301, 0, -999, 0, "2026-01-05T10:00:00.000Z")
	pich(s, 2, 2, cID, 101, 301, 2, 1, 0, "2026-01-05T11:00:00.000Z")
	pich(s, 3, 3, cID, 101, 301, 4, 1, 0, "2026-01-05T12:00:00.000Z")
	pich(s, 4, 4, cID, 101, 301, 0, 1, 0, "2026-01-06T10:00:00.000Z")
	pich(s, 5, 3, cID, 102, 302, 1, 2, 0, "2026-01-07T10:00:00.000Z")
	pich(s, 6, 4, cID, 102, 302, 0, 1, 1, "2026-01-10T12:00:00.000Z") // latest → LastActivityAt
	pich(s, 7, 2, cID, 103, 303, 3, 1, 0, "2026-01-08T10:00:00.000Z")

	// Outbound messages that make up the threads: p2's Invite + Message and p3's
	// Invite. Each carries a stable external id and personalized text; the pich
	// rows above point their result_id at these action_results.
	seedSent(s, 2, 301, 2, 2, "2-sent-invite-p2", "Hi Sam, let's connect!", "2026-01-05T11:00:00.000Z")
	seedSent(s, 3, 303, 2, 3, "2-sent-message-p2", "Thanks for connecting, Sam!", "2026-01-08T10:00:00.000Z")
	seedSent(s, 4, 301, 3, 4, "2-sent-invite-p3", "Hi Jane, let's connect!", "2026-01-05T12:00:00.000Z")

	// One inbound reply from p3 on the CheckForReplies step (action_version 302),
	// with its own LinkedIn identity. send_at precedes created_at to mirror LH's
	// detection-lags-the-message reality the cursor must tolerate. pich row 5
	// (p3's CheckForReplies) links to this action_result (id 1) so the reply
	// closes p3's thread after the Invite send above.
	seedReply(s, replyOpts{
		actionResultID:  1,
		actionVersionID: 302,
		personID:        3,
		messageID:       1,
		externalID:      "2-reply-ext-id-0001",
		messageText:     "Thanks for reaching out — happy to chat next week.",
		sentAt:          "2026-01-07T09:55:00.000Z",
		detectedAt:      "2026-01-07T18:30:00.000Z",
		first:           "Jane",
		last:            "Prospect",
		member:          "555666777",
		public:          "jane-prospect",
		headline:        "Head of Engineering at Acme",
	})

	// The chat mirror for p3: LH's copy of the same conversation, plus the two
	// messages that only ever existed in LinkedIn — our manual answer to Jane's
	// reply and her manual follow-up. Neither carries an
	// action_result_messages link, so the action-linked readers cannot see them.
	seedChat(s, chatOpts{
		chatID:        1,
		personID:      3,
		participantID: 1,
		ownerID:       2,
		linkedMessages: []linkedChatMessage{
			{messageID: 4, fromPerson: false},
			{messageID: 1, fromPerson: true},
		},
		manualMessages: []manualChatMessage{
			// Detection order is deliberately the REVERSE of send order: LH
			// imported this chat in one pass long after the fact, and within
			// that pass row insertion says nothing about when a message was
			// sent. A reader that timestamps or sorts by created_at puts these
			// two the wrong way round — which is the bug the thread tests lock.
			{
				messageID:  10,
				externalID: "2-manual-ours-0010",
				text:       "Great — booked us Thursday 3pm, talk then!",
				fromPerson: false,
				sentAt:     "2026-01-07T19:02:00.000Z",
				detectedAt: "2026-02-01T10:00:00.000Z",
			},
			{
				messageID:  11,
				externalID: "2-manual-theirs-0011",
				text:       "Perfect, see you Thursday.",
				fromPerson: true,
				sentAt:     "2026-01-08T08:10:00.000Z",
				detectedAt: "2026-02-01T09:00:00.000Z",
			},
			// Written years before this campaign ever touched Jane: the owner
			// already knew her, and LH mirrors that whole history into the same
			// chat. It belongs in the conversation but is no answer to our
			// outreach, so it must never reach the reply feed.
			{
				messageID:  12,
				externalID: "2-manual-preoutreach-0012",
				text:       "Hey! Long time no see — how have you been?",
				fromPerson: true,
				sentAt:     "2023-05-04T11:00:00.000Z",
				detectedAt: "2026-02-01T08:00:00.000Z",
			},
		},
	})

	seedDailyLimits(s, 90, 25)
	return s.err
}

// linkedChatMessage points the chat mirror at a message the action-linked
// readers already return, so the merge is exercised on messages both sources
// hold.
type linkedChatMessage struct {
	messageID  int
	fromPerson bool
}

// manualChatMessage is a message that exists only in the chat mirror — typed by
// hand in LinkedIn, never linked to an action result.
type manualChatMessage struct {
	messageID  int
	externalID string
	text       string
	fromPerson bool
	sentAt     string
	detectedAt string
}

// chatOpts declares one mirrored conversation between the account owner and a
// campaign person. participantID/ownerID are the two chat_participants rows a
// message hangs on; which one it hangs on is what the readers derive direction
// from.
type chatOpts struct {
	chatID         int
	personID       int
	participantID  int
	ownerID        int
	linkedMessages []linkedChatMessage
	manualMessages []manualChatMessage
}

func seedChat(s *seeder, o chatOpts) {
	s.exec(`INSERT INTO chats(id, type, platform, li_account_id, created_at, updated_at)
	        VALUES (?, 'DIRECT', 'linkedin', 1, ?, ?)`, o.chatID, fixedTime, fixedTime)
	s.exec(`INSERT INTO chat_participants(id, chat_id, person_id, created_at, updated_at)
	        VALUES (?, ?, ?, ?, ?)`, o.participantID, o.chatID, o.personID, fixedTime, fixedTime)
	s.exec(`INSERT INTO chat_participants(id, chat_id, person_id, created_at, updated_at)
	        VALUES (?, ?, ?, ?, ?)`, o.ownerID, o.chatID, selfPersonID, fixedTime, fixedTime)

	participant := func(fromPerson bool) int {
		if fromPerson {
			return o.participantID
		}
		return o.ownerID
	}
	link := func(id, participantID, messageID int) {
		s.exec(`INSERT INTO participant_messages(id, chat_participant_id, message_id, created_at, updated_at)
		        VALUES (?, ?, ?, ?, ?)`, id, participantID, messageID, fixedTime, fixedTime)
	}

	for _, m := range o.linkedMessages {
		link(m.messageID, participant(m.fromPerson), m.messageID)
	}
	for _, m := range o.manualMessages {
		s.exec(`INSERT INTO messages(id, type, message_text, send_at, created_at, updated_at)
		        VALUES (?, 'DEFAULT', ?, ?, ?, ?)`,
			m.messageID, m.text, m.sentAt, m.detectedAt, m.detectedAt)
		s.exec(`INSERT INTO message_external_ids(id, message_id, external_id, li_account_id, created_at, updated_at)
		        VALUES (?, ?, ?, 1, ?, ?)`,
			m.messageID, m.messageID, m.externalID, fixedTime, fixedTime)
		link(m.messageID, participant(m.fromPerson), m.messageID)
	}
}

// replyOpts declares one inbound reply plus the replier's LinkedIn identity.
type replyOpts struct {
	actionResultID  int
	actionVersionID int
	personID        int
	messageID       int
	externalID      string
	messageText     string
	sentAt          string
	detectedAt      string
	first, last     string
	member, public  string
	headline        string
}

// seedReply writes the rows ReadCampaignReplies joins: the replier's
// mini-profile and member/public external ids, an action_results attribution
// row, the message with its stable external id, and the action_result_messages
// link marked 'Replied'.
func seedReply(s *seeder, o replyOpts) {
	s.exec(`INSERT INTO person_mini_profile(id, person_id, first_name, first_name_uppercase, last_name, last_name_uppercase, headline, created_at, updated_at)
	        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		o.personID, o.personID, o.first, upper(o.first), o.last, upper(o.last), o.headline, fixedTime, fixedTime)
	s.exec(`INSERT INTO person_external_ids(id, person_id, external_id, external_id_uppercase, type_group, is_member_id, created_at, updated_at)
	        VALUES (?, ?, ?, ?, 'member', 1, ?, ?)`,
		o.personID*10, o.personID, o.member, upper(o.member), fixedTime, fixedTime)
	s.exec(`INSERT INTO person_external_ids(id, person_id, external_id, external_id_uppercase, type_group, is_member_id, created_at, updated_at)
	        VALUES (?, ?, ?, ?, 'public', NULL, ?, ?)`,
		o.personID*10+1, o.personID, o.public, upper(o.public), fixedTime, fixedTime)

	s.exec(`INSERT INTO action_results(id, action_version_id, person_id, result, target_platform, li_account_id, created_at)
	        VALUES (?, ?, ?, 0, 'linkedin', 1, ?)`,
		o.actionResultID, o.actionVersionID, o.personID, o.detectedAt)
	s.exec(`INSERT INTO messages(id, type, message_text, send_at, created_at, updated_at)
	        VALUES (?, 'DEFAULT', ?, ?, ?, ?)`,
		o.messageID, o.messageText, o.sentAt, o.detectedAt, o.detectedAt)
	s.exec(`INSERT INTO message_external_ids(id, message_id, external_id, li_account_id, created_at, updated_at)
	        VALUES (?, ?, ?, 1, ?, ?)`,
		o.messageID, o.messageID, o.externalID, fixedTime, fixedTime)
	s.exec(`INSERT INTO action_result_messages(id, action_result_id, message_id, type, created_at, updated_at)
	        VALUES (?, ?, ?, 'Replied', ?, ?)`,
		o.actionResultID, o.actionResultID, o.messageID, o.detectedAt, o.detectedAt)
}

// seedScraper: a campaign whose steps are only extract/visit/webhook types, so
// classification yields scraper and the agent drops it.
func seedScraper(db *sql.DB) error {
	s := &seeder{db: db}
	seedVersion(s, 205)
	liAccountsFull(s, ptr(ownerName), ptr(ownerEmail), ptr(ownerAvatar))

	const cID = 600
	seedCampaignRow(s, cID, "camp-uuid-600", "Scraper Campaign", "Extraction only", 2, 0, 0, 1)
	seedWorkflow(s, cID, 8, []workflowStep{
		{actionID: 110, configID: 210, versionID: 310, cvaID: 5, actionType: "VisitProfile", settings: visitSettings},
		{actionID: 111, configID: 211, versionID: 311, cvaID: 6, actionType: "CollectFromList", settings: visitSettings},
		{actionID: 112, configID: 212, versionID: 312, cvaID: 7, actionType: "SendWebhook", settings: visitSettings},
	})
	return s.err
}

// seedDailyLimits writes daily_limits.max_limit (General) and an Invite-type
// per-day cap via limit_types + limit_type_period_max_credits.
func seedDailyLimits(s *seeder, general, invite int) {
	s.exec(`INSERT INTO daily_limits(id, li_account_id, max_limit) VALUES (1, 1, ?)`, general)
	s.exec(`INSERT INTO limit_types(id, type, created_at, updated_at) VALUES (1, 'Invite', ?, ?)`, fixedTime, fixedTime)
	s.exec(`INSERT INTO limit_type_period_max_credits(
	            id, limit_type_id, period, max_credits, is_deleted, li_account_id, created_at, updated_at)
	        VALUES (1, 1, 86400, ?, 0, 1, ?, ?)`, invite, fixedTime, fixedTime)
}
