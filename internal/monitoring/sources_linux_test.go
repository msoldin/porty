package monitoring

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSourcePermissionFailureIsUnavailable(t *testing.T) {
	service, now := historyService(t)
	service.acceptBatch("cpu", fixtureBatch(*now, 24), nil, *now)
	*now = now.Add(2 * time.Second)
	service.acceptBatch("cpu", Batch{}, os.ErrPermission, *now)
	got, _ := service.Snapshot("")
	reading := got.Current.Readings["cpu.busy"]
	if reading.State != StateUnavailable || reading.Reason != "permission_denied" || reading.Value != nil {
		t.Fatalf("permission failure presented as a reading: %#v", reading)
	}
}

func linuxFixture(t *testing.T) (*LinuxSources, string, *time.Time) {
	t.Helper()
	dir := t.TempDir()
	for _, part := range []string{"proc", "sys", "root"} {
		if err := os.MkdirAll(filepath.Join(dir, part), 0700); err != nil {
			t.Fatal(err)
		}
	}
	source, err := OpenLinuxSources(LinuxOptions{Mode: "host", HostProc: filepath.Join(dir, "proc"), HostSys: filepath.Join(dir, "sys"), HostRoot: filepath.Join(dir, "root")})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	source.now = func() time.Time { return now }
	t.Cleanup(func() { _ = source.Close() })
	return source, dir, &now
}
func writeFixture(t *testing.T, dir, path, value string) {
	t.Helper()
	full := filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
}
func collectFixture(t *testing.T, owner *LinuxSources, id string) Batch {
	t.Helper()
	sources, err := owner.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range sources {
		if source.ID() == id {
			batch, err := source.Collect(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			return batch
		}
	}
	t.Fatalf("source %s missing", id)
	return Batch{}
}
func metricReading(t *testing.T, batch Batch, metric MetricKind) Reading {
	t.Helper()
	for _, series := range batch.Series {
		if series.Metric == metric {
			return batch.Readings[series.ID]
		}
	}
	t.Fatalf("metric %s missing", metric)
	return Reading{}
}

func TestHostModeNeverUsesContainerFallback(t *testing.T) {
	owner, dir, _ := linuxFixture(t)
	writeFixture(t, dir, "proc/net/dev", "eth0: 999 0 0 0 0 0 0 0 888 0 0 0 0 0 0 0\n")
	sources, _ := owner.Discover(context.Background())
	for _, source := range sources {
		if source.ID() == "network" {
			if _, err := source.Collect(context.Background()); err == nil {
				t.Fatal("fell back to container network counters")
			}
		}
	}
	info := owner.Host(context.Background())
	if info.Name == "" || info.Name == "localhost" {
		t.Fatal("missing host identity must be explicit")
	}
}
func TestRootedReadRejectsSymlinkEscapeAndOversize(t *testing.T) {
	owner, dir, _ := linuxFixture(t)
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "proc", "stat")); err != nil {
		t.Fatal(err)
	}
	if _, err := readBounded(owner.proc, "stat", 4096); err == nil {
		t.Fatal("read escaped configured root")
	}
	writeFixture(t, dir, "proc/large", strings.Repeat("x", 4097))
	if _, err := readBounded(owner.proc, "large", 4096); err == nil {
		t.Fatal("oversized input accepted")
	}
	if _, err := readBounded(owner.proc, "../secret", 4096); err == nil {
		t.Fatal("traversal accepted")
	}
}
func TestHostIdentityUsesConfiguredHostRoot(t *testing.T) {
	owner, dir, _ := linuxFixture(t)
	writeFixture(t, dir, "root/etc/hostname", "host-machine\n")
	writeFixture(t, dir, "proc/stat", "btime 1700000000\n")
	info := owner.Host(context.Background())
	if info.Name != "host-machine" || info.BootTime == nil || info.BootTime.Unix() != 1700000000 {
		t.Fatalf("host identity %#v", info)
	}
}
