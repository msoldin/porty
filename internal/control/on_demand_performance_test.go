//go:build linux

package control_test

import (
	"context"
	"net"
	"runtime"
	"sort"
	"testing"
	"time"

	ctl "github.com/msoldin/porty/internal/control"
	"github.com/msoldin/porty/internal/ondemand"
	store "github.com/msoldin/porty/internal/sqlite"
	"golang.org/x/sys/unix"
)

// This is an opt-in Docker loopback measurement, not a physical-NIC or Minecraft
// benchmark. Report measured ratios; do not turn noisy timing into unit gates.
func measureOnDemandDocker(t *testing.T, ctx context.Context, groups *store.OnDemandStore, service *ctl.OnDemandService, sender *net.UDPConn, destination *net.UDPAddr) {
	t.Helper()
	start := func() func() {
		child, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		controller := ondemand.NewController(groups, service)
		go func() { defer close(done); _ = controller.Run(child) }()
		return func() { cancel(); <-done }
	}
	runtime.GC()
	var before, after unix.Rusage
	unix.Getrusage(unix.RUSAGE_SELF, &before)
	began := time.Now()
	stop := start()
	select {
	case <-time.After(15 * time.Second):
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	stop()
	unix.Getrusage(unix.RUSAGE_SELF, &after)
	cpu := time.Duration(after.Utime.Nano() + after.Stime.Nano() - before.Utime.Nano() - before.Stime.Nano())
	t.Logf("running-group observation: CPU %.4f%% of one core over %s; process peak-RSS increase %d KiB (daemon and kernel memory excluded)", 100*float64(cpu)/float64(time.Since(began)), time.Since(began).Round(time.Millisecond), after.Maxrss-before.Maxrss)
	type result struct{ rate, p99 float64 }
	measure := func() result {
		payload := make([]byte, 64)
		response := make([]byte, 64)
		latencies := make([]int64, 0, 8192)
		began := time.Now()
		count := 0
		for time.Since(began) < 2*time.Second {
			tick := time.Now()
			sender.SetDeadline(tick.Add(time.Second))
			if _, err := sender.WriteToUDP(payload, destination); err != nil {
				t.Fatal(err)
			}
			if _, _, err := sender.ReadFromUDP(response); err != nil {
				t.Fatal(err)
			}
			if len(latencies) < cap(latencies) {
				latencies = append(latencies, time.Since(tick).Nanoseconds())
			}
			count++
		}
		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		return result{rate: float64(count) / time.Since(began).Seconds(), p99: float64(latencies[(len(latencies)-1)*99/100]) / 1e6}
	}
	var ratios, deltas []float64
	for round := 0; round < 5; round++ {
		var baseline, active result
		if round%2 == 0 {
			baseline = measure()
			stop = start()
			active = measure()
			stop()
		} else {
			stop = start()
			active = measure()
			stop()
			baseline = measure()
		}
		ratios = append(ratios, active.rate/baseline.rate)
		deltas = append(deltas, active.p99-baseline.p99)
		t.Logf("64-byte UDP round %d: native %.0f/s, observed %.0f/s, ratio %.4f, p99 delta %.4f ms", round+1, baseline.rate, active.rate, ratios[round], deltas[round])
	}
	sort.Float64s(ratios)
	sort.Float64s(deltas)
	t.Logf("five alternating rounds: median throughput ratio %.4f; median p99 delta %.4f ms", ratios[2], deltas[2])
}
