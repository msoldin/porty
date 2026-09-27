package compose

import (
	"context"
	"fmt"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/moby/moby/api/types/container"
	"strings"
	"testing"
	"time"
)

type allowGuard struct{}

func (allowGuard) CheckProject(context.Context, string) error { return nil }
func TestApplyNarrowsProjectBeforeUpAndPreservesAnonymousVolumes(t *testing.T) {
	c, p, _ := snapshotFixture()
	service := &recordingCompose{}
	c.service = service
	c.guard = allowGuard{}
	app := p.Services["app"]
	app.DependsOn = types.DependsOnConfig{"db": {Condition: types.ServiceConditionStarted}}
	app.Volumes = []types.ServiceVolumeConfig{{Type: "volume", Target: "/data"}}
	p.Services["app"] = app
	p.Services["db"] = types.ServiceConfig{Name: "db", Image: "postgres:17"}
	change := ImageChange{Service: "app", TargetReference: "alpine@sha256:" + strings.Repeat("b", 64)}
	if err := c.ApplyUpdate(context.Background(), PreparedUpdate{Snapshot: UpdateSnapshot{Project: p}, Changes: []ImageChange{change}}); err != nil {
		t.Fatal(err)
	}
	if len(service.project.Services) != 1 || service.project.Services["app"].DependsOn != nil && len(service.project.Services["app"].DependsOn) != 0 || service.project.Services["app"].Image != change.TargetReference {
		t.Fatalf("unsafe selection: %+v", service.project.Services)
	}
	if !service.options.Create.Inherit || service.options.Create.RemoveOrphans || !service.options.Create.IgnoreOrphans || service.options.Create.Build != nil || service.options.Create.RecreateDependencies != api.RecreateNever || service.project.Services["app"].PullPolicy != types.PullPolicyNever || len(service.project.Services["app"].Volumes) != 1 {
		t.Fatalf("unsafe options: %+v", service.options)
	}
	if p.Services["app"].Image != app.Image || len(p.Services) != 2 {
		t.Fatal("source snapshot mutated")
	}
}
func TestApplyRejectsDependencySideEffects(t *testing.T) {
	for _, kind := range []string{"hook", "link", "volumes", "namespace", "restart", "empty"} {
		t.Run(kind, func(t *testing.T) {
			c, p, _ := snapshotFixture()
			c.guard = allowGuard{}
			service := &recordingCompose{}
			c.service = service
			s := p.Services["app"]
			switch kind {
			case "hook":
				s.PostStart = []types.ServiceHook{{}}
			case "link":
				s.Links = []string{"db"}
			case "volumes":
				s.VolumesFrom = []string{"db"}
			case "namespace":
				s.NetworkMode = "service:db"
			case "restart":
				s.DependsOn = types.DependsOnConfig{"db": {Restart: true}}
			}
			p.Services["app"] = s
			changes := []ImageChange{{Service: "app", TargetReference: "alpine@sha256:" + strings.Repeat("b", 64)}}
			if kind == "empty" {
				changes = nil
			}
			if err := c.ApplyUpdate(context.Background(), PreparedUpdate{Snapshot: UpdateSnapshot{Project: p}, Changes: changes}); err == nil || len(service.calls) != 0 {
				t.Fatalf("unsafe apply: %v %v", err, service.calls)
			}
		})
	}
}

type verificationClock struct {
	now    time.Time
	waits  int
	onWait func(int)
}

func (c *verificationClock) Now() time.Time { return c.now }
func (c *verificationClock) Wait(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.now = c.now.Add(d)
	c.waits++
	if c.onWait != nil {
		c.onWait(c.waits)
	}
	return nil
}
func TestVerifyRequiresThirtyContinuousSeconds(t *testing.T) {
	c, p, d := snapshotFixture()
	clock := &verificationClock{now: time.Now()}
	c.updateClock = clock
	before, err := c.snapshotProject(context.Background(), p, Request{})
	if err != nil {
		t.Fatal(err)
	}
	clock.onWait = func(n int) {
		if n == 10 {
			d.inspected.RestartCount++
		}
	}
	result, err := c.VerifyUpdate(context.Background(), PreparedUpdate{Snapshot: before, Changes: []ImageChange{{Service: "app", AfterImageID: d.inspected.Image}}})
	if err != nil || result.RecoveryRequired || clock.waits < 40 {
		t.Fatalf("verification waits=%d result=%+v err=%v", clock.waits, result, err)
	}
}
func TestVerifyChecksActualTargetImage(t *testing.T) {
	c, p, _ := snapshotFixture()
	snapshot, err := c.snapshotProject(context.Background(), p, Request{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := c.VerifyUpdate(context.Background(), PreparedUpdate{Snapshot: snapshot, Changes: []ImageChange{{Service: "app", AfterImageID: "sha256:" + strings.Repeat("f", 64)}}})
	if err == nil || !result.RecoveryRequired {
		t.Fatalf("wrong image accepted: %+v %v", result, err)
	}
}

func TestVerifyWaitsForHealthcheckAndUsesFiveMinuteDeadline(t *testing.T) {
	for _, recover := range []bool{true, false} {
		t.Run(fmt.Sprint(recover), func(t *testing.T) {
			c, p, d := snapshotFixture()
			snapshot, err := c.snapshotProject(context.Background(), p, Request{})
			if err != nil {
				t.Fatal(err)
			}
			d.inspected.State.Health = &container.Health{Status: "starting"}
			clock := &verificationClock{now: time.Now()}
			c.updateClock = clock
			if recover {
				clock.onWait = func(n int) {
					if n == 10 {
						d.inspected.State.Health.Status = "healthy"
					}
				}
			}
			result, err := c.VerifyUpdate(context.Background(), PreparedUpdate{Snapshot: snapshot, Changes: []ImageChange{{Service: "app", AfterImageID: d.inspected.Image}}})
			if recover {
				if err != nil || result.RecoveryRequired || clock.waits != 10 {
					t.Fatalf("did not wait for healthcheck: %+v %v waits=%d", result, err, clock.waits)
				}
			} else if err == nil || clock.waits != 300 {
				t.Fatalf("deadline not enforced: %v waits=%d", err, clock.waits)
			}
		})
	}
}
