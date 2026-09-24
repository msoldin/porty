package control

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/docker/compose/v5/pkg/api"
	portycompose "github.com/msoldin/porty/internal/compose"
	portyop "github.com/msoldin/porty/internal/operation"
	portystack "github.com/msoldin/porty/internal/stack"
)

var (
	ErrContainerNotFound          = errors.New("container not found in stack")
	ErrContainerStateConflict     = errors.New("container action conflicts with its state")
	ErrContainerArchived          = errors.New("archived stack cannot run container actions")
	ErrUnsupportedContainerAction = errors.New("unsupported container action")
	ErrInvalidContainerSelection  = errors.New("invalid container selection")
)

type Container struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Service  string          `json:"service"`
	State    string          `json:"state"`
	Health   string          `json:"health"`
	Image    string          `json:"image"`
	Networks []string        `json:"networks"`
	Ports    []ContainerPort `json:"ports"`
}

type ContainerPort struct {
	Host          string `json:"host"`
	TargetPort    int    `json:"targetPort"`
	PublishedPort int    `json:"publishedPort"`
	Protocol      string `json:"protocol"`
}

func (c *ControlPlane) Containers(ctx context.Context, id portystack.StackID) ([]Container, error) {
	stack, err := c.lookup.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	values, err := c.environment.Values(ctx, id)
	if err != nil {
		return nil, err
	}
	request := portycompose.Request{StackDir: filepath.Join(c.root, stack.DirectoryName), ProjectName: stack.ComposeProjectName, Environment: values}
	rows, err := c.runtime.Status(ctx, request)
	if err != nil {
		return nil, err
	}
	items := make([]Container, 0, len(rows))
	for _, row := range rows {
		if row.Project == stack.ComposeProjectName {
			items = append(items, containerFromSummary(row))
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Service != items[j].Service {
			return items[i].Service < items[j].Service
		}
		if items[i].Name != items[j].Name {
			return items[i].Name < items[j].Name
		}
		return items[i].ID < items[j].ID
	})
	return items, nil
}

func containerFromSummary(row api.ContainerSummary) Container {
	networks := append([]string{}, row.Networks...)
	sort.Strings(networks)
	ports := make([]ContainerPort, 0, len(row.Publishers))
	for _, publisher := range row.Publishers {
		ports = append(ports, ContainerPort{
			Host: publisher.URL, TargetPort: publisher.TargetPort,
			PublishedPort: publisher.PublishedPort, Protocol: publisher.Protocol,
		})
	}
	sort.Slice(ports, func(i, j int) bool {
		a, b := ports[i], ports[j]
		if a.TargetPort != b.TargetPort {
			return a.TargetPort < b.TargetPort
		}
		if a.PublishedPort != b.PublishedPort {
			return a.PublishedPort < b.PublishedPort
		}
		if a.Protocol != b.Protocol {
			return a.Protocol < b.Protocol
		}
		return a.Host < b.Host
	})
	return Container{
		ID: row.ID, Name: row.Name, Service: row.Service,
		State: strings.ToLower(string(row.State)), Health: strings.ToLower(string(row.Health)),
		Image: row.Image, Networks: networks, Ports: ports,
	}
}

func containerActionAllowed(state, action string) bool {
	switch action {
	case "start":
		return state == "created" || state == "exited"
	case "stop", "restart":
		return state == "running"
	default:
		return false
	}
}

func (c *ControlPlane) StartContainerAction(ctx context.Context, id portystack.StackID, containerID, action string) (portyop.Operation, error) {
	if action != "start" && action != "stop" && action != "restart" {
		return portyop.Operation{}, ErrUnsupportedContainerAction
	}
	release, err := c.coordinator.Try(false, string(id))
	if err != nil {
		return portyop.Operation{}, err
	}
	handoff := false
	defer func() {
		if !handoff {
			release()
		}
	}()
	stack, err := c.lookup.ByID(ctx, id)
	if err != nil {
		return portyop.Operation{}, err
	}
	if stack.ArchivedAt != nil {
		return portyop.Operation{}, ErrContainerArchived
	}
	values, err := c.environment.Values(ctx, id)
	if err != nil {
		return portyop.Operation{}, err
	}
	request := portycompose.Request{StackDir: filepath.Join(c.root, stack.DirectoryName), ProjectName: stack.ComposeProjectName, Environment: values}
	rows, err := c.runtime.Status(ctx, request)
	if err != nil {
		return portyop.Operation{}, err
	}
	var selected *api.ContainerSummary
	for i := range rows {
		if rows[i].ID == containerID && rows[i].Project == stack.ComposeProjectName {
			selected = &rows[i]
			break
		}
	}
	if selected == nil {
		return portyop.Operation{}, ErrContainerNotFound
	}
	if !containerActionAllowed(strings.ToLower(string(selected.State)), action) {
		return portyop.Operation{}, ErrContainerStateConflict
	}
	operation, err := c.operations.Start(ctx, portyop.OperationRequest{
		Kind: "container_" + action, ScopeType: "stack", ScopeID: string(id), Secrets: mapValues(values),
	}, func(jobCtx context.Context) (string, error) {
		defer release()
		return "", c.runtime.ContainerAction(jobCtx, request, containerID, action)
	})
	if err != nil {
		return portyop.Operation{}, err
	}
	handoff = true
	return operation, nil
}

func (c *ControlPlane) StartContainerBatchAction(ctx context.Context, id portystack.StackID, containerIDs []string, action string) (portyop.Operation, error) {
	if action != "start" && action != "stop" && action != "restart" {
		return portyop.Operation{}, ErrUnsupportedContainerAction
	}
	if len(containerIDs) == 0 || len(containerIDs) > 20 {
		return portyop.Operation{}, ErrInvalidContainerSelection
	}
	ids := append([]string(nil), containerIDs...)
	seen := make(map[string]bool, len(ids))
	for _, containerID := range ids {
		if containerID == "" || len(containerID) > 128 || seen[containerID] {
			return portyop.Operation{}, ErrInvalidContainerSelection
		}
		for _, ch := range containerID {
			if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_') {
				return portyop.Operation{}, ErrInvalidContainerSelection
			}
		}
		seen[containerID] = true
	}
	release, err := c.coordinator.Try(false, string(id))
	if err != nil {
		return portyop.Operation{}, err
	}
	handoff := false
	defer func() {
		if !handoff {
			release()
		}
	}()
	stack, err := c.lookup.ByID(ctx, id)
	if err != nil {
		return portyop.Operation{}, err
	}
	if stack.ArchivedAt != nil {
		return portyop.Operation{}, ErrContainerArchived
	}
	values, err := c.environment.Values(ctx, id)
	if err != nil {
		return portyop.Operation{}, err
	}
	request := portycompose.Request{StackDir: filepath.Join(c.root, stack.DirectoryName), ProjectName: stack.ComposeProjectName, Environment: values}
	rows, err := c.runtime.Status(ctx, request)
	if err != nil {
		return portyop.Operation{}, err
	}
	owned := make(map[string]api.ContainerSummary, len(rows))
	for _, row := range rows {
		if row.Project == stack.ComposeProjectName {
			owned[row.ID] = row
		}
	}
	for _, containerID := range ids {
		row, ok := owned[containerID]
		if !ok {
			return portyop.Operation{}, ErrContainerNotFound
		}
		if !containerActionAllowed(strings.ToLower(string(row.State)), action) {
			return portyop.Operation{}, ErrContainerStateConflict
		}
	}
	operation, err := c.operations.Start(ctx, portyop.OperationRequest{
		Kind: "container_batch_" + action, ScopeType: "stack", ScopeID: string(id), Secrets: mapValues(values),
	}, func(jobCtx context.Context) (string, error) {
		defer release()
		var output strings.Builder
		failed := false
		for _, containerID := range ids {
			if err := c.runtime.ContainerAction(jobCtx, request, containerID, action); err != nil {
				failed = true
				fmt.Fprintf(&output, "%s: failed\n", containerID)
			} else {
				fmt.Fprintf(&output, "%s: succeeded\n", containerID)
			}
		}
		if failed {
			return output.String(), errors.New("one or more container actions failed")
		}
		return output.String(), nil
	})
	if err != nil {
		return portyop.Operation{}, err
	}
	handoff = true
	return operation, nil
}
