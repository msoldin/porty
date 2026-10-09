package monitoring

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSensorSelectionDoesNotRelabelGPUAsCPU(t *testing.T) {
	owner, dir, _ := linuxFixture(t)
	writeFixture(t, dir, "sys/class/hwmon/hwmon0/name", "amdgpu")
	writeFixture(t, dir, "sys/class/hwmon/hwmon0/temp1_label", "edge")
	writeFixture(t, dir, "sys/class/hwmon/hwmon0/temp1_input", "42000")
	s := &sensorSource{owner: owner}
	batch, err := s.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, device := range batch.Devices {
		if device.Default {
			t.Fatal("GPU temperature selected as CPU")
		}
	}
	writeFixture(t, dir, "sys/class/hwmon/hwmon1/name", "coretemp")
	writeFixture(t, dir, "sys/class/hwmon/hwmon1/temp1_label", "Package id 0")
	writeFixture(t, dir, "sys/class/hwmon/hwmon1/temp1_input", "-1250")
	batch, err = s.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, device := range batch.Devices {
		if device.Default {
			found = true
			if batch.Readings[device.ID+".temperature"].Value == nil || *batch.Readings[device.ID+".temperature"].Value != -1.25 {
				t.Fatal("negative temperature lost")
			}
		}
	}
	if !found {
		t.Fatal("CPU package sensor not selected")
	}
}
func TestSensorSymlinkEscapeIsUnavailable(t *testing.T) {
	owner, dir, _ := linuxFixture(t)
	writeFixture(t, dir, "sys/devices/core/temp1_input", "45000")
	writeFixture(t, dir, "sys/devices/core/name", "coretemp")
	if err := os.MkdirAll(filepath.Join(dir, "sys/class/hwmon"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../devices/core", filepath.Join(dir, "sys/class/hwmon/hwmon0")); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	writeFixture(t, outside, "temp1_input", "99000")
	if err := os.Symlink(outside, filepath.Join(dir, "sys/class/hwmon/hwmon1")); err != nil {
		t.Fatal(err)
	}
	batch, err := (&sensorSource{owner: owner}).Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Devices) != 1 || *metricReading(t, batch, MetricTemperature).Value != 45 {
		t.Fatal("in-root sensor missing or escape read")
	}
}

func TestSensorSelectsKnownCPUCoreWhenPackageIsMissing(t *testing.T) {
	owner, dir, _ := linuxFixture(t)
	writeFixture(t, dir, "sys/class/hwmon/hwmon0/name", "coretemp")
	writeFixture(t, dir, "sys/class/hwmon/hwmon0/temp1_label", "Core 0")
	writeFixture(t, dir, "sys/class/hwmon/hwmon0/temp1_input", "41000")
	batch, err := (&sensorSource{owner: owner}).Collect(context.Background())
	if err != nil || len(batch.Devices) != 1 || !batch.Devices[0].Default {
		t.Fatalf("known CPU fallback missing: %+v, %v", batch.Devices, err)
	}
}
func TestSensorFindsThermalCPUAlongsideNonCPUSensors(t *testing.T) {
	owner, dir, _ := linuxFixture(t)
	writeFixture(t, dir, "sys/class/hwmon/hwmon0/name", "amdgpu")
	writeFixture(t, dir, "sys/class/hwmon/hwmon0/temp1_input", "51000")
	writeFixture(t, dir, "sys/class/thermal/thermal_zone0/type", "x86_pkg_temp")
	writeFixture(t, dir, "sys/class/thermal/thermal_zone0/temp", "42000")
	batch, err := (&sensorSource{owner: owner}).Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, device := range batch.Devices {
		if device.Default && device.Driver == "x86_pkg_temp" {
			return
		}
	}
	t.Fatal("thermal CPU sensor hidden by unrelated GPU sensor")
}
