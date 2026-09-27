package autoupdate_test

import (
	"context"
	"fmt"
	"github.com/msoldin/porty/internal/autoupdate"
	store "github.com/msoldin/porty/internal/sqlite"
	"github.com/msoldin/porty/internal/stack"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type timer struct {
	ch chan time.Time
	at time.Time
}

func (t *timer) C() <-chan time.Time { return t.ch }
func (t *timer) Stop()               {}

type clock struct {
	mu      sync.Mutex
	now     time.Time
	timers  []*timer
	waiting chan struct{}
}

func (c *clock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *clock) NewTimer(d time.Duration) autoupdate.Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &timer{ch: make(chan time.Time, 1), at: c.now.Add(d)}
	c.timers = append(c.timers, t)
	select {
	case c.waiting <- struct{}{}:
	default:
	}
	return t
}
func (c *clock) advance(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = now
	for _, t := range c.timers {
		if !t.at.After(now) {
			select {
			case t.ch <- now:
			default:
			}
		}
	}
}

type executor struct {
	started chan autoupdate.Run
	finish  chan struct{}
	store   *store.AutoUpdateStore
}

func (e *executor) CheckAndUpdate(ctx context.Context, run autoupdate.Run) error {
	e.started <- run
	select {
	case <-e.finish:
		return e.store.FinishRun(context.Background(), run.ID, "unchanged", "", nil)
	case <-ctx.Done():
		return ctx.Err()
	}
}
func TestSchedulerRunsMidnightBatchFairly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, 9, 27, 23, 59, 0, 0, time.UTC)
	s := store.NewAutoUpdateStore(db)
	ss := store.NewStackStore(db)
	for i := range 5 {
		id := stack.StackID(fmt.Sprintf("s%d", i))
		if err := ss.Create(ctx, stack.Stack{ID: id, DirectoryName: string(id), ComposeProjectName: string(id), CreatedAt: now}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.SavePolicy(ctx, id, autoupdate.PolicyUpdate{Enabled: true, Expression: autoupdate.DefaultExpression}, now); err != nil {
			t.Fatal(err)
		}
	}
	clock := &clock{now: now, waiting: make(chan struct{}, 10)}
	e := &executor{started: make(chan autoupdate.Run, 10), finish: make(chan struct{}, 10), store: s}
	scheduler := autoupdate.NewScheduler(s, e, clock)
	done := make(chan error, 1)
	go func() { done <- scheduler.Run(ctx) }()
	select {
	case <-clock.waiting:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not wait")
	}
	clock.advance(now.Add(time.Minute))
	seen := map[string]bool{}
	for range 2 {
		select {
		case run := <-e.started:
			seen[string(run.StackID)] = true
		case <-time.After(time.Second):
			t.Fatal("midnight worker did not start")
		}
	}
	select {
	case <-e.started:
		t.Fatal("more than two active workers")
	default:
	}
	for range 3 {
		e.finish <- struct{}{}
		select {
		case run := <-e.started:
			seen[string(run.StackID)] = true
		case <-time.After(time.Second):
			t.Fatal("batch starved")
		}
	}
	if len(seen) != 5 {
		t.Fatalf("processed=%v", seen)
	}
	e.finish <- struct{}{}
	e.finish <- struct{}{}
	clock.advance(now.Add(-time.Hour))
	scheduler.Notify()
	select {
	case run := <-e.started:
		t.Fatalf("clock rollback repeated %+v", run)
	case <-time.After(10 * time.Millisecond):
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop")
	}
}
