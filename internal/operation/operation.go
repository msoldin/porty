package operation

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/msoldin/porty/internal/alert"
)

type OperationRepository interface {
	CreateOperation(context.Context, Operation) error
	UpdateOperation(context.Context, Operation) error
	CompleteOperation(context.Context, Operation, Result) ([]alert.Alert, error)
}

type OperationPublisher interface {
	PublishOperation(Operation)
}

type OperationRequest struct {
	Timeout       time.Duration
	ID            string
	Kind          string
	ScopeType     string
	ScopeID       string
	RequestKey    string
	InitiatedBy   string
	Secrets       []string
	DiscardOutput bool
	AlertTargets  []alert.Key
	Trigger       string
	StackName     string
}

type Result struct {
	Update *UpdateCompletion
	Output string
	Err    error
	Alerts []alert.Change
}

type trackedOperation struct {
	done      chan struct{}
	cancel    context.CancelFunc
	operation Operation
	err       error
}

var ErrShuttingDown = errors.New("operation service is shutting down")

type OperationService struct {
	stopped   bool
	mu        sync.Mutex
	jobs      map[string]*trackedOperation
	completed []string
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
	return &OperationService{jobs: make(map[string]*trackedOperation), store: store, publisher: publisher, timeout: timeout, maxOutput: maxOutput, now: time.Now}
}

func (s *OperationService) Start(requestCtx context.Context, request OperationRequest, run func(context.Context) (string, error)) (Operation, error) {
	return s.StartTracked(requestCtx, request, func(ctx context.Context) Result { output, err := run(ctx); return Result{Output: output, Err: err} }, nil)
}

// StartTracked transfers release ownership only after acceptance. The release
// runs even if persistence fails before the runtime callback can execute.
func (s *OperationService) StartTracked(requestCtx context.Context, request OperationRequest, run func(context.Context) Result, release func()) (Operation, error) {
	operationID := request.ID
	if operationID == "" {
		operationID = NewOperationID()
	}
	operation := Operation{
		ID: operationID, Kind: request.Kind, ScopeType: request.ScopeType, ScopeID: request.ScopeID,
		RequestKey: request.RequestKey, InitiatedBy: request.InitiatedBy, Status: OperationQueued,
		AlertTargets: request.AlertTargets, Trigger: request.Trigger, StackName: request.StackName,
	}
	if operation.Trigger == "" {
		operation.Trigger = "manual"
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return Operation{}, ErrShuttingDown
	}
	if err := s.store.CreateOperation(requestCtx, operation); err != nil {
		s.mu.Unlock()
		return Operation{}, err
	}
	timeout := request.Timeout
	if timeout <= 0 {
		timeout = s.timeout
	}
	if timeout > 20*time.Minute {
		timeout = 20 * time.Minute
	}
	jobCtx, cancel := context.WithTimeout(context.Background(), timeout)
	job := &trackedOperation{done: make(chan struct{}), cancel: cancel}
	s.jobs[operation.ID] = job
	s.mu.Unlock()
	s.publish(operation)
	go func() {
		completed, err := s.execute(jobCtx, operation, request.Secrets, request.DiscardOutput, run, release)
		cancel()
		completed.Output = "" // Wait reports status; authoritative output stays in persistent history.
		s.mu.Lock()
		defer s.mu.Unlock()
		job.operation = completed
		job.err = err
		close(job.done)
		s.completed = append(s.completed, operation.ID)
		if len(s.completed) > 128 {
			delete(s.jobs, s.completed[0])
			s.completed = s.completed[1:]
		}
	}()
	return operation, nil
}

func (s *OperationService) execute(ctx context.Context, operation Operation, secrets []string, discardOutput bool, run func(context.Context) Result, release func()) (Operation, error) {
	if release != nil {
		defer release()
	}
	operation.Status = OperationRunning
	operation.StartedAt = s.now().UTC()
	if err := s.store.UpdateOperation(ctx, operation); err != nil {
		slog.Error("operation running state could not be saved", "operation_id", operation.ID)
		return operation, err
	}
	s.publish(operation)
	result := run(ctx)
	output, err := result.Output, result.Err
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
	if err != nil && len(result.Alerts) == 0 {
		for _, key := range operation.AlertTargets {
			result.Alerts = append(result.Alerts, alert.Change{Kind: "failure", Key: key, StackName: operation.StackName, OccurrenceID: operation.ID, OperationID: operation.ID, Summary: "Stack operation failed. See operation details.", ObservedAt: operation.CompletedAt, CanResolveManually: true})
		}
	}
	// The store receives only already-redacted output. Alert text is controlled
	// by producers, but redact it again before persisting it across features.
	for i := range result.Alerts {
		for _, secret := range secrets {
			if secret != "" {
				result.Alerts[i].Summary = strings.ReplaceAll(result.Alerts[i].Summary, secret, "[REDACTED]")
			}
		}
	}
	if result.Update != nil {
		operation.ServiceUpdates = result.Update.Services
	}
	result.Output = operation.Output
	finishCtx, finishCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer finishCancel()
	changed, saveErr := s.store.CompleteOperation(finishCtx, operation, result)
	if saveErr != nil {
		slog.Error("operation completion could not be saved", "operation_id", operation.ID)
		return operation, saveErr
	}
	s.publish(operation)
	if publisher, ok := s.publisher.(alert.Publisher); ok {
		for _, a := range changed {
			publisher.PublishAlert(a)
		}
	}
	return operation, nil
}

func NewOperationID() string { return "op_" + randomID(12) }

func (s *OperationService) publish(operation Operation) {
	if s.publisher != nil {
		s.publisher.PublishOperation(operation)
	}
}

// Wait returns the accepted operation's terminal status after its coordinator is released.
func (s *OperationService) Wait(ctx context.Context, id string) (Operation, error) {
	s.mu.Lock()
	job := s.jobs[id]
	s.mu.Unlock()
	if job == nil {
		return Operation{}, errors.New("tracked operation unavailable")
	}
	select {
	case <-ctx.Done():
		return Operation{}, ctx.Err()
	case <-job.done:
		return job.operation, job.err
	}
}

func (s *OperationService) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	s.stopped = true
	jobs := make([]*trackedOperation, 0, len(s.jobs))
	for _, job := range s.jobs {
		jobs = append(jobs, job)
	}
	s.mu.Unlock()
	for _, job := range jobs {
		select {
		case <-job.done:
		case <-ctx.Done():
			for _, pending := range jobs {
				pending.cancel()
			}
			return ctx.Err()
		}
	}
	return nil
}
