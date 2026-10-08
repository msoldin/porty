//go:build linux

package traffic

import (
	"net"
	"os"
	"runtime"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestListenerMaximumFootprintAndCleanup(t *testing.T) {
	if os.Getenv("PORTY_LISTENER_PERF") != "1" {
		t.Skip("opt-in resource measurement")
	}
	// Initialize netpoll before measuring descriptor and memory deltas.
	warm, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	warm.Close()
	fdCount := func() int {
		entries, err := os.ReadDir("/proc/self/fd")
		if err != nil {
			t.Fatal(err)
		}
		return len(entries)
	}
	baselineFD := fdCount()
	runtime.GC()
	var before, after unix.Rusage
	unix.Getrusage(unix.RUSAGE_SELF, &before)
	var held []net.PacketConn
	var bindings []Binding
	for range MaxBindings {
		socket, err := net.ListenPacket("udp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, socket)
		bindings = append(bindings, Binding{Network: "udp4", Address: socket.LocalAddr().String()})
	}
	for _, socket := range held {
		socket.Close()
	}
	var listeners []*Listener
	for i := 0; i < MaxBindings; i += 4 {
		l, err := Listen(Config{Generation: uint64(i + 1), Threshold: 1, Window: time.Second, Bindings: bindings[i : i+4]})
		if err != nil {
			t.Fatal(err)
		}
		listeners = append(listeners, l)
		defer l.Close()
	}
	var idleStart unix.Rusage
	unix.Getrusage(unix.RUSAGE_SELF, &idleStart)
	began := time.Now()
	time.Sleep(10 * time.Second)
	unix.Getrusage(unix.RUSAGE_SELF, &after)
	cpu := time.Duration(after.Utime.Nano() + after.Stime.Nano() - idleStart.Utime.Nano() - idleStart.Stime.Nano())
	t.Logf("64 groups / 256 sleeping UDP endpoints: idle CPU %.4f%% core; process peak-RSS increase %d KiB; descriptors %d", 100*float64(cpu)/float64(time.Since(began)), after.Maxrss-before.Maxrss, fdCount()-baselineFD)
	for _, l := range listeners {
		if err := l.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if got := fdCount(); got != baselineFD {
		t.Fatalf("descriptor leak: before %d, after %d", baselineFD, got)
	}
	if after.Maxrss-before.Maxrss > 32*1024 {
		t.Fatal("listener process memory increase exceeded 32 MiB")
	}
}
