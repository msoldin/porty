package control_test

import (
	"context"
	"errors"
	"github.com/docker/compose/v5/pkg/api"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/msoldin/porty/internal/alert"
	ctl "github.com/msoldin/porty/internal/control"
	op "github.com/msoldin/porty/internal/operation"
	repo "github.com/msoldin/porty/internal/repository"
	sqlstore "github.com/msoldin/porty/internal/sqlite"
	stack "github.com/msoldin/porty/internal/stack"
)

type finalEvents struct{ done chan op.Operation }

func (e *finalEvents) PublishOperation(o op.Operation) {
	if o.Status == op.OperationFailed || o.Status == op.OperationSucceeded {
		e.done <- o
	}
}

func TestManualRecoveryRequiresExpectedServiceAndLeavesUnrelatedFailure(t *testing.T) {
	for _, oneoff := range []bool{false, true} {
		t.Run(map[bool]string{false: "service", true: "oneoff"}[oneoff], func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, "gateway"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "gateway", "docker-compose.yml"), []byte("services:\n  app:\n    image: alpine\n"), 0600); err != nil {
				t.Fatal(err)
			}
			db, err := sqlstore.Open(ctx, filepath.Join(t.TempDir(), "p.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			alerts := sqlstore.NewAlertStore(db)
			for _, problem := range []string{"deployment", "stop"} {
				_, err := alerts.Apply(ctx, []alert.Change{{Kind: "failure", Key: alert.Key{StackID: "stk_gateway", Problem: problem, Target: "stack"}, OccurrenceID: problem, Summary: "Failed", ObservedAt: time.Now()}})
				if err != nil {
					t.Fatal(err)
				}
			}
			row := api.ContainerSummary{ID: "a", Name: "app-1", Service: "app", Project: "porty-gateway", State: "running", Health: "healthy"}
			if oneoff {
				row.Labels = map[string]string{api.OneoffLabel: "True"}
			}
			runtime := &controlRuntime{status: []api.ContainerSummary{row}}
			co := op.NewCoordinator()
			events := &finalEvents{done: make(chan op.Operation, 1)}
			control := ctl.NewControlPlane(root, controlLookup{}, stack.NewEnvironmentService(controlEnvironmentStore{}), repo.NewRepositoryService(controlGit{}), runtime, op.NewOperationService(sqlstore.NewOperationStore(db), events, time.Second, 1024), op.NewDeploymentService(runtime, &capturingDeploymentStore{saved: make(chan op.Deployment, 1)}, co), co, nil, nil)
			control.SetAlertReader(alerts)
			if _, err := control.StartAction(ctx, "stk_gateway", "deploy"); err != nil {
				t.Fatal(err)
			}
			select {
			case <-events.done:
			case <-time.After(time.Second):
				t.Fatal("no completion")
			}
			deployment, err := alerts.Find(ctx, alert.Key{StackID: "stk_gateway", Problem: "deployment", Target: "stack"})
			if err != nil {
				t.Fatal(err)
			}
			if (deployment.ResolvedAt == nil) != oneoff {
				t.Fatalf("recovery incorrectly classified: %+v", deployment)
			}
			unrelated, err := alerts.Find(ctx, alert.Key{StackID: "stk_gateway", Problem: "stop", Target: "stack"})
			if err != nil || unrelated.ResolvedAt != nil {
				t.Fatalf("unrelated resolved: %+v %v", unrelated, err)
			}
		})
	}
}

func TestManualDeploymentFailureCreatesPersistentAlert(t *testing.T) {
	ctx := context.Background()
	db, err := sqlstore.Open(ctx, filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	events := &finalEvents{done: make(chan op.Operation, 1)}
	ops := op.NewOperationService(sqlstore.NewOperationStore(db), events, time.Second, 1024)
	co := op.NewCoordinator()
	runtime := &controlRuntime{deployErr: errors.New("cannot start secret")}
	control := ctl.NewControlPlane("/srv/repository", controlLookup{}, stack.NewEnvironmentService(controlEnvironmentStore{}), repo.NewRepositoryService(controlGit{}), runtime, ops, op.NewDeploymentService(runtime, &capturingDeploymentStore{saved: make(chan op.Deployment, 1)}, co), co, nil, nil)
	if _, err := control.StartAction(ctx, "stk_gateway", "deploy"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-events.done:
	case <-time.After(time.Second):
		t.Fatal("no completion")
	}
	page, err := sqlstore.NewAlertStore(db).List(ctx, alert.Filter{})
	if err != nil || page.Total != 1 || page.Items[0].Key.Problem != "deployment" || page.Items[0].StackName != "gateway" {
		t.Fatalf("alert: %+v %v", page, err)
	}
}

func TestManualAlertLifecycleSurvivesRestartAndRecurrence(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "gateway"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "gateway", "docker-compose.yml"), []byte("services:\n  app:\n    image: alpine\n"), 0600); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(t.TempDir(), "p.db")
	db, err := sqlstore.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { db.Close() }()
	runtime := &controlRuntime{deployErr: errors.New("failed to start"), status: []api.ContainerSummary{{ID: "a", Name: "app-1", Service: "app", Project: "porty-gateway", State: "running", Health: "healthy"}}}
	alerts := sqlstore.NewAlertStore(db)
	opsStore := sqlstore.NewOperationStore(db)
	events := &finalEvents{done: make(chan op.Operation, 1)}
	co := op.NewCoordinator()
	control := ctl.NewControlPlane(root, controlLookup{}, stack.NewEnvironmentService(controlEnvironmentStore{}), repo.NewRepositoryService(controlGit{}), runtime, op.NewOperationService(opsStore, events, time.Second, 1024), op.NewDeploymentService(runtime, &capturingDeploymentStore{saved: make(chan op.Deployment, 10)}, co), co, nil, nil)
	control.SetAlertReader(alerts)
	run := func() op.Operation {
		t.Helper()
		deadline := time.Now().Add(time.Second)
		for {
			_, err := control.StartAction(ctx, "stk_gateway", "deploy")
			if err == nil {
				break
			}
			if !errors.Is(err, op.ErrOperationConflict) || time.Now().After(deadline) {
				t.Fatal(err)
			}
			time.Sleep(time.Millisecond)
		}
		select {
		case result := <-events.done:
			return result
		case <-time.After(time.Second):
			t.Fatal("operation not completed")
			return op.Operation{}
		}
	}
	failed := run()
	if failed.Status != op.OperationFailed {
		t.Fatal(failed.Status)
	}
	page, err := alerts.List(ctx, alert.Filter{})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("alerts=%+v %v", page, err)
	}
	item, err := alerts.Acknowledge(ctx, alert.Mutation{ID: page.Items[0].ID, ExpectedRevision: 1, ActorID: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	runtime.deployErr = nil
	if run().Status != op.OperationSucceeded {
		t.Fatal("recovery failed")
	}
	recovered, err := alerts.Get(ctx, item.ID)
	if err != nil || recovered.ResolvedAt == nil || recovered.AcknowledgedAt == nil {
		t.Fatalf("recovery=%+v %v", recovered, err)
	}
	historical, err := opsStore.Operation(ctx, failed.ID)
	if err != nil || historical.Status != op.OperationFailed {
		t.Fatal("recovery changed failure history")
	}
	runtime.deployErr = errors.New("failed again")
	run()
	reopened, err := alerts.Get(ctx, item.ID)
	if err != nil || reopened.Episode != 2 || reopened.AcknowledgedAt != nil || reopened.ResolvedAt != nil {
		t.Fatalf("recurrence=%+v %v", reopened, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = sqlstore.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := sqlstore.NewOperationStore(db).FailInterrupted(ctx, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	persisted, err := sqlstore.NewAlertStore(db).Get(ctx, item.ID)
	if err != nil || persisted.Revision != reopened.Revision || persisted.Count != reopened.Count {
		t.Fatalf("restart duplicated alert: %+v %v", persisted, err)
	}
}
