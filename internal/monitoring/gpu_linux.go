package monitoring

import (
	"context"
	"strconv"
	"strings"
	"time"
)

type gpuDeviceReader interface {
	Read(context.Context) (Batch, error)
	Close() error
}
type gpuSource struct {
	id     string
	reader gpuDeviceReader
}

func (s *gpuSource) ID() string                                 { return s.id }
func (s *gpuSource) Collect(ctx context.Context) (Batch, error) { return s.reader.Read(ctx) }
func (s *gpuSource) Close() error                               { return s.reader.Close() }

type gpuDiscovery interface {
	Discover(context.Context) ([]Source, []Coverage)
	Close() error
}
type unavailableGPU struct {
	device Device
	reason string
}

func (r *unavailableGPU) Close() error { return nil }
func (r *unavailableGPU) Read(context.Context) (Batch, error) {
	batch := Batch{Devices: []Device{r.device}}
	now := time.Now()
	for _, item := range []struct {
		metric MetricKind
		unit   Unit
	}{{MetricGPUBusy, UnitPercent}, {MetricGPUMemoryTotal, UnitBytes}, {MetricGPUMemoryUsed, UnitBytes}, {MetricTemperature, UnitCelsius}} {
		addReading(&batch, r.device.ID, item.metric, item.unit, unavailable(r.reason, now))
	}
	return batch, nil
}
func (o *LinuxSources) discoverDRM() ([]Source, []Coverage, error) {
	names, omitted, err := readDirectory(o.sys, "class/drm", 256)
	if err != nil {
		return nil, nil, err
	}
	next := map[string]Source{}
	for _, card := range names {
		if !strings.HasPrefix(card, "card") {
			continue
		}
		if _, err := strconv.ParseUint(strings.TrimPrefix(card, "card"), 10, 16); err != nil {
			continue
		}
		root := "class/drm/" + card + "/device"
		vendorData, err := readBounded(o.sys, root+"/vendor", scalarLimit)
		if err != nil {
			continue
		}
		vendor := strings.TrimSpace(string(vendorData))
		if vendor != "0x1002" && vendor != "0x8086" {
			continue
		} // NVML owns NVIDIA identity and visibility.
		if len(next) >= 16 {
			omitted++
			continue
		}
		target, _ := o.sys.Readlink(root)
		hardware, _ := readBounded(o.sys, root+"/device", scalarLimit)
		key := card + "|" + target + "|" + vendor + "|" + string(hardware)
		if existing := o.gpus[key]; existing != nil {
			next[key] = existing
			continue
		}
		o.gpuEpoch++
		id := stableID("gpu", key, strconv.FormatUint(o.gpuEpoch, 10))
		driver, label := "amdgpu", "AMD "+card
		if vendor == "0x8086" {
			driver = "intel"
			label = "Intel " + card
		}
		device := Device{ID: id, Kind: DeviceGPU, Name: label, Driver: driver, UtilizationBasis: "vendor"}
		var reader gpuDeviceReader = &unavailableGPU{device: device, reason: "unsupported"}
		if vendor == "0x1002" {
			reader = &amdReader{owner: o, path: root, device: device}
		} else {
			reader = &intelReader{owner: o, card: card, device: device}
		}
		next[key] = &gpuSource{id: id, reader: reader}
	}
	o.gpus = next
	sources := make([]Source, 0, len(next))
	for _, source := range next {
		sources = append(sources, source)
	}
	var coverage []Coverage
	if omitted > 0 {
		coverage = []Coverage{{Source: "gpu", Partial: true, Omitted: omitted, Reason: "limit_exceeded"}}
	}
	return sources, coverage, nil
}
