package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/msoldin/porty/internal/domain"
)

type ComposeRequest = domain.ComposeRequest

type ComposeRuntime interface {
	Validate(context.Context, ComposeRequest) error
	Digest(context.Context, ComposeRequest) (string, error)
	Deploy(context.Context, ComposeRequest, bool) error
}

type DeploymentRepository interface {
	SaveDeployment(context.Context, domain.Deployment) error
}

type DeployRequest struct {
	StackID     domain.StackID
	StackDir    string
	ProjectName string
	Environment map[string]string
	GitCommit   string
	Dirty       bool
	DiffDigest  string
	Recreate    bool
}

type DeploymentService struct {
	runtime     ComposeRuntime
	store       DeploymentRepository
	coordinator *Coordinator
	now         func() time.Time
}

func NewDeploymentService(runtime ComposeRuntime, store DeploymentRepository, coordinator *Coordinator) *DeploymentService {
	return &DeploymentService{runtime: runtime, store: store, coordinator: coordinator, now: time.Now}
}

func (s *DeploymentService) Deploy(ctx context.Context, request DeployRequest) (domain.Deployment, error) {
	release, err := s.coordinator.Try(false, string(request.StackID))
	if err != nil {
		return domain.Deployment{}, err
	}
	defer release()
	started := s.now().UTC()
	deployment := domain.Deployment{
		ID: "dep_" + randomID(12), StackID: request.StackID, OperationID: "op_" + randomID(12),
		GitCommit: request.GitCommit, Dirty: request.Dirty, DiffDigest: request.DiffDigest,
		Status: domain.DeploymentFailed, StartedAt: started,
	}
	composeRequest := ComposeRequest{StackDir: request.StackDir, ProjectName: request.ProjectName, Environment: request.Environment}
	if err := s.runtime.Validate(ctx, composeRequest); err != nil {
		deployment.ErrorCode = "compose_validation_failed"
		return s.finish(ctx, deployment, err)
	}
	digest, err := s.runtime.Digest(ctx, composeRequest)
	if err != nil {
		deployment.ErrorCode = "compose_digest_failed"
		return s.finish(ctx, deployment, err)
	}
	deployment.ComposeDigest = digest
	if err := s.runtime.Deploy(ctx, composeRequest, request.Recreate); err != nil {
		deployment.ErrorCode = "compose_deploy_failed"
		return s.finish(ctx, deployment, err)
	}
	deployment.Status = domain.DeploymentSucceeded
	return s.finish(ctx, deployment, nil)
}

func (s *DeploymentService) finish(ctx context.Context, deployment domain.Deployment, operationErr error) (domain.Deployment, error) {
	deployment.CompletedAt = s.now().UTC()
	deployment.Duration = deployment.CompletedAt.Sub(deployment.StartedAt)
	if err := s.store.SaveDeployment(ctx, deployment); err != nil {
		if operationErr != nil {
			return deployment, errors.Join(operationErr, err)
		}
		return deployment, err
	}
	return deployment, operationErr
}

type Coordinator struct {
	mu         sync.Mutex
	repository bool
	stacks     map[string]bool
}

var ErrOperationConflict = errors.New("conflicting operation in progress")

func NewCoordinator() *Coordinator { return &Coordinator{stacks: make(map[string]bool)} }

func (c *Coordinator) Try(repository bool, stackID string) (func(), error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if repository {
		if c.repository || len(c.stacks) > 0 {
			return nil, ErrOperationConflict
		}
		c.repository = true
	} else if c.repository {
		return nil, ErrOperationConflict
	}
	if stackID != "" {
		if c.stacks[stackID] {
			if repository {
				c.repository = false
			}
			return nil, ErrOperationConflict
		}
		c.stacks[stackID] = true
	}
	return func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if repository {
			c.repository = false
		}
		delete(c.stacks, stackID)
	}, nil
}

func randomID(size int) string {
	value := make([]byte, size)
	_, _ = rand.Read(value)
	return hex.EncodeToString(value)
}
