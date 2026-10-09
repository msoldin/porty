package monitoring

import (
	"context"
	"math"
	"strconv"
	"strings"
	"time"
)

type amdReader struct {
	owner  *LinuxSources
	path   string
	device Device
}

func (r *amdReader) Close() error { return nil }
func (r *amdReader) scalar(name string, now time.Time, max float64) Reading {
	data, err := readBounded(r.owner.sys, r.path+"/"+name, scalarLimit)
	if err != nil {
		return unavailable(sourceReason(err), now)
	}
	value, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	if err != nil || float64(value) > max {
		return unavailable("invalid_data", now)
	}
	return available(float64(value), now)
}
func (r *amdReader) Read(ctx context.Context) (Batch, error) {
	if err := ctx.Err(); err != nil {
		return Batch{}, err
	}
	now := r.owner.now()
	batch := Batch{Devices: []Device{r.device}}
	addReading(&batch, r.device.ID, MetricGPUBusy, UnitPercent, r.scalar("gpu_busy_percent", now, 100))
	total := r.scalar("mem_info_vram_total", now, math.MaxUint64)
	used := r.scalar("mem_info_vram_used", now, math.MaxUint64)
	if total.Value != nil && used.Value != nil && *used.Value > *total.Value {
		used = unavailable("invalid_data", now)
	}
	addReading(&batch, r.device.ID, MetricGPUMemoryTotal, UnitBytes, total)
	addReading(&batch, r.device.ID, MetricGPUMemoryUsed, UnitBytes, used)
	temperature := unavailable("unsupported", now)
	names, _, err := readDirectory(r.owner.sys, r.path+"/hwmon", 16)
	if err == nil {
		for _, name := range names {
			data, err := readBounded(r.owner.sys, r.path+"/hwmon/"+name+"/temp1_input", scalarLimit)
			if err != nil {
				continue
			}
			value, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
			if err == nil && value >= -273150 && value <= 1000000 {
				temperature = available(float64(value)/1000, now)
				break
			}
		}
	}
	addReading(&batch, r.device.ID, MetricTemperature, UnitCelsius, temperature)
	return batch, nil
}
