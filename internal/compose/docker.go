package compose

import (
	"context"
	"io"

	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/flags"
	"github.com/docker/compose/v5/pkg/api"
	composeengine "github.com/docker/compose/v5/pkg/compose"
	sdkclient "github.com/docker/go-sdk/client"
	"github.com/moby/moby/client"
)

// dockerBridge supplies Compose with the SDK daemon client while retaining
// Docker's configuration and context implementation.
type dockerBridge struct {
	command.Cli
	api client.APIClient
}

func (b dockerBridge) Client() client.APIClient { return b.api }

// The classic builder runs through the daemon API. Enabling BuildKit here
// makes Compose invoke an external Buildx plugin.
func (dockerBridge) BuildKitEnabled() (bool, error) { return false, nil }

func newDockerService(ctx context.Context) (api.Compose, io.Closer, error) {
	sdk, err := sdkclient.New(ctx, sdkclient.WithHealthCheck(
		func(context.Context) func(sdkclient.SDKClient) error {
			return func(sdkclient.SDKClient) error { return nil }
		},
	))
	if err != nil {
		return nil, nil, err
	}
	cli, err := command.NewDockerCli()
	if err != nil {
		_ = sdk.Close()
		return nil, nil, err
	}
	if err := cli.Initialize(&flags.ClientOptions{}); err != nil {
		_ = sdk.Close()
		return nil, nil, err
	}
	service, err := composeengine.NewComposeService(dockerBridge{Cli: cli, api: sdk})
	if err != nil {
		_ = sdk.Close()
		return nil, nil, err
	}
	return service, sdk, nil
}
