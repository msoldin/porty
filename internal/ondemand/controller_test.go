package ondemand

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/msoldin/porty/internal/stack"
	"github.com/msoldin/porty/internal/traffic"
)

type controllerStore struct {
	mu    sync.Mutex
	group Group
}

func (s *controllerStore) ListGroups(context.Context, stack.StackID) ([]Group, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return []Group{s.group}, nil
}
func (s *controllerStore) GetGroup(context.Context, stack.StackID, string) (Group, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.group, nil
}
func (s *controllerStore) PauseGroup(_ context.Context, g Group, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.group.Revision != g.Revision {
		return ErrConflict
	}
	s.group.PausedReason = reason
	s.group.Revision++
	return nil
}

type controllerExecutor struct{ wakes chan Action }

func (e *controllerExecutor) ObserveOnDemand(_ context.Context, g Group) (Sample, error) {
	return Sample{Phase: g.Phase, ObservedAt: time.Now(), Evidence: g.Evidence}, nil
}
func (e *controllerExecutor) ExecuteOnDemand(ctx context.Context, g Group, a Action, _ Sample, ports Ports) error {
	if err := ports.Release(g); err != nil {
		return err
	}
	select {
	case e.wakes <- a:
	case <-ctx.Done():
	}
	return nil
}
func TestControllerWakesFromOneDatagramAndReleasesPortsOnShutdown(t *testing.T) {
	socket, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := socket.LocalAddr().String()
	socket.Close()
	p := DefaultPolicy()
	p.Name = "Game"
	p.Enabled = true
	p.Members = []string{"game"}
	store := &controllerStore{group: Group{ID: "group", StackID: "stack", Policy: p, Revision: 1, Phase: Sleeping, Evidence: Evidence{Bindings: []traffic.Binding{{Network: "udp4", Address: address}}}}}
	executor := &controllerExecutor{wakes: make(chan Action, 1)}
	controller := NewController(store, executor)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- controller.Run(ctx) }()
	defer func() { cancel(); <-done }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		probe, err := net.ListenPacket("udp4", address)
		if err != nil {
			break
		}
		probe.Close()
		if time.Now().After(deadline) {
			t.Fatal("listener not armed")
		}
		time.Sleep(time.Millisecond)
	}
	client, err := net.Dial("udp4", address)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.Write([]byte("one"))
	select {
	case a := <-executor.wakes:
		if a != WakeUp {
			t.Fatal(a)
		}
	case <-time.After(time.Second):
		t.Fatal("single datagram did not wake promptly")
	}
	controller.SuspendStack("stack")
	replacement, err := net.ListenPacket("udp4", address)
	if err != nil {
		t.Fatalf("port retained: %v", err)
	}
	replacement.Close()
}
