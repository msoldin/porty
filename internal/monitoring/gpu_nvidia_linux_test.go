//go:build linux && cgo

package monitoring

import (
	"context"
	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"sync/atomic"
	"testing"
	"time"
)

type fakeNVML struct {
	ret     nvml.Return
	devices []nvml.Device
	closed  bool
}

func (f *fakeNVML) Init() nvml.Return                  { return f.ret }
func (f *fakeNVML) Shutdown() nvml.Return              { f.closed = true; return nvml.SUCCESS }
func (f *fakeNVML) DeviceGetCount() (int, nvml.Return) { return len(f.devices), nvml.SUCCESS }
func (f *fakeNVML) DeviceGetHandleByIndex(i int) (nvml.Device, nvml.Return) {
	return f.devices[i], nvml.SUCCESS
}

type fakeNVDevice struct {
	nvml.Device
	blocked chan struct{}
	calls   atomic.Int32
}

func (f *fakeNVDevice) GetUUID() (string, nvml.Return) { return "GPU-fixture", nvml.SUCCESS }
func (f *fakeNVDevice) GetName() (string, nvml.Return) { return "NVIDIA fixture", nvml.SUCCESS }
func (f *fakeNVDevice) GetUtilizationRates() (nvml.Utilization, nvml.Return) {
	f.calls.Add(1)
	if f.blocked != nil {
		<-f.blocked
	}
	return nvml.Utilization{Gpu: 42}, nvml.SUCCESS
}
func (f *fakeNVDevice) GetMemoryInfo() (nvml.Memory, nvml.Return) {
	return nvml.Memory{Total: 4096, Used: 1024}, nvml.SUCCESS
}
func (f *fakeNVDevice) GetTemperature(nvml.TemperatureSensors) (uint32, nvml.Return) {
	return 0, nvml.ERROR_NOT_SUPPORTED
}
func (f *fakeNVDevice) GetMigMode() (int, int, nvml.Return) { return 0, 0, nvml.ERROR_NOT_SUPPORTED }
func TestNVIDIAUnavailableLibraryDoesNotPreventCollection(t *testing.T) {
	session := &nvidiaSession{api: &fakeNVML{ret: nvml.ERROR_LIBRARY_NOT_FOUND}}
	sources, coverage := session.Discover(context.Background())
	if len(sources) != 0 || len(coverage) != 1 || coverage[0].Reason != "unsupported" {
		t.Fatal(sources, coverage)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
}
func TestNVIDIAUnsupportedFieldPreservesUtilization(t *testing.T) {
	api := &fakeNVML{devices: []nvml.Device{&fakeNVDevice{}}}
	session := &nvidiaSession{api: api}
	sources, _ := session.Discover(context.Background())
	if len(sources) != 1 {
		t.Fatal(sources)
	}
	batch, err := sources[0].Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if *metricReading(t, batch, MetricGPUBusy).Value != 42 || metricReading(t, batch, MetricTemperature).State != StateUnavailable {
		t.Fatal(batch)
	}
	sources[0].Close()
	if api.closed {
		t.Fatal("device retirement closed shared session")
	}
	session.Close()
	if !api.closed {
		t.Fatal("session leaked")
	}
}
func TestNVIDIACallsDoNotOverlap(t *testing.T) {
	device := &fakeNVDevice{blocked: make(chan struct{})}
	session := &nvidiaSession{api: &fakeNVML{devices: []nvml.Device{device}}}
	sources, _ := session.Discover(context.Background())
	service, _ := NewService(ServiceOptions{Sources: sources})
	service.schedule(context.Background())
	waitFor(t, func() bool { return device.calls.Load() == 1 })
	service.schedule(context.Background())
	if device.calls.Load() != 1 {
		t.Fatal("overlapping NVML calls")
	}
	close(device.blocked)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := service.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	session.Close()
}
