//go:build windows

package main

import (
	"context"
	"os"
	"path/filepath"

	log "github.com/sirupsen/logrus"
	"golang.org/x/sys/windows/svc"
)

const (
	serviceName = "LhAgent"
	logDir      = `C:\ProgramData\lh-agent\logs`
	logFileName = "lh-agent.log"
	maxLogBytes = 10 << 20 // rotate at ~10 MiB
)

func inWindowsService() (bool, error) {
	return svc.IsWindowsService()
}

// configureServiceLogging redirects logrus to a file: a service started by the
// SCM has no console to inherit stdout. A one-shot size check on startup keeps
// the log from growing without bound across restarts (best-effort — any
// failure leaves logging at its default and the agent still runs).
func configureServiceLogging() {
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return
	}
	path := filepath.Join(logDir, logFileName)
	if info, err := os.Stat(path); err == nil && info.Size() > maxLogBytes {
		_ = os.Rename(path, path+".1") // keep one previous generation
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	log.SetOutput(f)
}

// agentService adapts the agent's run loop to the Windows service control
// protocol. The SCM calls Execute on its own goroutine; we run the agent
// alongside and translate Stop/Shutdown into context cancellation.
type agentService struct{}

func (agentService) Execute(_ []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	changes <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- runAgent(ctx) }()

	const accepted = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.Running, Accepts: accepted}

	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				log.Info("service received stop request")
				changes <- svc.Status{State: svc.StopPending}
				cancel()
				<-errCh // let the agent flush SQLite handles before we report Stopped
				return false, 0
			default:
				log.WithField("cmd", c.Cmd).Warn("unexpected service control request")
			}
		case err := <-errCh:
			// The agent returned on its own — almost always a config error at
			// startup. Report a non-zero exit so the SCM's recovery kicks in.
			changes <- svc.Status{State: svc.StopPending}
			if err != nil {
				log.WithError(err).Error("agent exited with error")
				return false, 1
			}
			return false, 0
		}
	}
}

func runWindowsService() error {
	return svc.Run(serviceName, agentService{})
}
