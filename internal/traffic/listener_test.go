package traffic

import (
	"errors"
	"net"
	"strconv"
	"testing"
	"time"
)

func freeAddress(t *testing.T, network string) string {
	t.Helper()
	if network == "udp4" {
		c, err := net.ListenPacket(network, "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		a := c.LocalAddr().String()
		c.Close()
		return a
	}
	l, err := net.Listen(network, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	a := l.Addr().String()
	l.Close()
	return a
}
func listenFixture(t *testing.T, network string, threshold uint32, window time.Duration) (*Listener, string) {
	t.Helper()
	address := freeAddress(t, network)
	l, err := Listen(Config{Generation: 17, Threshold: threshold, Window: window, Bindings: []Binding{{Network: network, Address: address}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := l.Close(); err != nil {
			t.Error(err)
		}
	})
	return l, address
}
func wakeWithin(t *testing.T, l *Listener) Wake {
	t.Helper()
	select {
	case w := <-l.Events():
		return w
	case <-time.After(time.Second):
		t.Fatal("wake notification missing")
		return Wake{}
	}
}
func TestListenerWakesOnOneDatagramWithoutMoreTraffic(t *testing.T) {
	l, address := listenFixture(t, "udp4", 1, time.Second)
	c, err := net.Dial("udp4", address)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Write([]byte("wake")); err != nil {
		t.Fatal(err)
	}
	w := wakeWithin(t, l)
	if w.Generation != 17 || w.Network != "udp4" {
		t.Fatalf("wrong wake: %+v", w)
	}
	state, err := l.Snapshot()
	if err != nil || !state.Pending || state.Attempts != 1 {
		t.Fatalf("wake not latched: %+v %v", state, err)
	}
	if _, err := net.ListenPacket("udp4", address); err == nil {
		t.Fatal("listener released port before coordinated admission")
	}
}
func TestListenerCountsCompletedTCPConnections(t *testing.T) {
	l, address := listenFixture(t, "tcp4", 3, time.Second)
	for range 2 {
		c, err := net.Dial("tcp4", address)
		if err != nil {
			t.Fatal(err)
		}
		c.Close()
	}
	select {
	case <-l.Events():
		t.Fatal("woke below threshold")
	case <-time.After(20 * time.Millisecond):
	}
	c, err := net.Dial("tcp4", address)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	wakeWithin(t, l)
}
func TestListenerClosesBeforeNativeServerBinds(t *testing.T) {
	for _, network := range []string{"tcp4", "udp4"} {
		t.Run(network, func(t *testing.T) {
			l, address := listenFixture(t, network, 1, time.Second)
			c, err := net.Dial(network, address)
			if err != nil {
				t.Fatal(err)
			}
			if network == "udp4" {
				c.Write([]byte("x"))
			}
			c.Close()
			wakeWithin(t, l)
			if err := l.Close(); err != nil {
				t.Fatal(err)
			}
			if err := l.Close(); err != nil {
				t.Fatal(err)
			}
			if network == "udp4" {
				server, err := net.ListenPacket(network, address)
				if err != nil {
					t.Fatal(err)
				}
				server.Close()
			} else {
				server, err := net.Listen(network, address)
				if err != nil {
					t.Fatal(err)
				}
				server.Close()
			}
			if _, err := l.Snapshot(); !errors.Is(err, ErrClosed) {
				t.Fatalf("closed listener usable: %v", err)
			}
		})
	}
}
func TestListenerRollsBackAllReservationsAfterBindConflict(t *testing.T) {
	first := freeAddress(t, "tcp4")
	busy, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	_, err = Listen(Config{Generation: 1, Threshold: 1, Window: time.Second, Bindings: []Binding{{Network: "tcp4", Address: first}, {Network: "tcp4", Address: busy.Addr().String()}}})
	if err == nil {
		t.Fatal("conflicting binding accepted")
	}
	released, err := net.Listen("tcp4", first)
	if err != nil {
		t.Fatalf("partial reservation leaked: %v", err)
	}
	released.Close()
}
func TestListenerRejectsInvalidConfiguration(t *testing.T) {
	base := Config{Generation: 1, Threshold: 1, Window: time.Second, Bindings: []Binding{{Network: "tcp4", Address: "127.0.0.1:25565"}}}
	for _, change := range []func(*Config){func(c *Config) { c.Generation = 0 }, func(c *Config) { c.Threshold = 0 }, func(c *Config) { c.Threshold = 1001 }, func(c *Config) { c.Window = time.Millisecond }, func(c *Config) { c.Bindings = nil }, func(c *Config) { c.Bindings = []Binding{{Network: "tcp", Address: "localhost:25565"}} }, func(c *Config) { c.Bindings = []Binding{{Network: "udp4", Address: "127.0.0.1:0"}} }} {
		c := base
		change(&c)
		if err := Validate(c); err == nil {
			t.Fatalf("invalid configuration accepted: %+v", c)
		}
	}
}
func TestListenerThresholdExpiresFromFirstAttempt(t *testing.T) {
	l, address := listenFixture(t, "udp4", 2, 30*time.Millisecond)
	c, err := net.Dial("udp4", address)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.Write([]byte("one"))
	time.Sleep(50 * time.Millisecond)
	c.Write([]byte("two"))
	time.Sleep(5 * time.Millisecond)
	if s, err := l.Snapshot(); err != nil || s.Pending {
		t.Fatalf("expired attempt counted: %+v %v", s, err)
	}
	c.Write([]byte("three"))
	wakeWithin(t, l)
}
func TestListenerAggregatesProtocolsForOneGroup(t *testing.T) {
	tcpAddress := freeAddress(t, "tcp4")
	udpAddress := freeAddress(t, "udp4")
	l, err := Listen(Config{Generation: 1, Threshold: 2, Window: time.Second, Bindings: []Binding{{Network: "tcp4", Address: tcpAddress}, {Network: "udp4", Address: udpAddress}}})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	tcp, err := net.Dial("tcp4", tcpAddress)
	if err != nil {
		t.Fatal(err)
	}
	tcp.Close()
	udp, err := net.Dial("udp4", udpAddress)
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	udp.Write([]byte("wake"))
	wakeWithin(t, l)
}
func TestListenerIPv6Wake(t *testing.T) {
	for _, network := range []string{"tcp6", "udp6"} {
		t.Run(network, func(t *testing.T) {
			reserve, err := net.ListenPacket("udp6", "[::1]:0")
			if err != nil {
				t.Skipf("IPv6 unavailable: %v", err)
			}
			port := reserve.LocalAddr().(*net.UDPAddr).Port
			reserve.Close()
			address := net.JoinHostPort("::1", strconv.Itoa(port))
			l, err := Listen(Config{Generation: 1, Threshold: 1, Window: time.Second, Bindings: []Binding{{Network: network, Address: address}}})
			if err != nil {
				t.Fatal(err)
			}
			defer l.Close()
			c, err := net.Dial(network, address)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if network == "udp6" {
				c.Write([]byte("wake"))
			}
			wakeWithin(t, l)
		})
	}
}
