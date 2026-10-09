package monitoring

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"math"
	"os"
	"path"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unsafe"
)

type perfReader interface {
	Read() (value, enabled, running uint64, err error)
	Close() error
}
type perfCounter struct{ file *os.File }

func openPerfCounter(typeID uint32, config uint64) (perfReader, error) {
	return openPerfCounterOnCPU(typeID, config, 0)
}
func openPerfCounterOnCPU(typeID uint32, config uint64, cpu int) (perfReader, error) {
	attr := unix.PerfEventAttr{Type: typeID, Size: uint32(unsafe.Sizeof(unix.PerfEventAttr{})), Config: config, Read_format: unix.PERF_FORMAT_TOTAL_TIME_ENABLED | unix.PERF_FORMAT_TOTAL_TIME_RUNNING}
	fd, err := unix.PerfEventOpen(&attr, -1, cpu, -1, unix.PERF_FLAG_FD_CLOEXEC)
	if err != nil {
		return nil, err
	}
	return &perfCounter{file: os.NewFile(uintptr(fd), "gpu-perf")}, nil
}
func (p *perfCounter) Read() (uint64, uint64, uint64, error) {
	var data [24]byte
	n, err := p.file.Read(data[:])
	if err != nil {
		return 0, 0, 0, err
	}
	if n != len(data) {
		return 0, 0, 0, errInvalidData
	}
	return binary.NativeEndian.Uint64(data[:8]), binary.NativeEndian.Uint64(data[8:16]), binary.NativeEndian.Uint64(data[16:]), nil
}
func (p *perfCounter) Close() error { return p.file.Close() }
func intelEngineUsage(activeDelta, totalDelta uint64) (float64, bool) {
	if totalDelta == 0 || activeDelta > totalDelta {
		return 0, false
	}
	return 100 * float64(activeDelta) / float64(totalDelta), true
}

type perfSample struct{ value, enabled, running uint64 }

func readPerf(reader perfReader) (perfSample, error) {
	v, e, r, err := reader.Read()
	return perfSample{v, e, r}, err
}
func scaledPerfDelta(before, after perfSample) (float64, bool) {
	if after.value < before.value || after.enabled <= before.enabled || after.running <= before.running {
		return 0, false
	}
	enabled, running := after.enabled-before.enabled, after.running-before.running
	if running > enabled {
		return 0, false
	}
	value := float64(after.value-before.value) * float64(enabled) / float64(running)
	return value, !math.IsNaN(value) && !math.IsInf(value, 0)
}

type intelEngine struct {
	name                          string
	active, total                 perfReader
	previousActive, previousTotal perfSample
	at                            time.Time
	valid                         bool
	reason                        string
}
type intelReader struct {
	owner       *LinuxSources
	device      Device
	card        string
	engines     []intelEngine
	reason      string
	omitted     int
	initialized bool
	openCounter func(uint32, uint64, int) (perfReader, error)
	attempted   time.Time
}

func (r *intelReader) Close() error {
	var err error
	for i := range r.engines {
		e := &r.engines[i]
		if e.active != nil {
			err = errors.Join(err, e.active.Close())
			e.active = nil
		}
		if e.total != nil {
			err = errors.Join(err, e.total.Close())
			e.total = nil
		}
	}
	return err
}
func (r *intelReader) Read(ctx context.Context) (Batch, error) {
	if err := ctx.Err(); err != nil {
		return Batch{}, err
	}
	now := r.owner.now()
	if r.card != "" && !r.initialized && (r.attempted.IsZero() || now.Sub(r.attempted) >= 10*time.Second) {
		r.attempted = now
		r.initialize()
	}
	r.device.UtilizationBasis = "busiest_engine"
	batch := Batch{Devices: []Device{r.device}}
	best := unavailable(r.reason, now)
	if r.reason == "" {
		best = unavailable("unsupported", now)
	}
	measured, waiting := 0, false
	partial := r.omitted > 0
	for i := range r.engines {
		e := &r.engines[i]
		id := stableID("engine", r.device.ID, e.name)
		batch.Devices = append(batch.Devices, Device{ID: id, ParentID: r.device.ID, Kind: DeviceEngine, Name: e.name, Driver: r.device.Driver})
		reading := unavailable(e.reason, now)
		if e.active != nil {
			current, err := readPerf(e.active)
			var total perfSample
			if err == nil && e.total != nil {
				total, err = readPerf(e.total)
			}
			if err != nil {
				reading = unavailable(sourceReason(err), now)
				e.valid = false
			} else {
				reading = collecting(now)
				elapsed := now.Sub(e.at)
				if e.valid && elapsed > 0 && elapsed <= StaleAfter {
					active, ok := scaledPerfDelta(e.previousActive, current)
					percent := 100 * active / float64(elapsed.Nanoseconds())
					if e.total != nil {
						denominator, totalOK := scaledPerfDelta(e.previousTotal, total)
						ok = ok && totalOK && denominator > 0
						if ok {
							percent = 100 * active / denominator
						}
					}
					if ok && percent >= 0 && percent <= 100.000001 {
						reading = available(math.Min(percent, 100), now)
					}
				}
				e.previousActive, e.previousTotal, e.at, e.valid = current, total, now, true
			}
		}
		if reading.State == StateAvailable {
			measured++
			if best.Value == nil || *reading.Value > *best.Value {
				best = reading
			}
		} else {
			partial = true
			if reading.State == StateCollecting {
				waiting = true
			} else if best.Value == nil {
				best = reading
			}
		}
		addReading(&batch, id, MetricEngineBusy, UnitPercent, reading)
	}
	if measured == 0 && waiting {
		best = collecting(now)
	}
	addReading(&batch, r.device.ID, MetricGPUBusy, UnitPercent, best)
	for _, item := range []struct {
		metric MetricKind
		unit   Unit
	}{{MetricGPUMemoryTotal, UnitBytes}, {MetricGPUMemoryUsed, UnitBytes}, {MetricTemperature, UnitCelsius}} {
		addReading(&batch, r.device.ID, item.metric, item.unit, unavailable("unsupported", now))
	}
	if partial || len(r.engines) == 0 {
		batch.Coverage = []Coverage{{Source: r.device.ID, Partial: true, Omitted: r.omitted, Reason: "partial_coverage"}}
	}
	return batch, nil
}

// Event encoding comes from the running driver's sysfs format, never from a
// shared vendor encoding. This adapter supports config (not config1/config2).
func encodePerfEvent(event string, formats map[string]string) (uint64, error) {
	var result, occupied uint64
	for _, term := range strings.Split(strings.TrimSpace(event), ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(term), "=")
		if !ok {
			value = "1"
		}
		n, err := strconv.ParseUint(value, 0, 64)
		if err != nil {
			return 0, errInvalidData
		}
		format, exists := formats[key]
		if !exists {
			return 0, errInvalidData
		}
		register, bits, ok := strings.Cut(strings.TrimSpace(format), ":")
		if !ok || register != "config" {
			return 0, errInvalidData
		}
		position := uint(0)
		var mask, encoded uint64
		for _, part := range strings.Split(bits, ",") {
			loText, hiText, ranged := strings.Cut(part, "-")
			if !ranged {
				hiText = loText
			}
			lo, err := strconv.ParseUint(loText, 10, 6)
			if err != nil {
				return 0, errInvalidData
			}
			hi, err := strconv.ParseUint(hiText, 10, 6)
			if err != nil || hi < lo {
				return 0, errInvalidData
			}
			for bit := lo; bit <= hi; bit++ {
				if position >= 64 || mask&(uint64(1)<<bit) != 0 {
					return 0, errInvalidData
				}
				mask |= uint64(1) << bit
				encoded |= ((n >> position) & 1) << bit
				position++
			}
		}
		if position < 64 && n>>position != 0 || occupied&mask != 0 {
			return 0, errInvalidData
		}
		occupied |= mask
		result |= encoded
	}
	return result, nil
}
func (r *intelReader) initialize() {
	// Only readers without an open counter retry initialization.
	r.engines = nil
	r.omitted = 0
	openCounter := r.openCounter
	if openCounter == nil {
		openCounter = openPerfCounterOnCPU
	}
	root := "class/drm/" + r.card + "/device"
	driver, err := r.owner.sys.Readlink(root + "/driver")
	if err != nil {
		r.reason = sourceReason(err)
		return
	}
	driver = path.Base(driver)
	if driver != "i915" && driver != "xe" {
		r.reason = "unsupported"
		return
	}
	r.device.Driver = driver
	uevent, err := readBounded(r.owner.sys, root+"/uevent", scalarLimit)
	if err != nil {
		r.reason = sourceReason(err)
		return
	}
	pci := ""
	for _, line := range strings.Split(string(uevent), "\n") {
		if value, ok := strings.CutPrefix(line, "PCI_SLOT_NAME="); ok {
			pci = value
		}
	}
	if pci == "" || strings.ContainsAny(pci, "/\\") {
		r.reason = "unsupported"
		return
	}
	pmu := "bus/event_source/devices/" + driver + "_" + strings.ReplaceAll(pci, ":", "_")
	typeData, err := readBounded(r.owner.sys, pmu+"/type", scalarLimit)
	if err != nil && driver == "i915" {
		// The unsuffixed legacy PMU is valid only for the integrated GPU.
		if pci == "0000:00:02.0" {
			pmu = "bus/event_source/devices/i915"
			typeData, err = readBounded(r.owner.sys, pmu+"/type", scalarLimit)
		}
	}
	if err != nil {
		r.reason = sourceReason(err)
		return
	}
	typeID, err := strconv.ParseUint(strings.TrimSpace(string(typeData)), 10, 32)
	if err != nil {
		r.reason = "invalid_data"
		return
	}
	formatNames, extra, err := readDirectory(r.owner.sys, pmu+"/format", 16)
	if err != nil || extra > 0 {
		r.reason = "unsupported"
		return
	}
	formats := map[string]string{}
	for _, name := range formatNames {
		data, err := readBounded(r.owner.sys, pmu+"/format/"+name, scalarLimit)
		if err != nil {
			r.reason = sourceReason(err)
			return
		}
		formats[name] = string(data)
	}
	cpu := 0
	if data, err := readBounded(r.owner.sys, pmu+"/cpumask", scalarLimit); err == nil {
		first := strings.FieldsFunc(strings.TrimSpace(string(data)), func(c rune) bool { return c == ',' || c == '-' })
		if len(first) == 0 {
			r.reason = "invalid_data"
			return
		}
		cpu, err = strconv.Atoi(first[0])
		if err != nil || cpu < 0 {
			r.reason = "invalid_data"
			return
		}
	}
	type eventPair struct{ name, active, total string }
	var events []eventPair
	if driver == "i915" {
		names, extra, err := readDirectory(r.owner.sys, pmu+"/events", 512)
		if err != nil {
			r.reason = sourceReason(err)
			return
		}
		r.omitted += extra
		for _, name := range names {
			if !strings.HasSuffix(name, "-busy") {
				continue
			}
			if len(events) >= 16 {
				r.omitted++
				continue
			}
			data, err := readBounded(r.owner.sys, pmu+"/events/"+name, scalarLimit)
			if err != nil {
				r.omitted++
				continue
			}
			events = append(events, eventPair{name: strings.TrimSuffix(name, "-busy"), active: string(data)})
		}
	} else {
		engines, omitted, err := queryXeEngines(r.owner, r.card)
		if err != nil {
			r.reason = sourceReason(err)
			return
		}
		r.omitted += omitted
		active, err := readBounded(r.owner.sys, pmu+"/events/engine-active-ticks", scalarLimit)
		if err != nil {
			r.reason = sourceReason(err)
			return
		}
		total, err := readBounded(r.owner.sys, pmu+"/events/engine-total-ticks", scalarLimit)
		if err != nil {
			r.reason = sourceReason(err)
			return
		}
		for _, engine := range engines {
			suffix := fmt.Sprintf(",gt=%d,engine_class=%d,engine_instance=%d", engine.gt, engine.class, engine.instance)
			events = append(events, eventPair{name: fmt.Sprintf("GT%d %s %d", engine.gt, engineClassName(engine.class), engine.instance), active: strings.TrimSpace(string(active)) + suffix, total: strings.TrimSpace(string(total)) + suffix})
		}
	}
	for _, event := range events {
		engine := intelEngine{name: event.name}
		config, err := encodePerfEvent(event.active, formats)
		if err == nil {
			engine.active, err = openCounter(uint32(typeID), config, cpu)
		}
		if err == nil && event.total != "" {
			config, err = encodePerfEvent(event.total, formats)
			if err == nil {
				engine.total, err = openCounter(uint32(typeID), config, cpu)
			}
		}
		if err != nil {
			if engine.active != nil {
				engine.active.Close()
				engine.active = nil
			}
			engine.reason = sourceReason(err)
		}
		r.engines = append(r.engines, engine)
	}
	r.initialized = false
	for _, engine := range r.engines {
		if engine.active != nil {
			r.initialized = true
			break
		}
	}
	r.reason = "unsupported"
}
func engineClassName(class uint16) string {
	switch class {
	case 0:
		return "Render"
	case 1:
		return "Copy"
	case 2:
		return "Video"
	case 3:
		return "Enhance"
	case 4:
		return "Compute"
	default:
		return "Unknown"
	}
}

type xeEngine struct{ class, instance, gt uint16 }

// drm_xe_device_query is 40 bytes; engines reply = 8-byte header plus
// 32-byte drm_xe_engine records (Linux include/uapi/drm/xe_drm.h).
type xeQuery struct {
	extensions  uint64
	query, size uint32
	data        uint64
	reserved    [2]uint64
}

func queryXeEngines(owner *LinuxSources, card string) ([]xeEngine, int, error) {
	names, _, err := readDirectory(owner.sys, "class/drm/"+card+"/device/drm", 32)
	if err != nil {
		return nil, 0, err
	}
	node := ""
	for _, name := range names {
		if strings.HasPrefix(name, "renderD") {
			node = name
			break
		}
	}
	if node == "" {
		return nil, 0, os.ErrNotExist
	}
	expected, err := readBounded(owner.sys, "class/drm/"+node+"/dev", scalarLimit)
	if err != nil {
		return nil, 0, err
	}
	var major, minor uint32
	if n, err := fmt.Sscanf(string(expected), "%d:%d", &major, &minor); err != nil || n != 2 || major != 226 {
		return nil, 0, errInvalidData
	}
	if owner.root == nil {
		return nil, 0, os.ErrNotExist
	}
	file, err := owner.root.Open("dev/dri/" + node)
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()
	var st unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &st); err != nil {
		return nil, 0, err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFCHR || unix.Major(uint64(st.Rdev)) != major || unix.Minor(uint64(st.Rdev)) != minor {
		return nil, 0, errInvalidData
	}
	query := xeQuery{}
	// _IOWR('d', 0x40, struct drm_xe_device_query), supported Linux Go targets.
	request := uintptr(3)<<30 | uintptr(unsafe.Sizeof(query))<<16 | uintptr('d')<<8 | 0x40
	ioctl := func() error {
		_, _, errno := unix.Syscall(unix.SYS_IOCTL, file.Fd(), request, uintptr(unsafe.Pointer(&query)))
		if errno != 0 {
			return errno
		}
		return nil
	}
	if err := ioctl(); err != nil {
		return nil, 0, err
	}
	if query.size < 8 || query.size > 8+256*32 {
		return nil, 0, errSourceLimit
	}
	buffer := make([]byte, query.size)
	query.data = uint64(uintptr(unsafe.Pointer(&buffer[0])))
	if err := ioctl(); err != nil {
		return nil, 0, err
	}
	runtime.KeepAlive(buffer)
	if query.size > uint32(len(buffer)) {
		return nil, 0, errInvalidData
	}
	return parseXeEngines(buffer[:query.size])
}
func parseXeEngines(data []byte) ([]xeEngine, int, error) {
	if len(data) < 8 {
		return nil, 0, errInvalidData
	}
	count := int(binary.NativeEndian.Uint32(data[:4]))
	if count > (len(data)-8)/32 {
		return nil, 0, errInvalidData
	}
	result := []xeEngine{}
	omitted := 0
	for i := 0; i < count; i++ {
		row := data[8+i*32:]
		engine := xeEngine{binary.NativeEndian.Uint16(row), binary.NativeEndian.Uint16(row[2:]), binary.NativeEndian.Uint16(row[4:])}
		if engine.class > 4 {
			continue
		}
		if len(result) >= 16 {
			omitted++
			continue
		}
		result = append(result, engine)
	}
	return result, omitted, nil
}
