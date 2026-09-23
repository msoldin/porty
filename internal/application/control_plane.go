package application

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	portyrepo "github.com/msoldin/porty/internal/repository"
	"path/filepath"
	"strings"

	"github.com/msoldin/porty/internal/domain"
)

type RuntimeController interface {
	Validate(context.Context, ComposeRequest) error
	Status(context.Context, ComposeRequest) (string, error)
	Digest(context.Context, ComposeRequest) (string, error)
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
	repository  *portyrepo.RepositoryService
	runtime     RuntimeController
	operations  *OperationService
	deployments *DeploymentService
	coordinator *Coordinator
	logs        LogPublisher
	stateStore  interface {
		LatestDeployment(context.Context, domain.StackID) (domain.Deployment, error)
	}
}

func (c *ControlPlane) ConfigureState(store interface {
	LatestDeployment(context.Context, domain.StackID) (domain.Deployment, error)
}) {
	c.stateStore = store
}

type LogPublisher interface{ PublishLog(string, string) }

func NewControlPlane(root string, lookup StackLookup, environment *EnvironmentService, repository *portyrepo.RepositoryService, runtime RuntimeController, operations *OperationService, deployments *DeploymentService, coordinator *Coordinator, logPublishers ...LogPublisher) *ControlPlane {
	control := &ControlPlane{root: root, lookup: lookup, environment: environment, repository: repository, runtime: runtime, operations: operations, deployments: deployments, coordinator: coordinator}
	if len(logPublishers) > 0 {
		control.logs = logPublishers[0]
	}
	return control
}

func (c *ControlPlane) RepositoryStatus(ctx context.Context) (portyrepo.GitStatus, error) {
	return c.repository.Status(ctx)
}

func (c *ControlPlane) RepositoryHistory(ctx context.Context, limit int) ([]portyrepo.GitCommit, error) {
	return c.repository.History(ctx, limit)
}

func (c *ControlPlane) RepositoryHistoryPage(ctx context.Context, limit, offset int) ([]portyrepo.GitCommit, error) {
	return c.repository.HistoryPage(ctx, limit, offset)
}

func (c *ControlPlane) StackDiff(ctx context.Context, id domain.StackID) (string, error) {
	stack, err := c.lookup.ByID(ctx, id)
	if err != nil {
		return "", err
	}
	return c.repository.Diff(ctx, stack.DirectoryName)
}

func (c *ControlPlane) CommitStack(ctx context.Context, id domain.StackID, message string) (string, error) {
	release, err := c.coordinator.Try(false, string(id))
	if err != nil {
		return "", err
	}
	defer release()
	stack, err := c.lookup.ByID(ctx, id)
	if err != nil {
		return "", err
	}
	return c.repository.Commit(ctx, stack.DirectoryName, message)
}

func (c *ControlPlane) StartRepositoryAction(ctx context.Context, action string) (domain.Operation, error) {
	if action != "fetch" && action != "pull" && action != "push" {
		return domain.Operation{}, errors.New("unsupported repository action")
	}
	release, err := c.coordinator.Try(true, "")
	if err != nil {
		return domain.Operation{}, err
	}
	operation, startErr := c.operations.Start(ctx, OperationRequest{Kind: action, ScopeType: "repository"}, func(jobCtx context.Context) (string, error) {
		defer release()
		var actionErr error
		if action == "fetch" {
			actionErr = c.repository.Fetch(jobCtx)
		} else if action == "pull" {
			actionErr = c.repository.Pull(jobCtx)
		} else {
			actionErr = c.repository.Push(jobCtx)
		}
		return "", actionErr
	})
	if startErr != nil {
		release()
	}
	return operation, startErr
}

func (c *ControlPlane) StackState(ctx context.Context, id domain.StackID) (domain.StackState, error) {
	stack, err := c.lookup.ByID(ctx, id)
	if err != nil {
		return domain.StackState{}, err
	}
	values, err := c.environment.Values(ctx, id)
	if err != nil {
		return domain.StackState{}, err
	}
	request := ComposeRequest{StackDir: filepath.Join(c.root, stack.DirectoryName), ProjectName: stack.ComposeProjectName, Environment: values}
	status, err := c.runtime.Status(ctx, request)
	if err != nil {
		return domain.StackState{}, err
	}
	containers := parseContainerStates(status)
	digest, err := c.runtime.Digest(ctx, request)
	if err != nil {
		return domain.StackState{}, err
	}
	var snapshot *domain.DeploymentSnapshot
	if c.stateStore != nil {
		latest, latestErr := c.stateStore.LatestDeployment(ctx, id)
		if latestErr == nil {
			snapshot = &domain.DeploymentSnapshot{Status: latest.Status, ComposeDigest: latest.ComposeDigest, GitCommit: latest.GitCommit}
		}
		if latestErr != nil && !errors.Is(latestErr, sql.ErrNoRows) {
			return domain.StackState{}, latestErr
		}
	}
	return domain.StackState{Runtime: domain.AggregateRuntime(containers), Freshness: domain.ClassifyDeployment(snapshot, digest, false)}, nil
}

func parseContainerStates(output string) []domain.ContainerState {
	var rows []struct {
		State  string `json:"State"`
		Health string `json:"Health"`
	}
	if err := json.Unmarshal([]byte(output), &rows); err != nil {
		for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
			var row struct {
				State  string `json:"State"`
				Health string `json:"Health"`
			}
			if json.Unmarshal([]byte(line), &row) == nil {
				rows = append(rows, row)
			}
		}
	}
	result := make([]domain.ContainerState, 0, len(rows))
	for _, row := range rows {
		if strings.EqualFold(row.Health, "unhealthy") {
			result = append(result, domain.ContainerUnhealthy)
		} else if strings.EqualFold(row.State, "running") {
			result = append(result, domain.ContainerRunning)
		} else {
			result = append(result, domain.ContainerStopped)
		}
	}
	return result
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
		release, err := c.coordinator.Try(false, string(id))
		if err != nil {
			return domain.Operation{}, err
		}
		operationID := NewOperationID()
		operation, startErr := c.operations.Start(ctx, OperationRequest{ID: operationID, Kind: action, ScopeType: "stack", ScopeID: string(id), Secrets: mapValues(values)}, func(jobCtx context.Context) (string, error) {
			defer release()
			status, statusErr := c.repository.Status(jobCtx)
			if statusErr != nil {
				return "", statusErr
			}
			head, headErr := c.repository.Head(jobCtx)
			if headErr != nil {
				return "", headErr
			}
			diffDigest := ""
			if status.Dirty {
				diff, diffErr := c.repository.Diff(jobCtx, stack.DirectoryName)
				if diffErr != nil {
					return "", diffErr
				}
				digest := sha256.Sum256([]byte(diff))
				diffDigest = "sha256:" + hex.EncodeToString(digest[:])
			}
			deployment, err := c.deployments.DeployLocked(jobCtx, DeployRequest{StackID: id, OperationID: operationID, StackDir: request.StackDir, ProjectName: request.ProjectName, Environment: values, GitCommit: head, Dirty: status.Dirty, DiffDigest: diffDigest, Recreate: action == "recreate"})
			return fmt.Sprintf("deployment %s", deployment.ID), err
		})
		if startErr != nil {
			release()
		}
		return operation, startErr
	case "validate", "status", "start", "stop", "restart", "pull", "logs":
		release, err := c.coordinator.Try(false, string(id))
		if err != nil {
			return domain.Operation{}, err
		}
		operation, startErr := c.operations.Start(ctx, OperationRequest{Kind: action, ScopeType: "stack", ScopeID: string(id), Secrets: mapValues(values), DiscardOutput: action == "logs"}, func(jobCtx context.Context) (string, error) {
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
				output, logErr := c.runtime.Logs(jobCtx, request, 500)
				if logErr == nil && c.logs != nil {
					c.logs.PublishLog(string(id), output)
				}
				return output, logErr
			}
			return "", err
		})
		if startErr != nil {
			release()
		}
		return operation, startErr
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
