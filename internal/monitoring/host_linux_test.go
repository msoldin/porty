package monitoring

import (
	"testing"
	"time"
)

func TestCPUBusyExcludesIOWaitAndGuestDoubleCount(t *testing.T) {
	owner, dir, now := linuxFixture(t)
	writeFixture(t, dir, "proc/stat", "cpu 100 0 100 700 100 0 0 0 50 0\ncpu0 100 0 100 700 100 0 0 0 50 0\n")
	first := collectFixture(t, owner, "cpu")
	if metricReading(t, first, MetricCPUBusy).State != StateCollecting {
		t.Fatal("initial rate is not a baseline")
	}
	*now = now.Add(2 * time.Second)
	writeFixture(t, dir, "proc/stat", "cpu 130 0 120 730 120 0 0 0 70 0\ncpu0 130 0 120 730 120 0 0 0 70 0\n")
	second := collectFixture(t, owner, "cpu")
	reading := metricReading(t, second, MetricCPUBusy)
	if reading.Value == nil || *reading.Value != 50 {
		t.Fatalf("CPU should be 50%%: %#v", reading)
	}
}
func TestMemoryUsesAvailableCapacity(t *testing.T) {
	owner, dir, _ := linuxFixture(t)
	writeFixture(t, dir, "proc/meminfo", "MemTotal: 1000 kB\nMemAvailable: 600 kB\nMemFree: 100 kB\nSwapTotal: 200 kB\nSwapFree: 150 kB\n")
	batch := collectFixture(t, owner, "memory")
	if *metricReading(t, batch, MetricMemoryUsed).Value != 409600 || *metricReading(t, batch, MetricMemoryPercent).Value != 40 || *metricReading(t, batch, MetricSwapUsed).Value != 51200 {
		t.Fatal("available memory or swap accounting incorrect")
	}
}
func TestNetworkRateUsesElapsedTime(t *testing.T) {
	rate, ok := counterRate(100, 500, 2*time.Second)
	if !ok || rate != 200 {
		t.Fatal("incorrect byte rate")
	}
	for _, test := range []struct {
		previous, current uint64
		elapsed           time.Duration
	}{{500, 100, 2 * time.Second}, {1, 2, 0}, {1, 2, 7 * time.Second}} {
		if _, ok := counterRate(test.previous, test.current, test.elapsed); ok {
			t.Fatal("invalid rate did not reset")
		}
	}
	owner, dir, now := linuxFixture(t)
	writeFixture(t, dir, "proc/1/net/dev", "eth0: 100 0 0 0 0 0 0 0 200 0 0 0 0 0 0 0\n")
	writeFixture(t, dir, "sys/class/net/eth0/ifindex", "2\n")
	collectFixture(t, owner, "network")
	*now = now.Add(2 * time.Second)
	writeFixture(t, dir, "proc/1/net/dev", "eth0: 500 0 0 0 0 0 0 0 400 0 0 0 0 0 0 0\n")
	batch := collectFixture(t, owner, "network")
	if *metricReading(t, batch, MetricNetworkReceive).Value != 200 || *metricReading(t, batch, MetricNetworkSend).Value != 100 {
		t.Fatal("host network rate incorrect")
	}
}
func TestInterfaceReplacementResetsRate(t *testing.T) {
	owner, dir, now := linuxFixture(t)
	writeFixture(t, dir, "proc/1/net/dev", "eth0: 100 0 0 0 0 0 0 0 200 0 0 0 0 0 0 0\n")
	writeFixture(t, dir, "sys/class/net/eth0/ifindex", "2")
	first := collectFixture(t, owner, "network")
	*now = now.Add(2 * time.Second)
	writeFixture(t, dir, "proc/1/net/dev", "")
	collectFixture(t, owner, "network")
	*now = now.Add(2 * time.Second)
	writeFixture(t, dir, "proc/1/net/dev", "eth0: 500 0 0 0 0 0 0 0 400 0 0 0 0 0 0 0\n")
	last := collectFixture(t, owner, "network")
	if first.Devices[0].ID == last.Devices[0].ID || metricReading(t, last, MetricNetworkReceive).State != StateCollecting {
		t.Fatal("replacement reused identity or rate")
	}
}
func TestNetworkSelectsIPv6DefaultRoute(t *testing.T) {
	owner, dir, _ := linuxFixture(t)
	writeFixture(t, dir, "proc/1/net/dev", "eth0: 1 0 0 0 0 0 0 0 1 0 0 0 0 0 0 0\neth1: 1 0 0 0 0 0 0 0 1 0 0 0 0 0 0 0\n")
	writeFixture(t, dir, "proc/1/net/ipv6_route", "00000000000000000000000000000000 00 00000000000000000000000000000000 00 00000000000000000000000000000000 00000064 00000000 00000000 00000001 eth1\n")
	batch := collectFixture(t, owner, "network")
	for _, device := range batch.Devices {
		if device.Default && device.Name != "eth1" {
			t.Fatalf("wrong IPv6 default: %s", device.Name)
		}
	}
}
