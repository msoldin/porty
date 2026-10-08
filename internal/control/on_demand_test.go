package control_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/msoldin/porty/internal/compose"
	ctl "github.com/msoldin/porty/internal/control"
	"github.com/msoldin/porty/internal/ondemand"
	op "github.com/msoldin/porty/internal/operation"
	repo "github.com/msoldin/porty/internal/repository"
	store "github.com/msoldin/porty/internal/sqlite"
	"github.com/msoldin/porty/internal/stack"
	"github.com/msoldin/porty/internal/traffic"
)

type demandRuntime struct {
	controlRuntime
	snapshot      compose.OnDemandSnapshot
	counts        uint64
	starts, stops int
	failure       error
}

func (r *demandRuntime) CheckOnDemandHost(context.Context, string) error { return nil }
func (r *demandRuntime) SnapshotOnDemand(context.Context, compose.Request, []string) (compose.OnDemandSnapshot, error) {
	return r.snapshot, nil
}
func (r *demandRuntime) OnDemandCounters(context.Context, []string) (map[string]compose.OnDemandCounters, time.Time, error) {
	return map[string]compose.OnDemandCounters{"one": {Received: r.counts}}, time.Now(), nil
}
func (r *demandRuntime) StartOnDemand(context.Context, compose.OnDemandSnapshot, time.Duration) error {
	r.starts++
	if r.failure == nil {
		r.snapshot.Running = true
	}
	return r.failure
}
func (r *demandRuntime) StopOnDemand(context.Context, compose.OnDemandSnapshot, int) error {
	r.stops++
	if r.failure == nil {
		r.snapshot.Running = false
	}
	return r.failure
}

type demandPorts struct {
	released, reserved bool
	failure            error
}

func (p *demandPorts) Release(ondemand.Group) error { p.released = true; return nil }
func (p *demandPorts) Reserve(ondemand.Group) error { p.reserved = true; return p.failure }
func demandControlFixture(t *testing.T) (*ctl.OnDemandService, *store.OnDemandStore, *demandRuntime, ondemand.Group) {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	stacks := store.NewStackStore(db)
	if err := stacks.Create(ctx, stack.Stack{ID: "s", DirectoryName: "game", ComposeProjectName: "game", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	operations := store.NewOperationStore(db)
	if err := operations.CreateOperation(ctx, op.Operation{ID: "baseline", Kind: "deploy", ScopeType: "stack", ScopeID: "s", Status: op.OperationSucceeded}); err != nil {
		t.Fatal(err)
	}
	deployments := store.NewDeploymentStore(db)
	if err := deployments.SaveDeployment(ctx, op.Deployment{ID: "baseline", OperationID: "baseline", StackID: "s", Status: op.DeploymentSucceeded, ComposeDigest: "source", StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	runtime := &demandRuntime{snapshot: compose.OnDemandSnapshot{ProjectName: "game", SourceDigest: "source", Running: true, Containers: []compose.OnDemandContainer{{ID: "one", Service: "game", ImageID: "image", StartedAt: "epoch", Running: true}}, Bindings: []traffic.Binding{{Network: "udp4", Address: "127.0.0.1:25565"}}}}
	coordinator := op.NewCoordinator()
	control := ctl.NewControlPlane(t.TempDir(), stacks, stack.NewEnvironmentService(stacks), repo.NewRepositoryService(controlGit{}), runtime, op.NewOperationService(operations, nil, time.Minute, 1024), nil, coordinator, deployments, nil)
	groups := store.NewOnDemandStore(db)
	service := ctl.NewOnDemandService(control, groups)
	p := ondemand.DefaultPolicy()
	p.Name = "Game"
	p.Enabled = true
	p.Members = []string{"game"}
	group, err := service.SaveGroup(ctx, "s", "", ondemand.PolicyUpdate{Policy: p})
	if err != nil {
		t.Fatal(err)
	}
	return service, groups, runtime, group
}
func TestOnDemandSleepCommitsOnlyAfterPortReservation(t *testing.T) {
	service, groups, runtime, g := demandControlFixture(t)
	ctx := context.Background()
	sample, err := service.ObserveOnDemand(ctx, g)
	if err != nil {
		t.Fatal(err)
	}
	ports := &demandPorts{}
	if err := service.ExecuteOnDemand(ctx, g, ondemand.Sleep, sample, ports); err != nil {
		t.Fatal(err)
	}
	fresh, _ := groups.GetGroup(ctx, "s", g.ID)
	if runtime.stops != 1 || !ports.reserved || fresh.Phase != ondemand.Sleeping {
		t.Fatalf("incomplete sleep: %+v", fresh)
	}
}
func TestOnDemandRejectsTrafficOrHoldBeforeStop(t *testing.T) {
	for _, mode := range []string{"traffic", "hold", "configuration"} {
		t.Run(mode, func(t *testing.T) {
			service, groups, runtime, g := demandControlFixture(t)
			ctx := context.Background()
			sample, err := service.ObserveOnDemand(ctx, g)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "traffic":
				runtime.counts++
			case "hold":
				if err := groups.HoldGroup(ctx, "s", g.ID, g.Revision, "Manual hold."); err != nil {
					t.Fatal(err)
				}
			case "configuration":
				runtime.snapshot.SourceDigest = "changed"
			}
			if err := service.ExecuteOnDemand(ctx, g, ondemand.Sleep, sample, &demandPorts{}); err == nil {
				t.Fatal("stale stop admitted")
			}
			if runtime.stops != 0 {
				t.Fatal("unsafe stop executed")
			}
		})
	}
}
func TestOnDemandPausesPartialFailureAndPortConflict(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(map[bool]string{false: "runtime", true: "port"}[conflict], func(t *testing.T) {
			service, groups, runtime, g := demandControlFixture(t)
			ctx := context.Background()
			sample, _ := service.ObserveOnDemand(ctx, g)
			ports := &demandPorts{}
			if conflict {
				ports.failure = errors.New("bind conflict")
			} else {
				runtime.failure = errors.New("partial stop")
			}
			if err := service.ExecuteOnDemand(ctx, g, ondemand.Sleep, sample, ports); err == nil {
				t.Fatal("failure hidden")
			}
			fresh, _ := groups.GetGroup(ctx, "s", g.ID)
			if fresh.Phase != ondemand.Unknown || fresh.PausedReason == "" {
				t.Fatalf("failure remained active: %+v", fresh)
			}
		})
	}
}
