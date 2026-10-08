package compose

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"github.com/msoldin/porty/internal/traffic"
)

var ErrOnDemandIneligible = errors.New("container group is not eligible for on-demand activation")

type onDemandDocker interface {
	ContainerList(context.Context, client.ContainerListOptions) (client.ContainerListResult, error)
	ContainerInspect(context.Context, string, client.ContainerInspectOptions) (client.ContainerInspectResult, error)
	ContainerStart(context.Context, string, client.ContainerStartOptions) (client.ContainerStartResult, error)
	ContainerStop(context.Context, string, client.ContainerStopOptions) (client.ContainerStopResult, error)
	ContainerStats(context.Context, string, client.ContainerStatsOptions) (client.ContainerStatsResult, error)
	NetworkInspect(context.Context, string, client.NetworkInspectOptions) (client.NetworkInspectResult, error)
}
type OnDemandContainer struct {
	ID, Service, ImageID, StartedAt string
	RestartCount                    int
	Running, Healthcheck            bool
}
type OnDemandSnapshot struct {
	ProjectName, SourceDigest string
	Running                   bool
	Containers                []OnDemandContainer // dependency order; reverse this order for stop.
	Bindings                  []traffic.Binding   // running publication; retain verified bindings while asleep.
	ExternalDependencies      []string
}
type OnDemandCounters struct{ Received, Sent uint64 }

func (c *Client) SnapshotOnDemand(ctx context.Context, request Request, members []string) (OnDemandSnapshot, error) {
	project, err := Load(ctx, request)
	if err != nil {
		return OnDemandSnapshot{}, err
	}
	return c.inspectOnDemandProject(ctx, project, request, members)
}
func (c *Client) inspectOnDemandProject(ctx context.Context, p *types.Project, request Request, members []string) (OnDemandSnapshot, error) {
	snapshot := OnDemandSnapshot{ProjectName: p.Name}
	reject := func(reason string) (OnDemandSnapshot, error) {
		return snapshot, fmt.Errorf("%w: %s", ErrOnDemandIneligible, reason)
	}
	if c.demand == nil {
		return reject("Docker inspection unavailable")
	}
	if len(members) < 1 || len(members) > 8 || len(p.DisabledServices) != 0 {
		return reject("select 1–8 services without profiles")
	}
	selected := map[string]bool{}
	for _, name := range members {
		s, ok := p.Services[name]
		if !ok || selected[name] || s.GetScale() != 1 {
			return reject("services must exist, be unique and have one replica")
		}
		selected[name] = true
	}
	external := map[string]bool{}
	for name, s := range p.Services {
		for dep, condition := range s.DependsOn {
			if selected[dep] && !selected[name] {
				return reject("an unselected service depends on this group")
			}
			if selected[name] {
				if condition.Condition != "" && condition.Condition != types.ServiceConditionStarted && condition.Condition != types.ServiceConditionHealthy {
					return reject("completion dependencies are unsupported")
				}
				if _, ok := p.Services[dep]; !ok {
					return reject("dependency is unavailable")
				}
				if !selected[dep] {
					external[dep] = true
				}
			}
		}
	}
	order, err := onDemandOrder(p, selected)
	if err != nil {
		return snapshot, err
	}
	digest, err := Digest(p, request.Environment)
	if err != nil {
		return snapshot, err
	}
	snapshot.SourceDigest = digest
	rows, err := c.demand.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: client.Filters{}.Add("label", api.ProjectLabel+"="+p.Name)})
	if err != nil {
		return reject("unable to inspect containers")
	}
	if len(rows.Items) > 1024 {
		return reject("container inspection limit exceeded")
	}
	seen := map[string][]container.InspectResponse{}
	for _, row := range rows.Items {
		name := row.Labels[api.ServiceLabel]
		if !selected[name] && !external[name] {
			continue
		}
		result, err := c.demand.ContainerInspect(ctx, row.ID, client.ContainerInspectOptions{})
		if err != nil {
			return reject("container changed during inspection")
		}
		actual := result.Container
		if actual.ID != row.ID || actual.Config == nil || actual.State == nil || actual.HostConfig == nil {
			return reject("runtime identity unavailable")
		}
		labels := actual.Config.Labels
		if labels[api.ProjectLabel] != p.Name || labels[api.ServiceLabel] != name || strings.EqualFold(labels[api.OneoffLabel], "true") {
			return reject("unexpected container ownership")
		}
		if actual.State.Paused || actual.State.Restarting || actual.State.Dead {
			return reject("container is paused, restarting or dead")
		}
		seen[name] = append(seen[name], actual)
	}
	for name := range external {
		s := p.Services[name]
		if len(seen[name]) != s.GetScale() || len(seen[name]) == 0 {
			return reject("external dependency is missing")
		}
		requireHealth := hasOnDemandHealthcheck(s)
		for member := range selected {
			if p.Services[member].DependsOn[name].Condition == types.ServiceConditionHealthy {
				requireHealth = true
			}
		}
		for _, actual := range seen[name] {
			if !onDemandReady(actual, requireHealth) {
				return reject("external dependency must already be running and healthy")
			}
		}
		snapshot.ExternalDependencies = append(snapshot.ExternalDependencies, name)
	}
	sort.Strings(snapshot.ExternalDependencies)
	configuredPorts := 0
	for _, name := range order {
		if len(seen[name]) != 1 {
			return reject("missing or scaled selected service")
		}
		actual := seen[name][0]
		s := p.Services[name]
		if actual.Config.Labels[api.ContainerNumberLabel] != "1" {
			return reject("replica identity unavailable")
		}
		if actual.State.Running {
			if !onDemandReady(actual, hasOnDemandHealthcheck(s)) {
				return reject("selected service is not healthy")
			}
		} else if actual.State.Status != "exited" {
			return reject("selected service must be running or fully stopped")
		}
		if len(snapshot.Containers) > 0 && snapshot.Running != actual.State.Running {
			return reject("group contains mixed running and stopped services")
		}
		snapshot.Running = actual.State.Running
		mode := string(actual.HostConfig.NetworkMode)
		if mode == "host" || mode == "none" || strings.HasPrefix(mode, "container:") || s.NetworkMode == "host" || s.NetworkMode == "none" || strings.HasPrefix(s.NetworkMode, "service:") || strings.HasPrefix(s.NetworkMode, "container:") {
			return reject("selected services require independent bridge networks")
		}
		networks := map[string]bool{}
		if actual.NetworkSettings != nil {
			for name, n := range actual.NetworkSettings.Networks {
				if n != nil && n.NetworkID != "" {
					networks[n.NetworkID] = true
				} else {
					networks[name] = true
				}
			}
		}
		if len(networks) == 0 {
			if mode != "" && mode != "default" {
				networks[mode] = true
			} else if len(s.Networks) == 0 {
				networks[p.Name+"_default"] = true
			} else {
				for name := range s.Networks {
					n := p.Networks[name]
					if n.Name != "" {
						networks[n.Name] = true
					} else {
						networks[p.Name+"_"+name] = true
					}
				}
			}
		}
		for name := range networks {
			result, err := c.demand.NetworkInspect(ctx, name, client.NetworkInspectOptions{})
			if err != nil || result.Network.Driver != "bridge" {
				return reject("only ordinary bridge networks are supported")
			}
		}
		bindings, err := onDemandBindings(s, actual)
		if err != nil {
			return snapshot, err
		}
		configuredPorts += len(s.Ports)
		snapshot.Bindings = append(snapshot.Bindings, bindings...)
		snapshot.Containers = append(snapshot.Containers, OnDemandContainer{ID: actual.ID, Service: name, ImageID: actual.Image, StartedAt: actual.State.StartedAt, RestartCount: actual.RestartCount, Running: actual.State.Running, Healthcheck: hasOnDemandHealthcheck(s)})
	}
	if configuredPorts == 0 || len(snapshot.Bindings) > traffic.MaxBindings {
		return reject("group requires bounded, fixed published ports")
	}
	if snapshot.Running {
		if err := traffic.Validate(traffic.Config{Generation: 1, Threshold: 1, Window: time.Second, Bindings: snapshot.Bindings}); err != nil {
			return reject("published endpoints are invalid or unavailable")
		}
	}
	sort.Slice(snapshot.Bindings, func(i, j int) bool {
		a, b := snapshot.Bindings[i], snapshot.Bindings[j]
		if a.Network != b.Network {
			return a.Network < b.Network
		}
		return a.Address < b.Address
	})
	return snapshot, nil
}
func onDemandOrder(p *types.Project, selected map[string]bool) ([]string, error) {
	marks := map[string]int{}
	var order []string
	var visit func(string) error
	visit = func(name string) error {
		if marks[name] == 1 {
			return fmt.Errorf("%w: dependency cycle", ErrOnDemandIneligible)
		}
		if marks[name] == 2 {
			return nil
		}
		marks[name] = 1
		var deps []string
		for dep := range p.Services[name].DependsOn {
			if selected[dep] {
				deps = append(deps, dep)
			}
		}
		sort.Strings(deps)
		for _, dep := range deps {
			if err := visit(dep); err != nil {
				return err
			}
		}
		marks[name] = 2
		order = append(order, name)
		return nil
	}
	var names []string
	for name := range selected {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := visit(name); err != nil {
			return nil, err
		}
	}
	return order, nil
}
func hasOnDemandHealthcheck(s types.ServiceConfig) bool {
	return s.HealthCheck != nil && !s.HealthCheck.Disable && len(s.HealthCheck.Test) > 0 && s.HealthCheck.Test[0] != "NONE"
}
func onDemandReady(c container.InspectResponse, health bool) bool {
	if c.State == nil || !c.State.Running || c.State.Status != "running" || c.State.Paused || c.State.Restarting || c.State.Dead {
		return false
	}
	if c.State.Health != nil {
		return c.State.Health.Status == container.Healthy
	}
	return !health
}
func onDemandBindings(s types.ServiceConfig, actual container.InspectResponse) ([]traffic.Binding, error) {
	reject := func(reason string) ([]traffic.Binding, error) {
		return nil, fmt.Errorf("%w: %s", ErrOnDemandIneligible, reason)
	}
	allowed := map[network.Port]bool{}
	for _, p := range s.Ports {
		published, err := strconv.ParseUint(p.Published, 10, 16)
		if err != nil || published == 0 || p.Target == 0 || p.Target > 65535 {
			return reject("dynamic ports and port ranges are unsupported")
		}
		protocol := p.Protocol
		if protocol == "" {
			protocol = "tcp"
		}
		if protocol != "tcp" && protocol != "udp" {
			return reject("only TCP and UDP ports are supported")
		}
		if protocol == "udp" && published != uint64(p.Target) {
			return reject("UDP requires matching host and container ports for reliable retries")
		}
		key, ok := network.PortFrom(uint16(p.Target), network.IPProtocol(protocol))
		if !ok {
			return reject("invalid published port")
		}
		allowed[key] = true
		matched := false
		for _, binding := range actual.HostConfig.PortBindings[key] {
			if binding.HostPort == p.Published && (p.HostIP == "" || p.HostIP == binding.HostIP.String()) {
				matched = true
			}
		}
		if !matched {
			return reject("published ports differ from Compose configuration")
		}
	}
	for key, bindings := range actual.HostConfig.PortBindings {
		if len(bindings) > 0 && !allowed[key] {
			return reject("unexpected published ports")
		}
		for _, binding := range bindings {
			matched := false
			for _, configured := range s.Ports {
				protocol := configured.Protocol
				if protocol == "" {
					protocol = "tcp"
				}
				if uint32(key.Num()) == configured.Target && string(key.Proto()) == protocol && binding.HostPort == configured.Published && (configured.HostIP == "" || configured.HostIP == binding.HostIP.String()) {
					matched = true
				}
			}
			if !matched {
				return reject("unexpected published endpoint")
			}
		}
	}
	if !actual.State.Running {
		return nil, nil
	}
	if actual.NetworkSettings == nil {
		return reject("published endpoints unavailable")
	}
	var result []traffic.Binding
	for key := range allowed {
		bindings := actual.NetworkSettings.Ports[key]
		if len(bindings) == 0 {
			return reject("published endpoints unavailable")
		}
		for _, b := range bindings {
			matched := false
			for _, configured := range actual.HostConfig.PortBindings[key] {
				if configured.HostPort == b.HostPort && (!configured.HostIP.IsValid() || configured.HostIP.IsUnspecified() || configured.HostIP.Unmap() == b.HostIP.Unmap()) {
					matched = true
				}
			}
			if !matched {
				return reject("runtime endpoint differs from configured publication")
			}
			port, err := strconv.ParseUint(b.HostPort, 10, 16)
			if err != nil || port == 0 || !b.HostIP.IsValid() {
				return reject("published endpoint is invalid")
			}
			address := b.HostIP.Unmap()
			networkName := string(key.Proto()) + "4"
			if address.Is6() {
				networkName = string(key.Proto()) + "6"
			}
			result = append(result, traffic.Binding{Network: networkName, Address: netip.AddrPortFrom(address, uint16(port)).String()})
		}
	}
	return result, nil
}

func (c *Client) OnDemandCounters(ctx context.Context, ids []string) (map[string]OnDemandCounters, time.Time, error) {
	if c.demand == nil || len(ids) == 0 || len(ids) > 8 {
		return nil, time.Time{}, ErrOnDemandIneligible
	}
	result := make(map[string]OnDemandCounters, len(ids))
	var observed time.Time
	for _, id := range ids {
		response, err := c.demand.ContainerStats(ctx, id, client.ContainerStatsOptions{})
		if err != nil {
			return nil, time.Time{}, err
		}
		if response.Body == nil {
			return nil, time.Time{}, errors.New("network counters unavailable")
		}
		b, readErr := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil {
			return nil, time.Time{}, errors.Join(readErr, closeErr)
		}
		if len(b) > 1<<20 {
			return nil, time.Time{}, errors.New("network counter response exceeds limit")
		}
		var sample struct {
			ID       string                            `json:"id"`
			Read     time.Time                         `json:"read"`
			Networks map[string]container.NetworkStats `json:"networks"`
		}
		if err := json.Unmarshal(b, &sample); err != nil {
			return nil, time.Time{}, err
		}
		now := time.Now()
		if sample.ID != id || sample.Read.IsZero() || now.Sub(sample.Read) > 10*time.Second || sample.Read.After(now.Add(time.Second)) || len(sample.Networks) == 0 || len(sample.Networks) > 32 {
			return nil, time.Time{}, errors.New("network counters are incomplete or stale")
		}
		if observed.IsZero() || sample.Read.Before(observed) {
			observed = sample.Read
		}
		var counts OnDemandCounters
		for _, n := range sample.Networks {
			if ^uint64(0)-counts.Received < n.RxBytes || ^uint64(0)-counts.Sent < n.TxBytes {
				return nil, time.Time{}, errors.New("network counter overflow")
			}
			counts.Received += n.RxBytes
			counts.Sent += n.TxBytes
		}
		result[id] = counts
	}
	return result, observed, nil
}
