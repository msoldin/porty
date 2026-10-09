package monitoring

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
)

type sensorSource struct {
	owner    *LinuxSources
	previous map[string]string
	epoch    uint64
}

func (s *sensorSource) ID() string   { return "sensors" }
func (s *sensorSource) Close() error { return nil }
func (s *sensorSource) Collect(ctx context.Context) (Batch, error) {
	if err := ctx.Err(); err != nil {
		return Batch{}, err
	}
	now := s.owner.now()
	batch := Batch{}
	next := map[string]string{}
	omitted := 0
	selected := false
	hwmons, extra, err := readDirectory(s.owner.sys, "class/hwmon", 128)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Batch{}, err
	}
	omitted += extra
	add := func(key, name, driver, input string, cpu bool) {
		if len(next) >= 128 {
			omitted++
			return
		}
		id := s.previous[key]
		if id == "" {
			s.epoch++
			id = stableID("sensor", key, strconv.FormatUint(s.epoch, 10))
		}
		next[key] = id
		device := Device{ID: id, Kind: DeviceSensor, Name: name, Driver: driver, Default: cpu && !selected}
		if device.Default {
			selected = true
		}
		batch.Devices = append(batch.Devices, device)
		data, err := readBounded(s.owner.sys, input, scalarLimit)
		reading := unavailable(sourceReason(err), now)
		if err == nil {
			value, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
			if err != nil || value < -273150 || value > 1000000 {
				reading = unavailable("invalid_data", now)
			} else {
				reading = available(float64(value)/1000, now)
			}
		}
		addReading(&batch, id, MetricTemperature, UnitCelsius, reading)
	}
	for _, hwmon := range hwmons {
		root := "class/hwmon/" + hwmon
		name, err := readBounded(s.owner.sys, root+"/name", scalarLimit)
		if err != nil {
			continue
		}
		driver := strings.TrimSpace(string(name))
		target, _ := s.owner.sys.Readlink(root)
		entries, extra, err := readDirectory(s.owner.sys, root, 512)
		omitted += extra
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !strings.HasPrefix(entry, "temp") || !strings.HasSuffix(entry, "_input") {
				continue
			}
			stem := strings.TrimSuffix(entry, "_input")
			if _, err := strconv.ParseUint(strings.TrimPrefix(stem, "temp"), 10, 16); err != nil {
				continue
			}
			labelData, _ := readBounded(s.owner.sys, root+"/"+stem+"_label", scalarLimit)
			label := strings.TrimSpace(string(labelData))
			if label == "" {
				label = stem
			}
			cpu := driver == "coretemp" && strings.HasPrefix(label, "Package") || (driver == "k10temp" || driver == "zenpower") && (label == "Tctl" || label == "Tdie")
			add(root+"|"+target+"|"+driver+"|"+stem, driver+" · "+label, driver, root+"/"+entry, cpu)
		}
	}
	// Thermal zones supplement hwmon when no hwmon temperature is exposed.
	if len(next) == 0 {
		zones, extra, err := readDirectory(s.owner.sys, "class/thermal", 128)
		omitted += extra
		if err == nil {
			for _, zone := range zones {
				if !strings.HasPrefix(zone, "thermal_zone") {
					continue
				}
				root := "class/thermal/" + zone
				kind, err := readBounded(s.owner.sys, root+"/type", scalarLimit)
				if err != nil {
					continue
				}
				label := strings.TrimSpace(string(kind))
				target, _ := s.owner.sys.Readlink(root)
				add(root+"|"+target+"|"+label, label, label, root+"/temp", label == "x86_pkg_temp" || label == "cpu-thermal")
			}
		}
	}
	s.previous = next
	if len(batch.Devices) == 0 {
		batch.Coverage = append(batch.Coverage, Coverage{Source: "sensors", Partial: true, Reason: "no_device"})
	}
	if omitted > 0 {
		batch.Coverage = append(batch.Coverage, Coverage{Source: "sensors", Partial: true, Omitted: omitted, Reason: "limit_exceeded"})
	}
	return batch, nil
}
