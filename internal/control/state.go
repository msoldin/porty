package control

import portyop "github.com/msoldin/porty/internal/operation"

type DeploymentFreshness string

const (
	DeploymentNever          DeploymentFreshness = "never_deployed"
	DeploymentCurrent        DeploymentFreshness = "current"
	DeploymentChangesPending DeploymentFreshness = "changes_pending"
	DeploymentDeploying      DeploymentFreshness = "deploying"
	DeploymentUnverifiable   DeploymentFreshness = "unverifiable"
)

type DeploymentSnapshot struct {
	Status        portyop.DeploymentStatus
	ComposeDigest string
	GitCommit     string
}

func ClassifyDeployment(last *DeploymentSnapshot, desiredDigest string, active bool) DeploymentFreshness {
	if active {
		return DeploymentDeploying
	}
	if last == nil {
		return DeploymentNever
	}
	if last.Status != portyop.DeploymentSucceeded || desiredDigest == "" {
		return DeploymentUnverifiable
	}
	if last.ComposeDigest == desiredDigest {
		return DeploymentCurrent
	}
	return DeploymentChangesPending
}

type ContainerState string

const (
	ContainerRunning   ContainerState = "running"
	ContainerStopped   ContainerState = "stopped"
	ContainerSleeping  ContainerState = "sleeping"
	ContainerUnhealthy ContainerState = "unhealthy"
)

type RuntimeState string

const (
	RuntimeRunning   RuntimeState = "running"
	RuntimeStopped   RuntimeState = "stopped"
	RuntimeSleeping  RuntimeState = "sleeping"
	RuntimeOnDemand  RuntimeState = "on_demand"
	RuntimePartial   RuntimeState = "partial"
	RuntimeUnhealthy RuntimeState = "unhealthy"
)

func AggregateRuntime(containers []ContainerState) RuntimeState {
	if len(containers) == 0 {
		return RuntimeStopped
	}
	running, sleeping := 0, 0
	for _, state := range containers {
		if state == ContainerUnhealthy {
			return RuntimeUnhealthy
		}
		if state == ContainerRunning {
			running++
		}
		if state == ContainerSleeping {
			sleeping++
		}
	}
	if running == len(containers) {
		return RuntimeRunning
	}
	if sleeping == len(containers) {
		return RuntimeSleeping
	}
	if sleeping > 0 {
		if running+sleeping == len(containers) {
			return RuntimeOnDemand
		}
		return RuntimePartial
	}
	if running == 0 {
		return RuntimeStopped
	}
	return RuntimePartial
}
