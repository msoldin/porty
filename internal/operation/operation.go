package operation

import (
	"context"
	"errors"
	"strings"
	"time"
)

type OperationRepository interface {
	CreateOperation(context.Context, Operation) error
	UpdateOperation(context.Context, Operation) error
}

type OperationPublisher interface {
	PublishOperation(Operation)
}

type OperationRequest struct {
	ID            string
	Kind          string
	ScopeType     string
	ScopeID       string
	RequestKey    string
	InitiatedBy   string
	Secrets       []string
	DiscardOutput bool
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

func (s *OperationService) Start(requestCtx context.Context, request OperationRequest, run func(context.Context) (string, error)) (Operation, error) {
	operationID := request.ID
	if operationID == "" {
		operationID = NewOperationID()
	}
	operation := Operation{
		ID: operationID, Kind: request.Kind, ScopeType: request.ScopeType, ScopeID: request.ScopeID,
		RequestKey: request.RequestKey, InitiatedBy: request.InitiatedBy, Status: OperationQueued,
	}
	if err := s.store.CreateOperation(requestCtx, operation); err != nil {
		return Operation{}, err
	}
	s.publish(operation)
	go s.execute(operation, request.Secrets, request.DiscardOutput, run)
	return operation, nil
}

func (s *OperationService) execute(operation Operation, secrets []string, discardOutput bool, run func(context.Context) (string, error)) {
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()
	operation.Status = OperationRunning
	operation.StartedAt = s.now().UTC()
	_ = s.store.UpdateOperation(ctx, operation)
	s.publish(operation)
	output, err := run(ctx)
	if err != nil && strings.TrimSpace(output) == "" {
		output = err.Error()
	}
	for _, secret := range secrets {
		if secret != "" {
			output = strings.ReplaceAll(output, secret, "[REDACTED]")
		}
	}
	if len(output) > s.maxOutput {
		output = output[:s.maxOutput]
		operation.OutputTruncated = true
	}
	if !discardOutput {
		operation.Output = output
	}
	operation.CompletedAt = s.now().UTC()
	switch {
	case err == nil:
		operation.Status = OperationSucceeded
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		operation.Status = OperationCancelled
		operation.ErrorCode = "operation_cancelled"
	default:
		operation.Status = OperationFailed
		operation.ExitCode = 1
		operation.ErrorCode = "operation_failed"
	}
	_ = s.store.UpdateOperation(context.Background(), operation)
	s.publish(operation)
}

func NewOperationID() string { return "op_" + randomID(12) }

func (s *OperationService) publish(operation Operation) {
	if s.publisher != nil {
		s.publisher.PublishOperation(operation)
	}
}
