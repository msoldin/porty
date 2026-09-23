package application_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/msoldin/porty/internal/application"
	"github.com/msoldin/porty/internal/domain"
)

func TestOperationContinuesAfterRequestDisconnectAndRedactsOutput(t *testing.T) {
	store := &memoryOperationStore{completed: make(chan domain.Operation, 1)}
	service := application.NewOperationService(store, nil, time.Second, 64)
	requestContext, cancel := context.WithCancel(context.Background())
	operation, err := service.Start(requestContext, application.OperationRequest{Kind: "pull", ScopeType: "repository", Secrets: []string{"secret"}}, func(context.Context) (string, error) {
		time.Sleep(20 * time.Millisecond)
		return "secret" + strings.Repeat("x", 80), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case completed := <-store.completed:
		if completed.ID != operation.ID || completed.Status != domain.OperationSucceeded {
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
	store := &memoryOperationStore{completed: make(chan domain.Operation, 1)}
	service := application.NewOperationService(store, nil, time.Second, 64)
	_, err := service.Start(context.Background(), application.OperationRequest{Kind: "logs", ScopeType: "stack", DiscardOutput: true}, func(context.Context) (string, error) {
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
	store := &memoryOperationStore{completed: make(chan domain.Operation, 1)}
	service := application.NewOperationService(store, nil, time.Second, 64)
	_, err := service.Start(context.Background(), application.OperationRequest{Kind: "start", ScopeType: "stack", Secrets: []string{"secret"}}, func(context.Context) (string, error) {
		return "", errors.New("docker compose up: permission denied for secret: " + strings.Repeat("x", 80))
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case completed := <-store.completed:
		if completed.Status != domain.OperationFailed || completed.ErrorCode != "operation_failed" {
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
	operation domain.Operation
	completed chan domain.Operation
}

func (s *memoryOperationStore) CreateOperation(_ context.Context, operation domain.Operation) error {
	s.mu.Lock()
	s.operation = operation
	s.mu.Unlock()
	return nil
}

func (s *memoryOperationStore) UpdateOperation(_ context.Context, operation domain.Operation) error {
	s.mu.Lock()
	s.operation = operation
	s.mu.Unlock()
	if operation.Status == domain.OperationSucceeded || operation.Status == domain.OperationFailed || operation.Status == domain.OperationCancelled {
		s.completed <- operation
	}
	return nil
}
