package monitoring

import (
	"context"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"math"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

func filesystemUsage(totalBlocks, freeBlocks, availableBlocks, blockSize uint64) (total, used, available uint64, percent float64, err error) {
	if totalBlocks == 0 || blockSize == 0 || freeBlocks > totalBlocks || availableBlocks > freeBlocks || totalBlocks > math.MaxUint64/blockSize {
		return 0, 0, 0, 0, errInvalidData
	}
	return totalBlocks * blockSize, (totalBlocks - freeBlocks) * blockSize, availableBlocks * blockSize, 100 * float64(totalBlocks-freeBlocks) / float64(totalBlocks), nil
}

type filesystemSource struct {
	owner        *LinuxSources
	device       Device
	major, minor uint32
	inspect      func(*os.File) (uint32, uint32, unix.Statfs_t, error)
}

func (s *filesystemSource) ID() string   { return s.device.ID }
func (s *filesystemSource) Close() error { return nil }
func inspectFilesystem(file *os.File) (uint32, uint32, unix.Statfs_t, error) {
	var st unix.Stat_t
	var fs unix.Statfs_t
	if err := unix.Fstat(int(file.Fd()), &st); err != nil {
		return 0, 0, fs, err
	}
	// The opened descriptor stays pinned across both checks.
	if err := unix.Fstatfs(int(file.Fd()), &fs); err != nil {
		return 0, 0, fs, err
	}
	return unix.Major(uint64(st.Dev)), unix.Minor(uint64(st.Dev)), fs, nil
}
func (s *filesystemSource) Collect(ctx context.Context) (Batch, error) {
	if err := ctx.Err(); err != nil {
		return Batch{}, err
	}
	now := s.owner.now()
	batch := Batch{Devices: []Device{s.device}}
	reason := "host_mount_unavailable"
	var total, used, availableBytes uint64
	var percent float64
	for _, mount := range s.device.MountPaths {
		if !strings.HasPrefix(mount, "/") || path.Clean(mount) != mount || s.owner.root == nil {
			continue
		}
		relative := strings.TrimPrefix(mount, "/")
		if relative == "" {
			relative = "."
		}
		file, err := s.owner.root.Open(relative)
		if err != nil {
			reason = sourceReason(err)
			continue
		}
		inspect := s.inspect
		if inspect == nil {
			inspect = inspectFilesystem
		}
		major, minor, fs, err := inspect(file)
		file.Close()
		if err != nil {
			reason = sourceReason(err)
			continue
		}
		if major != s.major || minor != s.minor {
			reason = "host_mount_unavailable"
			continue
		}
		if fs.Bsize <= 0 {
			reason = "invalid_data"
			continue
		}
		total, used, availableBytes, percent, err = filesystemUsage(fs.Blocks, fs.Bfree, fs.Bavail, uint64(fs.Bsize))
		if err != nil {
			reason = sourceReason(err)
			continue
		}
		reason = ""
		break
	}
	for _, item := range []struct {
		metric MetricKind
		unit   Unit
		value  float64
	}{
		{MetricFilesystemTotal, UnitBytes, float64(total)}, {MetricFilesystemUsed, UnitBytes, float64(used)}, {MetricFilesystemAvailable, UnitBytes, float64(availableBytes)}, {MetricFilesystemPercent, UnitPercent, percent},
	} {
		r := unavailable(reason, now)
		if reason == "" {
			r = available(item.value, now)
		}
		addReading(&batch, s.device.ID, item.metric, item.unit, r)
	}
	return batch, nil
}
func decodeMountPath(value string) (string, error) {
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] != '\\' {
			out.WriteByte(value[i])
			continue
		}
		if i+3 >= len(value) {
			return "", errInvalidData
		}
		escape := value[i+1 : i+4]
		switch escape {
		case "040":
			out.WriteByte(' ')
		case "011":
			out.WriteByte('\t')
		case "012":
			out.WriteByte('\n')
		case "134":
			out.WriteByte('\\')
		default:
			return "", errInvalidData
		}
		i += 3
	}
	return out.String(), nil
}
func realFilesystem(kind string) bool {
	switch kind {
	case "proc", "sysfs", "tmpfs", "devtmpfs", "devpts", "cgroup", "cgroup2", "securityfs", "debugfs", "tracefs", "configfs", "pstore", "mqueue", "hugetlbfs", "fusectl", "rpc_pipefs", "autofs", "binfmt_misc", "overlay", "nsfs", "ramfs", "efivarfs":
		return false
	default:
		return true
	}
}
func (o *LinuxSources) discoverFilesystems() ([]Source, []Coverage, error) {
	prefix := "self/"
	if o.options.Mode == "host" {
		prefix = "1/"
	}
	data, err := readBounded(o.proc, prefix+"mountinfo", tableLimit)
	if err != nil {
		return nil, nil, err
	}
	mounts := map[string]*filesystemSource{}
	order := []string{}
	omitted := 0
	err = scanLines(data, func(line string) error {
		fields := strings.Fields(line)
		if len(fields) < 10 {
			return errInvalidData
		}
		separator := -1
		for i := 6; i < len(fields); i++ {
			if fields[i] == "-" {
				separator = i
				break
			}
		}
		if separator < 0 || separator+3 >= len(fields) {
			return errInvalidData
		}
		kind := fields[separator+1]
		if !realFilesystem(kind) {
			return nil
		}
		var major, minor uint32
		if n, err := fmt.Sscanf(fields[2], "%d:%d", &major, &minor); err != nil || n != 2 {
			return errInvalidData
		}
		mount, err := decodeMountPath(fields[4])
		if err != nil {
			return err
		}
		if !strings.HasPrefix(mount, "/") || path.Clean(mount) != mount {
			return errInvalidData
		}
		key := fields[2] + "|" + kind
		if current := mounts[key]; current != nil {
			if len(current.device.MountPaths) < 16 {
				current.device.MountPaths = append(current.device.MountPaths, mount)
			} else {
				omitted++
			}
			return nil
		}
		if len(mounts) >= 64 {
			omitted++
			return nil
		}
		mounts[key] = &filesystemSource{owner: o, major: major, minor: minor, device: Device{Kind: DeviceFilesystem, Name: mount, Driver: kind, MountPaths: []string{mount}, Default: mount == "/"}}
		// Mount ID prevents a replacement from continuing the old history.
		mounts[key].device.ID = stableID("fs", key, fields[0], fields[separator+2])
		order = append(order, key)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	sources := make([]Source, 0, len(order))
	for _, key := range order {
		sources = append(sources, mounts[key])
	}
	var coverage []Coverage
	if omitted > 0 {
		coverage = []Coverage{{Source: "filesystems", Omitted: omitted, Partial: true, Reason: "limit_exceeded"}}
	}
	return sources, coverage, nil
}

type diskCounter struct {
	read, write uint64
	at          time.Time
	id          string
	epoch       uint64
}
type diskSource struct {
	owner    *LinuxSources
	previous map[string]diskCounter
	epoch    uint64
}

func (s *diskSource) ID() string   { return "disks" }
func (s *diskSource) Close() error { return nil }
func (s *diskSource) Collect(ctx context.Context) (Batch, error) {
	if err := ctx.Err(); err != nil {
		return Batch{}, err
	}
	data, err := readBounded(s.owner.proc, "diskstats", tableLimit)
	if err != nil {
		s.previous = nil
		return Batch{}, err
	}
	now := s.owner.now()
	batch := Batch{}
	next := map[string]diskCounter{}
	omitted := 0
	totals := [2]float64{}
	eligible, fresh := 0, 0
	err = scanLines(data, func(line string) error {
		fields := strings.Fields(line)
		if len(fields) < 14 {
			return errInvalidData
		}
		name := fields[2]
		if !validID(name) || strings.ContainsAny(name, "/\\") {
			return errInvalidData
		}
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") {
			return nil
		}
		if len(next) >= 64 {
			omitted++
			return nil
		}
		read, err := strconv.ParseUint(fields[5], 10, 64)
		if err != nil || read > math.MaxUint64/512 {
			return errInvalidData
		}
		write, err := strconv.ParseUint(fields[9], 10, 64)
		if err != nil || write > math.MaxUint64/512 {
			return errInvalidData
		}
		root := "class/block/" + name
		sequence, _ := readBounded(s.owner.sys, root+"/diskseq", scalarLimit)
		epoch := s.previous[name].epoch
		if epoch == 0 {
			s.epoch++
			epoch = s.epoch
		}
		id := stableID("disk", fields[0], fields[1], name, string(sequence), strconv.FormatUint(epoch, 10))
		current := diskCounter{read: read * 512, write: write * 512, at: now, id: id, epoch: epoch}
		next[name] = current
		batch.Devices = append(batch.Devices, Device{ID: id, Kind: DeviceBlock, Name: name})
		readings := [2]Reading{collecting(now), collecting(now)}
		if previous, ok := s.previous[name]; ok && previous.id == id {
			for i, pair := range [][2]uint64{{previous.read, current.read}, {previous.write, current.write}} {
				if rate, ok := counterRate(pair[0], pair[1], now.Sub(previous.at)); ok {
					readings[i] = available(rate, now)
				}
			}
		}
		addReading(&batch, id, MetricDiskRead, UnitRate, readings[0])
		addReading(&batch, id, MetricDiskWrite, UnitRate, readings[1])
		// Partitions and stacked block devices are details, never additional physical I/O.
		_, partitionErr := readBounded(s.owner.sys, root+"/partition", scalarLimit)
		slaves, _, slavesErr := readDirectory(s.owner.sys, root+"/slaves", 1)
		_, devErr := readBounded(s.owner.sys, root+"/dev", scalarLimit)
		target := ""
		if s.owner.sys != nil {
			target, _ = s.owner.sys.Readlink(root)
		}
		if errors.Is(partitionErr, os.ErrNotExist) && len(slaves) == 0 && (slavesErr == nil || errors.Is(slavesErr, os.ErrNotExist)) && devErr == nil && !strings.Contains(target, "/virtual/") {
			eligible++
			if readings[0].State == StateAvailable && readings[1].State == StateAvailable {
				fresh++
				totals[0] += *readings[0].Value
				totals[1] += *readings[1].Value
			}
		}
		return nil
	})
	if err != nil {
		s.previous = nil
		return Batch{}, err
	}
	s.previous = next
	batch.Devices = append([]Device{{ID: "disk-total", Kind: DeviceBlock, Name: "Physical disks", Default: true}}, batch.Devices...)
	for i, metric := range []MetricKind{MetricDiskRead, MetricDiskWrite} {
		reading := collecting(now)
		if eligible == 0 {
			reading = unavailable("unsupported", now)
		} else if fresh == eligible {
			reading = available(totals[i], now)
		}
		addReading(&batch, "disk-total", metric, UnitRate, reading)
	}
	if omitted > 0 || fresh < eligible || eligible == 0 {
		batch.Coverage = []Coverage{{Source: "disks", Omitted: omitted, Partial: true, Reason: "partial_coverage"}}
	}
	return batch, nil
}

// Incremental directory reads prevent sysfs enumeration from allocating unbounded slices.
// An omitted value of one means at least one further entry.
func readDirectory(root *os.Root, name string, limit int) ([]string, int, error) {
	if root == nil {
		return nil, 0, os.ErrNotExist
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()
	entries, err := file.ReadDir(limit + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, 0, err
	}
	omitted := 0
	if len(entries) > limit {
		entries = entries[:limit]
		omitted = 1
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, omitted, nil
}
