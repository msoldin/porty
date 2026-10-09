package monitoring

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func fixtureBatch(now time.Time, value float64) Batch {
	return Batch{Devices: []Device{{ID: "cpu", Kind: DeviceCPU, Name: "Host CPU", Default: true}}, Series: []Series{{ID: "cpu.busy", DeviceID: "cpu", Metric: MetricCPUBusy, Unit: UnitPercent}}, Readings: map[string]Reading{"cpu.busy": {Value: &value, State: StateAvailable, SampledAt: now, LastSuccessAt: &now}}}
}

type testSource struct {
	id      string
	blocked chan struct{}
	started chan struct{}
	calls   atomic.Int32
	closed  atomic.Bool
}

func (s *testSource) ID() string { return s.id }
func (s *testSource) Collect(context.Context) (Batch, error) {
	s.calls.Add(1)
	if s.started != nil {
		select {
		case s.started <- struct{}{}:
		default:
		}
	}
	if s.blocked != nil {
		<-s.blocked
	}
	return fixtureBatch(time.Now(), 24), nil
}
func (s *testSource) Close() error { s.closed.Store(true); return nil }
func waitFor(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not reached")
}

func TestServiceKeepsOtherSourcesFreshUnderBlockedRead(t *testing.T) {
	blocked := &testSource{id: "blocked", blocked: make(chan struct{}), started: make(chan struct{}, 1)}
	healthy := &testSource{id: "cpu"}
	service, err := NewService(ServiceOptions{Sources: []Source{blocked, healthy}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer close(blocked.blocked)
	go service.Run(ctx)
	waitFor(t, func() bool {
		snapshot, _ := service.Snapshot("")
		return snapshot.Current.Readings["cpu.busy"].State == StateAvailable
	})
	service.schedule(ctx)
	if blocked.calls.Load() != 1 {
		t.Fatal("overlapping blocked source calls")
	}
	if blocked.closed.Load() {
		t.Fatal("closed in-flight source")
	}
}

func TestServiceStopsWithinShutdownDeadline(t *testing.T) {
	blocked := &testSource{id: "cpu", blocked: make(chan struct{}), started: make(chan struct{}, 1)}
	service, _ := NewService(ServiceOptions{Sources: []Source{blocked}})
	go service.Run(context.Background())
	<-blocked.started
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := service.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown: %v", err)
	}
	if blocked.closed.Load() {
		t.Fatal("closed a reader during its call")
	}
	close(blocked.blocked)
	waitFor(t, func() bool { return blocked.closed.Load() })
}

func TestServiceMarksReadingStaleAfterSixSeconds(t *testing.T) {
	service, now := historyService(t)
	service.acceptBatch("cpu", fixtureBatch(*now, 24), nil, *now)
	*now = now.Add(6 * time.Second)
	got, _ := service.Snapshot("")
	reading := got.Current.Readings["cpu.busy"]
	if reading.State != StateStale || reading.Value == nil || *reading.Value != 24 || reading.LastSuccessAt == nil {
		t.Fatalf("lost last known value: %#v", reading)
	}
}

func TestServiceKeepsLastSuccessUnderSourceFailure(t *testing.T) {
	service, now := historyService(t)
	service.acceptBatch("cpu", fixtureBatch(*now, 24), nil, *now)
	*now = now.Add(2 * time.Second)
	service.acceptBatch("cpu", Batch{}, errors.New("secret driver details"), *now)
	got, _ := service.Snapshot("")
	reading := got.Current.Readings["cpu.busy"]
	if reading.State != StateStale || reading.Reason != "read_failed" || reading.Value == nil {
		t.Fatalf("incorrect failure state: %#v", reading)
	}
}

func TestServiceBoundsInventoryAndRejectsInvalidReadings(t *testing.T) {
	service, now := historyService(t)
	batch := fixtureBatch(*now, 24)
	for i := 0; i < 300; i++ {
		batch.Devices = append(batch.Devices, Device{ID: time.Duration(i).String(), Kind: DeviceCPU, Name: "extra"})
	}
	batch.Readings["not-declared"] = Reading{State: StateAvailable}
	service.acceptBatch("cpu", batch, nil, *now)
	got, _ := service.Snapshot("")
	if len(got.Inventory.Devices) > 256 || len(got.Coverage) == 0 {
		t.Fatal("inventory was not bounded")
	}
	if _, exists := got.Current.Readings["not-declared"]; exists {
		t.Fatal("undeclared reading accepted")
	}
}

func TestServiceRejectsLateReadingAfterNewerObservation(t *testing.T) {
	service, now := historyService(t)
	service.acceptBatch("cpu", fixtureBatch(*now, 24), nil, *now)
	service.acceptBatch("cpu", fixtureBatch(now.Add(-time.Second), 90), nil, now.Add(-time.Second))
	got, _ := service.Snapshot("")
	if *got.Current.Readings["cpu.busy"].Value != 24 {
		t.Fatal("late source overwrote fresh observation")
	}
}

func TestServiceRetainsSourcesUnderRepeatedDiscoveryFailure(t *testing.T) {
	healthy := &testSource{id: "cpu"}
	service, _ := NewService(ServiceOptions{Sources: []Source{healthy}, Discover: func(context.Context) ([]Source, error) { return nil, errors.New("unavailable") }})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go service.Run(ctx)
	waitFor(t, func() bool {
		snapshot, _ := service.Snapshot("")
		return snapshot.Current.Readings["cpu.busy"].State == StateAvailable
	})
	if healthy.closed.Load() {
		t.Fatal("failed discovery retired a working source")
	}
}

func TestServiceBoundsRetiredReadersUnderRepeatedHotplug(t *testing.T) {
	blocked := make(chan struct{})
	service, _ := NewService(ServiceOptions{})
	defer func() { close(blocked); _ = service.Shutdown(context.Background()) }()
	for i := 0; i < maxSources+3; i++ {
		source := &testSource{id: fmt.Sprintf("hotplug-%d", i), blocked: blocked, started: make(chan struct{}, 1)}
		service.mu.Lock()
		service.replaceSourcesLocked([]Source{source})
		service.mu.Unlock()
		service.schedule(context.Background())
		if i < maxSources {
			<-source.started
		}
	}
	service.mu.Lock()
	slots := len(service.slots)
	service.mu.Unlock()
	if slots != maxSources {
		t.Fatalf("retired readers escaped the bound: retained %d slots, want %d", slots, maxSources)
	}
}

func TestServiceSharesCollectionAcrossSnapshotClients(t *testing.T) {
	source := &testSource{id: "cpu"}
	service, _ := NewService(ServiceOptions{Sources: []Source{source}})
	defer service.Shutdown(context.Background())
	service.schedule(context.Background())
	waitFor(t, func() bool { snapshot, _ := service.Snapshot(""); return len(snapshot.Current.Readings) > 0 })
	goroutines := runtime.NumGoroutine()
	var clients sync.WaitGroup
	for i := 0; i < 20; i++ {
		clients.Add(1)
		go func() {
			defer clients.Done()
			for j := 0; j < 50; j++ {
				if _, err := service.Snapshot(""); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	clients.Wait()
	if got := source.calls.Load(); got != 1 {
		t.Fatalf("client reads triggered %d collections", got)
	}
	if got := runtime.NumGoroutine(); got > goroutines+2 {
		t.Fatalf("client reads retained goroutines: %d -> %d", goroutines, got)
	}
	t.Logf("20 clients / 1000 snapshots: collector calls=%d, goroutines before=%d after=%d", source.calls.Load(), goroutines, runtime.NumGoroutine())
}
