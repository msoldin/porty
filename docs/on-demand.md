# On-demand container groups

Configure **Stack → Settings → On-demand containers** after deploying the stack.
Add a named group with one to eight Compose service names. Each service must have
one existing replica. Groups cannot share services or published endpoints.

Porty fully stops the group's container processes after inactivity. Containers,
their writable layers, and volumes remain. While stopped, ordinary TCP/UDP
listeners reserve the published ports. A wake releases those listeners before
starting the same container IDs through the Docker SDK, in dependency order.
Running traffic follows Docker's normal network path, without a Porty proxy.

## Wake and idle behavior

The default threshold is **one UDP datagram or one completed TCP connection**.
TCP SYN packets alone do not count. Higher thresholds count these attempts across
the group's endpoints within a window starting at the first attempt. Incoming
payload is discarded; packets and connections are not buffered or replayed.
Clients must retry if their initial request arrives while the server is asleep.

| Setting | Default | Range |
| --- | --- | --- |
| Wake attempts | 1 | 1–1,000 |
| Wake window | 1 second | 10 ms–60 seconds |
| Idle timeout | 10 minutes | 1 minute–24 hours |
| Minimum runtime | 2 minutes | 0–60 minutes |
| Startup timeout | 5 minutes | 30 seconds–15 minutes |
| Stop grace | 2 minutes per service | 10 seconds–2 minutes |

Docker network byte counters are sampled every five seconds, with at most four
concurrent observations. Any incoming or outgoing traffic on any interface of a
member resets the idle timer. Scans, server-list queries, monitoring, and outgoing
background traffic can wake or keep services running. Missing/stale observations
never authorize a stop. Porty rechecks policy, runtime identity and activity under
the stack lock immediately before an automatic stop. Traffic arriving during
shutdown may require a later retry.

Minecraft Java's TCP connection or Bedrock's UDP query can trigger wake. A
Minecraft process still needs its normal world-loading time. Configure an
appropriate Docker healthcheck if "running" must mean the application is ready;
without a healthcheck Porty can verify only the running process. The first client
attempt can fail, and listing/querying a server can wake it without a player
joining. Actual Java and Bedrock servers have not yet been included in the live
test matrix.

## Deployment requirements

- Local, rootful Linux Docker over a Unix socket. Native Porty must share dockerd's
  network namespace; containerized Porty must use `network_mode: host` or
  `--network host`. Remote daemons and rootless networking are rejected.
- Porty proves container ownership with its private process marker. Native host
  proof reads the local socket peer and its namespace-specific procfs socket
  table. Restricted procfs access or an intervening socket proxy can make the
  feature unavailable; ordinary management remains available.
- Targets need ordinary bridge networks and fixed published TCP/UDP ports.
  Dynamic ranges, host/shared/container network modes, scaled members, profiles,
  one-shot dependencies and outside dependents are unsupported. External
  dependencies must already be running and cannot belong to another group.
- **UDP host and container ports must match.** Same-client-socket retries failed
  with remapped UDP in the tested Docker Desktop environment. Porty does not
  modify conntrack or firewall rules to repair this.
- Porty must be able to bind each published address and port while the target is
  stopped. Low ports require the OS's normal binding permission. Port conflicts
  pause automation instead of taking another process's port.
- Run one Porty instance per workspace/daemon. The maximum is 64 groups and 256
  published endpoints, with two transitions at once across distinct stacks.

No eBPF, custom kernel module, kernel-version-specific program, privileged
container, NET_ADMIN, NET_RAW, route, interface, sysctl or firewall edits are used.
Docker continues managing its own normal publication rules when containers start
and stop. Closing or killing Porty releases its listeners through the OS.

For a host-networked Porty container, explicitly bind the management UI to
loopback; `-p` does not restrict a host-network listener:

```sh
docker run --rm --network host \
  -v /srv/porty:/var/lib/porty \
  -v /var/run/docker.sock:/var/run/docker.sock \
  --group-add "$(stat -c %g /var/run/docker.sock)" \
  porty:local --listen 127.0.0.1:8080 --data-dir /var/lib/porty
```

The existing socket-group/data-directory ownership requirements still apply.
The OCI image provides an empty private Docker SDK configuration. For a new
native service account, create its private `.docker` directory and a `config.json`
containing `{}` in its home directory; preserve any existing Docker credentials.

## Holds, recovery and other operations

**Hold** suspends both automatic wake and sleep without stopping running services.
**Disable** and **Remove** also leave running containers up. A manually accepted
stack/member stop records a durable hold for every affected group. Starting or
restarting manually does not clear it. Manual runtime changes require explicit
**Resume**, which revalidates the group. Acknowledging an alert never resumes it.

Interrupted/partial transitions, container replacement and unexpected runtime
changes pause automation. Review the operation history, recover containers
manually, then Resume. Porty never replays a persisted transition after restart.
Running groups get a fresh idle window; verified sleeping groups rearm listeners.
Porty's own Compose stack is protected from automatic stop.

Automatic image updates retain their fully-running-stack eligibility check and
never wake a sleeping group. Container recreation invalidates the group's saved
identity and requires Resume. Direct external Docker starts may conflict with
reserved ports; Hold or Disable first.

GeoIP filtering is not implemented. Sleeping listeners retain the initiating
source address for future wake admission. Filtering native running traffic needs
a separate enforcement mechanism.

## Verification and limits

On 2026-10-08, Docker Desktop 29.5.2 on amd64/WSL2 passed real exact-container
stop, durable sleeping reconciliation, single-datagram wake, same-socket UDP retry,
native TCP echo, and a subsequent single-connection TCP wake. Echo fixtures
reached verified running about 287–373 ms after a wake datagram and 337–350 ms
after the TCP attempt. A separate automatic-idle test fully terminated the
container after 70.2 seconds with a 60-second idle timeout and no minimum runtime,
including observation-window initialization and polling. These are not Minecraft
startup measurements.

One running group consumed about 0.0781% of one Porty-process CPU core during a
15-second observation probe. Five alternating two-second 64-byte UDP loopback
rounds had a median observed/native transaction-rate ratio of 1.0368 and median
p99 RTT delta of -0.0115 ms. Individual ratios ranged from 0.9360 to 1.1717: noise
prevents a precise 98% throughput guarantee. A separate 64-group/256-UDP-listener
probe measured 0.0012% idle core and released all 256 descriptors.

Both warmed process peak-RSS deltas were zero; that does **not** establish a total
incremental memory bound. Docker daemon/kernel costs, maximum running-group
sampling load, physical-NIC throughput, native-host distributions, arm64 runtime, real
Minecraft servers, and sustained hostile traffic still need release measurements.
The Linux arm64 cross-build passed.
No claim of universal Linux compatibility or a completed performance matrix is
made from these fixtures.

Reproduce on an appropriate local Docker test host:

```sh
./deploy/on_demand_test.sh
PORTY_ON_DEMAND_IDLE_TEST=1 ./deploy/on_demand_test.sh
PORTY_ON_DEMAND_PERF=1 ./deploy/on_demand_test.sh
PORTY_LISTENER_PERF=1 go test ./internal/traffic \
  -run TestListenerMaximumFootprintAndCleanup -count=1 -v
```

The Docker script creates labeled disposable fixtures and removes its image and
containers. It mounts the Docker socket into its controller fixture; only run it
where Docker administrator access is appropriate. Logs and screenshots are not
committed.
