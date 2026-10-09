package monitoring

import (
	"context"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFilesystemUsagePreservesReservedCapacity(t *testing.T) {
	total, used, available, percent, err := filesystemUsage(100, 30, 20, 4096)
	if err != nil || total != 409600 || used != 286720 || available != 81920 || percent != 70 {
		t.Fatal(total, used, available, percent, err)
	}
	for _, values := range [][4]uint64{{0, 0, 0, 4096}, {1, 2, 0, 4096}, {100, 30, 40, 4096}, {^uint64(0), 0, 0, 4096}} {
		if _, _, _, _, err := filesystemUsage(values[0], values[1], values[2], values[3]); err == nil {
			t.Fatal("invalid capacity accepted")
		}
	}
}
func TestDiskAggregateCountsPhysicalLeavesOnce(t *testing.T) {
	owner, dir, now := linuxFixture(t)
	writeFixture(t, dir, "proc/diskstats", "8 0 sda 1 0 100 0 1 0 200 0 0 0 0\n8 1 sda1 1 0 100 0 1 0 200 0 0 0 0\n253 0 dm-0 1 0 100 0 1 0 200 0 0 0 0 0\n")
	writeFixture(t, dir, "sys/class/block/sda/dev", "8:0")
	if err := os.MkdirAll(filepath.Join(dir, "sys/class/block/sda/slaves"), 0700); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, dir, "sys/class/block/sda1/partition", "1")
	writeFixture(t, dir, "sys/class/block/dm-0/slaves/sda/dev", "8:0")
	source := &diskSource{owner: owner}
	source.Collect(context.Background())
	*now = now.Add(2 * time.Second)
	writeFixture(t, dir, "proc/diskstats", "8 0 sda 1 0 104 0 1 0 208 0 0 0 0\n8 1 sda1 1 0 104 0 1 0 208 0 0 0 0\n253 0 dm-0 1 0 104 0 1 0 208 0 0 0 0 0\n")
	batch, err := source.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r := batch.Readings["disk-total.disk_read_rate"]; r.Value == nil || *r.Value != 1024 {
		t.Fatalf("physical read double counted: %#v", r)
	}
	if r := batch.Readings["disk-total.disk_write_rate"]; r.Value == nil || *r.Value != 2048 {
		t.Fatalf("physical write double counted: %#v", r)
	}
}
func TestFilesystemSourceRejectsRootEscape(t *testing.T) {
	owner, dir, _ := linuxFixture(t)
	if err := os.Symlink(t.TempDir(), filepath.Join(dir, "root", "escape")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/escape", "/../outside"} {
		s := &filesystemSource{owner: owner, device: Device{ID: "fs", Kind: DeviceFilesystem, MountPaths: []string{path}}, major: 1, minor: 1}
		batch, err := s.Collect(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if metricReading(t, batch, MetricFilesystemTotal).State != StateUnavailable {
			t.Fatal("root escape accepted")
		}
	}
}
func TestFilesystemRejectsUnpropagatedHostMount(t *testing.T) {
	owner, dir, _ := linuxFixture(t)
	writeFixture(t, dir, "root/mnt/disk/placeholder", "")
	s := &filesystemSource{owner: owner, device: Device{ID: "fs", Kind: DeviceFilesystem, MountPaths: []string{"/mnt/disk"}}, major: 999, minor: 999}
	batch, err := s.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if metricReading(t, batch, MetricFilesystemTotal).Reason != "host_mount_unavailable" {
		t.Fatal("measured parent filesystem")
	}
}
func TestFilesystemDiscoveryDeduplicatesBindsAndExcludesPseudoFilesystems(t *testing.T) {
	owner, dir, _ := linuxFixture(t)
	var st unix.Stat_t
	if err := unix.Stat(filepath.Join(dir, "root"), &st); err != nil {
		t.Fatal(err)
	}
	dev := fmt.Sprintf("%d:%d", unix.Major(uint64(st.Dev)), unix.Minor(uint64(st.Dev)))
	writeFixture(t, dir, "proc/1/mountinfo", fmt.Sprintf("1 0 %s / / rw - ext4 /dev/sda rw\n2 0 %s / /bind\\040space rw - ext4 /dev/sda rw\n3 0 0:3 / /tmp rw - tmpfs tmpfs rw\n4 0 0:4 / /overlay rw - overlay overlay rw\n5 0 0:5 / /nfs rw - nfs server:/data rw\n", dev, dev))
	sources, coverage, err := owner.discoverFilesystems()
	if err != nil || len(sources) != 2 || len(coverage) != 0 {
		t.Fatal(len(sources), coverage, err)
	}
	for _, source := range sources {
		fs := source.(*filesystemSource)
		if fs.major == unix.Major(uint64(st.Dev)) && fs.minor == unix.Minor(uint64(st.Dev)) {
			if len(fs.device.MountPaths) != 2 || fs.device.MountPaths[1] != "/bind space" {
				t.Fatal(fs.device.MountPaths)
			}
			batch, err := fs.Collect(context.Background())
			if err != nil || metricReading(t, batch, MetricFilesystemTotal).State != StateAvailable {
				t.Fatal(batch, err)
			}
		}
	}
}
func TestStorageDiscoveryReportsOmittedDevices(t *testing.T) {
	owner, dir, _ := linuxFixture(t)
	var mounts string
	for i := 1; i <= 66; i++ {
		mounts += fmt.Sprintf("%d 0 8:%d / /disk%d rw - ext4 /dev/d%d rw\n", i, i, i, i)
	}
	writeFixture(t, dir, "proc/1/mountinfo", mounts)
	sources, coverage, err := owner.discoverFilesystems()
	if err != nil || len(sources) != 64 || len(coverage) != 1 || coverage[0].Omitted != 2 {
		t.Fatal(len(sources), coverage, err)
	}
}
func TestFilesystemSourceDoesNotBlockOtherMetrics(t *testing.T) {
	owner, _, _ := linuxFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	fs := &filesystemSource{owner: owner, device: Device{ID: "fs", Kind: DeviceFilesystem, MountPaths: []string{"/"}}}
	fs.inspect = func(*os.File) (uint32, uint32, unix.Statfs_t, error) {
		close(entered)
		<-release
		return 0, 0, unix.Statfs_t{}, os.ErrPermission
	}
	healthy := &testSource{id: "cpu"}
	service, _ := NewService(ServiceOptions{Sources: []Source{fs, healthy}})
	service.schedule(context.Background())
	<-entered
	waitFor(t, func() bool {
		snapshot, _ := service.Snapshot("")
		return snapshot.Current.Readings["cpu.busy"].State == StateAvailable
	})
	close(release)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := service.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
}
