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

	mu             sync.RWMutex
	reportInterval time.Duration
}

func New(cfg *Config, version string) *Agent {
	return &Agent{
		cfg:            cfg,
		client:         client.New(cfg.APIEndpoint, cfg.APIKey, cfg.DisableKeepAlive),
		reader:         lh.NewReader(),
		known:          newKnown(),
		version:        version,
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

	// Preload step definitions only for campaigns whose version differs from
	// the platform's known version — steady-state cycles do zero extra IO.
	actionsByCampaign := make(map[int64][]lh.CampaignActionRow)
	for _, c := range campaigns {
		if a.known.hasCampaignAtVersion(acc.ID, int(c.ID), c.Version) {
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

	req := a.buildReport(acc.ID, campaigns, funnels, owner, actionsByCampaign, limits)
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
	limits lh.DailyLimits,
) *client.AccountReportRequest {
	req := &client.AccountReportRequest{
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

		// Re-register on first sight AND on any LH version bump — knownState
		// pins to (accountId, campaignId, version) so renames / pause toggles
		// / step edits propagate to the platform automatically.
		if !a.known.hasCampaignAtVersion(accountID, cid, c.Version) {
			actions := actionsByCampaign[c.ID]
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
				Actions:        buildActions(actions),
			})
		}

		f := funnels[c.ID]
		req.Funnels = append(req.Funnels, client.CampaignFunnel{
			CampaignID: cid,
			Messaged:   f.Messaged,
			Replied:    f.Replied,
			Target:     f.Target,
			IsPaused:   c.IsPaused,
			IsArchived: c.IsArchived,
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

// buildActions maps LH action rows to the wire format. CoolDown (ms) is
// folded into delayValue/delayUnit so the platform can render the cadence
// even when LH has no explicit Waiter step between actions. We round to the
// next-larger unit so a 60_000ms cooldown reads as "1 minute" rather than
// "60000 milliseconds".
func buildActions(rows []lh.CampaignActionRow) []client.CampaignAction {
	out := make([]client.CampaignAction, 0, len(rows))
	for _, r := range rows {
		a := client.CampaignAction{
			Type:    r.Type,
			Body:    r.Body,
			Example: r.Example,
		}
		if r.CoolDownMs != nil && *r.CoolDownMs > 0 {
			v, u := coolDownToDelay(*r.CoolDownMs)
			a.DelayValue = &v
			a.DelayUnit = &u
		}
		out = append(out, a)
	}
	return out
}

func coolDownToDelay(ms int64) (int, string) {
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
		// Round up so sub-minute cooldowns don't degrade to "0 minutes".
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
