package app

import (
	"context"
	"errors"
	"github.com/msoldin/porty/internal/autoupdate"
	"github.com/msoldin/porty/internal/compose"
	"github.com/msoldin/porty/internal/control"
	"github.com/msoldin/porty/internal/sqlite"
	"log/slog"
	"time"
)

func (a *Application) startScheduler(parent context.Context, store *sqlite.AutoUpdateStore, controller *control.ControlPlane, guard compose.SelfGuard, ready func(context.Context) (bool, error)) {
	ctx, cancel := context.WithCancel(parent)
	a.stopScheduler = cancel
	done := make(chan struct{})
	a.schedulerDone = done
	a.scheduler = autoupdate.NewScheduler(store, controller, nil)
	go func() {
		defer close(done)
		for {
			if ctx.Err() != nil {
				return
			}
			configured, err := ready(ctx)
			if err == nil && configured && guard.CheckProject(ctx, "") == nil {
				if err := controller.ReconcileUpdates(ctx); err != nil {
					controller.SuspendUpdates()
					slog.Error("automatic update reconciliation unavailable")
					return
				}
				err := a.scheduler.Run(ctx)
				if err != nil && !errors.Is(err, context.Canceled) {
					controller.SuspendUpdates()
					slog.Error("automatic update scheduler stopped")
				}
				return
			}
			timer := time.NewTimer(10 * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
}
