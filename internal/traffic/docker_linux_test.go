//go:build linux && (amd64 || arm64)

package traffic

import (
	"context"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

// This test owns every container it starts and only publishes on Docker's
// private bridge gateway. No Docker socket is mounted into a test container.
func TestDockerPublishedUDPRetryFromSameSocket(t *testing.T) {
	if os.Getenv("PORTY_TRAFFIC_DOCKER_TEST") != "1" {
		t.Skip("set PORTY_TRAFFIC_DOCKER_TEST=1 for the Docker publication gate")
	}
	image := os.Getenv("PORTY_TRAFFIC_TEST_IMAGE")
	if image == "" {
		t.Fatal("PORTY_TRAFFIC_TEST_IMAGE must contain the prepared test image")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	sdk, err := client.New(client.FromEnv)
	if err != nil {
		t.Fatal(err)
	}
	defer sdk.Close()
	bridge, err := sdk.NetworkInspect(ctx, "bridge", client.NetworkInspectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(bridge.Network.IPAM.Config) == 0 {
		t.Fatal("bridge has no gateway")
	}
	gateway := bridge.Network.IPAM.Config[0].Gateway
	clientNetwork := container.NetworkMode("bridge")
	if os.Getenv("PORTY_TRAFFIC_DOCKER_LOOPBACK") == "1" {
		gateway = netip.MustParseAddr("127.0.0.1")
		clientNetwork = "host"
	}
	address := gateway
	hostPort := strconv.Itoa(30000 + rand.IntN(25000))
	serverPort := "25565"
	if os.Getenv("PORTY_TRAFFIC_DOCKER_SAME_PORT") == "1" {
		serverPort = hostPort
	}
	create := func(role string, hc *container.HostConfig) string {
		t.Helper()
		executable := "/traffic.test"
		result, err := sdk.ContainerCreate(ctx, client.ContainerCreateOptions{Config: &container.Config{Image: image, Entrypoint: []string{executable}, Cmd: []string{"-test.run=^TestDockerTrafficHelper$", "-test.timeout=35s"}, Env: []string{"PORTY_TRAFFIC_ROLE=" + role, "PORTY_TRAFFIC_DEST=" + net.JoinHostPort(gateway.String(), hostPort), "PORTY_TRAFFIC_SERVER_PORT=" + serverPort}, Tty: true, Labels: map[string]string{"io.porty.test": "traffic-feasibility"}}, HostConfig: hc})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
			defer stop()
			if _, err := sdk.ContainerRemove(cleanup, result.ID, client.ContainerRemoveOptions{Force: true, RemoveVolumes: true}); err != nil {
				t.Errorf("fixture cleanup: %v", err)
			}
		})
		return result.ID
	}
	start := func(id string) {
		t.Helper()
		if _, err := sdk.ContainerStart(ctx, id, client.ContainerStartOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	await := func(id, text string) {
		t.Helper()
		for {
			logs, err := sdk.ContainerLogs(ctx, id, client.ContainerLogsOptions{ShowStdout: true, ShowStderr: true, Tail: "100"})
			if err != nil {
				t.Fatal(err)
			}
			b, err := io.ReadAll(io.LimitReader(logs, 16384))
			logs.Close()
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(b), text) {
				return
			}
			if strings.Contains(string(b), "FAIL") {
				t.Fatalf("fixture failed: %s", b)
			}
			select {
			case <-ctx.Done():
				t.Fatalf("waiting for %q: %s", text, b)
			case <-time.After(20 * time.Millisecond):
			}
		}
	}
	port := network.MustParsePort(serverPort + "/udp")
	target := create("server", &container.HostConfig{NetworkMode: "bridge", CapDrop: []string{"ALL"}, PortBindings: network.PortMap{port: {{HostIP: address, HostPort: hostPort}}}})
	start(target)
	await(target, "SERVER_READY")
	baselineClient := create("client", &container.HostConfig{NetworkMode: clientNetwork, CapDrop: []string{"ALL"}})
	start(baselineClient)
	await(baselineClient, "SAME_SOCKET_RETRY_OK")
	t.Log("baseline UDP publication reachable before sleep")
	timeout := 1
	if _, err := sdk.ContainerStop(ctx, target, client.ContainerStopOptions{Timeout: &timeout}); err != nil {
		t.Fatal(err)
	}
	monitor := create("monitor", &container.HostConfig{NetworkMode: "host", CapDrop: []string{"ALL"}})
	start(monitor)
	await(monitor, "MONITOR_READY")
	sender := create("client", &container.HostConfig{NetworkMode: clientNetwork, CapDrop: []string{"ALL"}})
	start(sender)
	await(monitor, "WAKE_RECEIVED")
	// The client sends exactly one packet then pauses. Only after the recorded
	// wake does the harness start the same stopped container by its exact ID.
	start(target)
	await(sender, "SAME_SOCKET_RETRY_OK")
	t.Log("one UDP datagram woke the monitor; retry from the same client socket reached the restarted exact container")
}

func TestDockerTrafficHelper(t *testing.T) {
	role := os.Getenv("PORTY_TRAFFIC_ROLE")
	if role == "" {
		t.Skip("Docker fixture helper")
	}
	dest, err := net.ResolveUDPAddr("udp4", os.Getenv("PORTY_TRAFFIC_DEST"))
	if err != nil {
		t.Fatal(err)
	}
	switch role {
	case "server":
		serverPort, err := strconv.Atoi(os.Getenv("PORTY_TRAFFIC_SERVER_PORT"))
		if err != nil {
			t.Fatal(err)
		}
		l, err := net.ListenUDP("udp4", &net.UDPAddr{Port: serverPort})
		if err != nil {
			t.Fatal(err)
		}
		defer l.Close()
		fmt.Println("SERVER_READY")
		b := make([]byte, 128)
		for {
			n, a, err := l.ReadFromUDP(b)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := l.WriteToUDP(b[:n], a); err != nil {
				t.Fatal(err)
			}
		}
	case "monitor":
		address := dest.AddrPort().Addr().Unmap()
		interfaces, err := net.Interfaces()
		if err != nil {
			t.Fatal(err)
		}
		index := 0
		for _, i := range interfaces {
			addrs, err := i.Addrs()
			if err != nil {
				t.Fatal(err)
			}
			for _, a := range addrs {
				p, err := netip.ParsePrefix(a.String())
				if err == nil && p.Addr() == address {
					index = i.Index
				}
			}
		}
		if index == 0 {
			t.Fatalf("private gateway %s is not visible in host network", address)
		}
		m, err := Listen(Config{Generation: 1, Threshold: 1, Window: time.Second, Bindings: []Binding{{Network: "udp4", Address: dest.String()}}})
		if err != nil {
			t.Fatal(err)
		}
		defer m.Close()
		fmt.Println("MONITOR_READY")
		select {
		case <-m.Events():
			if err := m.Close(); err != nil {
				t.Fatal(err)
			}
			fmt.Println("WAKE_RECEIVED")
		case <-time.After(20 * time.Second):
			s, err := m.Snapshot()
			t.Fatalf("no wake: %+v %v", s, err)
		}
		time.Sleep(10 * time.Second)
	case "client":
		c, err := net.ListenUDP("udp4", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		if _, err := c.WriteToUDP([]byte("wake"), dest); err != nil {
			t.Fatal(err)
		}
		fmt.Println("ONE_PACKET_SENT")
		time.Sleep(2 * time.Second)
		b := make([]byte, 128)
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
			c.SetDeadline(time.Now().Add(200 * time.Millisecond))
			if _, err := c.WriteToUDP([]byte("retry"), dest); err != nil {
				continue
			}
			n, _, err := c.ReadFromUDP(b)
			if err == nil && string(b[:n]) == "retry" {
				fmt.Println("SAME_SOCKET_RETRY_OK")
				return
			}
		}
		t.Fatal("UDP retry remained unreachable")
	default:
		t.Fatalf("unknown fixture role %q", role)
	}
}
