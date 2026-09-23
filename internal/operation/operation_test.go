package operation_test

import (
	"context"
	"errors"
	portyop "github.com/msoldin/porty/internal/operation"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestOperationContinuesAfterRequestDisconnectAndRedactsOutput(t *testing.T) {
	store := &memoryOperationStore{completed: make(chan portyop.Operation, 1)}
	service := portyop.NewOperationService(store, nil, time.Second, 64)
	requestContext, cancel := context.WithCancel(context.Background())
	operation, err := service.Start(requestContext, portyop.OperationRequest{Kind: "pull", ScopeType: "repository", Secrets: []string{"secret"}}, func(context.Context) (string, error) {
		time.Sleep(20 * time.Millisecond)
		return "secret" + strings.Repeat("x", 80), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case completed := <-store.completed:
		if completed.ID != operation.ID || completed.Status != portyop.OperationSucceeded {
			t.Fatalf("completed operation = %#v", completed)
		}
		if strings.Contains(completed.Output, "secret") || len(completed.Output) != 64 || !completed.OutputTruncated {
			t.Fatalf("unsafe output = %q truncated=%v", completed.Output, completed.OutputTruncated)
		}
	case <-time.After(time.Second):
		t.Fatal("operation did not complete after request cancellation")
	}
}

func TestOperationCanDiscardSensitiveOutput(t *testing.T) {
	store := &memoryOperationStore{completed: make(chan portyop.Operation, 1)}
	service := portyop.NewOperationService(store, nil, time.Second, 64)
	_, err := service.Start(context.Background(), portyop.OperationRequest{Kind: "logs", ScopeType: "stack", DiscardOutput: true}, func(context.Context) (string, error) {
		return "container secret output", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case completed := <-store.completed:
		if completed.Output != "" {
			t.Fatalf("discarded output persisted as %q", completed.Output)
		}
	case <-time.After(time.Second):
		t.Fatal("operation did not complete")
	}
}

func TestFailedOperationRecordsBoundedRedactedError(t *testing.T) {
	store := &memoryOperationStore{completed: make(chan portyop.Operation, 1)}
	service := portyop.NewOperationService(store, nil, time.Second, 64)
	_, err := service.Start(context.Background(), portyop.OperationRequest{Kind: "start", ScopeType: "stack", Secrets: []string{"secret"}}, func(context.Context) (string, error) {
		return "", errors.New("docker compose up: permission denied for secret: " + strings.Repeat("x", 80))
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case completed := <-store.completed:
		if completed.Status != portyop.OperationFailed || completed.ErrorCode != "operation_failed" {
			t.Fatalf("completed operation = %#v", completed)
		}
		if !strings.Contains(completed.Output, "permission denied") || strings.Contains(completed.Output, "secret") || len(completed.Output) != 64 || !completed.OutputTruncated {
			t.Fatalf("unsafe failure output = %q truncated=%v", completed.Output, completed.OutputTruncated)
		}
	case <-time.After(time.Second):
		t.Fatal("operation did not complete")
	}
}

type memoryOperationStore struct {
	mu        sync.Mutex
	operation portyop.Operation
	completed chan portyop.Operation
}

func (s *memoryOperationStore) CreateOperation(_ context.Context, operation portyop.Operation) error {
	s.mu.Lock()
	s.operation = operation
	s.mu.Unlock()
	return nil
}

func (s *memoryOperationStore) UpdateOperation(_ context.Context, operation portyop.Operation) error {
	s.mu.Lock()
	s.operation = operation
	s.mu.Unlock()
	if operation.Status == portyop.OperationSucceeded || operation.Status == portyop.OperationFailed || operation.Status == portyop.OperationCancelled {
		s.completed <- operation
	}
	return nil
}
