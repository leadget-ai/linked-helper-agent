package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leadget/lh-agent/internal/client"
	"github.com/leadget/lh-agent/internal/lh"
)

func TestSameVersionCampaignReportsRealExamplesWithoutRegister(t *testing.T) {
	a := &Agent{reader: lh.NewReader(), known: newKnown(), agentID: "test-agent"}
	defer a.reader.Close()
	a.known.replace(client.KnownState{Accounts: []int{454999}, Campaigns: []client.KnownCampaign{{AccountID: 454999, CampaignID: 500, Version: 7, HasMessages: true}}})
	acc := lh.Account{ID: 454999, DBPath: filepath.Join("..", "lh", "testdata", "fixtures", "campaign-v205.db")}
	campaigns, err := a.reader.ReadCampaigns(context.Background(), acc.DBPath, "")
	if err != nil {
		t.Fatal(err)
	}
	kinds, steps, seqs := a.classifyCampaigns(context.Background(), acc, campaigns)
	actions := a.loadActionsForRegister(context.Background(), acc, campaigns, kinds)
	req := a.buildReport(acc.ID, campaigns, map[int64]lh.Funnel{}, nil, actions, kinds, steps, lh.DailyLimits{})
	if len(req.RegisterCampaigns) != 0 {
		t.Fatal("known unchanged campaign must not register again")
	}
	if len(req.Funnels) != 1 || len(req.Funnels[0].Steps) != 2 {
		t.Fatal("missing steady-state steps")
	}
	if len(seqs[500]) != 2 {
		t.Fatal("expected per-action sequence mapping")
	}
	sample := req.Funnels[0].Steps[0].SentExample
	if sample == nil || *sample == nil || **sample != "Hi Sam, let's connect!" {
		t.Fatal("steady report must include real stored outbound body")
	}
}

func TestMessageExampleWirePresence(t *testing.T) {
	steps := []client.FunnelStep{{SeqNumber: 1}, {SeqNumber: 2}}
	body := "Real body"
	attachMessageExamples(steps, map[int64]int{10: 1, 11: 2}, map[int64]lh.CampaignMessageExample{10: {Body: body}})
	wire, err := json.Marshal(steps)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(wire), `"sentExample":"Real body"`) || !strings.Contains(string(wire), `"sentExample":null`) || !strings.Contains(string(wire), `"sentExampleSubject":null`) {
		t.Fatal("supported schemas must distinguish actual samples from explicit null")
	}
	omitted, err := json.Marshal(client.FunnelStep{SeqNumber: 1})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(omitted), "sentExample") {
		t.Fatal("unsupported sample fields must be omitted")
	}
}

func TestBlankInviteDoesNotShiftMessageSamples(t *testing.T) {
	noBody, hasBody := false, true
	stats := []lh.StepStat{
		{ActionID: 10, Type: "InvitePerson", Sent: 9, HasMessageBody: &noBody},
		{ActionID: 11, Type: "CheckForReplies", Replied: 2, HasMessageBody: &noBody},
		{ActionID: 12, Type: "MessageToPerson", Sent: 8, HasMessageBody: &hasBody},
		{ActionID: 13, Type: "CheckForReplies", Replied: 3, HasMessageBody: &noBody},
		{ActionID: 14, Type: "MessageToPerson", Sent: 4, HasMessageBody: &hasBody},
	}
	steps := buildFunnelSteps(stats)
	seqs := buildActionSeqMap(stats)
	attachMessageExamples(steps, seqs, map[int64]lh.CampaignMessageExample{10: {Body: "Ignore blank invitation"}, 12: {Body: "First actual"}, 14: {Body: "Second actual"}})
	if len(steps) != 2 || seqs[12] != 1 || seqs[14] != 2 {
		t.Fatal("blank invite must not consume message sequence")
	}
	if steps[0].Sent != 8 || steps[0].Replied != 3 || **steps[0].SentExample != "First actual" || **steps[1].SentExample != "Second actual" {
		t.Fatal("counts and actual samples must share body-aware sequence")
	}
}
