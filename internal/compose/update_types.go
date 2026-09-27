package compose

import (
	"context"
	"errors"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/moby/moby/client"
	"time"
)

var ErrUpdateIneligible = errors.New("stack is not eligible for automatic updates")
var ErrSelfProtected = errors.New("Porty hosting stack cannot auto-update")
var ErrProtectionUnavailable = errors.New("Porty runtime protection unavailable")

type UpdateSnapshot struct {
	Project      *types.Project
	SourceDigest string
	Containers   []UpdateContainer
	Excluded     map[string]string
}
type UpdateContainer struct {
	ID, Service, ImageID, State, Health, Platform string
	Replica, RestartCount                         int
	StartedAt                                     time.Time
}
type updateDocker interface {
	ContainerList(context.Context, client.ContainerListOptions) (client.ContainerListResult, error)
	ContainerInspect(context.Context, string, client.ContainerInspectOptions) (client.ContainerInspectResult, error)
	ImageInspect(context.Context, string, ...client.ImageInspectOption) (client.ImageInspectResult, error)
}
type SelfGuard interface {
	CheckProject(context.Context, string) error
}

type ImageChange struct {
	Service         string `json:"service"`
	SourceReference string `json:"sourceReference"`
	TargetReference string `json:"targetReference"`
	Platform        string `json:"platform"`
	BeforeImageID   string `json:"beforeImageId"`
	AfterImageID    string `json:"afterImageId"`
	ManifestDigest  string `json:"manifestDigest"`
}
type PreparedUpdate struct {
	Snapshot UpdateSnapshot
	Changes  []ImageChange
}
type imageDockerAPI interface {
	DistributionInspect(context.Context, string, client.DistributionInspectOptions) (client.DistributionInspectResult, error)
	ImagePull(context.Context, string, client.ImagePullOptions) (client.ImagePullResponse, error)
	ImageInspect(context.Context, string, ...client.ImageInspectOption) (client.ImageInspectResult, error)
}
