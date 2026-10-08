//go:build linux

package control_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/docker/compose/v5/pkg/api"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"github.com/msoldin/porty/internal/compose"
	ctl "github.com/msoldin/porty/internal/control"
	"github.com/msoldin/porty/internal/ondemand"
	op "github.com/msoldin/porty/internal/operation"
	repo "github.com/msoldin/porty/internal/repository"
	store "github.com/msoldin/porty/internal/sqlite"
	"github.com/msoldin/porty/internal/stack"
)

// Run through deploy/on_demand_test.sh: the controller shares Docker's host
// network and proves its own container with the normal private-marker guard.
func TestOnDemandDockerLifecycle(t *testing.T) {
	if os.Getenv("PORTY_ON_DEMAND_DOCKER_TEST") != "1" {
		t.Skip("opt-in local Docker integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	image := os.Getenv("PORTY_ON_DEMAND_TEST_IMAGE")
	if image == "" {
		t.Fatal("test image missing")
	}
	sdk, err := client.New(client.FromEnv)
	if err != nil {
		t.Fatal(err)
	}
	defer sdk.Close()
	probe, err := net.ListenPacket("udp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(30000+rand.IntN(10000))))
	if err != nil {
		t.Fatal(err)
	}
	port := probe.LocalAddr().(*net.UDPAddr).Port
	probe.Close()
	project := "porty-demand-" + strconv.Itoa(port)
	ports := network.PortMap{}
	for _, protocol := range []string{"tcp", "udp"} {
		ports[network.MustParsePort(fmt.Sprintf("%d/%s", port, protocol))] = []network.PortBinding{{HostIP: netip.MustParseAddr("0.0.0.0"), HostPort: strconv.Itoa(port)}}
	}
	created, err := sdk.ContainerCreate(ctx, client.ContainerCreateOptions{Config: &container.Config{Image: image, Entrypoint: []string{"/control.test"}, Cmd: []string{"-test.run=^TestOnDemandGameFixture$", "-test.timeout=120s"}, Env: []string{"PORTY_GAME_FIXTURE_PORT=" + strconv.Itoa(port)}, Labels: map[string]string{api.ProjectLabel: project, api.ServiceLabel: "game", api.ContainerNumberLabel: "1", "io.porty.test": "on-demand"}}, HostConfig: &container.HostConfig{NetworkMode: "bridge", PortBindings: ports, CapDrop: []string{"ALL"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if _, err := sdk.ContainerRemove(cleanup, created.ID, client.ContainerRemoveOptions{Force: true, RemoveVolumes: true}); err != nil {
			t.Error(err)
		}
	}()
	if _, err := sdk.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	directory := filepath.Join(root, "game")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf("services:\n  game:\n    image: %s\n    network_mode: bridge\n    ports:\n      - '%d:%d/tcp'\n      - '%d:%d/udp'\n", image, port, port, port, port)
	if err := os.WriteFile(filepath.Join(directory, "docker-compose.yml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	runtime, resources, err := compose.NewDockerClient(ctx, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer resources.Close()
	request := compose.Request{StackDir: directory, ProjectName: project}
	digest, err := runtime.Digest(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stacks := store.NewStackStore(db)
	if err := stacks.Create(ctx, stack.Stack{ID: "s", DirectoryName: "game", ComposeProjectName: project, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	operations := store.NewOperationStore(db)
	if err := operations.CreateOperation(ctx, op.Operation{ID: "baseline", Kind: "deploy", ScopeType: "stack", ScopeID: "s", Status: op.OperationSucceeded}); err != nil {
		t.Fatal(err)
	}
	deployments := store.NewDeploymentStore(db)
	if err := deployments.SaveDeployment(ctx, op.Deployment{ID: "baseline", OperationID: "baseline", StackID: "s", Status: op.DeploymentSucceeded, ComposeDigest: digest, StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	coordinator := op.NewCoordinator()
	jobs := op.NewOperationService(operations, nil, time.Minute, 4096)
	defer jobs.Shutdown(context.Background())
	control := ctl.NewControlPlane(root, stacks, stack.NewEnvironmentService(stacks), repo.NewRepositoryService(controlGit{}), runtime, jobs, nil, coordinator, deployments, nil)
	groups := store.NewOnDemandStore(db)
	service := ctl.NewOnDemandService(control, groups)
	p := ondemand.DefaultPolicy()
	p.Name = "Game"
	p.Enabled = true
	p.Members = []string{"game"}
	p.StopGraceSeconds = 10
	p.StartupSeconds = 30
	group, err := service.SaveGroup(ctx, "s", "", ondemand.PolicyUpdate{Policy: p})
	if err != nil {
		t.Fatal(err)
	}
	sleepGroup := func(g ondemand.Group) {
		t.Helper()
		for deadline := time.Now().Add(8 * time.Second); ; {
			sample, err := service.ObserveOnDemand(ctx, g)
			if err != nil {
				t.Fatal(err)
			}
			err = service.ExecuteOnDemand(ctx, g, ondemand.Sleep, sample, &demandPorts{})
			if err == nil {
				return
			}
			if !errors.Is(err, ondemand.ErrConflict) || time.Now().After(deadline) {
				t.Fatal(err)
			}
			// Background bridge/ARP traffic may legitimately invalidate this
			// test's stop decision. Obtain new evidence instead of bypassing it.
			time.Sleep(100 * time.Millisecond)
		}
	}
	// Exercise the real coordinated stop; the fake port owner lets the controller
	// acquire reservations on startup, covering persisted sleeping reconciliation.
	sleepGroup(group)
	stopped, err := sdk.ContainerInspect(ctx, created.ID, client.ContainerInspectOptions{})
	if err != nil || stopped.Container.State.Running {
		t.Fatalf("process not terminated: %v", err)
	}
	runCtx, stopController := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- service.Run(runCtx); close(done) }()
	defer func() { stopController(); <-done }()
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	for deadline := time.Now().Add(10 * time.Second); ; {
		reservation, err := net.ListenPacket("udp4", address)
		if err != nil {
			break
		}
		reservation.Close()
		if time.Now().After(deadline) {
			t.Fatal("sleeping ports not reserved")
		}
		time.Sleep(10 * time.Millisecond)
	}
	sender, err := net.ListenUDP("udp4", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	destination, _ := net.ResolveUDPAddr("udp4", address)
	began := time.Now()
	if _, err := sender.WriteToUDP([]byte("wake"), destination); err != nil {
		t.Fatal(err)
	}
	// Do not send another packet until Docker reports running: one datagram must
	// be sufficient, independently of later application-level retries.
	for {
		current, err := groups.GetGroup(ctx, "s", group.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Phase == ondemand.Running {
			break
		}
		if current.PausedReason != "" || time.Since(began) > 15*time.Second {
			history, _ := operations.Operations(ctx, 5)
			t.Fatalf("wake failed: %+v operations=%+v", current, history)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Logf("single UDP datagram to verified running: %s", time.Since(began))
	buffer := make([]byte, 64)
	reachable := false
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		sender.SetDeadline(time.Now().Add(200 * time.Millisecond))
		sender.WriteToUDP([]byte("retry"), destination)
		n, _, err := sender.ReadFromUDP(buffer)
		if err == nil && string(buffer[:n]) == "retry" {
			reachable = true
			break
		}
	}
	if !reachable {
		t.Fatal("same UDP socket could not reach restarted server")
	}
	connection, err := net.DialTimeout("tcp4", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	connection.SetDeadline(time.Now().Add(time.Second))
	connection.Write([]byte("tcp"))
	data := make([]byte, 3)
	_, err = io.ReadFull(connection, data)
	connection.Close()
	if err != nil || string(data) != "tcp" {
		t.Fatalf("native TCP traffic: %v", err)
	}
	actual, err := sdk.ContainerInspect(ctx, created.ID, client.ContainerInspectOptions{})
	if err != nil || actual.Container.ID != created.ID || !actual.Container.State.Running {
		t.Fatal("existing container identity not preserved")
	}
	t.Log("real Docker stop, durable sleep, one-datagram wake, same-socket UDP retry, native TCP, and exact identity passed")
	group, err = groups.GetGroup(ctx, "s", group.ID)
	if err != nil {
		t.Fatal(err)
	}
	sleepGroup(group)
	for deadline := time.Now().Add(10 * time.Second); ; {
		reservation, err := net.ListenPacket("udp4", address)
		if err != nil {
			break
		}
		reservation.Close()
		if time.Now().After(deadline) {
			t.Fatal("second sleep not armed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	began = time.Now()
	wakeConnection, _ := net.DialTimeout("tcp4", address, time.Second)
	if wakeConnection != nil {
		wakeConnection.Close()
	}
	for {
		current, err := groups.GetGroup(ctx, "s", group.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Phase == ondemand.Running {
			break
		}
		if current.PausedReason != "" || time.Since(began) > 15*time.Second {
			t.Fatalf("single TCP attempt failed to wake: %+v", current)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Logf("single completed TCP connection to verified running: %s", time.Since(began))
	if os.Getenv("PORTY_ON_DEMAND_IDLE_TEST") == "1" {
		group, err = groups.GetGroup(ctx, "s", group.ID)
		if err != nil {
			t.Fatal(err)
		}
		policy := group.Policy
		policy.IdleSeconds = 60
		policy.MinRuntimeSeconds = 0
		if _, err := service.SaveGroup(ctx, "s", group.ID, ondemand.PolicyUpdate{Policy: policy, ExpectedRevision: group.Revision}); err != nil {
			t.Fatal(err)
		}
		began = time.Now()
		for {
			current, err := groups.GetGroup(ctx, "s", group.ID)
			if err != nil {
				t.Fatal(err)
			}
			if current.Phase == ondemand.Sleeping {
				break
			}
			if current.PausedReason != "" || time.Since(began) > 75*time.Second {
				t.Fatalf("automatic idle stop failed: %+v", current)
			}
			time.Sleep(200 * time.Millisecond)
		}
		actual, err := sdk.ContainerInspect(ctx, created.ID, client.ContainerInspectOptions{})
		if err != nil || actual.Container.State == nil || actual.Container.State.Running {
			t.Fatalf("idle process still running: %v", err)
		}
		t.Logf("automatic idle timeout fully terminated container after %s", time.Since(began))
	}
	if os.Getenv("PORTY_ON_DEMAND_PERF") == "1" {
		stopController()
		<-done
		measureOnDemandDocker(t, ctx, groups, service, sender, destination)
	}
}

func TestOnDemandGameFixture(t *testing.T) {
	port := os.Getenv("PORTY_GAME_FIXTURE_PORT")
	if port == "" {
		t.Skip("container-only echo server")
	}
	udp, err := net.ListenPacket("udp4", ":"+port)
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	tcp, err := net.Listen("tcp4", ":"+port)
	if err != nil {
		t.Fatal(err)
	}
	defer tcp.Close()
	go func() {
		for {
			c, err := tcp.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	data := make([]byte, 65535)
	for {
		n, remote, err := udp.ReadFrom(data)
		if err != nil {
			return
		}
		if _, err := udp.WriteTo(data[:n], remote); err != nil {
			return
		}
	}
}
