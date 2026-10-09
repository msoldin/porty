package monitoring

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const scalarLimit = 4 << 10
const tableLimit = 4 << 20

var errInvalidData = errors.New("invalid metric data")
var errSourceLimit = errors.New("metric source limit exceeded")

type LinuxOptions struct{ Mode, HostProc, HostSys, HostRoot string }
type LinuxSources struct {
	options         LinuxOptions
	proc, sys, root *os.Root
	now             func() time.Time
	mu              sync.Mutex
	sources         map[string]Source
	closeOnce       sync.Once
	closeErr        error
}

func OpenLinuxSources(options LinuxOptions) (*LinuxSources, error) {
	if options.Mode == "" {
		options.Mode = "native"
	}
	if options.Mode != "native" && options.Mode != "host" && options.Mode != "disabled" {
		return nil, errors.New("invalid monitoring mode")
	}
	owner := &LinuxSources{options: options, now: time.Now, sources: map[string]Source{}}
	if options.Mode == "disabled" {
		return owner, nil
	}
	proc, sys, root := options.HostProc, options.HostSys, options.HostRoot
	if options.Mode == "native" {
		proc, sys, root = "/proc", "/sys", "/"
	}
	for _, path := range []string{proc, sys, root} {
		if !filepath.IsAbs(path) {
			return nil, errors.New("monitoring root must be absolute")
		}
	}
	// Missing roots are independent unavailable capabilities, not startup errors.
	owner.proc, _ = os.OpenRoot(proc)
	owner.sys, _ = os.OpenRoot(sys)
	owner.root, _ = os.OpenRoot(root)
	owner.sources["cpu"] = &cpuSource{owner: owner, previous: map[string]cpuCounters{}}
	owner.sources["memory"] = &memorySource{owner: owner}
	owner.sources["network"] = &networkSource{owner: owner, previous: map[string]networkCounters{}}
	return owner, nil
}
func (o *LinuxSources) Discover(ctx context.Context) ([]Source, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.options.Mode == "disabled" {
		return []Source{}, nil
	}
	if o.sources["disks"] == nil {
		o.sources["disks"] = &diskSource{owner: o}
	}
	if o.sources["sensors"] == nil {
		o.sources["sensors"] = &sensorSource{owner: o}
	}
	next := map[string]Source{}
	for _, id := range []string{"cpu", "memory", "network", "disks", "sensors"} {
		next[id] = o.sources[id]
	}
	filesystems, coverage, err := o.discoverFilesystems()
	if err != nil {
		coverage = append(coverage, Coverage{Source: "filesystems", Partial: true, Reason: sourceReason(err)})
		for id, source := range o.sources {
			if strings.HasPrefix(id, "fs-") {
				next[id] = source
			}
		}
	} else {
		for _, source := range filesystems {
			if existing := o.sources[source.ID()]; existing != nil {
				source = existing
			}
			next[source.ID()] = source
		}
	}
	status := &discoverySource{coverage: coverage}
	next[status.ID()] = status
	o.sources = next
	result := make([]Source, 0, len(next))
	for _, source := range next {
		result = append(result, source)
	}
	return result, nil
}
func (o *LinuxSources) Host(ctx context.Context) HostInfo {
	info := HostInfo{Name: "Host identity unavailable", OS: "Linux"}
	if ctx.Err() != nil {
		return info
	}
	if o.options.Mode == "native" {
		if name, err := os.Hostname(); err == nil {
			info.Name = safeLabel(name)
		}
	} else if data, err := readBounded(o.root, "etc/hostname", scalarLimit); err == nil && strings.TrimSpace(string(data)) != "" {
		info.Name = safeLabel(strings.TrimSpace(string(data)))
	}
	data, err := readBounded(o.proc, "stat", tableLimit)
	if err == nil {
		_ = scanLines(data, func(line string) error {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == "btime" {
				seconds, err := strconv.ParseInt(fields[1], 10, 64)
				if err == nil && seconds > 0 {
					at := time.Unix(seconds, 0).UTC()
					info.BootTime = &at
				}
			}
			return nil
		})
	}
	return info
}
func (o *LinuxSources) Close() error {
	o.closeOnce.Do(func() {
		for _, root := range []*os.Root{o.proc, o.sys, o.root} {
			if root != nil {
				o.closeErr = errors.Join(o.closeErr, root.Close())
			}
		}
	})
	return o.closeErr
}
func (o *LinuxSources) netPath(name string) string {
	if o.options.Mode == "host" {
		return "1/net/" + name
	}
	return "net/" + name
}
func readBounded(root *os.Root, path string, limit int) ([]byte, error) {
	if root == nil {
		return nil, os.ErrNotExist
	}
	file, err := root.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, errSourceLimit
	}
	return data, nil
}
func scanLines(data []byte, visit func(string) error) error {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 4096), 64<<10)
	for scanner.Scan() {
		if err := visit(scanner.Text()); err != nil {
			return err
		}
	}
	return scanner.Err()
}
func stableID(kind string, parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return kind + "-" + hex.EncodeToString(sum[:12])
}
func sourceReason(err error) string {
	switch {
	case errors.Is(err, os.ErrPermission):
		return "permission_denied"
	case errors.Is(err, os.ErrNotExist):
		return "source_missing"
	case errors.Is(err, errSourceLimit):
		return "limit_exceeded"
	case errors.Is(err, errInvalidData):
		return "invalid_data"
	default:
		return "read_failed"
	}
}

func addReading(batch *Batch, device string, metric MetricKind, unit Unit, reading Reading) {
	id := device + "." + string(metric)
	batch.Series = append(batch.Series, Series{ID: id, DeviceID: device, Metric: metric, Unit: unit})
	if batch.Readings == nil {
		batch.Readings = map[string]Reading{}
	}
	batch.Readings[id] = reading
}
func collecting(now time.Time) Reading {
	return Reading{State: StateCollecting, Reason: "collecting", SampledAt: now}
}

// Immutable discovery reports get new identities when capabilities change.
type discoverySource struct{ coverage []Coverage }

func (s *discoverySource) ID() string {
	data, _ := json.Marshal(s.coverage)
	return stableID("discovery", string(data))
}
func (s *discoverySource) Collect(context.Context) (Batch, error) {
	return Batch{Coverage: s.coverage}, nil
}
func (s *discoverySource) Close() error { return nil }
