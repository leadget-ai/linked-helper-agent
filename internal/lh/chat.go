package lh

// chatTables are the tables the raw-chat readers join. Linked Helper mirrors
// every LinkedIn conversation it syncs into this store — including messages
// typed by hand in LinkedIn itself, which never get an action_result_messages
// link and are therefore invisible to the action-linked readers. Absent on
// schema generations that predate the chat store, in which case the raw path is
// skipped and only action-linked messages are returned, exactly as before.
var chatTables = []string{
	"chats",
	"chat_participants",
	"participant_messages",
	"messages",
}

// hasChatStore reports whether the partition carries LH's chat mirror.
func hasChatStore(profile *DBProfile) bool {
	for _, table := range chatTables {
		if !profile.HasTable(table) {
			return false
		}
	}
	return true
}
