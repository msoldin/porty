package autoupdate

import (
	"context"
	"errors"
	"github.com/msoldin/porty/internal/alert"
	"sync"
	"time"
)

type Timer interface {
	C() <-chan time.Time
	Stop()
}
type Clock interface {
	Now() time.Time
	NewTimer(time.Duration) Timer
}
type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now().UTC() }

type systemTimer struct{ timer *time.Timer }

func (t systemTimer) C() <-chan time.Time          { return t.timer.C }
func (t systemTimer) Stop()                        { t.timer.Stop() }
func (SystemClock) NewTimer(d time.Duration) Timer { return systemTimer{time.NewTimer(d)} }

type SchedulerStore interface {
	Store
	NextScheduled(context.Context) (time.Time, error)
	MarkChecking(context.Context, Run, time.Time) error
	FinishRun(context.Context, string, string, string, []alert.Change) error
}
type Scheduler struct {
	store    SchedulerStore
	executor Executor
	clock    Clock
	wake     chan struct{}
}

func NewScheduler(store SchedulerStore, executor Executor, clock Clock) *Scheduler {
	if clock == nil {
		clock = SystemClock{}
	}
	return &Scheduler{store: store, executor: executor, clock: clock, wake: make(chan struct{}, 1)}
}
func (s *Scheduler) Notify() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *Scheduler) Run(parent context.Context) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	if err := s.store.ResetAfterStartup(ctx, s.clock.Now()); err != nil {
		return err
	}
	queue := make(chan Run, 200)
	failed := make(chan error, 1)
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case run := <-queue:
					if err := s.execute(ctx, run); err != nil {
						select {
						case failed <- err:
						default:
						}
						return
					}
				}
			}
		}()
	}
	defer func() { cancel(); workers.Wait() }()
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		policies, err := s.store.ListDue(ctx, s.clock.Now(), 200)
		if err != nil {
			return err
		}
		for _, policy := range policies {
			run, accepted, err := s.store.Admit(ctx, policy, s.clock.Now())
			if err != nil {
				return err
			}
			if !accepted {
				continue
			}
			select {
			case queue <- run:
			case err := <-failed:
				return err
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if len(policies) == 200 {
			continue
		}
		next, err := s.store.NextScheduled(ctx)
		if err != nil {
			return err
		}
		delay := 30 * time.Second
		if !next.IsZero() {
			delay = next.Sub(s.clock.Now())
			if delay < time.Millisecond {
				delay = time.Millisecond
			}
			if delay > 30*time.Second {
				delay = 30 * time.Second
			}
		}
		timer := s.clock.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case err := <-failed:
			timer.Stop()
			return err
		case <-s.wake:
			timer.Stop()
		case <-timer.C():
		}
	}
}
func (s *Scheduler) execute(ctx context.Context, run Run) error {
	remaining := run.ScheduledAt.Add(20 * time.Minute).Sub(s.clock.Now())
	if remaining <= 0 {
		return s.finish(run, "skipped", "capacity_deadline")
	}
	if err := s.store.MarkChecking(ctx, run, s.clock.Now()); err != nil {
		if errors.Is(err, ErrConflict) {
			return s.finish(run, "skipped", "policy_changed")
		}
		return err
	}
	workCtx, cancel := context.WithTimeout(ctx, remaining)
	defer cancel()
	return s.executor.CheckAndUpdate(workCtx, run)
}
func (s *Scheduler) finish(run Run, outcome, reason string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.store.FinishRun(ctx, run.ID, outcome, reason, nil)
}
