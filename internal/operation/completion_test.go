package operation_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/msoldin/porty/internal/alert"
	op "github.com/msoldin/porty/internal/operation"
)

func (s *memoryOperationStore) CompleteOperation(ctx context.Context, o op.Operation, r op.Result) ([]alert.Alert, error) {
	return nil, s.UpdateOperation(ctx, o)
}

type failingCompletionStore struct {
	memoryOperationStore
	failRunning  bool
	failComplete bool
}

func (s *failingCompletionStore) UpdateOperation(ctx context.Context, o op.Operation) error {
	if s.failRunning && o.Status == op.OperationRunning {
		return errors.New("storage unavailable")
	}
	return s.memoryOperationStore.UpdateOperation(ctx, o)
}
func (s *failingCompletionStore) CompleteOperation(ctx context.Context, o op.Operation, r op.Result) ([]alert.Alert, error) {
	if s.failComplete {
		return nil, errors.New("storage unavailable")
	}
	return nil, s.memoryOperationStore.UpdateOperation(ctx, o)
}

type operationEvents struct {
	mu     sync.Mutex
	values []op.Operation
}

func (s *operationEvents) PublishOperation(o op.Operation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values = append(s.values, o)
}

func TestOperationDoesNotRunWhenRunningWriteFails(t *testing.T) {
	store := &failingCompletionStore{failRunning: true}
	svc := op.NewOperationService(store, nil, time.Second, 1024)
	released := make(chan struct{})
	ran := false
	_, err := svc.StartTracked(context.Background(), op.OperationRequest{}, func(context.Context) op.Result { ran = true; return op.Result{} }, func() { close(released) })
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("coordinator not released")
	}
	if ran {
		t.Fatal("runtime mutated before durable running state")
	}
}
func TestOperationDoesNotPublishUncommittedCompletion(t *testing.T) {
	store := &failingCompletionStore{failComplete: true}
	events := &operationEvents{}
	svc := op.NewOperationService(store, events, time.Second, 1024)
	released := make(chan struct{})
	_, err := svc.StartTracked(context.Background(), op.OperationRequest{}, func(context.Context) op.Result { return op.Result{Err: errors.New("runtime failed")} }, func() { close(released) })
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("not released")
	}
	events.mu.Lock()
	defer events.mu.Unlock()
	for _, o := range events.values {
		if o.Status == op.OperationFailed || o.Status == op.OperationSucceeded {
			t.Fatalf("published uncommitted result: %+v", o)
		}
	}
}

func TestWaitReturnsOnlyAfterCompletionAndCoordinatorRelease(t *testing.T) {
	service := op.NewOperationService(&memoryOperationStore{completed: make(chan op.Operation, 1)}, nil, time.Second, 1024)
	released := false
	accepted, err := service.StartTracked(context.Background(), op.OperationRequest{Timeout: time.Minute}, func(ctx context.Context) op.Result {
		deadline, _ := ctx.Deadline()
		if time.Until(deadline) < 50*time.Second {
			return op.Result{Err: errors.New("request budget ignored")}
		}
		return op.Result{}
	}, func() { released = true })
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Wait(context.Background(), accepted.ID)
	if err != nil || result.Status != op.OperationSucceeded || !released {
		t.Fatalf("wait=%+v err=%v released=%v", result, err, released)
	}
}
