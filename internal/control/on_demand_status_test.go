package control_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/docker/compose/v5/pkg/api"
	ctl "github.com/msoldin/porty/internal/control"
	"github.com/msoldin/porty/internal/ondemand"
	"github.com/msoldin/porty/internal/stack"
)

type statusGroupStore struct {
	ctl.OnDemandStore
	groups []ondemand.Group
}

func (s statusGroupStore) ListGroups(context.Context, stack.StackID) ([]ondemand.Group, error) {
	return s.groups, nil
}

func TestOnDemandStatusDistinguishesSleepFromUnexpectedStops(t *testing.T) {
	for _, mode := range []string{"sleeping", "running companion", "stopped companion", "unhealthy companion", "held", "paused", "disabled", "starting", "replaced", "missing member", "running member"} {
		t.Run(mode, func(t *testing.T) {
			g := ondemand.Group{ID: "g", StackID: "stk_gateway", Phase: ondemand.Sleeping, Policy: ondemand.Policy{Enabled: true, Members: []string{"game"}}, Evidence: ondemand.Evidence{ContainerIDs: []string{"one"}}}
			rows := []api.ContainerSummary{{ID: "one", Service: "game", Project: "porty-gateway", State: "exited"}}
			wantSleeping, wantRuntime := false, "stopped"
			switch mode {
			case "sleeping":
				wantSleeping, wantRuntime = true, "sleeping"
			case "running companion":
				rows = append(rows, api.ContainerSummary{ID: "two", Service: "sidecar", Project: "porty-gateway", State: "running"})
				wantSleeping, wantRuntime = true, "on_demand"
			case "stopped companion":
				rows = append(rows, api.ContainerSummary{ID: "two", Service: "sidecar", Project: "porty-gateway", State: "exited"})
				wantSleeping, wantRuntime = true, "partial"
			case "unhealthy companion":
				rows = append(rows, api.ContainerSummary{ID: "two", Service: "sidecar", Project: "porty-gateway", State: "running", Health: "unhealthy"})
				wantSleeping, wantRuntime = true, "unhealthy"
			case "held":
				g.HoldReason = "Manual stop"
			case "paused":
				g.PausedReason = "Port unavailable"
			case "disabled":
				g.Enabled = false
			case "starting":
				g.Phase = ondemand.Starting
			case "replaced":
				rows[0].ID = "replacement"
			case "missing member":
				g.Members = append(g.Members, "missing")
				g.Evidence.ContainerIDs = append(g.Evidence.ContainerIDs, "missing")
			case "running member":
				rows[0].State = "running"
				wantRuntime = "running"
			}
			runtime := &controlRuntime{status: rows}
			control := ctl.NewControlPlane("/srv/repository", controlLookup{}, stack.NewEnvironmentService(controlEnvironmentStore{}), nil, runtime, nil, nil, nil, nil, nil)
			ctl.NewOnDemandService(control, statusGroupStore{groups: []ondemand.Group{g}})
			items, err := control.Containers(context.Background(), "stk_gateway")
			if err != nil || len(items) == 0 {
				t.Fatalf("containers: %v %v", items, err)
			}
			encoded, _ := json.Marshal(items[0])
			var result struct {
				OnDemandSleeping bool `json:"onDemandSleeping"`
			}
			if err := json.Unmarshal(encoded, &result); err != nil {
				t.Fatal(err)
			}
			if result.OnDemandSleeping != wantSleeping {
				t.Fatalf("sleeping = %v, want %v", result.OnDemandSleeping, wantSleeping)
			}
			if items[0].State != string(rows[0].State) {
				t.Fatal("Docker state was replaced")
			}
			state, err := control.StackState(context.Background(), "stk_gateway")
			if err != nil || string(state.Runtime) != wantRuntime {
				t.Fatalf("stack state = %v, want %s: %v", state.Runtime, wantRuntime, err)
			}
		})
	}
}
