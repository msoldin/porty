// Package traffic reserves sleeping services' ports and reports wake attempts.
// It performs no work on running application traffic and changes no network rules.
package traffic

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"time"
)

const MaxBindings = 256

var ErrClosed = errors.New("wake listener closed")

type Binding struct {
	Network string `json:"network"`
	Address string `json:"address"`
}
type Config struct {
	Generation uint64
	Threshold  uint32
	Window     time.Duration
	Bindings   []Binding
}
type Wake struct {
	Generation uint64
	Network    string
	Remote     netip.AddrPort
	At         time.Time
}
type State struct {
	Generation  uint64
	Attempts    uint64
	Pending     bool
	TriggeredAt time.Time
}

type Listener struct {
	mu          sync.Mutex
	config      Config
	state       State
	windowStart time.Time
	windowCount uint32
	closed      bool
	err         error
	events      chan Wake
	closers     []func() error
	wg          sync.WaitGroup
	releaseOnce sync.Once
	closeOnce   sync.Once
	closeErr    error
}

func Validate(config Config) error {
	if config.Generation == 0 {
		return errors.New("wake generation must be positive")
	}
	if config.Threshold < 1 || config.Threshold > 1000 {
		return errors.New("wake threshold must be between 1 and 1000 attempts")
	}
	if config.Window < 10*time.Millisecond || config.Window > time.Minute {
		return errors.New("wake window must be between 10ms and 60s")
	}
	if len(config.Bindings) == 0 || len(config.Bindings) > MaxBindings {
		return errors.New("wake bindings must contain between 1 and 256 endpoints")
	}
	seen := map[Binding]bool{}
	for _, b := range config.Bindings {
		a, err := netip.ParseAddrPort(b.Address)
		if err != nil || a.Port() == 0 || a.Addr().IsMulticast() || a.Addr().Zone() != "" {
			return fmt.Errorf("invalid wake address %q", b.Address)
		}
		switch b.Network {
		case "tcp4", "udp4":
			if !a.Addr().Is4() {
				return errors.New("IPv4 listener requires an IPv4 address")
			}
		case "tcp6", "udp6":
			if !a.Addr().Is6() || a.Addr().Is4In6() {
				return errors.New("IPv6 listener requires an IPv6 address")
			}
		default:
			return errors.New("wake network must be tcp4, tcp6, udp4 or udp6")
		}
		b.Address = a.String()
		if seen[b] {
			return errors.New("duplicate wake binding")
		}
		seen[b] = true
	}
	return nil
}

// Listen reserves every binding before exposing any reader. A partial failure
// releases all acquired ports. The caller closes the reservation under its
// operation lock before starting the application container.
func Listen(config Config) (*Listener, error) {
	if err := Validate(config); err != nil {
		return nil, err
	}
	config.Bindings = append([]Binding(nil), config.Bindings...)
	l := &Listener{config: config, state: State{Generation: config.Generation}, events: make(chan Wake, 1)}
	var readers []func()
	for _, b := range config.Bindings {
		switch b.Network {
		case "tcp4", "tcp6":
			address, err := net.ResolveTCPAddr(b.Network, b.Address)
			if err != nil {
				l.Close()
				return nil, err
			}
			socket, err := net.ListenTCP(b.Network, address)
			if err != nil {
				l.Close()
				return nil, fmt.Errorf("reserve %s %s: %w", b.Network, b.Address, err)
			}
			l.closers = append(l.closers, socket.Close)
			if err := boundTCPBacklog(socket); err != nil {
				l.Close()
				return nil, err
			}
			readers = append(readers, func() { l.readTCP(socket, b.Network) })
		case "udp4", "udp6":
			address, err := net.ResolveUDPAddr(b.Network, b.Address)
			if err != nil {
				l.Close()
				return nil, err
			}
			socket, err := net.ListenUDP(b.Network, address)
			if err != nil {
				l.Close()
				return nil, fmt.Errorf("reserve %s %s: %w", b.Network, b.Address, err)
			}
			l.closers = append(l.closers, socket.Close)
			// Only the existence of a datagram matters. Limit queued payload memory.
			if err := socket.SetReadBuffer(8192); err != nil {
				l.Close()
				return nil, err
			}
			readers = append(readers, func() { l.readUDP(socket, b.Network) })
		}
	}
	for _, read := range readers {
		l.wg.Go(read)
	}
	return l, nil
}

func (l *Listener) Events() <-chan Wake { return l.events }
func (l *Listener) Snapshot() (State, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return State{}, ErrClosed
	}
	if l.err != nil {
		return State{}, l.err
	}
	return l.state, nil
}
func (l *Listener) record(network string, remote netip.AddrPort) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed || l.err != nil || l.state.Pending {
		return true
	}
	now := time.Now()
	l.state.Attempts++
	if l.windowStart.IsZero() || now.Sub(l.windowStart) >= l.config.Window {
		l.windowStart = now
		l.windowCount = 0
	}
	l.windowCount++
	if l.windowCount < l.config.Threshold {
		return false
	}
	l.state.Pending = true
	l.state.TriggeredAt = now
	l.events <- Wake{Generation: l.config.Generation, Network: network, Remote: remote, At: now}
	// Stop reading this socket once latched, keeping its reservation until Close.
	// The bounded kernel queue absorbs/drops later attempts without a busy loop.
	return true
}
func (l *Listener) readTCP(socket *net.TCPListener, network string) {
	for {
		connection, err := socket.AcceptTCP()
		if err != nil {
			l.fail(err)
			return
		}
		remote := connection.RemoteAddr().(*net.TCPAddr).AddrPort()
		_ = connection.SetLinger(0)
		_ = connection.Close()
		if l.record(network, remote) {
			return
		}
	}
}
func (l *Listener) readUDP(socket *net.UDPConn, network string) {
	var byteBuffer [1]byte
	for {
		_, remote, err := socket.ReadFromUDPAddrPort(byteBuffer[:])
		if err != nil {
			l.fail(err)
			return
		}
		if l.record(network, remote) {
			return
		}
	}
}
func (l *Listener) fail(err error) {
	l.mu.Lock()
	if l.closed || l.err != nil {
		l.mu.Unlock()
		return
	}
	l.err = fmt.Errorf("wake listener unavailable: %w", err)
	l.mu.Unlock()
	l.release()
}
func (l *Listener) release() {
	l.releaseOnce.Do(func() {
		for _, closeSocket := range l.closers {
			if err := closeSocket(); err != nil && !errors.Is(err, net.ErrClosed) {
				l.closeErr = errors.Join(l.closeErr, err)
			}
		}
	})
}
func (l *Listener) Close() error {
	l.closeOnce.Do(func() {
		l.mu.Lock()
		l.closed = true
		l.mu.Unlock()
		l.release()
		l.wg.Wait()
		close(l.events)
	})
	return l.closeErr
}
