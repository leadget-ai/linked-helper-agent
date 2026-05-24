package agent

import (
	"context"
	"os"
	"runtime"
	"runtime/debug"
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

	req := a.buildReport(acc.ID, campaigns, funnels, owner)
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
		if owner != nil {
			req.RegisterAccount.Nickname = owner.Nickname
			if owner.ProfileURL != nil || owner.PublicID != nil {
				req.RegisterAccount.Owner = &client.AccountOwner{
					ProfileURL: owner.ProfileURL,
					PublicID:   owner.PublicID,
				}
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

		if !a.known.hasCampaign(accountID, cid) {
			req.RegisterCampaigns = append(req.RegisterCampaigns, client.RegisterCampaign{
				CampaignID:  cid,
				Name:        c.Name,
				Description: c.Description,
				Type:        c.Type,
				IsPaused:    c.IsPaused,
				IsArchived:  c.IsArchived,
				CreatedAt:   c.CreatedAt,
				// V1: workflow step definitions aren't reverse-engineered from
				// the `actions` table yet. Empty here means the platform will
				// create the Campaign row without messages — they were already
				// authored at CSV-export time when the user provisioned the
				// campaign through the platform UI.
				Actions: []client.CampaignAction{},
			})
		}

		f := funnels[c.ID]
		req.Funnels = append(req.Funnels, client.CampaignFunnel{
			CampaignID: cid,
			Messaged:   f.Messaged,
			Replied:    f.Replied,
			Target:     f.Target,
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
