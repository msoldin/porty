//go:build linux && cgo

package monitoring

import (
	"context"
	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"strconv"
	"sync"
	"time"
)

type nvmlAPI interface {
	Init() nvml.Return
	Shutdown() nvml.Return
	DeviceGetCount() (int, nvml.Return)
	DeviceGetHandleByIndex(int) (nvml.Device, nvml.Return)
}
type nvmlDevice interface {
	GetUUID() (string, nvml.Return)
	GetName() (string, nvml.Return)
	GetUtilizationRates() (nvml.Utilization, nvml.Return)
	GetMemoryInfo() (nvml.Memory, nvml.Return)
	GetTemperature(nvml.TemperatureSensors) (uint32, nvml.Return)
	GetMigMode() (int, int, nvml.Return)
}
type nvidiaSession struct {
	api                 nvmlAPI
	mu                  sync.RWMutex
	discoveryMu         sync.Mutex
	initialized, closed bool
	sources             map[string]Source
	epoch               uint64
}

func newNVIDIA() gpuDiscovery { return &nvidiaSession{api: nvml.New()} }
func nvmlReason(ret nvml.Return) string {
	switch ret {
	case nvml.SUCCESS:
		return ""
	case nvml.ERROR_NO_PERMISSION:
		return "permission_denied"
	case nvml.ERROR_NOT_SUPPORTED, nvml.ERROR_LIBRARY_NOT_FOUND, nvml.ERROR_DRIVER_NOT_LOADED, nvml.ERROR_FUNCTION_NOT_FOUND:
		return "unsupported"
	case nvml.ERROR_GPU_IS_LOST, nvml.ERROR_NOT_FOUND:
		return "source_missing"
	default:
		return "read_failed"
	}
}
func (s *nvidiaSession) Discover(ctx context.Context) ([]Source, []Coverage) {
	// Serialize inventory updates without queuing an exclusive lifecycle lock
	// behind a slow GPU read. Only shutdown needs to exclude all NVML calls.
	s.discoveryMu.Lock()
	defer s.discoveryMu.Unlock()
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed || ctx.Err() != nil {
		return nil, nil
	}
	failure := func(ret nvml.Return) ([]Source, []Coverage) {
		sources := make([]Source, 0, len(s.sources))
		for _, source := range s.sources {
			sources = append(sources, source)
		}
		return sources, []Coverage{{Source: "nvidia", Partial: true, Reason: nvmlReason(ret)}}
	}
	if !s.initialized {
		if ret := s.api.Init(); ret != nvml.SUCCESS {
			return failure(ret)
		}
		s.initialized = true
	}
	count, ret := s.api.DeviceGetCount()
	if ret != nvml.SUCCESS {
		return failure(ret)
	}
	omitted := 0
	if count > 16 {
		omitted = count - 16
		count = 16
	}
	next := map[string]Source{}
	partial := false
	for i := 0; i < count; i++ {
		handle, ret := s.api.DeviceGetHandleByIndex(i)
		if ret != nvml.SUCCESS {
			partial = true
			continue
		}
		uuid, ret := handle.GetUUID()
		if ret != nvml.SUCCESS || uuid == "" {
			partial = true
			continue
		}
		if source := s.sources[uuid]; source != nil {
			next[uuid] = source
			continue
		}
		name, ret := handle.GetName()
		if ret != nvml.SUCCESS {
			name = "NVIDIA GPU"
		}
		s.epoch++
		id := stableID("gpu", uuid, strconv.FormatUint(s.epoch, 10))
		next[uuid] = &gpuSource{id: id, reader: &nvidiaReader{session: s, handle: handle, device: Device{ID: id, Kind: DeviceGPU, Name: name, Driver: "nvidia", UtilizationBasis: "vendor"}}}
	}
	s.sources = next
	sources := make([]Source, 0, len(next))
	for _, source := range next {
		sources = append(sources, source)
	}
	var coverage []Coverage
	if omitted > 0 || partial {
		coverage = []Coverage{{Source: "nvidia", Partial: true, Omitted: omitted, Reason: "partial_coverage"}}
	}
	return sources, coverage
}
func (s *nvidiaSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed && s.initialized {
		s.api.Shutdown()
	}
	s.closed = true
	return nil
}

type nvidiaReader struct {
	session *nvidiaSession
	handle  nvmlDevice
	device  Device
}

func (r *nvidiaReader) Close() error { return nil } // Session lifetime belongs to LinuxSources.
func (r *nvidiaReader) Read(ctx context.Context) (Batch, error) {
	if err := ctx.Err(); err != nil {
		return Batch{}, err
	}
	r.session.mu.RLock()
	defer r.session.mu.RUnlock()
	if r.session.closed {
		return (&unavailableGPU{device: r.device, reason: "source_missing"}).Read(ctx)
	}
	now := time.Now()
	batch := Batch{Devices: []Device{r.device}}
	add := func(metric MetricKind, unit Unit, value float64, ret nvml.Return) {
		reading := unavailable(nvmlReason(ret), now)
		if ret == nvml.SUCCESS {
			reading = available(value, now)
		}
		addReading(&batch, r.device.ID, metric, unit, reading)
	}
	usage, ret := r.handle.GetUtilizationRates()
	if ret == nvml.SUCCESS && usage.Gpu > 100 {
		ret = nvml.ERROR_UNKNOWN
	}
	add(MetricGPUBusy, UnitPercent, float64(usage.Gpu), ret)
	memory, ret := r.handle.GetMemoryInfo()
	if ret == nvml.SUCCESS && memory.Used > memory.Total {
		ret = nvml.ERROR_UNKNOWN
	}
	add(MetricGPUMemoryTotal, UnitBytes, float64(memory.Total), ret)
	add(MetricGPUMemoryUsed, UnitBytes, float64(memory.Used), ret)
	temperature, ret := r.handle.GetTemperature(nvml.TEMPERATURE_GPU)
	add(MetricTemperature, UnitCelsius, float64(temperature), ret)
	mig, _, ret := r.handle.GetMigMode()
	if ret == nvml.SUCCESS && mig != 0 {
		batch.Coverage = []Coverage{{Source: r.device.ID, Partial: true, Reason: "partial_coverage"}}
	}
	return batch, nil
}
