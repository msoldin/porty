package control

import (
	portyop "github.com/msoldin/porty/internal/operation"
	"testing"
)

func TestDeploymentFreshnessUsesConfigurationDigestNotCommitIdentity(t *testing.T) {
	last := &DeploymentSnapshot{Status: portyop.DeploymentSucceeded, ComposeDigest: "sha256:current", GitCommit: "old-commit"}
	if got := ClassifyDeployment(last, "sha256:current", false); got != DeploymentCurrent {
		t.Fatalf("same configuration freshness = %q, want current", got)
	}
	if got := ClassifyDeployment(last, "sha256:changed", false); got != DeploymentChangesPending {
		t.Fatalf("changed configuration freshness = %q, want changes pending", got)
	}
	if got := ClassifyDeployment(last, "sha256:changed", true); got != DeploymentDeploying {
		t.Fatalf("active deployment freshness = %q, want deploying", got)
	}
}

func TestAggregateRuntimePreservesPartialAndUnhealthyStates(t *testing.T) {
	tests := []struct {
		name       string
		containers []ContainerState
		want       RuntimeState
	}{
		{name: "no containers", want: RuntimeStopped},
		{name: "all running", containers: []ContainerState{ContainerRunning, ContainerRunning}, want: RuntimeRunning},
		{name: "one stopped", containers: []ContainerState{ContainerRunning, ContainerStopped}, want: RuntimePartial},
		{name: "unhealthy dominates", containers: []ContainerState{ContainerRunning, ContainerUnhealthy}, want: RuntimeUnhealthy},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := AggregateRuntime(test.containers); got != test.want {
				t.Fatalf("AggregateRuntime() = %q, want %q", got, test.want)
			}
		})
	}
}
