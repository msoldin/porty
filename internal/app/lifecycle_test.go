package app

import (
	"context"
	op "github.com/msoldin/porty/internal/operation"
	store "github.com/msoldin/porty/internal/sqlite"
	"path/filepath"
	"testing"
	"time"
)

type closeProbe struct{ closed chan struct{} }

func (p closeProbe) Close() error { close(p.closed); return nil }
func TestShutdownDrainsBeforeDockerClose(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	operations := op.NewOperationService(store.NewOperationStore(db), nil, time.Minute, 1024)
	started, finish := make(chan struct{}), make(chan struct{})
	closed := make(chan struct{})
	_, err = operations.Start(ctx, op.OperationRequest{}, func(context.Context) (string, error) { close(started); <-finish; return "done", nil })
	if err != nil {
		t.Fatal(err)
	}
	<-started
	application := &Application{operations: operations, docker: closeProbe{closed}}
	done := make(chan error, 1)
	go func() { done <- application.Shutdown(ctx) }()
	select {
	case <-closed:
		t.Fatal("Docker closed before operation drained")
	case <-time.After(10 * time.Millisecond):
	}
	close(finish)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown stuck")
	}
	select {
	case <-closed:
	default:
		t.Fatal("Docker not closed")
	}
	if err := application.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
}
