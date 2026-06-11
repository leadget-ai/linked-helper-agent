package agent

import (
	"context"
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

	a.bootstrap(ctx)
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

// bootstrap seeds the known-state cache. Failure is non-fatal: an empty cache
// treats everything as new and the first report response refreshes it.
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

	kindByCampaign, stepsByCampaign := a.classifyCampaigns(accCtx, acc, campaigns)
	actionsByCampaign := a.loadActionsForRegister(accCtx, acc, campaigns, kindByCampaign)

	req := a.buildReport(acc.ID, campaigns, funnels, owner, actionsByCampaign, kindByCampaign, stepsByCampaign, limits)
	if req == nil {
		log.WithField("accountId", acc.ID).Debug("skipping empty partition")
		return
	}

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
) (map[int64]string, map[int64][]client.FunnelStep) {
	kinds := make(map[int64]string, len(campaigns))
	steps := make(map[int64][]client.FunnelStep, len(campaigns))
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
	}
	return kinds, steps
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
			LastActivityAt: f.LastActivityAt,
			LinkedinKind:   kindByCampaign[c.ID],
			Steps:          stepsByCampaign[c.ID],
		})
	}

	return req
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
