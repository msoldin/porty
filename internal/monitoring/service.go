package monitoring

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

type ServiceOptions struct {
	Host     HostInfo
	Sources  []Source
	Discover func(context.Context) ([]Source, error)
	Now      func() time.Time
}
type sourceSlot struct {
	source        Source
	busy, retired bool
}
type sourceBatch struct {
	batch Batch
	at    time.Time
}
type Service struct {
	mu                                        sync.Mutex
	now                                       func() time.Time
	host                                      HostInfo
	generation                                string
	sequence                                  uint64
	inventory                                 Inventory
	readings                                  map[string]Reading
	coverage                                  []Coverage
	batches                                   map[string]sourceBatch
	slots                                     map[string]*sourceSlot
	history                                   []storedSample
	historyBytes, historyLimit, responseLimit int
	discover                                  func(context.Context) ([]Source, error)
	discovering                               bool
	ctx                                       context.Context
	cancel                                    context.CancelFunc
	stopped                                   bool
	runOnce, stopOnce                         sync.Once
	workers                                   sync.WaitGroup
	done                                      chan struct{}
}

func NewService(options ServiceOptions) (*Service, error) {
	var generation [16]byte
	if _, err := rand.Read(generation[:]); err != nil {
		return nil, err
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	ctx, cancel := context.WithCancel(context.Background())
	service := &Service{now: options.Now, host: options.Host, generation: hex.EncodeToString(generation[:]), batches: map[string]sourceBatch{}, slots: map[string]*sourceSlot{}, readings: map[string]Reading{}, historyLimit: maxStoredBytes - (4 << 20), responseLimit: maxResponseBytes, discover: options.Discover, ctx: ctx, cancel: cancel, done: make(chan struct{})}
	service.host.Name = safeLabel(service.host.Name)
	service.host.OS = safeLabel(service.host.OS)
	service.replaceSourcesLocked(options.Sources)
	service.rebuildLocked()
	return service, nil
}

func (s *Service) Run(parent context.Context) {
	s.runOnce.Do(func() {
		stop := context.AfterFunc(parent, s.cancel)
		defer stop()
		ticker := time.NewTicker(SampleInterval)
		defer ticker.Stop()
		discovery := time.NewTicker(10 * time.Second)
		defer discovery.Stop()
		s.discoverSources()
		s.schedule(s.ctx)
		for {
			select {
			case <-s.ctx.Done():
				s.stop()
				return
			case <-ticker.C:
				s.record(s.now())
				s.schedule(s.ctx)
			case <-discovery.C:
				s.discoverSources()
			}
		}
	})
}

func (s *Service) schedule(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || ctx.Err() != nil {
		return
	}
	for id, slot := range s.slots {
		if slot.busy || slot.retired {
			continue
		}
		slot.busy = true
		s.workers.Add(1)
		go func(id string, slot *sourceSlot) {
			defer s.workers.Done()
			batch, err := slot.source.Collect(ctx)
			at := s.now()
			s.mu.Lock()
			slot.busy = false
			retired := slot.retired || s.stopped
			if !retired && s.slots[id] == slot {
				s.acceptBatchLocked(id, batch, err, at)
			}
			s.mu.Unlock()
			if retired {
				_ = slot.source.Close()
			}
		}(id, slot)
	}
}

func (s *Service) discoverSources() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.discover == nil || s.discovering || s.stopped {
		return
	}
	s.discovering = true
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		sources, err := s.discover(s.ctx)
		s.mu.Lock()
		defer s.mu.Unlock()
		s.discovering = false
		if s.stopped {
			for _, source := range sources {
				if _, exists := s.slots[source.ID()]; !exists {
					_ = source.Close()
				}
			}
			return
		}
		if err == nil {
			s.replaceSourcesLocked(sources)
		}
	}()
}

func (s *Service) replaceSourcesLocked(sources []Source) {
	keep := make(map[string]bool)
	for _, source := range sources {
		if source == nil || !validID(source.ID()) || len(keep) >= maxSources {
			continue
		}
		id := source.ID()
		keep[id] = true
		if _, exists := s.slots[id]; !exists {
			s.slots[id] = &sourceSlot{source: source}
		}
	}
	for id, slot := range s.slots {
		if !keep[id] {
			s.retireLocked(slot)
			delete(s.slots, id)
			delete(s.batches, id)
		}
	}
	s.rebuildLocked()
}
func (s *Service) retireLocked(slot *sourceSlot) {
	if slot.retired {
		return
	}
	slot.retired = true
	if !slot.busy {
		s.workers.Add(1)
		go func() { defer s.workers.Done(); _ = slot.source.Close() }()
	}
}
func (s *Service) stop() {
	s.stopOnce.Do(func() {
		s.cancel()
		s.mu.Lock()
		s.stopped = true
		for _, slot := range s.slots {
			s.retireLocked(slot)
		}
		s.mu.Unlock()
		go func() { s.workers.Wait(); close(s.done) }()
	})
}
func (s *Service) Shutdown(ctx context.Context) error {
	s.stop()
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) acceptBatch(id string, batch Batch, err error, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.acceptBatchLocked(id, batch, err, at)
}
func (s *Service) acceptBatchLocked(id string, batch Batch, err error, at time.Time) {
	old := s.batches[id]
	if at.Before(old.at) {
		return
	}
	if err != nil {
		batch = old.batch
		for key, reading := range batch.Readings {
			reading.State = StateStale
			reading.Reason = "read_failed"
			batch.Readings[key] = reading
		}
		batch.Coverage = []Coverage{{Source: id, Partial: true, Reason: "read_failed"}}
	}
	batch = normalizeBatch(batch)
	encoded, _ := json.Marshal(batch)
	// Reserve at most 1 MiB of serialized live source data, plus bounded decoded
	// inventory/readings; the remaining 60 MiB belongs to compact encoded history.
	size := len(encoded)
	for key, value := range s.batches {
		if key != id {
			data, _ := json.Marshal(value.batch)
			size += len(data)
		}
	}
	if size > 1<<20 {
		batch = Batch{Coverage: []Coverage{{Source: id, Partial: true, Reason: "limit_exceeded"}}}
	}
	s.batches[id] = sourceBatch{batch: batch, at: at}
	s.rebuildLocked()
}

var deviceLimits = map[DeviceKind]int{DeviceHost: 1, DeviceCPU: 256, DeviceMemory: 1, DeviceInterface: 64, DeviceBlock: 64, DeviceFilesystem: 64, DeviceSensor: 128, DeviceGPU: 16, DeviceEngine: 256}

func (s *Service) rebuildLocked() {
	inventory := Inventory{Devices: []Device{}, Series: []Series{}}
	readings := map[string]Reading{}
	coverage := []Coverage{}
	deviceIDs := map[string]bool{}
	seriesIDs := map[string]bool{}
	counts := map[DeviceKind]int{}
	sources := make([]string, 0, len(s.batches))
	for id := range s.batches {
		sources = append(sources, id)
	}
	sort.Strings(sources)
	for _, id := range sources {
		batch := s.batches[id].batch
		omitted := 0
		for _, device := range batch.Devices {
			if deviceIDs[device.ID] {
				continue
			}
			if counts[device.Kind] >= deviceLimits[device.Kind] {
				omitted++
				continue
			}
			counts[device.Kind]++
			deviceIDs[device.ID] = true
			inventory.Devices = append(inventory.Devices, device)
		}
		for _, series := range batch.Series {
			if !deviceIDs[series.DeviceID] || seriesIDs[series.ID] || len(inventory.Series) >= maxSeries {
				continue
			}
			seriesIDs[series.ID] = true
			inventory.Series = append(inventory.Series, series)
			if reading, ok := batch.Readings[series.ID]; ok {
				readings[series.ID] = reading
			}
		}
		coverage = append(coverage, batch.Coverage...)
		if omitted > 0 {
			coverage = append(coverage, Coverage{Source: id, Omitted: omitted, Partial: true, Reason: "limit_exceeded"})
		}
	}
	sort.Slice(inventory.Devices, func(i, j int) bool { return inventory.Devices[i].ID < inventory.Devices[j].ID })
	sort.Slice(inventory.Series, func(i, j int) bool { return inventory.Series[i].ID < inventory.Series[j].ID })
	encoded, _ := json.Marshal(inventory)
	sum := sha256.Sum256(encoded)
	inventory.Revision = hex.EncodeToString(sum[:])
	s.inventory = inventory
	s.readings = readings
	s.coverage = coverage
}

func normalizeBatch(batch Batch) Batch {
	result := Batch{Readings: map[string]Reading{}}
	for _, device := range batch.Devices {
		if len(result.Devices) >= 1024 {
			break
		}
		if !validID(device.ID) || deviceLimits[device.Kind] == 0 {
			continue
		}
		device.Name = safeLabel(device.Name)
		device.Driver = safeLabel(device.Driver)
		if device.UtilizationBasis != "vendor" && device.UtilizationBasis != "busiest_engine" {
			device.UtilizationBasis = ""
		}
		paths := device.MountPaths
		if len(paths) > 16 {
			paths = paths[:16]
		}
		device.MountPaths = make([]string, len(paths))
		for i, path := range paths {
			device.MountPaths[i] = safeLabel(path)
		}
		result.Devices = append(result.Devices, device)
	}
	for _, series := range batch.Series {
		if len(result.Series) >= maxSeries {
			break
		}
		if !validID(series.ID) || !validID(series.DeviceID) || !validMetric(series.Metric, series.Unit) {
			continue
		}
		result.Series = append(result.Series, series)
		reading, ok := batch.Readings[series.ID]
		if !ok {
			continue
		}
		reading.Reason = safeReason(reading.Reason)
		switch reading.State {
		case StateAvailable, StateStale, StateCollecting, StateUnavailable:
		default:
			reading = unavailable("invalid_data", reading.SampledAt)
		}
		if reading.Value != nil {
			value := *reading.Value
			if math.IsNaN(value) || math.IsInf(value, 0) || (series.Unit != UnitCelsius && value < 0) || (series.Unit == UnitPercent && value > 100) {
				reading = unavailable("invalid_data", reading.SampledAt)
			} else {
				reading.Value = &value
			}
		} else if reading.State == StateAvailable {
			reading = unavailable("invalid_data", reading.SampledAt)
		}
		if reading.LastSuccessAt != nil {
			at := *reading.LastSuccessAt
			reading.LastSuccessAt = &at
		}
		result.Readings[series.ID] = reading
	}
	for _, item := range batch.Coverage {
		if len(result.Coverage) >= 64 {
			break
		}
		item.Source = safeLabel(item.Source)
		item.Reason = safeReason(item.Reason)
		if item.Omitted < 0 {
			item.Omitted = 0
		}
		result.Coverage = append(result.Coverage, item)
	}
	return result
}
func validID(id string) bool {
	return len(id) > 0 && len(id) <= 128 && strings.IndexFunc(id, func(r rune) bool { return unicode.IsControl(r) }) < 0
}
func safeLabel(value string) string {
	runes := []rune(strings.ToValidUTF8(value, "�"))
	if len(runes) > 256 {
		runes = runes[:256]
	}
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, string(runes))
}
func safeReason(reason string) string {
	switch reason {
	case "", "collecting", "unsupported", "build_unsupported", "no_device", "permission_denied", "source_missing", "read_failed", "invalid_data", "collection_delayed", "counter_reset", "limit_exceeded", "disabled", "partial_coverage", "host_mount_unavailable":
		return reason
	default:
		return "read_failed"
	}
}
func validMetric(metric MetricKind, unit Unit) bool {
	switch metric {
	case MetricCPUBusy, MetricMemoryPercent, MetricGPUBusy, MetricEngineBusy, MetricFilesystemPercent:
		return unit == UnitPercent
	case MetricMemoryTotal, MetricMemoryAvailable, MetricMemoryUsed, MetricSwapTotal, MetricSwapUsed, MetricGPUMemoryTotal, MetricGPUMemoryUsed, MetricFilesystemTotal, MetricFilesystemUsed, MetricFilesystemAvailable:
		return unit == UnitBytes
	case MetricNetworkReceive, MetricNetworkSend, MetricDiskRead, MetricDiskWrite:
		return unit == UnitRate
	case MetricTemperature:
		return unit == UnitCelsius
	default:
		return false
	}
}
