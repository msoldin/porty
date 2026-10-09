package monitoring

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestAMDGPUReportsAvailableFieldsIndependently(t *testing.T) {
	owner, dir, _ := linuxFixture(t)
	root := "sys/class/drm/card0/device"
	writeFixture(t, dir, root+"/gpu_busy_percent", "37")
	writeFixture(t, dir, root+"/mem_info_vram_total", "4096")
	writeFixture(t, dir, root+"/mem_info_vram_used", "1024")
	writeFixture(t, dir, root+"/mem_info_gtt_total", "999999")
	reader := &amdReader{owner: owner, path: "class/drm/card0/device", device: Device{ID: "gpu", Kind: DeviceGPU, Name: "AMD", Driver: "amdgpu"}}
	batch, err := reader.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if *metricReading(t, batch, MetricGPUBusy).Value != 37 || metricReading(t, batch, MetricTemperature).State != StateUnavailable || *metricReading(t, batch, MetricGPUMemoryTotal).Value != 4096 {
		t.Fatal(batch)
	}
}
func TestGPUReplacementDoesNotReuseHistory(t *testing.T) {
	owner, dir, _ := linuxFixture(t)
	writeFixture(t, dir, "sys/class/drm/card0/device/vendor", "0x1002")
	first, _, err := owner.discoverDRM()
	if err != nil || len(first) != 1 {
		t.Fatal(first, err)
	}
	if err := os.RemoveAll(filepath.Join(dir, "sys/class/drm/card0")); err != nil {
		t.Fatal(err)
	}
	empty, _, err := owner.discoverDRM()
	if err != nil || len(empty) != 0 {
		t.Fatal(empty, err)
	}
	writeFixture(t, dir, "sys/class/drm/card0/device/vendor", "0x1002")
	last, _, err := owner.discoverDRM()
	if err != nil || len(last) != 1 || first[0].ID() == last[0].ID() {
		t.Fatal("GPU replacement reused identity", err)
	}
}
