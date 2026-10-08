# On Demand Container Groups Implementation Plan

## Delivery status after the portable revision

The portable implementation now includes sleeping TCP/UDP listeners, bounded
activity sampling, exact-ID Docker start/stop, durable policy/hold/recovery state,
coordinated operations, manual-action interlocks, authenticated API, and stack
settings. The concrete API is `/api/v1/stacks/{id}/on-demand` and
`/{group}`, with `/hold` and `/resume` actions. It uses no new dependencies.

Real Docker TCP/UDP activation and same-socket UDP retries, backend race suites,
frontend suites, and desktop/mobile settings journeys have passed. Resource
probes and unresolved release measurements are recorded in
[`docs/on-demand.md`](../../on-demand.md). The historical tasks below preserve
the original plan; their kernel-specific mechanisms were superseded. Physical
NIC, native-host/architecture, real Minecraft, and maximum running-group resource
gates remain unverified and must not be described as release-complete.

> **Backend revised during feasibility:** the user chose portable sleeping-port
> listeners and native running traffic. Follow
> `../specs/2026-10-08-on-demand-portable-revision.md` wherever this original
> checklist mentions eBPF, packet counters, capabilities or exact SYN triggers.
> Task 1's former prototype was measured and superseded; the portable listener
> and pure idle-policy foundation now pass tests. Remaining integration tasks
> below still apply with Docker network-byte sampling replacing packet capture.

> **For agentic workers:** Use superpowers:executing-plans, inline without subagents as required by AGENTS.md. Steps use checkbox syntax for tracking.

**Goal:** Wake opted-in container groups on a configurable packet threshold and stop them after inactivity, without proxying traffic or leaving host network changes.

**Architecture:** `traffic` owns bounded socket-attached eBPF observation. `ondemand` owns policy and lifecycle decisions; `control` admits and executes exact-container operations through the existing coordinator, operation service, and Compose/Docker adapter. SQLite, HTTP, app lifetime, and the stacks frontend retain their existing responsibilities.

**Tech stack:** Go, cilium/ebpf, embedded BPF, Docker SDK, Goose/SQLite, Preact, Vitest, Playwright.

**Spec:** `docs/superpowers/specs/2026-10-08-on-demand-containers-design.md`.

## Global constraints

- User authorized implementation on 2026-10-08 after review and packet-threshold refinement; execute inline. No additional planning approval or subagents.
- Only configured host endpoints; TCP and UDP, IPv4 and IPv6. Any matching incoming packet counts, including SYN/probes.
- Wake threshold 1 by default, range 1–1,000; first-packet counting window 1 second by default, range 10 ms–60 seconds. Trigger immediately at threshold; one pending notification per generation, with recovery after notification loss.
- Idle timeout 10 minutes (1 minute–24 hours), minimum runtime 2 minutes (0–1 hour), startup deadline 5 minutes (30 seconds–15 minutes), per-member stop grace 2 minutes (10 seconds–2 minutes).
- At most 64 groups, eight single-replica services per group, 256 expanded endpoints, 16 MiB kernel resources, and two concurrent group operations across distinct stacks.
- Preserve manual holds, provenance, self-protection, existing stack/repository coordination, alerts, auth/origin/CSRF, redaction, and rooted project loading.
- Linux local rootful Docker. Porty native or host-networked container; targets bridge-networked with fixed published ports. No persistent attachments, privileged fallback, sysctl/firewall/route edits, packet forwarding, or game-specific queries.
- Proposed performance gates: <1% of one idle core, <32 MiB incremental memory, >=98% useful throughput, <=0.2 ms additional p99 RTT/jitter, <=50 ms p99 threshold notification; separate startup timing. Measure, do not infer.
- Before editing existing symbols use GitNexus impact on the matching repository. Before every commit run detect_changes and diff checks. New symbols have no pre-existing callers.

## Review focus

- A single packet followed by silence must still wake after ring overflow: Task 1 kernel pending latch and sweep tests.
- Two observation points must not double-count the same ingress packet: Task 1 loopback/bridge tests and canonical binding.
- A manual stop or changed policy must invalidate a previously queued wake: Tasks 2, 4, 5 generation/revision and coordination tests.
- A stopped UDP target must become reachable when the same client socket retries: Task 1 real Docker/NAT fixture.
- Container replacement, daemon restart, and interrupted stop must never grant permission to mutate an unrelated container: Tasks 3–5 provenance, reconciliation, and exact-ID tests.

## Task 1: Kernel monitor and feasibility gate

**Files:** Create `internal/traffic/{types.go,monitor_linux.go,monitor_test.go,monitor_linux_test.go,generate.go}`, `internal/traffic/bpf/monitor.c`, generated BPF bindings/object, `deploy/traffic_test.sh`, and a disposable compiler/test Dockerfile. Modify `go.mod` and `go.sum`.

**Interfaces:** Produce `Endpoint{Group uint32, Address netip.Addr, Port uint16, Protocol uint8, InterfaceIndex int}`, `Group{ID uint32, Generation uint64, Threshold uint32, Window time.Duration, Armed bool}`, `Activity{Incoming, Outgoing uint64, Pending bool, Generation uint64}`, `Event{Group uint32, Generation uint64}`. `Open([]Endpoint, []Group) (*Monitor,error)`, `(*Monitor).Events() <-chan Event`, `Snapshot() (map[uint32]Activity,error)`, `ConfigureGroup(Group) error`, and idempotent `Close() error`. Configuration is immutable except group state; endpoint changes replace the monitor with a fresh idle baseline.

- [ ] Add validation and packet-fixture tests proving family/protocol/port bounds, deterministic endpoint identity, overlap rejection, and bounded groups. Run `rtk go test ./internal/traffic`; expect missing implementation, then implement validation to pass.
- [ ] Add opt-in kernel tests proving one SYN/UDP packet latches one wake, outgoing-only traffic does not wake, thresholds reset at exact expiry, multi-CPU accounting, malformed frames/VLAN/IPv6/fragment handling, and returning zero leaves gameplay delivery unchanged. Run against empty implementation to observe failures.
- [ ] Implement socket filter and map/ring adapter using cilium/ebpf. Compile in a disposable build container if clang is unavailable locally. Keep generated artifacts reproducible. Do not use unsupported socket-filter spinlocks; verify a bounded atomic threshold-state design or document a correctness-preserving alternative before adopting it.
- [ ] Run kernel tests in isolated network namespaces with only needed capabilities. Test non-root effective capabilities and descriptor cleanup. Run real Docker port-publication fixtures with owned disposable containers only. Run packet/latency/memory measurements; record hardware and limitations in the ledger. An environmental failure must be reported, not hidden as a passing skip.
- [ ] Run focused/race tests and diff/graph checks; commit `feat: add ephemeral kernel traffic monitoring` only after the deliverable is verified. Do not build the UI before resolving a fundamental backend failure.

## Task 2: Group policy and lifecycle decisions

**Files:** Create `internal/ondemand/{types.go,policy.go,policy_test.go,controller.go,controller_test.go}`.

**Interfaces:** Produce `Group` (stable string ID, stack ID, name, members, endpoints, timeouts, threshold/window, revision, enabled, hold/pause), `PolicyUpdate`, `Status`, `ActionRequest{GroupID, Revision, Generation, Action}`, and `Executor.Execute(context.Context,ActionRequest) error`. `Store` provides revisioned group CRUD, list-enabled, hold/pause, intent, and reconciliation persistence. `Controller` consumes `Store`, `Executor`, and the Task 1 monitor through a narrow consumer interface and exposes `Run(context.Context) error`, `Notify()`, and status snapshots. Domain types must not import `control`.

- [ ] Write table-driven tests for all spec defaults/ranges, unique membership and endpoints, no cross-stack targets, any-running-traffic idle refresh, threshold-one wake, stale generations, min-runtime boundaries, mixed state, and two-worker admission/coalescing. Run focused tests to confirm red.
- [ ] Implement pure validation and an injected-clock decision loop, with bounded pending requests and no per-packet jobs. Counter-read freshness is required before sleep; notification loss recovers from the pending map latch. No Docker or HTTP imports.
- [ ] Test cancellation, monitor loss, disabled/held groups, resumed fresh idle windows, and stale queued actions. Run `rtk go test -race ./internal/ondemand`; expect pass. Commit after graph/diff checks.

## Task 3: Durable policy and runtime evidence

**Files:** Create `internal/sqlite/{on_demand_store.go,on_demand_store_test.go,migrations/00007_on_demand.sql}` (choose next available migration number), `internal/compose/{on_demand.go,on_demand_test.go}`. Extend only necessary SDK-facing interfaces.

**Interfaces:** `sqlite.NewOnDemandStore(*sql.DB)` implements Task 2 Store. `compose.SnapshotOnDemand(context.Context,Request,[]string) (OnDemandSnapshot,error)` exposes baseline digest, exact IDs, published endpoints, dependency/readiness evidence, and generation-changing runtime facts. `StartOnDemand(context.Context,OnDemandSnapshot) error` and `StopOnDemand(context.Context,OnDemandSnapshot,time.Duration) error` mutate exact IDs only; checking ownership/state remains mandatory immediately before calls.

- [ ] Add failing SQLite tests for stale writes, membership uniqueness, transactional intent, holds, delete/archive, and recovery without secrets. Implement embedded schema and store in the existing transaction style. Run `rtk go test ./internal/sqlite`.
- [ ] Add SDK-adapter tests for missing/foreign/scaled/recreated targets, dynamic ports, dependency cycles/conditions, outside dependents, ordering, health readiness, and explicit stop grace. Implement using existing Docker/Compose objects; no create/pull/deploy in wake paths. Run `rtk go test ./internal/compose`.
- [ ] Run affected race tests and graph checks; commit the coherent persistence/runtime deliverable.

## Task 4: Coordinated group operations and recovery

**Files:** Create `internal/control/{on_demand.go,on_demand_recovery.go,on_demand_test.go}`; modify control wiring and operation completion types only where needed for atomic persistence.

**Interfaces:** Controller implements `ondemand.Executor.Execute(ctx,request) error`; exposes group eligibility/readiness and reconciliation to the policy service. Calls existing `Coordinator.Try(false,stackID)`, `OperationService.StartTracked`, and Task 3 runtime methods. Lock ownership is handed off exactly once.

- [ ] Add failing tests for self-protection/unknown protection, invalid baseline, changed revision/config/IDs, busy coordinator, failed intent persistence, single tracked operation, partial failures, timeouts, reverse stop order, and traffic during stop.
- [ ] Implement admission under the stack lock, durable intent before Docker mutation, completion publication/alerts, and operation wait/cancellation. Enforce the existing 20-minute operation cap. Never reacquire the same stack lock within an already-owned path.
- [ ] Test startup reconciliation: completed sleep may rearm, interrupted mutations pause, external stops pause, running groups receive full idle windows. Test stale samples cannot authorize stop. Run `rtk go test -race ./internal/control ./internal/operation ./internal/ondemand`; commit after checks.

## Task 5: Manual operations, deployment, and update interlocks

**Files:** Modify `internal/control/{containers.go,control.go,auto_update.go,update_recovery.go}`, `internal/stack/workspace.go`, associated tests, and focused completion helpers where appropriate.

**Interfaces:** A narrow on-demand policy collaborator handles coordinated manual holds and successful/failed mutation invalidation. Existing manual HTTP routes retain their contracts. Durable holds and operation admission must not leave an undocumented side effect on admission failure.

- [ ] Add failing tests: manual stack/member stop holds its whole group; start/restart does not clear hold; explicit resume validates; failed admission leaves policy intact; archived/deleted groups cannot wake; deploy/recreate refreshes IDs or pauses.
- [ ] Add auto-update tests for sleeping-stack ineligibility, applying/recovery exclusion, and evidence invalidation when a group sleeps during unlocked preparation. Implement hooks at control/workspace boundaries, preserving lock order.
- [ ] Test external Docker restart/replacement events are distinguished from owned transitions and cause reconciliation. Run affected packages with race detection; commit after checks.

## Task 6: Application lifetime and authenticated API

**Files:** Modify `internal/app/{app.go,lifecycle.go}` and config; create `internal/app/on_demand.go`, `internal/http/{routes_on_demand.go,on_demand_test.go}`; extend RouterOptions/contracts and HTTP error mapping.

**Interfaces:** `/api/v1/stacks/{id}/on-demand/groups` provides list/create; `/{groupID}` provides revisioned update/delete; `/hold` and `/resume` mutate policy. Status returns availability/state/reasons without secrets. App owns monitor/decision-loop/event-reader cancellation and closes all resources.

- [ ] Write failing API tests for auth, origin, CSRF, malformed/bounded input, stale revisions, wrong stack/group, and audited changes. Follow existing readRoute/mutationRoute contracts.
- [ ] Add lifecycle tests for zero-group teardown, setup readiness, startup failure cleanup, monitor loss, event reconnection, and shutdown during an operation. Implement one shared Docker event subscription with bounded reconciliation; no per-group daemon polling loop.
- [ ] Run `rtk go test -race ./internal/app ./internal/http ./internal/config` and `rtk go test ./...`; commit after checks.

## Task 7: Stack settings and observable state

**Files:** Create focused `web/src/features/stacks/OnDemandSettings.tsx`, `useOnDemand.ts`, colocated tests/styles; extend feature API/types, StackSettings and status presentation; add `web/e2e/on-demand.spec.ts`.

**Interfaces:** Feature hook owns API loading/mutations and status; presentation receives typed values and actions. No global state, backend concerns in UI copy, or new frontend dependencies.

- [ ] Add failing component tests for default single-packet wake, conditional counting-window input, service selection, stale-edit errors, sleeping/held/unavailable/recovery status, and hold/resume.
- [ ] Implement accessible settings using existing form/feedback components. Preserve stopped runtime truth while explaining intentional sleep. Reuse operation and alert navigation.
- [ ] Run frontend tests/typecheck/build and desktop/mobile browser tests with screenshots. Commit after graph and diff checks.

## Task 8: Packaging and release verification

**Files:** Modify `Dockerfile`, `deploy/systemd/porty.service`, packaging fixtures, operator/development documentation, and the spec's evidence status.

- [ ] Package the validated minimum capabilities, seccomp/address-family allowances, non-root behavior, explicit host-network UI binding, and reproducible BPF build. Keep ordinary management functional without monitor permissions.
- [ ] Execute cleanup, UDP same-tuple retries, Minecraft smoke, supported kernel/architecture, and performance tests from Task 1. Record exact results and unavailable matrix entries; do not claim release readiness without required evidence.
- [ ] Run `rtk go test ./...`, `rtk go vet ./...`, `rtk go build ./cmd/porty`; frontend test/typecheck/build and affected browser tests; `rtk ./deploy/package_test.sh`. Verify generated artifacts and formatting.
- [ ] Review the entire branch inline (user forbids subagents), fix important findings with red/green tests, run detect_changes against base, and commit scoped changes. Leave the feature branch for review without merging or publishing.
