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
	ContainerUnhealthy ContainerState = "unhealthy"
)

type RuntimeState string

const (
	RuntimeRunning   RuntimeState = "running"
	RuntimeStopped   RuntimeState = "stopped"
	RuntimePartial   RuntimeState = "partial"
	RuntimeUnhealthy RuntimeState = "unhealthy"
)

func AggregateRuntime(containers []ContainerState) RuntimeState {
	if len(containers) == 0 {
		return RuntimeStopped
	}
	running := 0
	for _, state := range containers {
		if state == ContainerUnhealthy {
			return RuntimeUnhealthy
		}
		if state == ContainerRunning {
			running++
		}
	}
	if running == len(containers) {
		return RuntimeRunning
	}
	if running == 0 {
		return RuntimeStopped
	}
	return RuntimePartial
}
