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
