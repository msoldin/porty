package app

import (
	"context"
	"errors"
	"github.com/msoldin/porty/internal/autoupdate"
	op "github.com/msoldin/porty/internal/operation"
	portyws "github.com/msoldin/porty/internal/websocket"
	"io"
	"net/http"
	"sync"
)

type Application struct {
	scheduler     *autoupdate.Scheduler
	handler       http.Handler
	operations    *op.OperationService
	docker        io.Closer
	hub           *portyws.Hub
	stopScheduler context.CancelFunc
	schedulerDone <-chan struct{}
	closeOnce     sync.Once
	shutdownErr   error
}

func (a *Application) ServeHTTP(w http.ResponseWriter, r *http.Request) { a.handler.ServeHTTP(w, r) }
func (a *Application) Shutdown(ctx context.Context) error {
	a.closeOnce.Do(func() {
		if a.stopScheduler != nil {
			a.stopScheduler()
		}
		if a.operations != nil {
			a.shutdownErr = a.operations.Shutdown(ctx)
		}
		if a.schedulerDone != nil {
			select {
			case <-a.schedulerDone:
			case <-ctx.Done():
				a.shutdownErr = errors.Join(a.shutdownErr, ctx.Err())
			}
		}
		if a.hub != nil {
			a.hub.CloseConnections()
		}
		if a.docker != nil {
			a.shutdownErr = errors.Join(a.shutdownErr, a.docker.Close())
		}
	})
	return a.shutdownErr
}
