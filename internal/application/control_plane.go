package application

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/msoldin/porty/internal/domain"
)

type RuntimeController interface {
	Validate(context.Context, ComposeRequest) error
	Status(context.Context, ComposeRequest) (string, error)
	Start(context.Context, ComposeRequest) error
	Stop(context.Context, ComposeRequest) error
	Restart(context.Context, ComposeRequest) error
	Deploy(context.Context, ComposeRequest, bool) error
	Pull(context.Context, ComposeRequest) error
	Logs(context.Context, ComposeRequest, int) (string, error)
}

type ControlPlane struct {
	root        string
	lookup      StackLookup
	environment *EnvironmentService
	repository  *RepositoryService
	runtime     RuntimeController
	operations  *OperationService
	deployments *DeploymentService
	coordinator *Coordinator
}

func NewControlPlane(root string, lookup StackLookup, environment *EnvironmentService, repository *RepositoryService, runtime RuntimeController, operations *OperationService, deployments *DeploymentService, coordinator *Coordinator) *ControlPlane {
	return &ControlPlane{root: root, lookup: lookup, environment: environment, repository: repository, runtime: runtime, operations: operations, deployments: deployments, coordinator: coordinator}
}

func (c *ControlPlane) RepositoryStatus(ctx context.Context) (domain.GitStatus, error) {
	return c.repository.Status(ctx)
}

func (c *ControlPlane) RepositoryHistory(ctx context.Context, limit int) ([]domain.GitCommit, error) {
	return c.repository.History(ctx, limit)
}

func (c *ControlPlane) StackDiff(ctx context.Context, id domain.StackID) (string, error) {
	stack, err := c.lookup.ByID(ctx, id)
	if err != nil {
		return "", err
	}
	return c.repository.Diff(ctx, stack.DirectoryName)
}

func (c *ControlPlane) CommitStack(ctx context.Context, id domain.StackID, message string) (string, error) {
	stack, err := c.lookup.ByID(ctx, id)
	if err != nil {
		return "", err
	}
	return c.repository.Commit(ctx, stack.DirectoryName, message)
}

func (c *ControlPlane) StartRepositoryAction(ctx context.Context, action string) (domain.Operation, error) {
	if action != "pull" && action != "push" {
		return domain.Operation{}, errors.New("unsupported repository action")
	}
	return c.operations.Start(ctx, OperationRequest{Kind: action, ScopeType: "repository"}, func(jobCtx context.Context) (string, error) {
		release, err := c.coordinator.Try(true, "")
		if err != nil {
			return "", err
		}
		defer release()
		if action == "pull" {
			err = c.repository.Pull(jobCtx)
		} else {
			err = c.repository.Push(jobCtx)
		}
		return "", err
	})
}

func (c *ControlPlane) StartAction(ctx context.Context, id domain.StackID, action string) (domain.Operation, error) {
	stack, err := c.lookup.ByID(ctx, id)
	if err != nil {
		return domain.Operation{}, err
	}
	values, err := c.environment.Values(ctx, id)
	if err != nil {
		return domain.Operation{}, err
	}
	request := ComposeRequest{StackDir: filepath.Join(c.root, stack.DirectoryName), ProjectName: stack.ComposeProjectName, Environment: values}
	switch action {
	case "deploy", "recreate":
		return c.operations.Start(ctx, OperationRequest{Kind: action, ScopeType: "stack", ScopeID: string(id), Secrets: mapValues(values)}, func(jobCtx context.Context) (string, error) {
			deployment, err := c.deployments.Deploy(jobCtx, DeployRequest{StackID: id, StackDir: request.StackDir, ProjectName: request.ProjectName, Environment: values, Recreate: action == "recreate"})
			return fmt.Sprintf("deployment %s", deployment.ID), err
		})
	case "validate", "status", "start", "stop", "restart", "pull", "logs":
		return c.operations.Start(ctx, OperationRequest{Kind: action, ScopeType: "stack", ScopeID: string(id), Secrets: mapValues(values)}, func(jobCtx context.Context) (string, error) {
			release, err := c.coordinator.Try(false, string(id))
			if err != nil {
				return "", err
			}
			defer release()
			switch action {
			case "validate":
				err = c.runtime.Validate(jobCtx, request)
			case "status":
				return c.runtime.Status(jobCtx, request)
			case "start":
				err = c.runtime.Start(jobCtx, request)
			case "stop":
				err = c.runtime.Stop(jobCtx, request)
			case "restart":
				err = c.runtime.Restart(jobCtx, request)
			case "pull":
				err = c.runtime.Pull(jobCtx, request)
			case "logs":
				return c.runtime.Logs(jobCtx, request, 500)
			}
			return "", err
		})
	default:
		return domain.Operation{}, errors.New("unsupported stack action")
	}
}

func mapValues(values map[string]string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	return result
}
