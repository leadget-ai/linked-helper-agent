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

	// On Windows the Service Control Manager launches us with no console, so
	// detect that first and, when true, redirect logs to a file before we emit
	// anything. On every other path this is a no-op.
	isService, err := inWindowsService()
	if err != nil {
		log.WithError(err).Fatal("failed to determine windows service mode")
	}
	if isService {
		configureServiceLogging()
	}

	log.WithFields(log.Fields{"version": version, "commit": commit, "service": isService}).
		Info("lh-agent starting")

	if isService {
		if err := runWindowsService(); err != nil {
			log.WithError(err).Fatal("windows service failed")
		}
		return
	}

	// Console / non-Windows path: tie the agent lifetime to OS signals.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.WithField("signal", sig.String()).Info("received shutdown signal")
		cancel()
	}()

	if err := runAgent(ctx); err != nil {
		log.WithError(err).Fatal("agent failed")
	}
	log.Info("lh-agent stopped")
}

// runAgent loads config and runs the agent until ctx is cancelled. Shared by
// the console path and the Windows service control handler.
func runAgent(ctx context.Context) error {
	cfg, err := agent.LoadConfig()
	if err != nil {
		return err
	}

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
	return nil
}
