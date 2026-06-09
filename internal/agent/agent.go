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

	// Per-account cycle gets its own short timeout so a misbehaving SQLite
	// query doesn't pin a goroutine forever.
	accountCycleTimeout = 2 * time.Minute
)

// Agent ties the LH reader to the API client and runs them on a ticker.
type Agent struct {
	cfg     *Config
	client  *client.Client
	reader  *lh.Reader
	known   *known
	version string
	// Persistent install id (UUID), read once at startup. Empty when the
	// data dir is unwritable — platform falls back to hostname matching.
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

// Run blocks until ctx is cancelled. Returns when the loop exits cleanly.
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

	// First cycle right away so the user sees data on the dashboard without
	// waiting for the first tick.
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

// bootstrap sends the one-shot identify call and seeds the known-state cache.
// Failure is non-fatal: the cache stays empty (treats everything as new) and
// the next cycle's report response will refresh it.
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

// cycle scans partitions and syncs each account in parallel (bounded).
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

	// Bound concurrency: SQLite reads are CPU-bound, the API is mostly
	// network-bound, so NumCPU is a reasonable upper bound.
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
			// One bad lh.db (corrupt SQLite header, an unexpected nil in
			// the driver, …) must not take the whole agent down. Log the
			// panic with stack and let the next cycle try this account
			// again from scratch.
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
		}()
	}
	wg.Wait()
}

func (a *Agent) syncAccount(ctx context.Context, acc lh.Account) {
	accCtx, cancel := context.WithTimeout(ctx, accountCycleTimeout)
	defer cancel()

	// Read everything we may need first; we still want a report attempt
	// even on a partial read because funnels are valuable on their own.
	// We read all campaigns (not just since-cursor) — the platform tells
	// us which ones it already knows about via the cache, and the response
	// will refresh that cache.
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

	// One read per cycle — used to pick the per-day cap for every campaign
	// without re-querying the limits tables per campaign.
	limits, _ := a.reader.ReadDailyLimits(accCtx, acc.DBPath)

	// One pass per campaign over its step stats: classifies the campaign
	// (inmail / regular / scraper, so scrapers are dropped) AND builds the
	// per-message funnel. A read failure defaults to regular so we never
	// silently lose a real outreach campaign.
	kindByCampaign := make(map[int64]string, len(campaigns))
	stepsByCampaign := make(map[int64][]client.FunnelStep, len(campaigns))
	for _, c := range campaigns {
		steps, err := a.reader.ReadCampaignStepStats(accCtx, acc.DBPath, c.ID)
		if err != nil {
			log.WithError(err).WithFields(log.Fields{
				"accountId":  acc.ID,
				"campaignId": c.ID,
			}).Warn("read step stats failed, treating as regular")
			kindByCampaign[c.ID] = lh.KindRegular
			continue
		}
		types := make([]string, 0, len(steps))
		for _, s := range steps {
			types = append(types, s.Type)
		}
		kindByCampaign[c.ID] = lh.ClassifyLinkedinKind(types)
		stepsByCampaign[c.ID] = buildFunnelSteps(steps)
	}

	// Preload step definitions only for non-scraper campaigns the platform
	// needs (re)registered — steady-state cycles do zero extra IO.
	actionsByCampaign := make(map[int64][]lh.CampaignActionRow)
	for _, c := range campaigns {
		if kindByCampaign[c.ID] == lh.KindScraper {
			continue
		}
		if !a.known.needsRegister(acc.ID, int(c.ID), c.Version) {
			continue
		}
		rows, err := a.reader.ReadCampaignActions(accCtx, acc.DBPath, c.ID)
		if err != nil {
			log.WithError(err).WithFields(log.Fields{
				"accountId":  acc.ID,
				"campaignId": c.ID,
			}).Warn("read campaign actions failed, sending campaign without messages")
			continue
		}
		actionsByCampaign[c.ID] = rows
	}

	req := a.buildReport(acc.ID, campaigns, funnels, owner, actionsByCampaign, kindByCampaign, stepsByCampaign, limits)
	if req == nil {
		// Partition is empty and not yet known to the platform — nothing to
		// send. The next cycle will check again.
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

// buildReport diffs local LH state against the cached known-state and returns
// only what the platform doesn't yet have, plus the always-overwriting
// funnel counters.
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

	// registerAccount goes only when (a) the platform doesn't know this LH
	// accountId yet AND (b) the partition has actually been used — either
	// the LH user signed in (owner info populated) or at least one campaign
	// exists. Empty partitions on disk are common (a freshly added LH login
	// the user hasn't opened yet) and should not spam Clients with
	// placeholder rows.
	if !a.known.hasAccount(accountID) && (owner != nil || len(campaigns) > 0) {
		req.RegisterAccount = &client.RegisterAccount{}
		if owner != nil && hasOwnerSignal(owner) {
			req.RegisterAccount.Email = owner.Email
			req.RegisterAccount.FullName = owner.FullName
			req.RegisterAccount.Avatar = owner.Avatar
			if owner.ExternalID != nil {
				s := strconv.FormatInt(*owner.ExternalID, 10)
				req.RegisterAccount.ExternalID = &s
			}
		}
	}

	// Empty partition + unknown account → nothing to do at all. Skip the
	// HTTP call so we don't burn quota or trigger a no-op upsert chain.
	if req.RegisterAccount == nil && !a.known.hasAccount(accountID) && len(campaigns) == 0 {
		return nil
	}

	for _, c := range campaigns {
		cid := int(c.ID)

		// Scraper campaigns (no messaging step — only extract/visit/webhook
		// actions) carry no outreach the platform tracks, so we drop them
		// entirely: neither registered nor funneled.
		if kindByCampaign[c.ID] == lh.KindScraper {
			continue
		}

		// Decide whether to (re)send the full campaign metadata this cycle:
		//   - unknown or version bump (rename / pause toggle / step edit) →
		//     always re-register;
		//   - known at the same version but the platform has no messages →
		//     backfill, BUT only when we actually have message-bearing actions
		//     to contribute. A campaign with no messageable steps (e.g. a
		//     note-less InvitePerson) would otherwise never reach hasMessages
		//     and we'd resend it every cycle forever.
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
// that become CampaignMessage rows and carry a seq number. Inter-message
// waits live on the non-message steps between them (CheckForReplies), so we
// accumulate those and attach them to the next message action.
var messageActionTypes = map[string]struct{}{
	"MessageToPerson": {},
	"InvitePerson":    {},
	"InMail":          {},
}

// hasMessageActions reports whether any step would become a CampaignMessage on
// the platform — same predicate the platform's syncMessages uses (messaging
// type AND a rendered body). Gates the message-backfill so we don't perpetually
// re-register campaigns that have no messageable steps to begin with.
func hasMessageActions(rows []lh.CampaignActionRow) bool {
	for _, r := range rows {
		if _, ok := messageActionTypes[r.Type]; ok && r.Body != nil {
			return true
		}
	}
	return false
}

// buildFunnelSteps turns per-action stats (workflow order) into per-message
// funnel rows. Seq numbering matches buildActions / the platform: one bump per
// messaging step. A messaging step's own replied is kept, and any following
// CheckForReplies' replied is folded onto it — LH records the reply on the
// check step that gates the next message, not on the message itself.
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

// buildActions maps LH action rows to the wire format, walking them in
// workflow order. LH encodes the wait between two messages as a separate step
// (CheckForReplies.moveToSuccessfulAfterMs) sitting between them, so we carry
// the accumulated wait forward and stamp it as the delay BEFORE the next
// messaging action. action_configs.coolDown is deliberately ignored: it's a
// fixed per-dispatch throttle (~60s for every send), not the sequence cadence,
// and folding it in made every message read as "+1 minute".
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

// hasOwnerSignal returns true when the LH owner row carried at least one
// matchable field. We use this to leave RegisterAccount fields nil for the
// "owner row exists but every field is null" edge case (the platform can
// then create a placeholder Client without forcing a match attempt).
func hasOwnerSignal(o *lh.AccountOwner) bool {
	return o.ExternalID != nil || o.Email != nil || o.FullName != nil
}
