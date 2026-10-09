package app

import (
	"context"
	"errors"
	"github.com/msoldin/porty/internal/config"
	"github.com/msoldin/porty/internal/monitoring"
)

func (a *Application) startMonitoring(ctx context.Context, cfg config.Monitoring) error {
	owner, err := monitoring.OpenLinuxSources(monitoring.LinuxOptions{Mode: cfg.Mode, HostProc: cfg.HostProc, HostSys: cfg.HostSys, HostRoot: cfg.HostRoot})
	if err != nil {
		return err
	}
	disabled := cfg.Mode == "disabled"
	service, err := monitoring.NewService(monitoring.ServiceOptions{Host: owner.Host(ctx), Sources: owner.InitialSources(), Discover: owner.Discover, Disabled: disabled})
	if err != nil {
		owner.Close()
		return err
	}
	runCtx, cancel := context.WithCancel(ctx)
	a.monitoring = service
	a.stopMonitoring = cancel
	done := make(chan error, 1)
	a.monitoringDone = done
	go func() {
		if disabled {
			<-runCtx.Done()
		} else {
			service.Run(runCtx)
		}
		// A timed-out application shutdown leaves one bounded cleanup waiter.
		// Roots and NVML stay valid until the last native call has returned.
		err := service.Shutdown(context.Background())
		done <- errors.Join(err, owner.Close())
		close(done)
	}()
	return nil
}
