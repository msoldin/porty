package application

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/msoldin/porty/internal/domain"
)

type OperationRepository interface {
	CreateOperation(context.Context, domain.Operation) error
	UpdateOperation(context.Context, domain.Operation) error
}

type OperationPublisher interface {
	PublishOperation(domain.Operation)
}

type OperationRequest struct {
	Kind        string
	ScopeType   string
	ScopeID     string
	RequestKey  string
	InitiatedBy string
	Secrets     []string
}

type OperationService struct {
	store     OperationRepository
	publisher OperationPublisher
	timeout   time.Duration
	maxOutput int
	now       func() time.Time
}

func NewOperationService(store OperationRepository, publisher OperationPublisher, timeout time.Duration, maxOutput int) *OperationService {
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	if maxOutput <= 0 {
		maxOutput = 256 << 10
	}
	return &OperationService{store: store, publisher: publisher, timeout: timeout, maxOutput: maxOutput, now: time.Now}
}

func (s *OperationService) Start(requestCtx context.Context, request OperationRequest, run func(context.Context) (string, error)) (domain.Operation, error) {
	operation := domain.Operation{
		ID: "op_" + randomID(12), Kind: request.Kind, ScopeType: request.ScopeType, ScopeID: request.ScopeID,
		RequestKey: request.RequestKey, InitiatedBy: request.InitiatedBy, Status: domain.OperationQueued,
	}
	if err := s.store.CreateOperation(requestCtx, operation); err != nil {
		return domain.Operation{}, err
	}
	s.publish(operation)
	go s.execute(operation, request.Secrets, run)
	return operation, nil
}

func (s *OperationService) execute(operation domain.Operation, secrets []string, run func(context.Context) (string, error)) {
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()
	operation.Status = domain.OperationRunning
	operation.StartedAt = s.now().UTC()
	_ = s.store.UpdateOperation(ctx, operation)
	s.publish(operation)
	output, err := run(ctx)
	for _, secret := range secrets {
		if secret != "" {
			output = strings.ReplaceAll(output, secret, "[REDACTED]")
		}
	}
	if len(output) > s.maxOutput {
		output = output[:s.maxOutput]
		operation.OutputTruncated = true
	}
	operation.Output = output
	operation.CompletedAt = s.now().UTC()
	switch {
	case err == nil:
		operation.Status = domain.OperationSucceeded
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		operation.Status = domain.OperationCancelled
		operation.ErrorCode = "operation_cancelled"
	default:
		operation.Status = domain.OperationFailed
		operation.ExitCode = 1
		operation.ErrorCode = "operation_failed"
	}
	_ = s.store.UpdateOperation(context.Background(), operation)
	s.publish(operation)
}

func (s *OperationService) publish(operation domain.Operation) {
	if s.publisher != nil {
		s.publisher.PublishOperation(operation)
	}
}
