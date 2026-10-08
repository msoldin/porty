# Traffic triggered container groups

Status: design proposed for review. No implementation or performance validation
has been performed.

## Intent and agreed requirements

Run opted-in game servers only when their configured network ports receive
traffic. Embed the feature in Porty, with gameplay performance close to the same
Docker deployment without monitoring and low additional resource usage.

Agreed behavior:

- Support TCP and UDP. Any matching traffic counts, including browser queries,
  probes, and keepalives; no game-specific player detection.
- Start a configured group of Compose services within one stack. A group defaults
  to one service. Stop all members after inactivity, terminating their processes
  while retaining containers and volumes.
- A first connection or datagram may fail during startup. Clients retry; Porty
  does not buffer, proxy, or replay application traffic.
- Support native Porty and containerized Porty on the same Linux Docker host.
- Do not install firewall rules, modify routing or interface settings, or leave
  monitoring attachments behind when Porty exits, including after a crash.
- Preserve a narrow extension point for future GeoIP filtering. Implement no
  country database or filtering policy in this feature.

The defaults and limits below are proposed product decisions for this review.

## Existing architecture and placement

Source inspection used `/home/msoldin/porty` and its matching GitNexus index at
commit `56811d6ea0491954dc26de1fa38da48e39dda851`.

`internal/control/containers.go` already validates stack ownership, acquires the
shared coordinator, and submits tracked container operations.
`internal/compose/client.go` starts and stops exact container IDs through the
Docker SDK. Ordinary stack start can create containers through Compose, which is
too broad for a network-triggered wakeup. `internal/operation/deployment.go`
coordinates stack and repository mutations; `internal/operation/operation.go`
owns bounded jobs, cancellation, persistence, and publication.

Automatic updates provide relevant precedents: revisioned policy in SQLite,
controlled mutation in `internal/control`, recovery before scheduling, and
application-owned scheduler lifetime. `internal/compose/self_guard.go` protects
Porty's hosting project using runtime evidence. Reuse that protection rather
than identifying Porty by image or container name.

| Location | Responsibility |
| --- | --- |
| `internal/ondemand` | Group policy, validation, activity timers, lifecycle decisions, and narrow store/executor/monitor contracts |
| `internal/traffic` | Linux socket/eBPF attachment, endpoint matching, bounded activity snapshots, and observation health |
| `internal/control` | Authoritative admission, exact target resolution, coordinator ownership, manual-action interlocks, and tracked group operations |
| `internal/compose` | Docker inspection, dependency/readiness evidence, exact-ID start/stop, and self-protection through existing SDK clients |
| `internal/operation` | Existing operation history and shutdown; group completion metadata where required |
| `internal/sqlite` | Embedded Goose migration and revisioned policy/recovery persistence |
| `internal/app` | Wiring, startup reconciliation, shared Docker observation, and orderly teardown |
| `internal/http` | Authenticated group settings, status, and override endpoints using existing guards |
| `web/src/features/stacks` | On-demand settings, group status, and existing operation/alert links |

Keep scheduling and packet observation independent of Docker. The controller
implements the executor contract defined by its consumer in `ondemand`; avoid
an import cycle. `traffic` knows endpoint IDs, not Compose projects or operation
types. The existing app wiring supplies adapters. Do not introduce a generic
automation framework, plugin registry, alternate Docker client, or another job
queue. The new packages are domain and kernel boundaries, not file-size splits.

## Network observation

Use a socket-attached eBPF program on an `AF_PACKET` monitoring socket in the
host network namespace. Match selected host address, protocol, and port tuples;
update bounded per-endpoint counters in kernel maps. Return zero from the socket
filter after accounting, so no packet payload enters Porty's socket receive
queue. This discards only the monitoring socket's delivery, not the original
packet. Normal Docker forwarding and the original connection endpoints remain
unchanged. The kernel's [socket-filter documentation](https://kernel.org/doc/html/latest/networking/filter.html)
and [counter example](https://github.com/torvalds/linux/blob/master/samples/bpf/sock_example.c)
describe these mechanisms.

Choose socket attachment instead of nftables rules because the attachment is
owned by an open socket. Porty exclusively owns all socket, program, and map
descriptors; they are close-on-exec, never transferred to another process, and
never pinned in bpffs. Closing all owners releases the resources on normal exit
or process death. A suspended but still-living process retains its resources;
the guarantee concerns exit, not `SIGSTOP`. BPF lifetime depends on attachment
ownership, so this design must not be replaced with a persistent tc attachment.
See [BPF lifetime](https://man7.org/linux/man-pages/man2/bpf.2.html).

Use `github.com/cilium/ebpf` for program/map loading after reviewing its selected
version and dependency footprint. It provides a maintained
[Go BPF implementation](https://github.com/cilium/ebpf); do not build a competing
loader or invoke tcpdump, nft, bpftool, or Docker executables. Generate and embed
the small BPF object at build time. Runtime installation must not require clang,
kernel headers, or a compiler toolchain.

Activity semantics:

- Incoming traffic to a monitored host endpoint wakes an armed sleeping group.
  While running, incoming and outgoing traffic on any group endpoint refreshes
  its idle timer. Outgoing traffic alone does not wake a stopped group.
- Match addresses as well as ports so unrelated forwarded traffic and the same
  port on a different host address do not select the wrong group.
- Monitor external interfaces and local-loopback access explicitly. Capture
  points must account for pre-DNAT ingress and post-SNAT responses. Docker bridge
  traffic can appear more than once; activity is a boolean/time signal, not a
  billing counter. Do not let duplicates produce duplicate operations.
- Support IPv4 and IPv6, VLAN headers, bounded IPv6 extension-header parsing,
  and first fragments with transport headers. Noninitial fragments do not reveal
  ports: a packet observed without a first fragment must not be attributed by
  guesswork. Fragmented real game traffic is part of the release tests.
- Traffic can be observed before an existing firewall rejects it. In version
  one it still counts; Porty never bypasses that firewall or claims that a wake
  proves application reachability.

No promiscuous mode or interface reconfiguration. Refresh bindings when host
addresses/interfaces change. Known coverage gaps, failed reads, or missing required
bindings make affected observation unavailable; they must not be interpreted as
zero activity. Reject malformed packets without disabling the monitor. Report
unsupported traffic paths instead of presenting an unsafe idle policy as working.

## Deployment contract

Initial scope is Linux with a local rootful Docker Engine. Porty can run natively
or as a container using `network_mode: host`. Host networking shares a namespace;
it does not add a route or firewall rule. A Docker socket mount alone does not
give a bridge-networked Porty access to host packet sockets.
[Docker host-network documentation](https://docs.docker.com/engine/network/drivers/host/)
describes the namespace behavior.

Game containers initially use ordinary bridge networking with fixed published
host ports. Porty can suggest endpoints from Docker inspection, but persists
them explicitly so observation survives container shutdown. Reject dynamic
published ports, overlapping group endpoint matches, host/macvlan/ipvlan/overlay
game networking, remote daemons, and rootless hosts in this first release.
Porty's own host-network deployment is supported independently of target network
mode. Direct container-IP access is outside the monitored contract.

The packaging feasibility gate must prove effective `CAP_NET_RAW` and BPF-load
permissions with the existing non-root image and native service. The initial
candidate is `CAP_NET_RAW` plus `CAP_BPF` on supported modern kernels, with only
the necessary seccomp syscall allowances. Confirm the actual minimum; never
silently escalate to privileged mode or `CAP_SYS_ADMIN`. Native systemd settings
must allow the required address families, including packet sockets and read-only
netlink discovery. Do not change host sysctls or globally relax BPF restrictions.
If the environment cannot support the monitor within this contract, mark the
feature unavailable while keeping ordinary Porty management usable.

Container deployment must configure Porty's UI bind address explicitly: host
networking removes the existing `127.0.0.1:8080:8080` publication boundary, and
the current image defaults to listening on all interfaces.

## Group policy and admission

Identify a group by stable group ID and stack ID; identify members by Compose
service names, never persisted container IDs. Resolve exact current IDs under
the stack coordinator immediately before a transition. Version one supports
one replica per member, at most eight members per group, and nonoverlapping
membership across groups. Reject scale changes until policy is revalidated.

Persist selected endpoints, members, policy revision, enabled state, manual hold,
pause reason, and transition recovery intent. Existing stacks default to off.

Proposed settings:

| Setting | Default | Allowed range |
| --- | --- | --- |
| Idle timeout | 10 minutes | 1 minute to 24 hours |
| Minimum running time after successful wake | 2 minutes | 0 to 1 hour |
| Group startup deadline | 5 minutes | 30 seconds to 15 minutes |
| Per-container stop grace | 2 minutes | 10 seconds to 2 minutes |

The configured stop grace overrides Docker's default for these automatic stops
and is shown explicitly to the operator. Stop dependents before dependencies,
allowing at most 16 minutes for eight serial stops plus bounded inspection within
the operation service's existing 20-minute maximum. Startup readiness shares one
group deadline, not a separate full timeout for every member.

Admission requires a successful current deployment baseline, unchanged relevant
Compose/environment configuration, complete expected container membership,
supported endpoints, healthy observation, and positive self-protection evidence.
Never automatically create, pull, build, deploy, or remove containers. Missing
containers require an administrator to deploy first. A changed image tag does
not cause a wake to pull a different image: start the existing container.

Read Compose dependency relationships. Start selected dependencies before their
dependents and stop in reverse order. Honor supported `service_started` and
`service_healthy` conditions. Dependencies outside the group must already satisfy
their conditions and are never started or stopped by the group. Reject dependency
cycles, one-shot completion dependencies, and cross-group dependencies in v1.
Also reject groups whose members have dependents elsewhere in the stack: stopping
a shared dependency must not silently break an unmanaged service. Show a concrete
eligibility reason so the operator can adjust the group.

Before every mutation, reuse `RuntimeGuard.CheckProject`; unknown ownership is
unavailable, not permission to continue. Never enable a group in Porty's hosting
stack, even when Porty itself is not selected. Keep guard inspection off the
packet and counter-sampling paths.

## Lifecycle and concurrency

The externally visible states are disabled, sleeping, starting, running,
stopping, held, unavailable, and recovery required. Manual hold and failure pause
are durable policy conditions; Docker state remains the source of runtime facts.

- Enabling a fully stopped group arms it without starting it. Enabling a fully
  running group starts a fresh idle window. Mixed state requires explicit
  reconciliation rather than guessing which processes may be stopped.
- An incoming counter change queues one wake decision per group. Bounded workers
  call the controller; no goroutine or operation is created for each packet.
- The controller acquires the existing stack coordinator, rereads policy and
  runtime evidence, records intent, and hands ownership of the lock to the
  tracked operation. Busy stacks retain one coalesced pending wake; disabling,
  holding, or changing policy invalidates it. Never acquire a second stack lock
  inside an already locked path.
- Readiness means every selected container is running and passes its configured
  Docker healthcheck; without a healthcheck require 30 seconds of uninterrupted
  running. Do not probe game ports, because probes would themselves count as
  traffic. Explain that Docker readiness cannot guarantee game-specific readiness.
- Sleep requires a full idle window, expired minimum runtime, and fresh monitor
  evidence. Recheck counters immediately before the first stop and before stopping
  further members. Traffic observed before the first mutation cancels the sleep.
- Once a member has stopped, retain a wake request and complete the orderly stop
  before restarting the group. This avoids mixed lifecycle commands. A packet can
  race the final check and Docker stop; clients may have to reconnect. Do not
  claim an atomic boundary between kernel observation and a Docker operation.
- Partial start/stop, readiness failure, and ambiguous Docker timeouts pause the
  group for recovery and raise one deduplicated alert. Do not kill newly started
  members as a speculative rollback or retry automatically under continuing
  traffic. The operator can inspect and resume after correcting the problem.

Start with at most two transitions across distinct stacks. The existing
coordinator still limits each stack to one mutation, including unrelated groups.
Admission persistence failure means no Docker mutation. Record policy revision,
resolved IDs, action, operation ID, and intended phase before any mutation; never
persist raw Compose projects, environment values, or packet contents.

## Integration with existing actions

Manual stop of a group member or its stack durably holds the affected group before
the existing operation starts. Persisting that hold and admitting the operation
must be coordinated; a failed admission must not leave a silent policy change.
Manual start/restart leaves the hold in place. An explicit Resume automation
action clears it after validation. Provide a Hold automation action for operators
who want to keep a running server up. Holds affect the whole group.

Successful deploy/recreate or automatic update refreshes member IDs and endpoint
evidence and starts a fresh observation window. Changed membership or ports
requires policy revalidation; failed or partially applied mutations pause the
group. Add this notification at control-plane completion boundaries rather than
duplicating orchestration in HTTP handlers. Use Docker events and periodic
reconciliation to detect external changes too.

Automatic updates keep their current requirement for a fully running healthy
stack. They do not wake sleeping groups to update them. Their applying/recovery
state blocks on-demand actions for that stack. Update preparation releases the
coordinator today; if a group sleeps during preparation, the update's existing
fresh-evidence check must prevent mutation, with a regression test proving it.

Archived/deleted stacks cannot wake. Archiving or deleting a stack disarms its
groups under existing coordination and removes observation matches. Policy
deletion and disable detach monitoring without stopping currently running
containers. Reject policy mutation while a group operation owns its stack lock;
return the normal conflict response instead of altering a running operation.

Unexpected external stop, removal, or mixed state pauses automation. A changed
container restart count causes reconciliation and a new minimum-runtime window;
persistent health failure requires recovery. On-demand operation outcomes and
Docker events must be correlated so Porty's own stops are not treated as external.
Recommend `restart: unless-stopped` for these services. Existing restart policies
remain untouched: Docker may start some manually stopped containers after daemon
restart, and Porty must reconcile that fact rather than promise persistent sleep.

## Restart and shutdown

At startup, reconcile Docker state and pending transition records before arming
groups. Interrupted mutations become recovery required; never replay them from
history. Running groups receive a fresh full idle window. Stopped groups may arm
only with a matching recorded completed sleep or explicit initial enable and
current baseline. Ambiguous stopped or partially running groups remain paused.
Do not count Porty downtime as inactivity or reuse old monotonic counter times.

Monitor loss, interface changes, or Docker unavailability suspend new automatic
mutations. Keep running workloads running and detach broken observation when
necessary. After recovery, take new baselines and wait a full idle window before
stopping anything. A hung sampling loop must be detected by a separate freshness
check at mutation admission; a zero delta is insufficient evidence.

Extend `Application.Shutdown`: stop admitting on-demand work, cancel its decision
loop, let the existing operation service drain/cancel admitted work within the
shutdown deadline, then close monitor descriptors and Docker resources. Persist
uncertain transitions for startup reconciliation. Do not stop game servers simply
because Porty shuts down. Already-submitted Docker stop requests can still finish
after Porty exits; closing a client does not undo a daemon-side mutation.

## Resource and performance contract

Use one host monitor with bounded endpoint maps and a single batched activity
sampling loop, initially every second. No capture rings containing payloads, flow
tracking, per-player maps, packet logging, or database writes per sample. Persist
policy and lifecycle transitions only; expose recent activity as volatile status.
Use per-CPU counters if measurements justify them and include their CPU-count
multiplier in the memory budget. Do not claim that avoiding payload copies removes
all packet-socket overhead: kernel packet taps can affect unrelated traffic too.

Initial bounds are 64 groups and 256 expanded address/protocol/port matches per
host, with a 16 MiB cap on feature-owned kernel maps. Reject configurations over
limits before attachment. A packet stream cannot allocate new map keys. At zero
enabled groups, close the monitor and stop its sampling timer. Share one Docker
event subscription and a bounded 30-second reconciliation pass; inspect details
only on changes and before transitions, not every second per group.

Proposed release budgets, measured against the same Porty build with the feature
disabled and identical Docker networking, are:

- At 64 sleeping groups and no traffic: less than 1% of one CPU core and 32 MiB
  additional total memory, including attributed kernel allocations/maps.
- At declared representative game-server load: at least 98% of baseline useful
  throughput and no more than 0.2 ms additional p99 round-trip latency or jitter.
  Report packet loss explicitly; feature-attributable loss at the baseline's
  sustainable offered load fails the gate.
- Wake decision admission within two seconds at p99 on an otherwise idle host;
  report Docker admission, container boot, and application readiness separately.
- No sustained growth in descriptors, goroutines, or memory after churn tests.

These are acceptance targets, not measured claims. Repeat A/B runs, disclose
hardware/kernel/CPU count and offered packet rates, include small UDP packets and
heavy unrelated host traffic, and measure host CPU as well as Porty CPU. If the
socket backend misses the budgets, revisit attachment choice before expanding UI
work; do not silently substitute a proxy or persistent host rules.

## Persistence and user interface

Use Goose tables for groups, service membership, endpoint definitions, and current
transition intent. Foreign keys follow existing stack lifecycle conventions;
operation history remains in existing tables. Use expected policy revisions and
transactional uniqueness for membership/endpoint reservations. Runtime wildcard
address overlap requires semantic validation, not only a SQL unique index.
Use sqlc only for selected stable reads, consistent with the current repository.

Add a focused On demand section to stack settings: group name, selected services,
TCP/UDP endpoints, idle timeout, advanced timeouts, enable, hold, and resume.
Show Sleeping separately from an unexplained stopped state, with startup,
unavailable, and recovery reasons and links to operation history and alerts.
Status must not claim stopped containers are running. Keep network implementation
details out of the normal flow; deployment prerequisites belong in actionable
unavailability messages and operator documentation.

Use stack-scoped group CRUD/status and hold/resume routes under
`/api/v1/stacks/{id}/on-demand/groups`. Reuse read/mutation route wrappers,
authentication, origin/CSRF guards, body limits, audit records, and conflict
responses. API calls and types stay in the stacks feature; presentation components
use a focused feature hook. No new global frontend state or separate dashboard.

## Future GeoIP filtering

Preserve endpoint identity and aggregate activity as the boundary between the
network backend and lifecycle controller. Socket filtering is observation-only;
actual GeoIP blocking needs a different enforcement attachment. A later design
can use an unpinned descriptor-owned link on compatible kernels while retaining
the lifecycle contracts. Do not imply every BPF attachment automatically detaches.

Future policy must logically precede wake/activity accounting: denied traffic
neither wakes nor keeps a group running. Because packet sockets can observe before
an enforcement hook, merely adding a firewall hook alongside the current observer
does not establish that ordering. Replace the observation attachment or share the
same versioned allow decision in the observer and enforcer, with atomic policy
activation and tests for denied traffic.

Resolve country data outside the packet path and compile allowed/denied prefixes
into bounded IPv4/IPv6 maps. Linux supports
[longest-prefix maps](https://docs.kernel.org/bpf/map_lpm_trie.html). Database
provider/licensing, unknown/private addresses, update consistency, proxy source
addresses, and enforcement placement belong to that future feature.

The agreed exit behavior means future Porty-owned filtering also disappears when
Porty exits; existing host firewall policy continues to apply. Crash-persistent
blocking would require a deliberate change to that requirement.

## Verification and delivery gates

Begin implementation with an isolated Linux networking feasibility gate.
It must establish the actual supported kernel/security matrix before committing
to the backend. Initial candidates for testing are Linux 6.1, 6.6, and 6.12 on
amd64 and arm64; a version number alone does not prove BPF is enabled or permitted.

1. Prove socket/map cleanup after normal exit, `SIGKILL`, container stop, failed
   startup, and repeated restarts. Compare firewall, routes, interface flags, and
   BPF/socket resources before and after. Porty must not mutate host networking;
   Docker's own ordinary lifecycle changes are evaluated separately.
2. Prove native and non-root container packaging, explicit UI binding, capability
   failure reporting, and zero-group teardown without broad privilege escalation.
3. Exercise real TCP and UDP wakeups through Docker published ports, IPv4/IPv6,
   remote and loopback clients, retransmission, fragmentation, and repeated sleep.
   Specifically retry UDP from the same source address/port across startup: stale
   conntrack/NAT state must not leave a woken server unreachable. Do not silently
   add host conntrack deletion, forwarding rules, or sysctl changes to fix it.
   A failure blocks support for that path and requires revisiting the design.
4. Measure the performance budgets above before building the full feature surface.

Then add failing regression tests before behavior changes:

- Pure controller tests with fake monotonic time: any traffic, incoming-only wake,
  outgoing keepalive, timeout boundaries, minimum runtime, coalescing, fairness,
  stale samples, disabled/held state, and traffic racing shutdown.
- Control/SDK tests: dependency order, readiness, exact IDs, partial failure,
  missing/recreated containers, policy revisions, self-protection, operation
  admission failures, deployment/update conflicts, and interrupted recovery.
- Kernel parser tests: malformed/truncated packets, both IP families, extension
  limits, VLANs, fragments, address overlap, and bounded resource use.
- HTTP/store tests: auth, origin, CSRF, stale writes, atomic holds/admission,
  persistence failure, bounded/redacted output, and group ownership. Preserve
  rooted-path and symlink protections when reading project evidence.
- Colocated frontend tests plus desktop/mobile browser coverage for configuration,
  sleeping status, manual hold/resume, unavailable monitoring, and recovery.

Run affected suites during implementation, followed by repository Go tests, vet,
build, frontend tests/typecheck/build, relevant browser tests, and packaging checks.
Keep privileged Docker/network integration tests isolated from the operator's real
workloads. Mocks do not satisfy the kernel, Docker wakeup, or performance gates.

Delivery order is feasibility and measurement, policy/controller persistence,
coordinated Docker transitions and recovery, app/API integration, then frontend
and deployment documentation. Detailed file-level tasks follow written-spec review.
