// Command lh-agent reads campaign data from a local Linked Helper desktop
// installation and pushes it to the leadget platform.
package main

import (
	"context"
	"net/http"
	_ "net/http/pprof" // optional, enabled via LHA_PPROF=true
	"os"
	"os/signal"
	"syscall"

	log "github.com/sirupsen/logrus"

	"github.com/leadget/lh-agent/internal/agent"
)

// Stamped by the linker at release time. Sent to the server in bootstrap so
// operators can tell which build is running on which install without SSHing.
var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	log.SetFormatter(&log.JSONFormatter{})
	level, err := log.ParseLevel(os.Getenv("LHA_LOG_LEVEL"))
	if err != nil {
		level = log.InfoLevel
	}
	log.SetLevel(level)

	log.WithFields(log.Fields{"version": version, "commit": commit}).Info("lh-agent starting")

	cfg, err := agent.LoadConfig()
	if err != nil {
		log.WithError(err).Fatal("failed to load config")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// SIGINT/SIGTERM trigger graceful shutdown — the main loop unwinds
	// through `ctx.Done()` and the SQLite handles flush on Reader.Close().
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.WithField("signal", sig.String()).Info("received shutdown signal")
		cancel()
	}()

	if os.Getenv("LHA_PPROF") == "true" {
		go func() {
			log.Info("pprof listening on :6060")
			if err := http.ListenAndServe("127.0.0.1:6060", nil); err != nil {
				log.WithError(err).Warn("pprof server failed")
			}
		}()
	}

	a := agent.New(cfg, version)
	a.Run(ctx)

	log.Info("lh-agent stopped")
}
