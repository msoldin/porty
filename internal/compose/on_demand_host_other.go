//go:build !linux

package compose

import "context"

func verifyNativeDockerNetwork(context.Context, string) error { return ErrOnDemandIneligible }
