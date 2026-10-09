package monitoring

import (
	"context"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

func counterRate(previous, current uint64, elapsed time.Duration) (float64, bool) {
	if current < previous || elapsed <= 0 || elapsed > StaleAfter {
		return 0, false
	}
	return float64(current-previous) / elapsed.Seconds(), true
}

type cpuCounters struct {
	total, busy uint64
	at          time.Time
}
type cpuSource struct {
	owner    *LinuxSources
	previous map[string]cpuCounters
}

func (s *cpuSource) ID() string   { return "cpu" }
func (s *cpuSource) Close() error { return nil }
func (s *cpuSource) Collect(ctx context.Context) (Batch, error) {
	if err := ctx.Err(); err != nil {
		return Batch{}, err
	}
	data, err := readBounded(s.owner.proc, "stat", tableLimit)
	if err != nil {
		s.previous = map[string]cpuCounters{}
		return Batch{}, err
	}
	now := s.owner.now()
	batch := Batch{}
	next := map[string]cpuCounters{}
	cores := 0
	omitted := 0
	err = scanLines(data, func(line string) error {
		fields := strings.Fields(line)
		if len(fields) == 0 || !strings.HasPrefix(fields[0], "cpu") {
			return nil
		}
		name := fields[0]
		if len(fields) < 5 {
			return errInvalidData
		}
		if name != "cpu" {
			if _, err := strconv.ParseUint(strings.TrimPrefix(name, "cpu"), 10, 32); err != nil {
				return errInvalidData
			}
			cores++
			if cores > 256 {
				omitted++
				return nil
			}
		}
		var values [8]uint64
		for i := 0; i < 8 && i+1 < len(fields); i++ {
			value, err := strconv.ParseUint(fields[i+1], 10, 64)
			if err != nil {
				return errInvalidData
			}
			values[i] = value
		}
		current := cpuCounters{at: now}
		for _, value := range values {
			if math.MaxUint64-current.total < value {
				return errInvalidData
			}
			current.total += value
		}
		current.busy = current.total - values[3] - values[4]
		next[name] = current
		kind, label := DeviceCPU, "CPU "+strings.TrimPrefix(name, "cpu")
		if name == "cpu" {
			kind, label = DeviceHost, "All CPUs"
		}
		device := Device{ID: name, Kind: kind, Name: label, Default: name == "cpu"}
		batch.Devices = append(batch.Devices, device)
		reading := collecting(now)
		if previous, ok := s.previous[name]; ok && now.Sub(previous.at) > 0 && now.Sub(previous.at) <= StaleAfter && current.total > previous.total && current.busy >= previous.busy {
			delta := current.total - previous.total
			busy := current.busy - previous.busy
			if busy <= delta {
				reading = available(100*float64(busy)/float64(delta), now)
			}
		}
		addReading(&batch, name, MetricCPUBusy, UnitPercent, reading)
		return nil
	})
	if err != nil {
		s.previous = map[string]cpuCounters{}
		return Batch{}, err
	}
	s.previous = next
	if len(next) == 0 {
		return Batch{}, errInvalidData
	}
	if omitted > 0 {
		batch.Coverage = []Coverage{{Source: "cpu", Omitted: omitted, Partial: true, Reason: "limit_exceeded"}}
	}
	return batch, nil
}

type memorySource struct{ owner *LinuxSources }

func (s *memorySource) ID() string   { return "memory" }
func (s *memorySource) Close() error { return nil }
func (s *memorySource) Collect(ctx context.Context) (Batch, error) {
	if err := ctx.Err(); err != nil {
		return Batch{}, err
	}
	data, err := readBounded(s.owner.proc, "meminfo", tableLimit)
	if err != nil {
		return Batch{}, err
	}
	values := map[string]uint64{}
	err = scanLines(data, func(line string) error {
		fields := strings.Fields(line)
		if len(fields) < 1 {
			return nil
		}
		switch fields[0] {
		case "MemTotal:", "MemAvailable:", "SwapTotal:", "SwapFree:":
		default:
			return nil
		}
		if len(fields) != 3 || fields[2] != "kB" {
			return errInvalidData
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil || value > math.MaxUint64/1024 {
			return errInvalidData
		}
		values[fields[0]] = value * 1024
		return nil
	})
	if err != nil {
		return Batch{}, err
	}
	now := s.owner.now()
	batch := Batch{Devices: []Device{{ID: "memory", Kind: DeviceMemory, Name: "Host memory", Default: true}}}
	total, totalOK := values["MemTotal:"]
	free, freeOK := values["MemAvailable:"]
	for _, metric := range []MetricKind{MetricMemoryTotal, MetricMemoryAvailable, MetricMemoryUsed, MetricMemoryPercent} {
		unit := UnitBytes
		reading := unavailable("invalid_data", now)
		if metric == MetricMemoryPercent {
			unit = UnitPercent
		}
		if totalOK && freeOK && total > 0 && free <= total {
			var value float64
			switch metric {
			case MetricMemoryTotal:
				value = float64(total)
			case MetricMemoryAvailable:
				value = float64(free)
			case MetricMemoryUsed:
				value = float64(total - free)
			case MetricMemoryPercent:
				value = 100 * float64(total-free) / float64(total)
			}
			reading = available(value, now)
		}
		addReading(&batch, "memory", metric, unit, reading)
	}
	swap, swapOK := values["SwapTotal:"]
	swapFree, swapFreeOK := values["SwapFree:"]
	for _, metric := range []MetricKind{MetricSwapTotal, MetricSwapUsed} {
		reading := unavailable("unsupported", now)
		if swapOK && swapFreeOK && swapFree <= swap {
			value := swap
			if metric == MetricSwapUsed {
				value = swap - swapFree
			}
			reading = available(float64(value), now)
		}
		addReading(&batch, "memory", metric, UnitBytes, reading)
	}
	return batch, nil
}

type networkCounters struct {
	rx, tx uint64
	epoch  uint64
	id     string
	at     time.Time
}
type networkSource struct {
	owner    *LinuxSources
	previous map[string]networkCounters
	epoch    uint64
}

func (s *networkSource) ID() string   { return "network" }
func (s *networkSource) Close() error { return nil }
func (s *networkSource) Collect(ctx context.Context) (Batch, error) {
	if err := ctx.Err(); err != nil {
		return Batch{}, err
	}
	data, err := readBounded(s.owner.proc, s.owner.netPath("dev"), tableLimit)
	if err != nil {
		s.previous = map[string]networkCounters{}
		return Batch{}, err
	}
	now := s.owner.now()
	next := map[string]networkCounters{}
	omitted := 0
	namespace := "unknown"
	if s.owner.proc != nil {
		path := "self/ns/net"
		if s.owner.options.Mode == "host" {
			path = "1/ns/net"
		}
		if value, err := s.owner.proc.Readlink(path); err == nil {
			namespace = value
		}
	}
	err = scanLines(data, func(line string) error {
		colon := strings.LastIndexByte(line, ':')
		if colon < 0 {
			return nil
		}
		name := strings.TrimSpace(line[:colon])
		if name == "" || strings.ContainsAny(name, "/\x00") || name == "." || name == ".." {
			return errInvalidData
		}
		if len(next) >= 64 {
			omitted++
			return nil
		}
		fields := strings.Fields(line[colon+1:])
		if len(fields) < 16 {
			return errInvalidData
		}
		rx, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			return errInvalidData
		}
		tx, err := strconv.ParseUint(fields[8], 10, 64)
		if err != nil {
			return errInvalidData
		}
		index, _ := readBounded(s.owner.sys, "class/net/"+name+"/ifindex", scalarLimit)
		address, _ := readBounded(s.owner.sys, "class/net/"+name+"/address", scalarLimit)
		epoch := s.previous[name].epoch
		if epoch == 0 {
			s.epoch++
			epoch = s.epoch
		}
		id := stableID("net", namespace, name, strings.TrimSpace(string(index)), strings.TrimSpace(string(address)), strconv.FormatUint(epoch, 10))
		next[name] = networkCounters{rx: rx, tx: tx, epoch: epoch, id: id, at: now}
		return nil
	})
	if err != nil {
		s.previous = map[string]networkCounters{}
		return Batch{}, err
	}
	names := make([]string, 0, len(next))
	for name := range next {
		names = append(names, name)
	}
	sort.Strings(names)
	preferred := s.preferredInterface(names)
	batch := Batch{}
	for _, name := range names {
		current := next[name]
		batch.Devices = append(batch.Devices, Device{ID: current.id, Kind: DeviceInterface, Name: name, Default: name == preferred})
		received, sent := collecting(now), collecting(now)
		if previous, ok := s.previous[name]; ok && previous.id == current.id {
			if rate, ok := counterRate(previous.rx, current.rx, now.Sub(previous.at)); ok {
				received = available(rate, now)
			}
			if rate, ok := counterRate(previous.tx, current.tx, now.Sub(previous.at)); ok {
				sent = available(rate, now)
			}
		}
		addReading(&batch, current.id, MetricNetworkReceive, UnitRate, received)
		addReading(&batch, current.id, MetricNetworkSend, UnitRate, sent)
	}
	s.previous = next
	if omitted > 0 {
		batch.Coverage = []Coverage{{Source: "network", Omitted: omitted, Partial: true, Reason: "limit_exceeded"}}
	}
	return batch, nil
}
func (s *networkSource) preferredInterface(names []string) string {
	eligible := map[string]bool{}
	for _, name := range names {
		state, _ := readBounded(s.owner.sys, "class/net/"+name+"/operstate", scalarLimit)
		eligible[name] = name != "lo" && strings.TrimSpace(string(state)) != "down"
	}
	preferred := ""
	best := uint64(math.MaxUint64)
	for _, kind := range []string{"route", "ipv6_route"} {
		data, err := readBounded(s.owner.proc, s.owner.netPath(kind), tableLimit)
		if err != nil {
			continue
		}
		_ = scanLines(data, func(line string) error {
			fields := strings.Fields(line)
			var name, metric, flags string
			var base int
			if kind == "route" {
				if len(fields) < 8 || fields[1] != "00000000" || fields[7] != "00000000" {
					return nil
				}
				name, metric, flags, base = fields[0], fields[6], fields[3], 10
			} else {
				if len(fields) < 10 || fields[0] != strings.Repeat("0", 32) || fields[1] != "00" {
					return nil
				}
				name, metric, flags, base = fields[9], fields[5], fields[8], 16
			}
			cost, err := strconv.ParseUint(metric, base, 64)
			flag, flagErr := strconv.ParseUint(flags, 16, 64)
			if err == nil && flagErr == nil && flag&1 != 0 && eligible[name] && cost < best {
				preferred = name
				best = cost
			}
			return nil
		})
	}
	if preferred != "" {
		return preferred
	}
	for _, name := range names {
		if eligible[name] {
			return name
		}
	}
	return ""
}
