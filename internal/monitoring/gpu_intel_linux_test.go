package monitoring

import (
	"context"
	"encoding/binary"
	"errors"
	"golang.org/x/sys/unix"
	"testing"
	"time"
)

type fakePerf struct {
	value, enabled, running uint64
	err                     error
	closed                  bool
}

func TestIntelXeQueryBoundsEngineList(t *testing.T) {
	data := make([]byte, 8+17*32)
	binary.NativeEndian.PutUint32(data, 17)
	engines, omitted, err := parseXeEngines(data)
	if err != nil || len(engines) != 16 || omitted != 1 {
		t.Fatal(len(engines), omitted, err)
	}
	binary.NativeEndian.PutUint32(data, 1000)
	if _, _, err := parseXeEngines(data); err == nil {
		t.Fatal("malformed engine list accepted")
	}
}

func (f *fakePerf) Read() (uint64, uint64, uint64, error) {
	return f.value, f.enabled, f.running, f.err
}
func (f *fakePerf) Close() error { f.closed = true; return nil }
func intelFixture(t *testing.T) (*intelReader, *fakePerf, *time.Time) {
	owner, _, now := linuxFixture(t)
	counter := &fakePerf{value: 100, enabled: 100, running: 100}
	reader := &intelReader{owner: owner, device: Device{ID: "gpu", Kind: DeviceGPU, Driver: "i915", UtilizationBasis: "busiest_engine"}, engines: []intelEngine{{name: "rcs0", active: counter}}}
	return reader, counter, now
}
func TestIntelI915UsageUsesEngineBusyCounters(t *testing.T) {
	r, counter, now := intelFixture(t)
	r.Read(context.Background())
	*now = now.Add(2 * time.Second)
	counter.value += 500000000
	counter.enabled += 2000000000
	counter.running += 1000000000
	batch, err := r.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := metricReading(t, batch, MetricGPUBusy); got.Value == nil || *got.Value != 50 {
		t.Fatal(got)
	}
	r.Close()
	if !counter.closed {
		t.Fatal("perf descriptor leaked")
	}
}
func TestIntelXeUsageUsesActiveAndTotalTicks(t *testing.T) {
	if value, ok := intelEngineUsage(25, 100); !ok || value != 25 {
		t.Fatal(value, ok)
	}
	for _, pair := range [][2]uint64{{25, 0}, {101, 100}} {
		if _, ok := intelEngineUsage(pair[0], pair[1]); ok {
			t.Fatal("invalid ratio accepted")
		}
	}
	r, active, now := intelFixture(t)
	total := &fakePerf{value: 100, enabled: 100, running: 100}
	r.device.Driver = "xe"
	r.engines[0].total = total
	r.Read(context.Background())
	*now = now.Add(2 * time.Second)
	active.value += 25
	total.value += 100
	active.enabled += 100
	active.running += 100
	total.enabled += 100
	total.running += 100
	batch, _ := r.Read(context.Background())
	if value := metricReading(t, batch, MetricGPUBusy).Value; value == nil || *value != 25 {
		t.Fatal(value)
	}
}
func TestIntelDeniedPerfReportsUnavailable(t *testing.T) {
	r, _, _ := intelFixture(t)
	r.engines[0].active = nil
	r.engines[0].reason = "permission_denied"
	batch, _ := r.Read(context.Background())
	if got := metricReading(t, batch, MetricGPUBusy); got.State != StateUnavailable || got.Reason != "permission_denied" {
		t.Fatal(got)
	}
}
func TestIntelPartialCoverageDoesNotClaimWholeGPUUsage(t *testing.T) {
	r, counter, now := intelFixture(t)
	r.engines = append(r.engines, intelEngine{name: "vcs0", reason: "permission_denied"})
	r.Read(context.Background())
	*now = now.Add(2 * time.Second)
	counter.value += 500000000
	counter.enabled += 2000000000
	counter.running += 2000000000
	batch, _ := r.Read(context.Background())
	if *metricReading(t, batch, MetricGPUBusy).Value != 25 || batch.Devices[0].UtilizationBasis != "busiest_engine" || len(batch.Coverage) != 1 || !batch.Coverage[0].Partial {
		t.Fatal(batch)
	}
}
func TestIntelCounterResetReturnsCollecting(t *testing.T) {
	r, counter, now := intelFixture(t)
	r.Read(context.Background())
	*now = now.Add(2 * time.Second)
	counter.value = 1
	batch, _ := r.Read(context.Background())
	if metricReading(t, batch, MetricGPUBusy).State != StateCollecting {
		t.Fatal("rollback fabricated usage")
	}
	*now = now.Add(7 * time.Second)
	counter.value = 10
	batch, _ = r.Read(context.Background())
	if metricReading(t, batch, MetricGPUBusy).State != StateCollecting {
		t.Fatal("gap fabricated usage")
	}
	counter.err = unix.ENODEV
	batch, _ = r.Read(context.Background())
	if metricReading(t, batch, MetricGPUBusy).State != StateUnavailable {
		t.Fatal("removed device available")
	}
}
func TestIntelEventEncodingUsesDiscoveredFormat(t *testing.T) {
	got, err := encodePerfEvent("event=0x2,gt=3,engine_class=1", map[string]string{"event": "config:0-7", "gt": "config:60-63", "engine_class": "config:8,10"})
	if err != nil || got != (uint64(3)<<60)|258 {
		t.Fatal(got, err)
	}
	for _, format := range []string{"config1:0-7", "config:64", "config:7-0", "config:0,0"} {
		if _, err := encodePerfEvent("event=0x2", map[string]string{"event": format}); err == nil {
			t.Fatal("bad format accepted", format)
		}
	}
	if _, err := encodePerfEvent("event=256", map[string]string{"event": "config:0-7"}); err == nil {
		t.Fatal("field overflow accepted")
	}
}
func TestIntelPerfFailureClosesAvailablePair(t *testing.T) {
	r, counter, _ := intelFixture(t)
	r.engines[0].total = &fakePerf{err: errors.New("bad read")}
	batch, _ := r.Read(context.Background())
	if metricReading(t, batch, MetricGPUBusy).State != StateUnavailable {
		t.Fatal(batch)
	}
	r.Close()
	if !counter.closed {
		t.Fatal("counter not closed")
	}
}
