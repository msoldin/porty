package control

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/docker/compose/v5/pkg/api"
	"github.com/msoldin/porty/internal/alert"
	compose "github.com/msoldin/porty/internal/compose"
	op "github.com/msoldin/porty/internal/operation"
)

type AlertReader interface {
	Find(context.Context, alert.Key) (alert.Alert, error)
}

func (c *ControlPlane) SetAlertReader(reader AlertReader) { c.alerts = reader }

func stackAlertTargets(id, action string) []alert.Key {
	problem := action
	switch action {
	case "deploy", "recreate":
		problem = "deployment"
	case "start", "stop", "restart", "pull":
	default:
		return nil
	}
	return []alert.Key{{StackID: id, Problem: problem, Target: "stack"}}
}

func containerAlertKey(id, action string, row api.ContainerSummary) alert.Key {
	// Compose's container name is stable across recreation, unlike its ID.
	return alert.Key{StackID: id, Problem: action, Target: "service:" + row.Service + "/replica:" + row.Name}
}

func (c *ControlPlane) previousAlerts(ctx context.Context, keys []alert.Key) (map[alert.Key]int64, error) {
	revisions := map[alert.Key]int64{}
	if c.alerts == nil {
		return revisions, nil
	}
	for _, key := range keys {
		a, err := c.alerts.Find(ctx, key)
		if errors.Is(err, alert.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if a.ResolvedAt == nil {
			revisions[key] = a.Revision
		}
	}
	return revisions, nil
}

func observedChange(request op.OperationRequest, key alert.Key, revision int64, err error, verified bool) []alert.Change {
	if err == nil && (!verified || revision == 0) {
		return nil
	}
	kind, summary := "recovery", "Verified recovery"
	if err != nil {
		kind = "failure"
		summary = "Stack operation failed. See operation details."
	}
	return []alert.Change{{Kind: kind, Key: key, StackName: request.StackName, OccurrenceID: request.ID, OperationID: request.ID, Summary: summary, ExpectedRevision: revision, ObservedAt: time.Now().UTC(), CanResolveManually: true}}
}

func (c *ControlPlane) startObserved(ctx context.Context, request op.OperationRequest, run func(context.Context) (string, error), verify func(context.Context) bool, release func()) (op.Operation, error) {
	if request.ID == "" {
		request.ID = op.NewOperationID()
	}
	revisions, err := c.previousAlerts(ctx, request.AlertTargets)
	if err != nil {
		return op.Operation{}, err
	}
	return c.operations.StartTracked(ctx, request, func(jobCtx context.Context) op.Result {
		output, err := run(jobCtx)
		result := op.Result{Output: output, Err: err}
		verified := false
		if err == nil && len(revisions) > 0 && verify != nil {
			verified = verify(jobCtx)
		}
		for _, key := range request.AlertTargets {
			result.Alerts = append(result.Alerts, observedChange(request, key, revisions[key], err, verified)...)
		}
		return result
	}, release)
}

func (c *ControlPlane) verifyStackAction(ctx context.Context, request compose.Request, action string) bool {
	if action == "pull" {
		return true
	} // successful pull reads the transfer to completion
	project, err := compose.Load(ctx, request)
	if err != nil {
		return false
	}
	rows, err := c.runtime.Status(ctx, request)
	if err != nil {
		return false
	}
	counts := map[string]int{}
	for _, row := range rows {
		if row.Project != request.ProjectName {
			continue
		}
		if strings.EqualFold(row.Labels[api.OneoffLabel], "true") {
			continue
		}
		if _, ok := project.Services[row.Service]; !ok {
			continue
		}
		state, health := strings.ToLower(string(row.State)), strings.ToLower(string(row.Health))
		if action == "stop" {
			if state == "running" || state == "restarting" || state == "paused" {
				return false
			}
			continue
		}
		if state != "running" || health != "" && health != "healthy" {
			return false
		}
		counts[row.Service]++
	}
	if action == "stop" {
		return true
	}
	for name, service := range project.Services {
		if counts[name] != service.GetScale() {
			return false
		}
	}
	return len(project.Services) > 0
}

func (c *ControlPlane) verifyContainerAction(ctx context.Context, request compose.Request, id, action string) bool {
	rows, err := c.runtime.Status(ctx, request)
	if err != nil {
		return false
	}
	for _, row := range rows {
		if row.ID == id && row.Project == request.ProjectName {
			if action == "stop" {
				return row.State == "exited" || row.State == "created"
			}
			return row.State == "running" && (row.Health == "" || row.Health == "healthy")
		}
	}
	return false
}

func (c *ControlPlane) startContainerBatchObserved(ctx context.Context, request op.OperationRequest, composeRequest compose.Request, ids []string, owned map[string]api.ContainerSummary, action string, release func()) (op.Operation, error) {
	request.ID = op.NewOperationID()
	for _, id := range ids {
		request.AlertTargets = append(request.AlertTargets, containerAlertKey(request.ScopeID, action, owned[id]))
	}
	revisions, err := c.previousAlerts(ctx, request.AlertTargets)
	if err != nil {
		return op.Operation{}, err
	}
	return c.operations.StartTracked(ctx, request, func(jobCtx context.Context) op.Result {
		var output strings.Builder
		result := op.Result{}
		for _, id := range ids {
			key := containerAlertKey(request.ScopeID, action, owned[id])
			err := c.runtime.ContainerAction(jobCtx, composeRequest, id, action)
			verified := err == nil && revisions[key] > 0 && c.verifyContainerAction(jobCtx, composeRequest, id, action)
			result.Alerts = append(result.Alerts, observedChange(request, key, revisions[key], err, verified)...)
			if err != nil {
				result.Err = errors.New("one or more container actions failed")
				fmt.Fprintf(&output, "%s: failed\n", id)
			} else {
				fmt.Fprintf(&output, "%s: succeeded\n", id)
			}
		}
		result.Output = output.String()
		return result
	}, release)
}
