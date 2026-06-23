package lh

import "testing"

func TestClassifyLinkedinKind(t *testing.T) {
	cases := []struct {
		name  string
		types []string
		want  string
	}{
		{"inmail wins over messaging", []string{"InMail", "InvitePerson"}, KindInMail},
		{"inmail alone", []string{"InMail"}, KindInMail},
		{"invite is regular", []string{"InvitePerson"}, KindRegular},
		{"message is regular", []string{"VisitProfile", "MessageToPerson"}, KindRegular},
		{"extract/visit only is scraper", []string{"VisitProfile", "CollectFromList", "SendWebhook"}, KindScraper},
		{"empty is scraper", nil, KindScraper},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyLinkedinKind(tc.types); got != tc.want {
				t.Errorf("ClassifyLinkedinKind(%v) = %q, want %q", tc.types, got, tc.want)
			}
		})
	}
}

func TestMessagesPerDayFor(t *testing.T) {
	invite := []CampaignActionRow{{Type: "InvitePerson"}}
	message := []CampaignActionRow{{Type: "MessageToPerson"}}

	cases := []struct {
		name    string
		limits  DailyLimits
		actions []CampaignActionRow
		want    *int // nil → no cap
	}{
		{"invite step uses Invite cap", DailyLimits{General: 90, Invite: 25}, invite, ptrInt(25)},
		{"message step uses General", DailyLimits{General: 90}, message, ptrInt(90)},
		{"invite with no Invite cap falls back to General", DailyLimits{General: 90}, invite, ptrInt(90)},
		{"no actions falls back to General", DailyLimits{General: 90}, nil, ptrInt(90)},
		{"no caps at all → nil", DailyLimits{}, invite, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.limits.MessagesPerDayFor(tc.actions)
			switch {
			case tc.want == nil && got != nil:
				t.Errorf("got %d, want nil", *got)
			case tc.want != nil && got == nil:
				t.Errorf("got nil, want %d", *tc.want)
			case tc.want != nil && got != nil && *got != *tc.want:
				t.Errorf("got %d, want %d", *got, *tc.want)
			}
		})
	}
}

func ptrInt(v int) *int { return &v }
