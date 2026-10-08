package ondemand

import (
	"context"
	"sync"
	"time"

	"github.com/msoldin/porty/internal/stack"
	"github.com/msoldin/porty/internal/traffic"
)

type ControllerStore interface {
	ListGroups(context.Context, stack.StackID) ([]Group, error)
	GetGroup(context.Context, stack.StackID, string) (Group, error)
	PauseGroup(context.Context, Group, string) error
}
type Executor interface {
	ObserveOnDemand(context.Context, Group) (Sample, error)
	ExecuteOnDemand(context.Context, Group, Action, Sample, Ports) error
}

// Ports is called only by the executor while it holds coordinated admission.
type Ports interface {
	Release(Group) error
	Reserve(Group) error
}
type Controller struct {
	store       ControllerStore
	executor    Executor
	mu          sync.Mutex
	workers     map[string]*groupWorker
	probes      chan struct{}
	transitions chan struct{}
	changed     chan struct{}
}
type groupWorker struct {
	mu        sync.Mutex
	group     Group
	listener  *traffic.Listener
	suspended bool
	reason    string
	cancel    context.CancelFunc
}

func NewController(store ControllerStore, executor Executor) *Controller {
	return &Controller{store: store, executor: executor, workers: map[string]*groupWorker{}, probes: make(chan struct{}, 4), transitions: make(chan struct{}, 2), changed: make(chan struct{}, 1)}
}

func (c *Controller) ObservationReason(g Group) string {
	if !g.Enabled || g.HoldReason != "" || g.PausedReason != "" {
		return ""
	}
	c.mu.Lock()
	w := c.workers[g.ID]
	c.mu.Unlock()
	if w == nil {
		return "Automation is waiting for runtime observation."
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.reason
}
func (c *Controller) Notify() {
	select {
	case c.changed <- struct{}{}:
	default:
	}
}

func (c *Controller) SuspendGroup(id string) {
	c.mu.Lock()
	w := c.workers[id]
	c.mu.Unlock()
	if w != nil {
		w.mu.Lock()
		w.suspended = true
		w.closeListener()
		w.mu.Unlock()
	}
	c.Notify()
}

// SuspendStack synchronously releases ports. The persisted revision must change
// before calling this, so a worker cannot re-arm a manually suspended policy.
func (c *Controller) SuspendStack(id stack.StackID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, w := range c.workers {
		w.mu.Lock()
		if w.group.StackID == id {
			w.suspended = true
			w.closeListener()
		}
		w.mu.Unlock()
	}
	c.Notify()
}
func (w *groupWorker) closeListener() {
	if w.listener != nil {
		w.listener.Close()
		w.listener = nil
	}
}
func (c *Controller) Release(g Group) error {
	c.mu.Lock()
	w := c.workers[g.ID]
	c.mu.Unlock()
	if w == nil {
		return ErrConflict
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.group.Revision != g.Revision || w.suspended {
		return ErrConflict
	}
	w.closeListener()
	return nil
}
func (c *Controller) Reserve(g Group) error {
	c.mu.Lock()
	w := c.workers[g.ID]
	c.mu.Unlock()
	if w == nil {
		return ErrConflict
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.group.Revision != g.Revision || w.suspended {
		return ErrConflict
	}
	if w.listener != nil {
		return nil
	}
	listener, err := traffic.Listen(traffic.Config{Generation: uint64(g.Revision), Threshold: g.WakeThreshold, Window: time.Duration(g.WakeWindowMS) * time.Millisecond, Bindings: g.Evidence.Bindings})
	if err != nil {
		return err
	}
	w.listener = listener
	return nil
}
func (c *Controller) Run(ctx context.Context) error {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	var workers sync.WaitGroup
	defer func() {
		c.mu.Lock()
		for _, w := range c.workers {
			w.cancel()
		}
		c.mu.Unlock()
		workers.Wait()
		c.mu.Lock()
		clear(c.workers)
		c.mu.Unlock()
	}()
	for {
		groups, err := c.store.ListGroups(ctx, "")
		if err != nil {
			return err
		}
		if len(groups) > MaxGroups {
			return ErrUnavailable
		}
		c.mu.Lock()
		seen := map[string]bool{}
		for _, g := range groups {
			seen[g.ID] = true
			if _, ok := c.workers[g.ID]; ok {
				continue
			}
			child, cancel := context.WithCancel(ctx)
			w := &groupWorker{group: g, cancel: cancel, reason: "Automation is waiting for runtime observation."}
			c.workers[g.ID] = w
			workers.Add(1)
			go func() { defer workers.Done(); c.runGroup(child, w) }()
		}
		for id, w := range c.workers {
			if !seen[id] {
				w.cancel()
				delete(c.workers, id)
			}
		}
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		case <-c.changed:
		}
	}
}
func (c *Controller) runGroup(ctx context.Context, w *groupWorker) {
	defer func() { w.mu.Lock(); w.closeListener(); w.mu.Unlock() }()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	tracker := Tracker{}
	for {
		if ctx.Err() != nil {
			return
		}
		w.mu.Lock()
		previous := w.group
		w.mu.Unlock()
		observation, cancel := context.WithTimeout(ctx, 8*time.Second)
		g, err := c.store.GetGroup(observation, previous.StackID, previous.ID)
		if err != nil {
			cancel()
			w.mu.Lock()
			w.closeListener()
			w.reason = "Policy could not be read. Automatic wake and sleep are unavailable."
			w.mu.Unlock()
			tracker.Invalidate(time.Now())
		} else {
			w.mu.Lock()
			if g.Revision != w.group.Revision {
				w.closeListener()
				w.suspended = false
			}
			w.group = g
			active := g.Enabled && g.HoldReason == "" && g.PausedReason == "" && !w.suspended && (g.Phase == Sleeping || g.Phase == Running)
			if !active {
				w.closeListener()
			}
			w.mu.Unlock()
			if active {
				acquired := false
				select {
				case c.probes <- struct{}{}:
					acquired = true
				case <-observation.Done():
				}
				if acquired {
					sample, observeErr := c.executor.ObserveOnDemand(observation, g)
					<-c.probes
					if observeErr != nil {
						tracker.Invalidate(time.Now())
						w.mu.Lock()
						w.closeListener()
						w.reason = "Runtime observation is unavailable. Check Docker access, host networking, deployment state, and container health. No automatic stop is permitted."
						w.mu.Unlock()
					} else {
						w.mu.Lock()
						w.reason = ""
						w.mu.Unlock()
						pending := false
						if g.Phase == Sleeping {
							if err := c.Reserve(g); err != nil {
								_ = c.store.PauseGroup(observation, g, "Published ports could not be reserved. Review port conflicts and resume.")
							} else {
								w.mu.Lock()
								if w.listener != nil {
									state, snapshotErr := w.listener.Snapshot()
									pending = snapshotErr == nil && state.Pending
									if snapshotErr != nil {
										w.closeListener()
										tracker.Invalidate(time.Now())
									}
								}
								w.mu.Unlock()
							}
						}
						decision := tracker.Evaluate(g, sample, pending, time.Now())
						if decision.PauseReason != "" {
							_ = c.store.PauseGroup(observation, g, decision.PauseReason)
							w.mu.Lock()
							w.closeListener()
							w.mu.Unlock()
						}
						if decision.Action != "" {
							select {
							case c.transitions <- struct{}{}:
								cancel()
								_ = c.executor.ExecuteOnDemand(ctx, g, decision.Action, sample, c)
								<-c.transitions
								tracker.Invalidate(time.Now())
							default:
							}
						}
					}
				}
			} else {
				tracker.Invalidate(time.Now())
			}
			cancel()
		}
		w.mu.Lock()
		var events <-chan traffic.Wake
		if w.listener != nil {
			events = w.listener.Events()
		}
		w.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-events:
		}
	}
}
