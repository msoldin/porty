package compose

import (
	"context"
	"io"
	"time"

	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/config/configfile"
	"github.com/docker/cli/cli/flags"
	"github.com/docker/compose/v5/pkg/api"
	composeengine "github.com/docker/compose/v5/pkg/compose"
	sdkclient "github.com/docker/go-sdk/client"
	"github.com/moby/moby/client"
)

// NewDockerClient constructs the Compose service backed by the Docker SDK.
func NewDockerClient(ctx context.Context, timeout time.Duration) (*Client, io.Closer, error) {
	service, closer, err := newDockerService(ctx)
	if err != nil {
		return nil, nil, err
	}
	return New(service, timeout), closer, nil
}

// dockerBridge supplies Compose with the SDK daemon client while retaining
// Docker's configuration and context implementation.
type dockerBridge struct {
	command.Cli
	api    client.APIClient
	config *configfile.ConfigFile
}

func (b dockerBridge) Client() client.APIClient { return b.api }

// Hide executable credential helpers while retaining credentials stored inline.
func (b dockerBridge) ConfigFile() *configfile.ConfigFile { return b.config }

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
	configuration := safeDockerConfig(cli.ConfigFile())
	service, err := composeengine.NewComposeService(dockerBridge{Cli: cli, api: sdk, config: configuration})
	if err != nil {
		_ = sdk.Close()
		return nil, nil, err
	}
	return service, sdk, nil
}

func safeDockerConfig(source *configfile.ConfigFile) *configfile.ConfigFile {
	configuration := configfile.New("")
	if source != nil {
		*configuration = *source
		configuration.CredentialsStore = ""
		configuration.CredentialHelpers = nil
	}
	return configuration
}
