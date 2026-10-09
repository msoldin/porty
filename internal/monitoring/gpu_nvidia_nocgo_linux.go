//go:build linux && !cgo

package monitoring

import "context"

type nvidiaUnavailable struct{}

func newNVIDIA() gpuDiscovery { return nvidiaUnavailable{} }
func (nvidiaUnavailable) Discover(context.Context) ([]Source, []Coverage) {
	return nil, []Coverage{{Source: "nvidia", Partial: true, Reason: "build_unsupported"}}
}
func (nvidiaUnavailable) Close() error { return nil }
