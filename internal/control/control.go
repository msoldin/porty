package control

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	portycompose "github.com/msoldin/porty/internal/compose"
	portyop "github.com/msoldin/porty/internal/operation"
	portyrepo "github.com/msoldin/porty/internal/repository"
	portystack "github.com/msoldin/porty/internal/stack"
	"path/filepath"
	"strings"
)

type RuntimeController interface {
	Validate(context.Context, portycompose.Request) error
	Status(context.Context, portycompose.Request) (string, error)
	Digest(context.Context, portycompose.Request) (string, error)
	Start(context.Context, portycompose.Request) error
	Stop(context.Context, portycompose.Request) error
	Restart(context.Context, portycompose.Request) error
	Deploy(context.Context, portycompose.Request, bool) error
	Pull(context.Context, portycompose.Request) error
	Logs(context.Context, portycompose.Request, int) (string, error)
}

type ControlPlane struct {
	root        string
	lookup      portystack.StackLookup
	environment *portystack.EnvironmentService
	repository  *portyrepo.RepositoryService
	runtime     RuntimeController
	operations  *portyop.OperationService
	deployments *portyop.DeploymentService
	coordinator *portyop.Coordinator
	logs        LogPublisher
	stateStore  DeploymentStateStore
}

type DeploymentStateStore interface {
	LatestDeployment(context.Context, portystack.StackID) (portyop.Deployment, error)
}

type LogPublisher interface{ PublishLog(string, string) }

func NewControlPlane(root string, lookup portystack.StackLookup, environment *portystack.EnvironmentService, repository *portyrepo.RepositoryService, runtime RuntimeController, operations *portyop.OperationService, deployments *portyop.DeploymentService, coordinator *portyop.Coordinator, stateStore DeploymentStateStore, logs LogPublisher) *ControlPlane {
	return &ControlPlane{root: root, lookup: lookup, environment: environment, repository: repository, runtime: runtime, operations: operations, deployments: deployments, coordinator: coordinator, stateStore: stateStore, logs: logs}
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

func (c *ControlPlane) StackDiff(ctx context.Context, id portystack.StackID) (string, error) {
	stack, err := c.lookup.ByID(ctx, id)
	if err != nil {
		return "", err
	}
	return c.repository.Diff(ctx, stack.DirectoryName)
}

func (c *ControlPlane) CommitStack(ctx context.Context, id portystack.StackID, message string) (string, error) {
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

func (c *ControlPlane) StartRepositoryAction(ctx context.Context, action string) (portyop.Operation, error) {
	if action != "fetch" && action != "pull" && action != "push" {
		return portyop.Operation{}, errors.New("unsupported repository action")
	}
	release, err := c.coordinator.Try(true, "")
	if err != nil {
		return portyop.Operation{}, err
	}
	operation, startErr := c.operations.Start(ctx, portyop.OperationRequest{Kind: action, ScopeType: "repository"}, func(jobCtx context.Context) (string, error) {
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

func (c *ControlPlane) StackState(ctx context.Context, id portystack.StackID) (StackState, error) {
	stack, err := c.lookup.ByID(ctx, id)
	if err != nil {
		return StackState{}, err
	}
	values, err := c.environment.Values(ctx, id)
	if err != nil {
		return StackState{}, err
	}
	request := portycompose.Request{StackDir: filepath.Join(c.root, stack.DirectoryName), ProjectName: stack.ComposeProjectName, Environment: values}
	status, err := c.runtime.Status(ctx, request)
	if err != nil {
		return StackState{}, err
	}
	containers := parseContainerStates(status)
	digest, err := c.runtime.Digest(ctx, request)
	if err != nil {
		return StackState{}, err
	}
	var snapshot *DeploymentSnapshot
	if c.stateStore != nil {
		latest, latestErr := c.stateStore.LatestDeployment(ctx, id)
		if latestErr == nil {
			snapshot = &DeploymentSnapshot{Status: latest.Status, ComposeDigest: latest.ComposeDigest, GitCommit: latest.GitCommit}
		}
		if latestErr != nil && !errors.Is(latestErr, sql.ErrNoRows) {
			return StackState{}, latestErr
		}
	}
	return StackState{Runtime: AggregateRuntime(containers), Freshness: ClassifyDeployment(snapshot, digest, false)}, nil
}

func parseContainerStates(output string) []ContainerState {
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
	result := make([]ContainerState, 0, len(rows))
	for _, row := range rows {
		if strings.EqualFold(row.Health, "unhealthy") {
			result = append(result, ContainerUnhealthy)
		} else if strings.EqualFold(row.State, "running") {
			result = append(result, ContainerRunning)
		} else {
			result = append(result, ContainerStopped)
		}
	}
	return result
}

func (c *ControlPlane) StartAction(ctx context.Context, id portystack.StackID, action string) (portyop.Operation, error) {
	stack, err := c.lookup.ByID(ctx, id)
	if err != nil {
		return portyop.Operation{}, err
	}
	values, err := c.environment.Values(ctx, id)
	if err != nil {
		return portyop.Operation{}, err
	}
	request := portycompose.Request{StackDir: filepath.Join(c.root, stack.DirectoryName), ProjectName: stack.ComposeProjectName, Environment: values}
	switch action {
	case "deploy", "recreate":
		release, err := c.coordinator.Try(false, string(id))
		if err != nil {
			return portyop.Operation{}, err
		}
		operationID := portyop.NewOperationID()
		operation, startErr := c.operations.Start(ctx, portyop.OperationRequest{ID: operationID, Kind: action, ScopeType: "stack", ScopeID: string(id), Secrets: mapValues(values)}, func(jobCtx context.Context) (string, error) {
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
			deployment, err := c.deployments.DeployLocked(jobCtx, portyop.DeployRequest{StackID: id, OperationID: operationID, StackDir: request.StackDir, ProjectName: request.ProjectName, Environment: values, GitCommit: head, Dirty: status.Dirty, DiffDigest: diffDigest, Recreate: action == "recreate"})
			return fmt.Sprintf("deployment %s", deployment.ID), err
		})
		if startErr != nil {
			release()
		}
		return operation, startErr
	case "validate", "status", "start", "stop", "restart", "pull", "logs":
		release, err := c.coordinator.Try(false, string(id))
		if err != nil {
			return portyop.Operation{}, err
		}
		operation, startErr := c.operations.Start(ctx, portyop.OperationRequest{Kind: action, ScopeType: "stack", ScopeID: string(id), Secrets: mapValues(values), DiscardOutput: action == "logs"}, func(jobCtx context.Context) (string, error) {
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
		return portyop.Operation{}, errors.New("unsupported stack action")
	}
}

func mapValues(values map[string]string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	return result
}
