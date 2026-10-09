package app

import (
	"context"
	"errors"
	"github.com/msoldin/porty/internal/config"
	"github.com/msoldin/porty/internal/monitoring"
	"testing"
	"time"
)

type blockedMonitor struct{ started, release chan struct{} }

func (s *blockedMonitor) ID() string   { return "blocked" }
func (s *blockedMonitor) Close() error { return nil }
func (s *blockedMonitor) Collect(context.Context) (monitoring.Batch, error) {
	close(s.started)
	<-s.release
	return monitoring.Batch{}, nil
}
func TestShutdownStopsMonitoringWithinDeadline(t *testing.T) {
	source := &blockedMonitor{started: make(chan struct{}), release: make(chan struct{})}
	service, _ := monitoring.NewService(monitoring.ServiceOptions{Sources: []monitoring.Source{source}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go service.Run(ctx)
	<-source.started
	app := &Application{monitoring: service, stopMonitoring: cancel}
	deadline, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stop()
	if err := app.Shutdown(deadline); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	close(source.release)
	if err := service.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func TestMonitoringDisabledReturnsStructuredState(t *testing.T) {
	app := &Application{}
	if err := app.startMonitoring(context.Background(), config.Monitoring{Mode: "disabled"}); err != nil {
		t.Fatal(err)
	}
	defer app.Shutdown(context.Background())
	snapshot, err := app.monitoring.Snapshot("")
	if err != nil || len(snapshot.Coverage) != 1 || snapshot.Coverage[0].Reason != "disabled" {
		t.Fatal(snapshot.Coverage, err)
	}
}
func TestMonitoringMissingHostMountsDoesNotPreventStartup(t *testing.T) {
	app := &Application{}
	missing := t.TempDir() + "/missing"
	if err := app.startMonitoring(context.Background(), config.Monitoring{Mode: "host", HostProc: missing, HostSys: missing, HostRoot: missing}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := app.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
}
