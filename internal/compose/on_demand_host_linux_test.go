//go:build linux

package compose

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
)

type proxiedDemandHostDocker struct {
	*demandHostDocker
	host string
}

func (d *proxiedDemandHostDocker) DaemonHost() string { return d.host }

func TestOnDemandHostExplainsUnverifiableNativeSocketPeer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "docker.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	g, d := newGuardFixture(t)
	g.docker = &proxiedDemandHostDocker{demandHostDocker: &demandHostDocker{guardDocker: d}, host: "unix://" + path}
	err = g.CheckOnDemandHost(context.Background(), "game")
	if !errors.Is(err, ErrOnDemandHostUnavailable) || !errors.Is(err, ErrProtectionUnavailable) {
		t.Fatalf("expected actionable host failure preserving protection failure, got %v", err)
	}
}

func TestNativeHostProofFindsSocketInPeerNetworkNamespace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "peer.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	socket, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	if err := socketInProcessNetwork(socket, os.Getpid()); err != nil {
		t.Fatalf("same namespace rejected: %v", err)
	}
	if err := socketInProcessNetwork(socket, -1); err == nil {
		t.Fatal("unverifiable namespace accepted")
	}
}

func TestNativeSocketActivationRejectsUntrustedPIDFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "docker.pid")
	for _, contents := range []string{"", "-1", "1\n2", "999999999999999999999999999999999999999"} {
		if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := trustedDockerPID(path); err == nil {
			t.Fatal("invalid daemon PID accepted")
		}
	}
}
