# Host Dashboard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` to implement this plan task by task. Repository instructions prohibit subagents, including review subagents. Steps use checkbox syntax for tracking. Read the approved spec before execution.

**Goal:** Make Porty's landing page an accurate host-health dashboard with eight metric cards and five minutes of live history, on native Linux and Docker installations.

**Architecture:** One application-owned monitoring service collects and retains host samples independently of HTTP clients and repository state. An authenticated cached-read endpoint supplies a feature-local Preact polling hook and compact SVG charts. Vendor adapters provide capability-based GPU readings without shelling out to monitoring commands.

**Tech Stack:** Go, gopsutil v4, official NVIDIA go-nvml, existing `golang.org/x/sys/unix`, Preact/TypeScript, Vitest/Testing Library and Playwright. A cgo/glibc OCI image supports NVML; a non-cgo build remains supported without NVIDIA readings.

**Spec:** [Approved host dashboard design](../specs/2026-10-09-host-dashboard-design.md).

## Global Constraints

- Sample every two seconds; retain up to 151 samples per series and no more than five minutes. Six seconds without a fresh reading is stale. No metric persistence.
- Cap discovery at 256 logical CPUs, 64 interfaces, 64 block devices, 64 filesystems, 128 temperature sensors, and 16 GPUs with 16 engines each. Report omissions and partial coverage.
- Stored monitoring data is bounded to 64 MiB; responses to 8 MiB. Evict oldest history first and report the actual retained window. No partial JSON.
- Initial and reset counters are collecting, never zero. Device replacement, counter rollback and sampling interruptions over six seconds reset rate baselines. Use monotonic elapsed time.
- One in-flight read per source and one browser request in flight. Browser timeout is five seconds. Hidden/unmounted dashboards stop polling; collection continues.
- Host mode never falls back to container paths. Basic monitoring requires no privileged mode, host PID namespace, or host networking. Read-only host mounts must include read-only submounts.
- Preserve auth, session refresh, no-store responses, origin/CSRF guards, rooted/symlink checks, non-root packaging, secret redaction and existing lock ordering. No application URL fetching in this phase.
- Dashboard is `#/`; inventory is `#/stacks`. Existing stack/container deep links remain valid. The dashboard works after sign-in even before repository setup.
- Use existing UI tokens; local feature state and API modules; SVG charts without a new chart dependency. No placeholder app-health section in production.
- Use `rtk` for shell commands, `gofmt` for changed Go files, and Prettier for changed frontend/documentation files. Do not commit runtime data or temporary screenshots.
- Execute inline, without subagents. Establish an isolated checkout with `superpowers:using-git-worktrees` before implementation. Do not implement before the user reviews this plan.
- Before editing an existing symbol, run GitNexus upstream impact in the execution checkout; warn on HIGH/CRITICAL risk, investigate UNKNOWN. Before each scoped commit, run complete `detect_changes(scope: all)` and inspect staged files. Partial/truncated change analysis must be resolved.

## Review Focus

1. Repository setup is pending or fails while the user opens Dashboard: host readings and sign-out remain usable; stack mutations remain guarded — Tasks 6 and 9.
2. A browser request times out during a shared session refresh, or completes after logout: do not cancel another caller's refresh, replay the cancelled request, or restore logged-out state — Task 7.
3. Interface/disk names are reused after unplug/replug: do not join unrelated counters/history or silently retain a stale selected device — Tasks 2, 3 and 8.
4. Wall time jumps or a driver/filesystem read blocks forever: bounded memory/workers, accurate monotonic rates, chart gaps and bounded application shutdown — Tasks 1, 3 and 6.
5. Long or hostile mount/sensor labels, many devices, and 320-pixel screens: escape content, report truncation, keep all controls reachable and charts readable — Tasks 1, 3, 8 and 11.

## Starting point and impact

Planning baseline: `4b5788a` in `/home/msoldin/porty`. The approved spec is the only change from the earlier product baseline. No monitoring package or dashboard feature exists yet.

GitNexus 1.6.12 refreshed successfully at this baseline: 5,656 nodes, 20,247 edges and 478 reported flows. It warns that process extraction is budget-limited and cross-language field references are incomplete. MCP disconnected after indexing; CLI impact queries succeeded. Treat graph results as navigation evidence and rerun impact before edits.

- `api` in `web/src/lib/http.ts`: **CRITICAL**, 51 direct callers and 137 impacted symbols. Its consumers include stack/editor/deployment, repository, alerts and account flows. Extend cancellation compatibly; run the full frontend suite after that task.
- `useWorkspaceData`: LOW in the graph, called by `Workspace`, with `App` upstream. It currently always refreshes repository-dependent resources and opens the operation stream.
- `internal/app.New`: LOW in the graph, called by `cmd/porty.main`; starts existing background services and owns dependency construction. Its tests and `Application.Shutdown` remain essential beyond that summary.
- `readRoute` adds `requireRepositoryReady`; use `authenticatedRoute` for monitoring. `App` currently blocks the entire workspace on repository loading/setup/errors. `Workspace` also checks stack loading before most destinations.
- Existing stack inventory is `web/src/features/stacks/Dashboard.tsx`. Sidebar, stack breadcrumb and container breadcrumb currently link inventory to `/`. The HTTP client retries authentication but accepts no abort signal.

## File responsibilities and shared contract

All new production monitoring files belong in `internal/monitoring`; tests are colocated. Use `model.go` for domain/wire types, `history.go` for retention/cursors, `service.go` for scheduling, `sources_linux.go` for root/discovery ownership, `host_linux.go` for CPU/RAM/network, `storage_linux.go` for disk/filesystem data, `sensors_linux.go` for temperatures, and focused GPU files. Do not create a general metrics/plugin framework.

Frontend ownership is `web/src/features/dashboard`: `types.ts`, `api.ts`, `useMonitoring.ts`, `monitoringState.ts`, `Dashboard.tsx`, `MetricCard.tsx`, `MetricChart.tsx`, `metricPresentation.ts`, and `dashboard.css`, with tests beside behavior. App files own routing and readiness; existing shared HTTP infrastructure continues to own auth refresh.

The following names and semantics are decisions shared between tasks. JSON uses lower camel case. `State`, `MetricKind`, `DeviceKind`, `Unit`, and reason codes are closed enums; never accept arbitrary metric keys from a client.

```go
type State string // collecting | available | unavailable | stale
type Reading struct {
    Value *float64
    State State
    Reason string // fixed sanitized code, never raw driver errors
    SampledAt time.Time
    LastSuccessAt *time.Time
}
type Series struct { ID, DeviceID string; Metric MetricKind; Unit Unit }
type Device struct {
    ID string; Kind DeviceKind; Name, Driver string
    MountPaths []string
    Default bool
    UtilizationBasis string // vendor | busiest_engine; empty if inapplicable
}
type Coverage struct { Source string; Omitted int; Partial bool; Reason string }
type Inventory struct { Revision string; Devices []Device; Series []Series }
type HostInfo struct { Name, OS string; BootTime *time.Time }
type Sample struct {
    Sequence string // decimal uint64, not a JSON number
    CapturedAt time.Time
    Readings map[string]Reading // keyed by Series.ID
}
type Snapshot struct {
    Generation, Cursor string; Reset bool
    ServerTime, WindowStart time.Time
    Host HostInfo; Inventory *Inventory
    Current Sample; Samples []Sample; Coverage []Coverage
}
type Batch struct {
    Devices []Device; Series []Series
    Readings map[string]Reading; Coverage []Coverage
}
type Source interface {
    ID() string
    Collect(context.Context) (Batch, error)
    Close() error
}
```

`MetricKind` covers CPU busy, memory total/available/used/percent, swap total/used, temperature, GPU busy/memory total/used, engine busy, network receive/send rate, disk read/write rate, and filesystem total/used/available/percent. Units are `percent`, `bytes`, `bytes_per_second`, and `celsius`. Device kinds are host, CPU, memory, interface, block device, filesystem, sensor, GPU and GPU engine. Use opaque stable IDs and explicit metadata, not parsing display names in the frontend.

One randomly generated collector generation identifies a Porty run. A bounded base64url cursor contains generation, decimal sequence and inventory revision; maximum length 256 bytes. Invalid encoding/fields are `ErrInvalidCursor`; previous generation, evicted sequence, or future sequence produce a reset. Reset responses always include inventory. Incremental responses include inventory only when its revision differs.

Clients retain at most 151 samples and five minutes, merge by generation/sequence, and never interpret a retained stale value as a new chart point. `Current` supports last-value display; charts accept only fresh available readings with distinct `SampledAt`. Wire timestamps are UTC RFC3339Nano; backend age/rate accounting retains monotonic time privately.

## Task 1: Bounded collection service and history

**Files:** Create `internal/monitoring/{model.go,history.go,service.go,history_test.go,service_test.go}`.

**Interfaces:** Define the shared types above. Produce `NewService(options ServiceOptions) (*Service, error)`, `(*Service).Run(ctx context.Context)`, `(*Service).Snapshot(cursor string) (Snapshot, error)`, and `(*Service).Shutdown(ctx context.Context) error`. `ServiceOptions` contains `Host HostInfo`, `Sources []Source`, `Discover func(context.Context) ([]Source, error)`, and `Now func() time.Time`. `Discover` is optional, runs at startup and every ten seconds, and returns a bounded complete source set; matching source IDs retain their current instance/baselines. Sources own monotonically timed readings; the coordinator owns sequence and sample history.

- [ ] Add fake-clock/fake-source tests: `TestHistoryExpiresSamplesAfterFiveMinutes`, `TestHistoryResetsForOldGeneration`, `TestHistoryRejectsMalformedCursor`, `TestServiceKeepsOtherSourcesFreshUnderBlockedRead`, `TestServiceStopsWithinShutdownDeadline`, and `TestHistoryReportsReducedWindowUnderByteLimit`. Assert that 152 two-second samples retain at most 151, unchanged inventory is omitted on incremental reads, and overflow above JavaScript's safe integer still round-trips as a string.
  ```go
  if len(got.Samples) > 151 { t.Fatal("history exceeded sample limit") }
  if got.Samples[0].Sequence != "9007199254740993" { t.Fatal("sequence lost precision") }
  if !got.Reset || got.Inventory == nil { t.Fatal("reset omitted inventory") }
  ```
- [ ] Run `rtk go test ./internal/monitoring -count=1`; confirm the new contracts/behavior fail before implementation.
- [ ] Implement a mutex-protected bounded history and one two-second coordinator. Sources have a single worker/in-flight slot; the coordinator never waits for blocked I/O. Store the observation timestamp, distinguish a carried current value from a fresh chart point, and reject results from retired source instances. Discovery also has one in-flight slot. Do not create a goroutine per skipped tick.
- [ ] Enforce source/device/series bounds before accepting batches. Define the four `StateCollecting`, `StateAvailable`, `StateUnavailable`, and `StateStale` constants. Bound labels to 256 Unicode code points, IDs to 128 bytes, total series to 4,096, and alternate mount paths to 16 per filesystem; disclose omissions and sanitize invalid text. `LinuxSources` caches source instances by identity across discoveries; do not leak discarded duplicate readers. Retire identities without closing resources during an in-flight call; close after it exits. Shutdown stops admission, cancels contexts and waits only to the supplied deadline. Do not claim cancellation can interrupt a native driver call.
- [ ] Store history in an explicitly accounted bounded representation, including metadata/strings and retained sample capacity. Evict oldest history before crossing 64 MiB. Preserve current metadata/readings under the fixed discovery caps. `Snapshot` bounds encoded JSON to 8 MiB by dropping oldest samples and updating `WindowStart`; an oversized bounded-current representation is an internal invariant failure, never malformed JSON.
- [ ] Run `rtk go test -race ./internal/monitoring -count=1`. Add/assert backward/forward wall-clock jump, six-second staleness, late completion and repeated discovery failure cases. Format, run change analysis, and commit `feat: add bounded host monitoring service`.

## Task 2: Explicit host sources, CPU, RAM and network

**Files:** Create `internal/monitoring/{sources_linux.go,sources_linux_test.go,host_linux.go,host_linux_test.go}` and fixture trees under `internal/monitoring/testdata/`. Modify `internal/config/{config.go,config_test.go}`, `go.mod`, and `go.sum`.

**Interfaces:** Add `config.Monitoring { Mode, HostProc, HostSys, HostRoot string }` and `Config.Monitoring`. Produce `OpenLinuxSources(options LinuxOptions) (*LinuxSources, error)`, `(*LinuxSources).Discover(ctx context.Context) ([]Source, error)`, `(*LinuxSources).Host(ctx context.Context) HostInfo`, and `(*LinuxSources).Close() error`. `LinuxOptions` has the same four fields as the config boundary; the application maps them explicitly. Host-file adapters produce `Source` batches from Task 1. Use `counterRate(previous, current uint64, elapsed time.Duration) (float64, bool)` for monotonic counter rates.

- [ ] Impact `Config`, `Default`, and `Load`. Add `TestMonitoringConfigUsesNativeDefaults`, `TestMonitoringConfigAppliesEnvironmentOverrides`, `TestHostModeNeverUsesContainerFallback`, `TestCPUBusyExcludesIOWaitAndGuestDoubleCount`, `TestMemoryUsesAvailableCapacity`, `TestNetworkRateUsesElapsedTime`, and `TestInterfaceReplacementResetsRate`. Fixtures deliberately give `/proc/net/dev`, `/proc/self/net/dev` and host `/proc/1/net/dev` different values.
  ```go
  rate, ok := counterRate(100, 500, 2*time.Second)
  if !ok || rate != 200 { t.Fatal("incorrect byte rate") }
  _, ok = counterRate(500, 100, 2*time.Second)
  if ok { t.Fatal("counter rollback must establish a new baseline") }
  ```
- [ ] Run `rtk go test ./internal/config ./internal/monitoring -count=1`; confirm the new tests fail.
- [ ] Add a pinned stable gopsutil v4 dependency after checking the selected version's Go floor and Linux imports. Configure paths through `common.EnvMap` in context. Do not use global gopsutil percent/rate caches. Read raw cumulative counters and calculate deltas with per-source baselines; elapsed over six seconds or a missing baseline produces collecting.
- [ ] Implement CPU normalization without guest double-counting and RAM as total minus available; clamp only small rounding error, treat impossible source values as unavailable. Select the default interface using the host's preferred IPv4/IPv6 default route, with deterministic active/non-loopback fallback. Count only that interface in the main network cards. Interface IDs include network namespace identity, interface index and a presence epoch so name/index reuse after removal resets history.
- [ ] Add `monitoring.mode`, `host_proc`, `host_sys`, `host_root` YAML fields with `PORTY_MONITORING_MODE`, `PORTY_MONITORING_HOST_PROC`, `PORTY_MONITORING_HOST_SYS`, `PORTY_MONITORING_HOST_ROOT`. Defaults: native and `/host/proc`, `/host/sys`, `/host/root`; accept only native/host/disabled and absolute nonempty host paths. Bad config fails validation; inaccessible configured sources return availability results without preventing Porty startup.
- [ ] Open roots once; use rooted descriptor operations for kernel-derived paths. Apply discovery caps while iterating, not after an unbounded bulk read. Cap individual scalar files to 4 KiB and table files to 4 MiB; report incomplete reads as partial/unavailable, never parse truncated input as complete. Use small rooted adapters where a library bulk API cannot enforce these limits. Host networking/mount views explicitly use `1/net/*` and `1/mountinfo`. Establish host boot/namespace identity from host sources. Missing host identity is not silently replaced with container hostname/boot metadata. No global environment mutation or shell commands.
- [ ] Run `rtk go test -race ./internal/monitoring ./internal/config -count=1`; cover IPv6-only hosts, missing default routes, permission failures, six-second counter gaps, and hotplug. Format, analyze changes, and commit `feat: collect host cpu memory and network metrics`.

## Task 3: Storage, filesystem capacity and temperature

**Files:** Create `internal/monitoring/{storage_linux.go,storage_linux_test.go,sensors_linux.go,sensors_linux_test.go}`; extend `sources_linux.go` and fixture trees.

**Interfaces:** `LinuxSources.Discover` adds block, per-filesystem and sensor sources using Task 1's `Source` contract. Produce `filesystemUsage(totalBlocks, freeBlocks, availableBlocks, blockSize uint64) (total, used, available uint64, percent float64, err error)` and stable filesystem/device IDs. Sources emit the exact storage/temperature `MetricKind` values already defined.

- [ ] Add `TestDiskAggregateCountsPhysicalLeavesOnce`, `TestFilesystemUsagePreservesReservedCapacity`, `TestFilesystemSourceRejectsRootEscape`, `TestFilesystemRejectsUnpropagatedHostMount`, `TestFilesystemSourceDoesNotBlockOtherMetrics`, `TestSensorSelectionDoesNotRelabelGPUAsCPU`, and `TestStorageDiscoveryReportsOmittedDevices`. A fixture contains a whole disk, its partition, an LVM/RAID layer, duplicate binds, tmpfs, overlay and one network filesystem.
  ```go
  total, used, available, percent, err := filesystemUsage(100, 30, 20, 4096)
  if err != nil || total != 409600 || used != 286720 || available != 81920 || percent != 70 {
      t.Fatal("reserved blocks must not be reported as used or available")
  }
  ```
- [ ] Run `rtk go test ./internal/monitoring -run 'TestDisk|TestFilesystem|TestSensor|TestStorage' -count=1`; confirm failure.
- [ ] Read block counters and sysfs relationships, sum eligible physical leaves once, and disclose partial/unsupported aggregation. Decode escaped mountinfo paths, identify real filesystems, preserve alternate mount paths, and deduplicate capacity by filesystem identity. Root path resolution under `HostRoot` and call `Fstatfs` on validated open descriptors to avoid check/use path races. Compare the opened filesystem's device/identity to current host mountinfo: a host mount not propagated into the container must be unavailable, not measured as its parent filesystem. Give each filesystem one independent source slot so a stalled NFS read cannot exhaust other metric families.
- [ ] Read bounded hwmon/thermal sensors, resolve only in-root sysfs links, convert millidegrees to Celsius and preserve negative legitimate temperatures. CPU package sensors are the default; do not guess CPU from an arbitrary sensor index. Reject NaN/Inf, overflow, nonsensical capacities and malformed values. Device disappearance/reappearance advances its presence identity. GPU sensor series can later reference the GPU device without duplicating host sensors.
- [ ] Run `rtk go test -race ./internal/monitoring -count=1`; include in-root versus escaping symlinks, `/../` traversal, replacement between discovery/open, spaces/newlines in labels, reserved blocks, zero capacity, all aggregation inputs unavailable, and no CPU temperature sensor. Format, analyze and commit `feat: collect storage and temperature metrics`.

## Task 4: AMD and NVIDIA GPU adapters

**Files:** Create `internal/monitoring/{gpu_linux.go,gpu_linux_test.go,gpu_amd_linux.go,gpu_amd_linux_test.go,gpu_nvidia_linux.go,gpu_nvidia_linux_test.go,gpu_nvidia_nocgo_linux.go}`; extend `sources_linux.go`, fixtures, `go.mod` and `go.sum`.

**Interfaces:** GPU discovery is part of `LinuxSources.Discover`. Define a narrow adapter-local `gpuDeviceReader` with `Read(ctx context.Context) (Batch, error)` and `Close() error`; its wrapper satisfies `Source`. `openNVIDIA(ctx context.Context) ([]Source, error)` has cgo and non-cgo implementations. A small unexported NVML interface contains only the calls actually used and supports test fakes.

- [ ] Add `TestAMDGPUReportsAvailableFieldsIndependently`, `TestNVIDIAUnavailableLibraryDoesNotPreventCollection`, `TestNVIDIAUnsupportedFieldPreservesUtilization`, `TestNVIDIACallsDoNotOverlap`, and `TestGPUReplacementDoesNotReuseHistory`. Assert absent temperature is unavailable while utilization remains available, shared memory never appears as dedicated VRAM, and labels/IDs use discovered device identity.
  ```go
  if got.Readings[busyID].State != StateAvailable || got.Readings[tempID].State != StateUnavailable {
      t.Fatal("one unsupported field must not hide other GPU metrics")
  }
  ```
- [ ] Run `rtk go test ./internal/monitoring -run 'TestAMD|TestNVIDIA|TestGPU' -count=1`; confirm failure.
- [ ] Implement AMD sysfs readers for `gpu_busy_percent`, supported VRAM capacity/use and hwmon temperatures. Pin official `github.com/NVIDIA/go-nvml`; initialize/load at runtime, enumerate bounded devices, and read utilization/memory/temperature. Match vendor IDs to sysfs identities where available; sanitize errors into fixed reasons.
- [ ] Put NVML imports behind `linux && cgo`; provide `linux && !cgo` sources with a `build_unsupported` reason. Do not include generated SDK code or shell out. Use one shared NVML session owned by `LinuxSources`; per-device retirement does not shut down the whole library. Retain one native call per source, and close the session only after its in-flight calls have returned; application shutdown still respects its deadline. Unsupported virtual/partition accounting is explicit partial/unavailable coverage.
- [ ] Run `rtk go test -race ./internal/monitoring -count=1`, `rtk env CGO_ENABLED=0 go test ./internal/monitoring -count=1`, and `rtk env CGO_ENABLED=0 go build -o /tmp/porty-dashboard-nocgo ./cmd/porty`. Record dependency/binary size change. Format, analyze and commit `feat: add amd and nvidia gpu monitoring`.

## Task 5: Intel GPU engine utilization

**Files:** Create `internal/monitoring/{gpu_intel_linux.go,gpu_intel_linux_test.go}` and i915/Xe fixtures; extend `gpu_linux.go` and `sources_linux.go`.

**Interfaces:** Intel sources reuse Task 4's reader and Task 1's batch contract. Define adapter-local `perfReader` with `Read() (value, enabled, running uint64, err error)` and `Close() error`; `openPerfCounter(typeID uint32, config uint64) (perfReader, error)` uses `unix.PerfEventOpen`. Produce `intelEngineUsage(activeDelta, totalDelta uint64) (float64, bool)` for Xe; i915 uses busy-nanosecond deltas and actual elapsed time with perf multiplexing accounting.

- [ ] Add `TestIntelI915UsageUsesEngineBusyCounters`, `TestIntelXeUsageUsesActiveAndTotalTicks`, `TestIntelDeniedPerfReportsUnavailable`, `TestIntelPartialCoverageDoesNotClaimWholeGPUUsage`, and `TestIntelCounterResetReturnsCollecting`. Feed different event encodings, absent engines and multiplexed counters.
  ```go
  percent, ok := intelEngineUsage(25, 100)
  if !ok || percent != 25 { t.Fatal("incorrect Xe engine utilization") }
  if _, ok := intelEngineUsage(25, 0); ok { t.Fatal("zero denominator is not zero usage") }
  ```
- [ ] Run `rtk go test ./internal/monitoring -run TestIntel -count=1`; confirm failure.
- [ ] Discover supported PMU types/events/formats from sysfs. For Xe, enumerate engines with the documented DRM device-query ABI and read active/total tick pairs; for i915 read engine-busy events. Use bounded syscall structures checked against current kernel headers. Do not hardcode one vendor encoding for both drivers, scan processes, infer usage from frequency, change kernel settings or add broad capabilities automatically.
- [ ] Emit per-engine readings and the maximum measured engine as the main GPU percentage with `UtilizationBasis: "busiest_engine"`. Unknown/denied engines set partial coverage. Validate device-node type/major/minor against the discovered DRM device before opening. Bound all event descriptors to the per-GPU engine limit and close them after retirement.
- [ ] Run `rtk go test -race ./internal/monitoring -count=1`; add unavailable PMU, bad event format, device removal, zero running time, repeated counter resets, and descriptor-close tests. Record i915/Xe hardware checks separately from fixture results. Format, analyze and commit `feat: add intel gpu engine monitoring`.

## Task 6: Application lifecycle and authenticated monitoring endpoint

**Files:** Create `internal/app/monitoring.go`, `internal/http/{routes_monitoring.go,monitoring_test.go}`; modify `internal/app/{app.go,lifecycle.go,lifecycle_test.go,app_test.go}` and `internal/http/{auth.go,api.go}`.

**Interfaces:** Add `MonitoringAPI interface { Snapshot(cursor string) (monitoring.Snapshot, error) }` and `RouterOptions.Monitoring`. Produce `registerMonitoringRoutes(mux *http.ServeMux, options RouterOptions)` and `(*Application).startMonitoring(ctx context.Context, cfg config.Monitoring) error`. The application stores the service and source-root owner for orderly shutdown. Disabled mode returns a structured disabled state without collecting.

- [ ] Impact `New`, `Application.Shutdown`, `RouterOptions`, and `registerAPIRoutes`. Add `TestMonitoringRequiresAuthentication`, `TestMonitoringWorksBeforeRepositorySetup`, `TestMonitoringReadsCachedSamplesWithoutCollection`, `TestMonitoringResponseIsNoStoreAndBounded`, `TestMonitoringRejectsMalformedCursor`, and `TestShutdownStopsMonitoringWithinDeadline`.
  ```go
  if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
      t.Fatal("authenticated monitoring must work without a ready repository")
  }
  if source.calls.Load() != callsBeforeRequests { t.Fatal("HTTP requests triggered collection") }
  ```
- [ ] Run `rtk go test ./internal/app ./internal/http -run 'TestMonitoring|TestShutdown' -count=1`; confirm failure.
- [ ] Wire one service outside repository-ready construction. Register `GET /api/v1/monitoring` through `authenticatedRoute`, not `readRoute`; accept only the bounded cursor. Map invalid cursor to 400 and auth failure to existing 401 behavior. Preserve partial/unavailable data as HTTP 200. Serialize a pre-bounded coherent snapshot, with JSON content type and no-store, including error responses.
- [ ] Start monitoring only after construction succeeds; close on constructor failure. Cancel collection early in shutdown and join it with the application deadline. Preserve operation drain-before-Docker-close behavior. Missing hardware/mounts must not change `/readyz` or prevent otherwise valid startup.
- [ ] Run `rtk go test -race ./internal/monitoring ./internal/config ./internal/app ./internal/http -count=1`; verify multiple clients, unauthenticated and expired sessions, repository failure, disabled monitoring and partial source failures. Run existing origin/CSRF tests unchanged. Format, analyze and commit `feat: expose authenticated cached host monitoring`.

## Task 7: Cancellable polling and client history

**Files:** Create `web/src/features/dashboard/{types.ts,api.ts,monitoringState.ts,monitoringState.test.ts,useMonitoring.ts,useMonitoring.test.tsx}`; modify `web/src/lib/{http.ts,http.test.ts}`.

**Interfaces:** Mirror Task 1's JSON schema exactly in `types.ts`. Extend `api<T>(path, method?, body?, headers?, signal?: AbortSignal): Promise<T>` and the internal `apiResponse` with an optional final signal. Produce `getMonitoring(cursor: string | undefined, signal: AbortSignal): Promise<Snapshot>`, `mergeMonitoring(previous: MonitoringState | undefined, response: Snapshot): MonitoringState`, and `useMonitoring(onUnauthorized: () => void): { state?: MonitoringState; loading: boolean; error: string; stale: boolean; retry: () => void }`. `MonitoringState` contains generation, cursor, inventory, current, samples, coverage, host, serverTime and windowStart from the response/merge.

- [ ] Re-run and report the **CRITICAL** impact on `api`; inspect `apiResponse` callers. Add `it("does not replay a cancelled read after shared session refresh")`, `it("keeps other callers refreshing when one read aborts")`, `it("resets history when Porty restarts")`, `it("pauses requests while hidden and catches up on return")`, and `it("ignores a response after sign-out or unmount")`.
  ```ts
  expect(fetchMock).toHaveBeenCalledTimes(1); // while the first metrics read is unresolved
  expect(merged.samples).toHaveLength(151);
  expect(merged.generation).toBe(restarted.generation);
  expect(onUnauthorized).not.toHaveBeenCalled(); // cancellation is not logout
  ```
- [ ] Run `rtk bun run test -- src/lib/http.test.ts src/features/dashboard` in `web/`; confirm new assertions fail.
- [ ] Pass the optional signal to request fetches and check it before auth retry. Await a shared refresh abortably for that caller without cancelling `refreshSession` globally. Use an abort listener with cleanup so the five-second deadline also bounds time waiting for refresh; preserve existing no-signal behavior and error types.
- [ ] Implement a single-flight visible-tab poll loop with a five-second abort timer, two-second schedule, immediate resume/retry, and cleanup on unmount. Follow existing refresh then unauthorized/logout handling; ignore completions from cancelled or previous hook generations. A local one-second freshness timer updates stale presentation even when requests hang.
- [ ] Merge exact string sequences using `BigInt` comparisons, clear on generation/reset, prune samples to five minutes/151, and use the server-time offset plus monotonic browser elapsed time for freshness. Keep null/gap semantics and unchanged inventory correctly. Do not read API data in presentation components.
- [ ] Run the focused tests, `rtk bun run test` and `rtk bun run typecheck` in `web/`. Confirm existing writes/CSRF, session refresh deduplication and `apiText` behavior. Format, analyze and commit `feat: add cancellable dashboard polling and history`.

## Task 8: Layout A cards and accessible charts

**Files:** Create `web/src/features/dashboard/{Dashboard.tsx,Dashboard.test.tsx,MetricCard.tsx,MetricChart.tsx,MetricChart.test.tsx,metricPresentation.ts,metricPresentation.test.ts,dashboard.css}`.

**Interfaces:** `Dashboard({ onUnauthorized }: { onUnauthorized: () => void })` consumes `useMonitoring`. `MetricChart({ series, unit }: { series: ChartSeries[]; unit: Unit })` is presentational; `ChartSeries` has a label and timestamped nullable values. `MetricCard` receives its label, formatted value, source, state, optional chart and disclosure content. `metricPresentation.ts` owns unit formatting, primary-selection fallback and plotting conversion; no API calls in these components.

- [ ] Add `it("shows all eight host metrics with their sources")`, `it("uses the same selected interface for upload and download")`, `it("does not hide a full filesystem in an average")`, `it("marks unavailable and stale values instead of zero")`, and `it("makes chart samples available with keyboard and touch")`. Add selector fallback after device replacement and browser-storage denial cases.
  ```ts
  expect(screen.getByText("Unavailable")).toBeVisible();
  expect(screen.getByText("CPU package")).toBeVisible();
  expect(
    screen.getByRole("button", { name: /temperature details/i }),
  ).toHaveAttribute("aria-expanded", "false");
  expect(screen.queryByText(/application shortcuts/i)).not.toBeInTheDocument();
  ```
- [ ] Run `rtk bun run test -- src/features/dashboard` in `web/`; confirm failure.
- [ ] Implement the selected four-by-two desktop card order, two-column intermediate layout and one-column narrow layout using existing tokens. Use native disclosure semantics or the repository's equivalent accessible controls. Disk capacity remains per filesystem, growing vertically; source and omission/partial labels remain visible. Render no production future-app placeholder.
- [ ] Add feature-local selectors persisted under a versioned origin-scoped storage key. Default from backend device metadata; Download and Upload share one ID. Explicitly announce removed selection fallback. Storage errors leave functional in-memory selections. Temperature fallback never relabels another device as CPU; Intel labels busiest-engine basis and shared memory correctly.
- [ ] Draw bounded SVG segments separated at missing/reset/device-change samples. Percentage scale is 0–100; rates start at zero; temperatures show their range. Read/write lines have labels and distinct styling beyond color. Provide focus/tap/hover sample inspection, numeric alternatives, reduced motion, and no constantly announced live-region chatter. Use proper text nodes for hostile labels.
- [ ] Run dashboard tests, `rtk bun run typecheck`, and `rtk bun run build` in `web/`. Check 320/390/768/1536 widths, light/dark themes, long labels and expanded details in the browser during Task 11. Format, analyze and commit `feat: render compact host monitoring dashboard`.

## Task 9: Landing route and repository-independent workspace

**Files:** Modify `web/src/app/{App.tsx,App.test.tsx,Workspace.tsx,WorkspaceShell.tsx,WorkspaceShell.test.tsx,useWorkspaceData.ts,useWorkspaceData.test.tsx,routes.ts,routes.test.ts}`, `web/src/features/operations/useOperationStream.ts`, `web/src/hooks/{useTopicStream.ts,useTopicStream.test.ts}`, `web/src/features/repository/RepositorySetup.tsx` and its colocated tests, `web/src/features/stacks/{StackDetail.tsx,ContainerDetail.tsx}` and affected tests. Rename `web/src/features/stacks/{Dashboard.tsx,Dashboard.test.tsx}` to `{StackInventory.tsx,StackInventory.test.tsx}` through GitNexus. Add a Dashboard entry to `web/src/components/Icon.tsx` if absent.

**Interfaces:** `useWorkspaceData(logout: () => void, enabled = true)` preserves existing return shape but suppresses repository-dependent refreshes/operation stream while disabled. Extend `useOperationStream(onOperation: (operation: Operation) => void, refresh: () => void, enabled = true)` and `useTopicStream(topic: string, onEvent: (event: TopicEvent) => void, refresh: () => void, enabled = true)` compatibly. `Workspace` accepts `repositoryStatus: RepositorySetupStatus | null`, `repositoryLoading: boolean`, `repositoryError: string`, and `retryRepository: () => void` in addition to its existing session/change/logout props. Define `requiresRepository(route: string): boolean` in `routes.ts`.

- [ ] Impact App, Workspace, Dashboard/StackInventory, WorkspaceShell, useWorkspaceData, useTopicStream and navigation callers. Add `it("opens host metrics before repository setup")`, `it("keeps Dashboard usable when repository loading fails")`, `it("keeps stack actions behind repository readiness")`, `it("returns from a container to Stacks")`, and `it("suppresses repository refreshes until setup completes")`.
  ```ts
  expect(screen.getByRole("link", { name: "Dashboard" })).toHaveAttribute(
    "href",
    "#/",
  );
  expect(screen.getByRole("link", { name: "Stacks" })).toHaveAttribute(
    "href",
    "#/stacks",
  );
  expect(screen.getByRole("heading", { name: "Dashboard" })).toBeVisible();
  expect(screen.queryByText("Loading stacks…")).not.toBeInTheDocument();
  ```
- [ ] Run the affected app/route/stack tests and observe new failures before edits.
- [ ] Move only the repository gate out of App; retain registration/auth failures as global auth flow. Allow Dashboard, account settings and existing repository-independent Alerts without repository readiness. Gate inventory/details, repository, operations and audit destinations as their backend contracts require. Show the existing RepositorySetup workflow inside the shared shell for gated destinations; change its root `main` elements to sections so there is exactly one main landmark. Repository errors appear there with a retry action, not over host metrics.
- [ ] Add Dashboard at `/`, move inventory to `/stacks`, update symbol-aware imports, navigation highlighting, stack/container breadcrumbs and the unknown-stack "Back to stacks" action. Keep the brand home link and all existing deep-link formats. Render Dashboard before workspace resource loading/error branches; keep stack-specific global notices off Dashboard. Do not duplicate the shell or instantiate hooks conditionally.
- [ ] Make disabled workspace refresh/stream paths no-ops and ignore late disabled-generation results. Re-enable after setup and retain existing resource refresh behavior for ready installations. Scope repository fetch failures separately in App; an authenticated dashboard must render while repository status is still pending.
- [ ] Run `rtk bun run test`, `rtk bun run typecheck` and `rtk bun run build` in `web/`; update old test expectations that intentionally opened inventory at root. Verify dirty-editor leave confirmation still applies when navigating home. Format, analyze and commit `feat: make host dashboard the repository-independent home page`.

## Task 10: Native and Docker packaging contract

**Files:** Modify `Dockerfile`, `deploy/{porty.example.yaml,package_test.sh}`, `docs/{operator-guide.md,development.md}`, and `.github/workflows/verify.yml`; create `deploy/monitoring_test.sh`. Preserve `deploy/systemd/porty.service` default restrictions; document optional GPU overrides rather than granting them universally.

**Interfaces:** OCI environment defaults to `PORTY_MONITORING_MODE=host`; native defaults remain native. `deploy/monitoring_test.sh` is opt-in and disposable, with fixed commands/argument arrays, isolated temporary mount fixtures, cleanup traps and no host permission changes. CI always checks both cgo and non-cgo builds; hardware/privileged host-mount tests are explicitly opt-in.

- [ ] Add failing packaging checks that the default image has a cgo/glibc-compatible runtime, runs as non-root, starts without NVML installed, preserves data directory permissions, and uses host mode without silently reading container metrics. Add a disposable nested-mount case verifying host binds and all submounts are read-only from the container.
- [ ] Run `rtk ./deploy/package_test.sh` and the new fixture-capable checks; record expected pre-change failures. Do not claim a skipped live Docker test passed.
- [ ] Replace Alpine builder/runtime with matching supported Debian slim images, pinning versions/digests using current official images at execution time. Enable cgo in the builder with its C toolchain; install only required runtime libraries, CA certificates and time zones. Do not bundle CUDA. Keep application user, entrypoint, Docker socket group guidance and data modes.
- [ ] Provide an executable/tested Docker recipe using read-only host proc/sys and `bind-recursive=readonly` host-root mounts. Docker documents Linux 5.12 or newer for recursive read-only mounts; verify runtime support and use `--mount`, which supports this option. Explain host-root read visibility; if the contract cannot be met, omit filesystem access and show unavailable rather than mount writable subtrees. Use private mount propagation and verify Task 3's mount-identity rejection when a new host mount is absent from the container. Document that newly attached filesystems may require refreshing the container mounts. Do not add host networking/PID mode or privileged mode for basic metrics.
- [ ] Document NVIDIA Toolkit `utility` access; optional AMD/Intel devices/groups; Intel `CAP_PERFMON` and a narrowly changed seccomp profile where required. Preserve native service restrictions by default and document an explicit GPU-only override. Explain rootless limitations, driver/build unsupported states, rate units and five-minute history reset.
- [ ] Run `rtk env PORTY_LIVE_DOCKER_CHECK=1 ./deploy/package_test.sh` on a Docker-enabled host, the explicit monitoring mount test, cgo/non-cgo builds, and existing on-demand packaging checks on an appropriate host. Record image-size and native-binary deltas. Format, analyze and commit `feat: package host monitoring for native and docker installs`.

## Task 11: End-to-end validation and final review

**Files:** Create `web/e2e/dashboard.spec.ts`; update setup/inventory navigation in `web/e2e/{critical.spec.ts,ui-overhaul.spec.ts,alerts.spec.ts,auto-update.spec.ts,on-demand.spec.ts}` only where the landing-route change requires it. Update `docs/development.md` with reproducible monitoring smoke-test steps if needed.

**Interfaces:** Reuse the existing executable-backed Playwright setup and `PORTY_E2E_BINARY`; monitoring API fixtures use the actual Task 6 JSON contract. Screenshots and measurement artifacts go under `/tmp/porty-dashboard-validation/`, not tracked source.

- [ ] Add browser regressions `"opens metrics before repository setup"`, `"navigates dashboard to stacks and a container and back"`, `"recovers from stale samples without inventing zeros"`, `"keeps selected device details readable on mobile"`, and `"clears metrics on logout"`. Exercise a fresh signup, repository outage and expired session.
  ```ts
  await expect(page.getByRole("heading", { name: "Dashboard" })).toBeVisible();
  await page.getByRole("link", { name: "Stacks", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Configure the stack repository" }),
  ).toBeVisible();
  await page.getByRole("link", { name: "Dashboard", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Dashboard" })).toBeVisible();
  ```
- [ ] Run the new browser tests against the built frontend and Go binary. If they expose new integration failures, reproduce the failure first, fix in the responsible module and rerun its focused tests. Use the frontend testing skill; prefer Browser plugin if available, otherwise document ordinary Playwright as the fallback.
- [ ] Create `/tmp/porty-dashboard-validation/`. Run `rtk bun run test`, `rtk bun run typecheck` and `rtk bun run build` from `web/`; then from the repo root run `rtk go test ./...`, `rtk go vet ./...`, `rtk go build -o /tmp/porty-dashboard-validation/porty ./cmd/porty` and `rtk env CGO_ENABLED=0 go build -o /tmp/porty-dashboard-validation/porty-nocgo ./cmd/porty`. Finally, from `web/`, run `rtk env PORTY_E2E_BINARY=/tmp/porty-dashboard-validation/porty bun run test:e2e`. This order embeds the current frontend. Run packaging checks from Task 10. Expected: each available required check exits zero; explicitly record environment-dependent skips.
- [ ] Inspect light/dark desktop 1536×1024, mobile 390×844 and narrow 320-pixel layouts, with long labels and all disclosures open. Save desktop/mobile screenshots and check console errors, keyboard focus, tap tooltips, chart gaps and no horizontal page overflow.
- [ ] Measure a five-minute run with zero, one and three dashboard tabs: record collector calls, CPU, retained memory, response sizes and goroutine/descriptor counts. Verify collection count is independent of client count and memory plateaus. Check real native/Docker host readings against independent host measurements and observe GPU sleep/power behavior. Mark each GPU driver combination verified or untested; fixtures do not establish hardware compatibility.
- [ ] Review the whole diff inline against the spec, security/resource limits and package boundaries. Run complete graph change analysis and `git diff --check`, inspect staged scope, and commit `test: verify host dashboard workflows and deployments`. Report passed checks, skipped hardware/environment checks, artifact paths and remaining limitations. Use the finishing-branch skill only after implementation and required available checks are complete; follow the repository's no-subagent instruction.

## Dependency and source notes

Task order is sequential: 1 establishes the service contract; 2–5 add bounded collectors; 6 exposes the cached API; 7–9 deliver the UI and routing; 10 provides the supported runtime setup; 11 verifies the integrated result. Per-task commits are reviewable increments, not a claim the complete feature is ready before Task 11.

Use the primary-source links in the spec for gopsutil, NVML, AMD, i915/Xe PMU and Linux perf permissions. Context7 confirms per-context gopsutil path overrides; the chosen v4 release's source remains authoritative where its README examples still mention v3. Before implementing Docker recursive read-only binds, verify the current [Docker bind-mount contract](https://docs.docker.com/engine/storage/bind-mounts/#recursive-mounts). Dependency versions and image digests are resolved and pinned during execution, not guessed in this document.

## Plan review

Self-review checked spec coverage, source ownership, shared signatures, five Review Focus cases and verification commands. No product code is changed by this plan. Implementation awaits the user's review; the selected execution method is inline under the repository's no-subagent rule.
