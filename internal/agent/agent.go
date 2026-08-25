package agent

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/leadget/lh-agent/internal/client"
	"github.com/leadget/lh-agent/internal/lh"
)

const (
	defaultReportInterval = 600 * time.Second
	minReportInterval     = 30 * time.Second
	maxReportInterval     = 60 * time.Minute

	accountCycleTimeout = 2 * time.Minute

	// replyBatchLimit caps replies shipped per account per cycle. Sync is
	// forward-only from a platform-issued cursor, so steady state is a handful
	// of rows; the cap only bounds a catch-up after the agent was offline for a
	// while. Set far above any same-millisecond detection cluster so a `>=`
	// cursor always advances past its ties.
	replyBatchLimit = 500

	// sendBatchLimit caps per-person sends shipped per campaign per cycle. Sends
	// are higher-volume than replies (one per recipient×step), so the first
	// backfill of an established campaign is spread across cycles by this cap;
	// steady state is a handful of rows. Same `>=` cursor + dedup as replies.
	sendBatchLimit = 1000
)

// Agent ties the LH reader to the API client and runs them on a ticker.
type Agent struct {
	cfg     *Config
	client  *client.Client
	reader  *lh.Reader
	known   *known
	version string
	agentID string

	mu             sync.RWMutex
	reportInterval time.Duration
}

func New(cfg *Config, version string) *Agent {
	id, err := LoadOrCreateAgentID()
	if err != nil {
		log.WithError(err).Warn("agent id init failed; platform will match by hostname")
	}
	return &Agent{
		cfg:            cfg,
		client:         client.New(cfg.APIEndpoint, cfg.APIKey, cfg.DisableKeepAlive),
		reader:         lh.NewReader(),
		known:          newKnown(),
		version:        version,
		agentID:        id,
		reportInterval: defaultReportInterval,
	}
}

// Run blocks until ctx is cancelled.
func (a *Agent) Run(ctx context.Context) {
	log.WithFields(log.Fields{
		"endpoint":      a.cfg.APIEndpoint,
		"partitionsDir": a.cfg.PartitionsDir,
	}).Info("agent starting")

	defer func() {
		if err := a.reader.Close(); err != nil {
			log.WithError(err).Warn("reader close failed")
		}
	}()

	a.cycle(ctx)

	ticker := time.NewTicker(a.getReportInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("agent stopping")
			return
		case <-ticker.C:
			prev := a.getReportInterval()
			a.cycle(ctx)
			if next := a.getReportInterval(); next != prev {
				ticker.Reset(next)
				log.WithFields(log.Fields{"prev": prev, "next": next}).
					Info("report interval changed")
			}
		}
	}
}

// bootstrap refreshes the known-state cache from the server. It runs at the
// start of every cycle, not just at process start: reports are built from
// this cache concurrently, so an account that reports first would otherwise
// never observe a server-side change (e.g. a reply-cursor reset) — its own
// boundary re-send would overwrite the change before any report response
// could bring the fresh state back. Failure is non-fatal: the cycle proceeds
// on the cached (possibly stale) state, exactly as it would have anyway.
func (a *Agent) bootstrap(ctx context.Context) {
	hostname, _ := os.Hostname()
	accounts, err := lh.Scan(a.cfg.PartitionsDir)
	partitionsCount := 0
	if err == nil {
		partitionsCount = len(accounts)
	}

	resp, err := a.client.Bootstrap(ctx, &client.BootstrapRequest{
		AgentID:         a.agentID,
		AgentVersion:    a.version,
		Hostname:        hostname,
		OS:              runtime.GOOS,
		PartitionsCount: partitionsCount,
	})
	if err != nil {
		log.WithError(err).Warn("bootstrap failed, will continue with empty known state")
		return
	}

	a.setReportInterval(time.Duration(resp.ReportInterval) * time.Second)
	a.known.replace(resp.KnownState)
	log.WithFields(log.Fields{
		"agentId":        a.agentID,
		"integrationId":  resp.IntegrationID,
		"reportInterval": resp.ReportInterval,
		"enabled":        resp.Enabled,
		"knownAccounts":  len(resp.KnownState.Accounts),
		"knownCampaigns": len(resp.KnownState.Campaigns),
	}).Info("bootstrap ok")
}

// cycle scans partitions and syncs each account in parallel, bounded by
// NumCPU (SQLite reads are the CPU-bound part).
func (a *Agent) cycle(ctx context.Context) {
	// LH keeps lh.db on a rollback journal, where our reader and its writer
	// cannot both hold the file. Between cycles we have no reason to be on it at
	// all, and LH — which needs an exclusive lock to recover its journal when it
	// starts — has every reason to find it free.
	defer func() {
		if err := a.reader.ReleaseHandles(); err != nil {
			log.WithError(err).Warn("release lh.db handles failed")
		}
	}()

	a.bootstrap(ctx)

	accounts, err := lh.Scan(a.cfg.PartitionsDir)
	if err != nil {
		log.WithError(err).Error("scan partitions failed")
		return
	}
	if len(accounts) == 0 {
		log.WithField("dir", a.cfg.PartitionsDir).Warn("no LH accounts found")
		return
	}

	concurrency := runtime.NumCPU()
	if concurrency > len(accounts) {
		concurrency = len(accounts)
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for _, acc := range accounts {
		acc := acc
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			a.syncAccountSafe(ctx, acc)
		}()
	}
	wg.Wait()
}

// syncAccountSafe isolates one account's sync: a single corrupt lh.db must
// not take the whole agent down, so panics are logged and the next cycle
// retries the account from scratch.
func (a *Agent) syncAccountSafe(ctx context.Context, acc lh.Account) {
	defer func() {
		if r := recover(); r != nil {
			log.WithFields(log.Fields{
				"accountId": acc.ID,
				"panic":     r,
				"stack":     string(debug.Stack()),
			}).Error("syncAccount panicked")
		}
	}()
	a.syncAccount(ctx, acc)
}

func (a *Agent) syncAccount(ctx context.Context, acc lh.Account) {
	accCtx, cancel := context.WithTimeout(ctx, accountCycleTimeout)
	defer cancel()

	campaigns, err := a.reader.ReadCampaigns(accCtx, acc.DBPath, "")
	if err != nil {
		log.WithError(err).WithField("accountId", acc.ID).Error("read campaigns failed")
		return
	}

	funnels, err := a.reader.ReadFunnels(accCtx, acc.DBPath)
	if err != nil {
		log.WithError(err).WithField("accountId", acc.ID).Error("read funnels failed")
		return
	}

	owner, _ := a.reader.ReadAccountOwner(accCtx, acc.DBPath)
	limits, _ := a.reader.ReadDailyLimits(accCtx, acc.DBPath)

	kindByCampaign, stepsByCampaign, seqByCampaign := a.classifyCampaigns(accCtx, acc, campaigns)
	actionsByCampaign := a.loadActionsForRegister(accCtx, acc, campaigns, kindByCampaign)

	req := a.buildReport(acc.ID, campaigns, funnels, owner, actionsByCampaign, kindByCampaign, stepsByCampaign, limits)
	if req == nil {
		log.WithField("accountId", acc.ID).Debug("skipping empty partition")
		return
	}

	a.appendReplies(accCtx, acc, campaigns, kindByCampaign, seqByCampaign, req)
	a.appendSends(accCtx, acc, campaigns, kindByCampaign, seqByCampaign, req)

	resp, err := a.client.Report(accCtx, acc.ID, req)
	if err != nil {
		log.WithError(err).WithField("accountId", acc.ID).Error("report failed")
		return
	}

	a.known.replace(resp.KnownState)
	if resp.ReportInterval > 0 {
		a.setReportInterval(time.Duration(resp.ReportInterval) * time.Second)
	}

	log.WithFields(log.Fields{
		"accountId":           acc.ID,
		"campaignsLocal":      len(campaigns),
		"registerAccount":     req.RegisterAccount != nil,
		"registerCampaigns":   len(req.RegisterCampaigns),
		"funnels":             len(req.Funnels),
		"replies":             len(req.Replies),
		"sends":               len(req.Sends),
		"accountRegistered":   resp.Applied.AccountRegistered,
		"campaignsRegistered": resp.Applied.CampaignsRegistered,
		"funnelsApplied":      resp.Applied.FunnelsApplied,
		"funnelsSkipped":      resp.Applied.FunnelsSkipped,
	}).Info("account synced")
}

// classifyCampaigns walks each campaign's step stats once, producing both its
// kind (inmail / regular / scraper) and the per-message funnel. A failed read
// defaults to regular so a real outreach campaign is never silently dropped.
func (a *Agent) classifyCampaigns(
	ctx context.Context,
	acc lh.Account,
	campaigns []lh.Campaign,
) (map[int64]string, map[int64][]client.FunnelStep, map[int64]map[int64]int) {
	kinds := make(map[int64]string, len(campaigns))
	steps := make(map[int64][]client.FunnelStep, len(campaigns))
	seqs := make(map[int64]map[int64]int, len(campaigns))
	for _, c := range campaigns {
		stats, err := a.reader.ReadCampaignStepStats(ctx, acc.DBPath, c.ID)
		if err != nil {
			log.WithError(err).WithFields(log.Fields{
				"accountId":  acc.ID,
				"campaignId": c.ID,
			}).Warn("read step stats failed, treating as regular")
			kinds[c.ID] = lh.KindRegular
			continue
		}
		types := make([]string, 0, len(stats))
		for _, s := range stats {
			types = append(types, s.Type)
		}
		kinds[c.ID] = lh.ClassifyLinkedinKind(types)
		steps[c.ID] = buildFunnelSteps(stats)
		seqs[c.ID] = buildActionSeqMap(stats)
	}
	return kinds, steps, seqs
}

// loadActionsForRegister reads step definitions only for the non-scraper
// campaigns that need (re)registering — steady-state cycles do zero extra IO.
func (a *Agent) loadActionsForRegister(
	ctx context.Context,
	acc lh.Account,
	campaigns []lh.Campaign,
	kindByCampaign map[int64]string,
) map[int64][]lh.CampaignActionRow {
	actions := make(map[int64][]lh.CampaignActionRow)
	for _, c := range campaigns {
		if kindByCampaign[c.ID] == lh.KindScraper {
			continue
		}
		if !a.known.needsRegister(acc.ID, int(c.ID), c.Version) {
			continue
		}
		rows, err := a.reader.ReadCampaignActions(ctx, acc.DBPath, c.ID)
		if err != nil {
			log.WithError(err).WithFields(log.Fields{
				"accountId":  acc.ID,
				"campaignId": c.ID,
			}).Warn("read campaign actions failed, sending campaign without messages")
			continue
		}
		actions[c.ID] = rows
	}
	return actions
}

// buildReport assembles the per-account payload: the full account snapshot,
// register blocks for campaigns the platform is missing, and the
// always-overwriting funnel counters. Returns nil for an empty partition the
// platform doesn't know yet — common for a freshly added LH login the user
// never opened, which should not spawn a placeholder Client.
func (a *Agent) buildReport(
	accountID int,
	campaigns []lh.Campaign,
	funnels map[int64]lh.Funnel,
	owner *lh.AccountOwner,
	actionsByCampaign map[int64][]lh.CampaignActionRow,
	kindByCampaign map[int64]string,
	stepsByCampaign map[int64][]client.FunnelStep,
	limits lh.DailyLimits,
) *client.AccountReportRequest {
	req := &client.AccountReportRequest{
		AgentID:           a.agentID,
		SyncedAt:          time.Now().UTC().Format(time.RFC3339),
		RegisterCampaigns: []client.RegisterCampaign{},
		Funnels:           make([]client.CampaignFunnel, 0, len(campaigns)),
		Replies:           []client.CampaignReply{},
		Sends:             []client.CampaignSend{},
	}

	hasOwner := owner != nil && hasOwnerSignal(owner)
	if hasOwner {
		req.RegisterAccount = buildRegisterAccount(owner)
	} else if !a.known.hasAccount(accountID) {
		if len(campaigns) == 0 {
			return nil
		}
		req.RegisterAccount = &client.RegisterAccount{}
	}

	for _, c := range campaigns {
		cid := int(c.ID)

		if kindByCampaign[c.ID] == lh.KindScraper {
			continue
		}

		// Backfill (same version, platform has no messages) only fires when
		// there are message-bearing actions to contribute — a campaign whose
		// steps carry no bodies (e.g. note-less InvitePerson) would otherwise
		// re-register every cycle forever.
		kc, isKnown := a.known.lookup(accountID, cid)
		versionChanged := !isKnown || kc.Version != c.Version
		actions := actionsByCampaign[c.ID]
		needsBackfill := isKnown && !kc.HasMessages && hasMessageActions(actions)
		if versionChanged || needsBackfill {
			req.RegisterCampaigns = append(req.RegisterCampaigns, client.RegisterCampaign{
				CampaignID:     cid,
				Name:           c.Name,
				Description:    c.Description,
				Type:           c.Type,
				IsPaused:       c.IsPaused,
				IsArchived:     c.IsArchived,
				CreatedAt:      c.CreatedAt,
				Version:        c.Version,
				MessagesPerDay: limits.MessagesPerDayFor(actions),
				LinkedinKind:   kindByCampaign[c.ID],
				Actions:        buildActions(actions),
			})
		}

		f := funnels[c.ID]
		req.Funnels = append(req.Funnels, client.CampaignFunnel{
			CampaignID:     cid,
			Messaged:       f.Messaged,
			Replied:        f.Replied,
			Target:         f.Target,
			IsPaused:       c.IsPaused,
			IsArchived:     c.IsArchived,
			Name:           c.Name,
			Description:    c.Description,
			LastActivityAt: f.LastActivityAt,
			LinkedinKind:   kindByCampaign[c.ID],
			Steps:          stepsByCampaign[c.ID],
		})
	}

	return req
}

// appendReplies fills req.Replies, but only for campaigns the platform already
// knows (present in a.known): a campaign registered this very cycle has no
// known-state entry yet, so its replies wait for the next cycle — that's what
// keeps the first report for a new account reply-free. For each known campaign
// we read from its per-campaign cursor (empty = backfill all), and the platform
// dedupes on ExternalID so the `>=` boundary re-reads cost nothing.
func (a *Agent) appendReplies(
	ctx context.Context,
	acc lh.Account,
	campaigns []lh.Campaign,
	kindByCampaign map[int64]string,
	seqByCampaign map[int64]map[int64]int,
	req *client.AccountReportRequest,
) {
	for _, c := range campaigns {
		if kindByCampaign[c.ID] == lh.KindScraper {
			continue
		}
		kc, known := a.known.lookup(acc.ID, int(c.ID))
		if !known {
			continue
		}
		rows, err := a.reader.ReadCampaignReplies(ctx, acc.DBPath, c.ID, kc.ReplyCursor, replyBatchLimit)
		if err != nil {
			log.WithError(err).WithFields(log.Fields{
				"accountId":  acc.ID,
				"campaignId": c.ID,
			}).Warn("read replies failed, skipping campaign's replies this cycle")
			continue
		}
		replies := buildReplies(rows)
		a.attachThreads(ctx, acc, c.ID, kc.ReplyCursor, rows, replies, seqByCampaign[c.ID])
		req.Replies = append(req.Replies, replies...)
	}
}

// attachThreads hangs the full conversation off each reply. rows and replies are
// index-parallel (buildReplies preserves order), so rows[i].PersonID is the
// person to reconstruct for replies[i]. A failed thread read leaves that reply
// thread-less rather than dropping the reply — the reply itself already carries
// the person's answer.
//
// The `>=` reply cursor re-reads its boundary row every cycle for dedup safety;
// that boundary reply's thread was already delivered when it was new, so we skip
// re-attaching it and only ship threads for replies strictly newer than the
// cursor. An empty cursor (first sync / backfill) is older than every row, so
// all replies get their thread once.
func (a *Agent) attachThreads(
	ctx context.Context,
	acc lh.Account,
	campaignID int64,
	replyCursor string,
	rows []lh.CampaignReply,
	replies []client.CampaignReply,
	seqByAction map[int64]int,
) {
	for i := range replies {
		if replyCursor != "" && rows[i].DetectedAt <= replyCursor {
			continue
		}
		messages, err := a.reader.ReadCampaignThread(ctx, acc.DBPath, campaignID, rows[i].PersonID)
		if err != nil {
			log.WithError(err).WithFields(log.Fields{
				"accountId":  acc.ID,
				"campaignId": campaignID,
				"personId":   rows[i].PersonID,
			}).Warn("read thread failed, shipping reply without its thread")
			continue
		}
		replies[i].Thread = buildThread(acc.ID, int(campaignID), messages, seqByAction)
	}
}

// buildReplies maps LH reply rows to the wire format. The platform dedupes on
// ExternalID, so the boundary row(s) the `>=` cursor re-reads each cycle are
// harmless.
func buildReplies(replies []lh.CampaignReply) []client.CampaignReply {
	out := make([]client.CampaignReply, 0, len(replies))
	for _, r := range replies {
		out = append(out, client.CampaignReply{
			CampaignID: int(r.CampaignID),
			ExternalID: r.ExternalID,
			Person: client.ReplyPerson{
				ExternalID: r.MemberID,
				ProfileURL: r.ProfileURL,
				FullName:   r.FullName,
				Headline:   r.Headline,
			},
			Subject:    r.Subject,
			Text:       r.Text,
			SentAt:     r.SentAt,
			DetectedAt: r.DetectedAt,
		})
	}
	return out
}

// buildThread maps LH thread messages to the wire format. ExternalID is
// namespaced the same way sends are (account:campaign:localMessageId) since LH's
// message id is unique only within one lh.db. Outbound messages take the seq
// number of the messaging step that produced them (via the same action->seq map
// the funnel and sends use); inbound replies carry no seq.
func buildThread(accountID, campaignID int, messages []lh.ThreadMessage, seqByAction map[int64]int) []client.ThreadMessage {
	out := make([]client.ThreadMessage, 0, len(messages))
	for _, m := range messages {
		msg := client.ThreadMessage{
			ExternalID: fmt.Sprintf("%d:%d:%d", accountID, campaignID, m.MessageID),
			Direction:  m.Direction,
			Body:       m.Body,
			OccurredAt: m.OccurredAt,
		}
		if m.Direction == lh.DirectionOutbound {
			if seq, ok := seqByAction[m.ActionID]; ok {
				s := seq
				msg.SeqNumber = &s
			}
		}
		out = append(out, msg)
	}
	return out
}

// appendSends fills req.Sends, mirroring appendReplies: only campaigns the
// platform already knows (present in a.known) ship sends, each read from its
// per-campaign SendCursor (empty = backfill all). The platform dedupes on the
// namespaced ExternalID so the `>=` boundary re-reads are harmless. Never
// touches funnel counters — those stay the aggregate source of truth.
func (a *Agent) appendSends(
	ctx context.Context,
	acc lh.Account,
	campaigns []lh.Campaign,
	kindByCampaign map[int64]string,
	seqByCampaign map[int64]map[int64]int,
	req *client.AccountReportRequest,
) {
	for _, c := range campaigns {
		if kindByCampaign[c.ID] == lh.KindScraper {
			continue
		}
		kc, known := a.known.lookup(acc.ID, int(c.ID))
		if !known {
			continue
		}
		rows, err := a.reader.ReadCampaignSends(ctx, acc.DBPath, c.ID, kc.SendCursor, sendBatchLimit)
		if err != nil {
			log.WithError(err).WithFields(log.Fields{
				"accountId":  acc.ID,
				"campaignId": c.ID,
			}).Warn("read sends failed, skipping campaign's sends this cycle")
			continue
		}
		req.Sends = append(req.Sends, buildSends(acc.ID, rows, seqByCampaign[c.ID])...)
	}
}

// buildSends maps LH send rows to the wire format, keeping ONLY messaging-step
// results — a person_in_campaigns_history row exists for every action
// (CheckForReplies, VisitProfile, …), but only messaging actions are real
// outbound sends. Membership in seqByAction (built from the same messaging-type
// walk as the funnel) is the filter, so "sends" line up with the funnel's
// "sent". ExternalID is namespaced (account:campaign:localId) because LH's
// per-person-history id is unique only within one lh.db; the platform dedupes
// on the result.
func buildSends(accountID int, sends []lh.CampaignSend, seqByAction map[int64]int) []client.CampaignSend {
	out := make([]client.CampaignSend, 0, len(sends))
	for _, s := range sends {
		seq, isMessaging := seqByAction[s.ActionID]
		if !isMessaging {
			continue
		}
		out = append(out, client.CampaignSend{
			CampaignID: int(s.CampaignID),
			ExternalID: fmt.Sprintf("%d:%d:%s", accountID, s.CampaignID, s.ExternalID),
			Person: client.ReplyPerson{
				ExternalID: s.MemberID,
				ProfileURL: s.ProfileURL,
				FullName:   s.FullName,
				Headline:   s.Headline,
			},
			SeqNumber:  seq,
			SentAt:     s.SentAt,
			DetectedAt: s.DetectedAt,
		})
	}
	return out
}

// buildActionSeqMap maps each messaging action's id to its 1-based seq number,
// using the same messaging-type walk as buildFunnelSteps so a send's seqNumber
// lines up with the platform's CampaignMessage rows. Non-messaging steps are
// omitted (their sends, if any, resolve to seq 0).
func buildActionSeqMap(steps []lh.StepStat) map[int64]int {
	out := make(map[int64]int, len(steps))
	seq := 0
	for _, s := range steps {
		if _, isMessage := messageActionTypes[s.Type]; isMessage {
			seq++
			out[s.ActionID] = seq
		}
	}
	return out
}

func buildRegisterAccount(owner *lh.AccountOwner) *client.RegisterAccount {
	account := &client.RegisterAccount{
		Email:       owner.Email,
		FullName:    owner.FullName,
		Avatar:      owner.Avatar,
		LastLoginAt: owner.LastLoginAt,
		SSI:         owner.SSI,
	}
	if owner.ExternalID != nil {
		s := strconv.FormatInt(*owner.ExternalID, 10)
		account.ExternalID = &s
	}
	if owner.ProfileURL != nil || owner.PublicSlug != nil {
		account.Owner = &client.AccountOwnerRef{
			ProfileURL: owner.ProfileURL,
			PublicID:   owner.PublicSlug,
		}
	}
	return account
}

func (a *Agent) getReportInterval() time.Duration {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.reportInterval
}

func (a *Agent) setReportInterval(d time.Duration) {
	clamped := d
	if clamped < minReportInterval {
		clamped = minReportInterval
	}
	if clamped > maxReportInterval {
		clamped = maxReportInterval
	}
	a.mu.Lock()
	a.reportInterval = clamped
	a.mu.Unlock()
}

// messageActionTypes mirrors the platform's MESSAGE_ACTION_TYPES — the steps
// that become CampaignMessage rows and carry a seq number.
var messageActionTypes = map[string]struct{}{
	"MessageToPerson": {},
	"InvitePerson":    {},
	"InMail":          {},
}

// hasMessageActions mirrors the platform's syncMessages predicate: a step
// becomes a CampaignMessage only when it is a messaging type AND has a body.
func hasMessageActions(rows []lh.CampaignActionRow) bool {
	for _, r := range rows {
		if _, ok := messageActionTypes[r.Type]; ok && r.Body != nil {
			return true
		}
	}
	return false
}

// buildFunnelSteps turns per-action stats into per-message funnel rows. LH
// records a reply on the CheckForReplies step that gates the next message,
// not on the message itself, so each non-message step's replied is folded
// back onto the preceding messaging step.
func buildFunnelSteps(steps []lh.StepStat) []client.FunnelStep {
	var out []client.FunnelStep
	seq := 0
	lastMsgIdx := -1
	for _, s := range steps {
		if _, isMessage := messageActionTypes[s.Type]; isMessage {
			seq++
			out = append(out, client.FunnelStep{SeqNumber: seq, Sent: s.Sent, Replied: s.Replied})
			lastMsgIdx = len(out) - 1
		} else if lastMsgIdx >= 0 && s.Replied > 0 {
			out[lastMsgIdx].Replied += s.Replied
		}
	}
	return out
}

// buildActions maps LH action rows to the wire format. LH encodes the wait
// between two messages as a separate step (CheckForReplies'
// moveToSuccessfulAfterMs), so accumulated waits are stamped as the delay
// BEFORE the next messaging action. action_configs.coolDown is deliberately
// ignored: it is a fixed per-dispatch throttle (~60s on every send), not the
// sequence cadence.
func buildActions(rows []lh.CampaignActionRow) []client.CampaignAction {
	out := make([]client.CampaignAction, 0, len(rows))
	var pendingWaitMs int64
	for _, r := range rows {
		a := client.CampaignAction{
			Type:           r.Type,
			Body:           r.Body,
			Example:        r.Example,
			Subject:        r.Subject,
			ExampleSubject: r.ExampleSubject,
		}
		if _, isMessage := messageActionTypes[r.Type]; isMessage {
			if pendingWaitMs > 0 {
				v, u := waitToDelay(pendingWaitMs)
				a.DelayValue = &v
				a.DelayUnit = &u
			}
			pendingWaitMs = 0
		} else if r.WaitMs != nil && *r.WaitMs > 0 {
			pendingWaitMs += *r.WaitMs
		}
		out = append(out, a)
	}
	return out
}

func waitToDelay(ms int64) (int, string) {
	const (
		minuteMs = int64(60_000)
		hourMs   = int64(60 * 60_000)
		dayMs    = int64(24 * 60 * 60_000)
	)
	switch {
	case ms >= dayMs && ms%dayMs == 0:
		return int(ms / dayMs), "DAYS"
	case ms >= hourMs && ms%hourMs == 0:
		return int(ms / hourMs), "HOURS"
	default:
		// Round up so sub-minute waits don't degrade to "0 minutes".
		v := (ms + minuteMs - 1) / minuteMs
		return int(v), "MINUTES"
	}
}

func hasOwnerSignal(o *lh.AccountOwner) bool {
	return o.ExternalID != nil || o.Email != nil || o.FullName != nil || o.ProfileURL != nil
}
