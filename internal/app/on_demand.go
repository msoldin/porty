package app

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/msoldin/porty/internal/control"
)

func (a *Application) startOnDemand(parent context.Context, service *control.OnDemandService, ready func(context.Context) (bool, error)) {
	if service == nil {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	a.stopOnDemand = cancel
	done := make(chan struct{})
	a.onDemandDone = done
	go func() {
		defer close(done)
		for ctx.Err() == nil {
			configured, err := ready(ctx)
			if err == nil && configured {
				if err := service.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
					slog.Error("on-demand activation stopped")
				}
				return
			}
			timer := time.NewTimer(5 * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
}
