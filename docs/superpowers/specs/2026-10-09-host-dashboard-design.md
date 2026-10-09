# Host monitoring dashboard

Date: 2026-10-09

Status: Conversational design approved; written specification awaiting review.

## Intent and agreed scope

Porty's administrator needs a quick view of the machine's current health. The
dashboard monitors the Linux host running Porty, whether Porty runs natively or
inside Docker. It is not a container resource dashboard or a long-term monitoring
system.

The user selected a built-in collector, a five-minute in-memory history, two-second
updates, and layout A: eight compact metric cards with subtle background charts
and expandable device details. Dashboard becomes the landing page; Stacks remains
a separate destination. Docker installations may require documented host mounts
and GPU access. NVIDIA, AMD, and Intel are in scope, with explicit unavailable
states when hardware, drivers, build support, or permissions prevent a reading.

URL-based application health checks and favicon shortcuts are a later phase.
Do not add URL fetching, application configuration, placeholders, migrations,
notifications, or alert thresholds for that future work in this release. The
mockup's future-phase annotation is explanatory, not production UI.

Success means the administrator can read the eight metrics immediately, inspect
individual devices when needed, and distinguish actual inactivity from missing
or stale data. Opening additional browsers must not multiply collection work.

## Layout and interaction

Use Porty's existing typography, sidebar, light/dark tokens, borders, and controls.
At desktop widths, show four cards per row:

| CPU      | RAM    | Temperature | GPU           |
| -------- | ------ | ----------- | ------------- |
| Download | Upload | Disk I/O    | Disk fullness |

Use two columns at intermediate widths and one column where two cards cannot
remain readable. Cards grow when their details open; content must not overlap or
require horizontal page scrolling. Disk fullness may grow to show multiple
filesystems. Do not average fullness across disks.

Each card has a label, a prominent current value, units, source/device context,
and an accessible disclosure for details. Network, temperature, and GPU details
include a selector for the main reading. Download and Upload share one interface
selection. Remember those choices in browser storage, scoped to the Porty origin;
invalid or removed selections visibly fall back to the documented default.

The heading identifies the host and shows monitoring freshness independently of
the existing operation-stream connection indicator. A live connection is not a
claim that every device or application is healthy.

Seven cards have five-minute sparklines. Disk fullness uses capacity bars. Disk
I/O has labeled read and write lines. Percentage charts use a fixed 0–100 scale;
rate charts start at zero and scale to the visible history. Temperature charts
show their range. Provide time and value inspection on focus/tap as well as hover.
Labels, current values, units, and state text remain useful without the charts.
Respect reduced motion and keep chart fills clear of readable text.

The selected visual reference is layout A in the local companion artifact
`.superpowers/brainstorm/101954-1791561252/content/dashboard-layouts.html`.
That ignored artifact is illustrative; the requirements in this document are the
durable source of truth. Its values are not measurements of the user's machine.

## Reading definitions

| Card          | Main reading                                               | Expanded details                                                                                     |
| ------------- | ---------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| CPU           | Host busy percentage across all logical CPUs               | Per-logical-CPU usage                                                                                |
| RAM           | Used / total and percentage                                | Available memory and swap used / total                                                               |
| Temperature   | Selected CPU sensor in degrees Celsius by default          | All readable temperature sensors and selection                                                       |
| GPU           | Selected GPU's utilization percentage                      | Individual GPUs, utilization basis, engines where applicable, memory and temperature where supported |
| Download      | Received bytes per second on the selected interface        | All discovered interface rates and selection                                                         |
| Upload        | Sent bytes per second on that same interface               | All discovered interface rates and selection                                                         |
| Disk I/O      | Host physical block-device read and write bytes per second | Individual device rates and aggregate coverage                                                       |
| Disk fullness | Used / total and percentage for each real filesystem       | Available capacity, type and mount path                                                              |

Display network and disk rates with decimal units, such as MB/s; display RAM and
GPU memory with binary units, such as GiB; display disk capacity with decimal
units, such as GB. API values use bytes, bytes per second, Celsius, and percentage
points so the frontend alone owns formatting.

CPU busy percentage excludes idle and I/O-wait time and does not double-count
guest counters. RAM used means total minus available, so reclaimable cache is not
presented as unavailable memory. Swap is separate from the RAM percentage.

Network includes LAN traffic; it is not an internet speed test. Default to an
active host interface on the preferred default route, then a stable active
non-loopback interface. Never sum bridges and their member interfaces. Keep
virtual interfaces available in details, with the selected interface named.

For disk I/O, sum eligible physical leaf devices once, excluding partitions,
loop/ram devices and higher storage layers that would double-count their backing
devices. Show excluded/unsupported aggregate coverage explicitly. If no reliable
physical aggregate is available, show unavailable and retain readable individual
device metrics. This is throughput, not latency, IOPS, or a disk utilization
percentage.

List real mounted storage filesystems, including accessible network filesystems;
exclude kernel pseudo-filesystems, temporary memory filesystems and container
overlay/bind duplicates. Deduplicate filesystem capacity by device/filesystem
identity, retaining alternative mount paths in details. Used capacity is total
minus free blocks; available capacity excludes blocks reserved from ordinary
users. Explain that distinction when reserved capacity is nonzero. Slow or
inaccessible filesystem reads must not stall the rest of monitoring.

Default temperature selection prefers a labeled CPU package sensor, then a CPU
sensor; without either, show CPU temperature unavailable and allow the user to
select another sensor. Never silently label a disk or GPU sensor as CPU.

Choose the first supported GPU in stable device order by default and show its
name. Never add GPU percentages together. Intel's main percentage represents its
busiest measured engine, explicitly labeled; vendor-native utilization is used
for NVIDIA and AMD. If engine coverage is incomplete, the result is partial,
not a claim about the whole GPU. Shared memory is not labeled dedicated VRAM.

## Collection and history

`internal/monitoring` owns the collector, metric types, rate calculations, device
identity, availability, and history. `internal/app` constructs it once, starts it
with the application context, and stops it during application shutdown. It does
not depend on stack locks, repository readiness, the Docker daemon, or the
operation event stream.

One coordinator schedules collection every two seconds, including with no
dashboard clients. Use monotonic elapsed time for counter deltas and UTC
timestamps for display. The first counter sample is a baseline: show collecting,
not a synthetic zero. Reset baselines after counter rollback, device replacement,
or a sampling interruption exceeding six seconds. Genuine unchanged counters
produce zero rates.

Use bounded independent workers for metric families and potentially blocking
device reads. Allow at most one in-flight read per source; a missed deadline
marks that source stale and does not create another worker for it. A late result
cannot overwrite a newer sample. Context cancellation cannot forcibly interrupt
every kernel or vendor call: shutdown must respect its application deadline even
if such a call does not return. Do not claim a hard hardware-call timeout.

Keep up to 151 samples per series and prune data older than five minutes, using
monotonic age. History is held by the backend, survives page reloads and tab
closures, and resets when Porty restarts. Retain history for expandable devices
as well as the default cards so changing a selector can show recent context.
Device identity must distinguish replacement devices and avoid joining their
histories under a reused display name.

Bound discovery to 256 logical CPUs, 64 interfaces, 64 block devices, 64
filesystems, 128 temperature sensors, and 16 GPUs with at most 16 engines each.
Expose omitted counts/partial coverage rather than silently claiming completeness.
Cap stored monitoring data at 64 MiB; evict the oldest history first and report
the actual retained window. A configured normal single-server installation
should remain well below that ceiling. Do not persist metrics in SQLite or logs.

States are `collecting`, `available`, `unavailable`, and `stale`, with sanitized
reason codes and last-success timestamps. No fresh value for six seconds is
stale. A known unsupported source or permission failure is unavailable. Show an
old value only with its stale label and timestamp. Charts contain gaps for
missing data; never interpolate over restart, counter reset, or device change.

## Linux sources and GPU support

Prefer gopsutil v4 for ordinary CPU, memory, disk, network, and sensor reads. It
supports alternate host paths and does not require cgo. Pass source locations
explicitly via its context configuration rather than mutating process-wide
environment variables. Keep small Linux-specific adapters where accurate host
namespace selection, bounded discovery, or device identity requires them.
See the [gopsutil documentation](https://github.com/shirou/gopsutil).

GPU adapters stay inside `internal/monitoring`; use focused files instead of a
general plugin framework:

- NVIDIA: use the official Go NVML bindings, dynamically loading the installed
  driver library. Missing library, unsupported device fields, and permission
  errors are availability results. Do not shell out to `nvidia-smi`.
- AMD: read the amdgpu sysfs utilization and memory counters plus hwmon
  temperatures. Treat absent attributes individually as unsupported.
- Intel: use i915 engine-busy PMU counters or Xe active/total engine counters when
  the driver exposes them. Discover event formats rather than assuming i915 and
  Xe have identical encodings; use the kernel device query where Xe engine
  enumeration requires it. Read through narrow Linux syscall adapters using
  `golang.org/x/sys/unix`, already present in Porty. If access or counters are
  absent, report unavailable. Do not use incomplete per-process fdinfo sampling
  as host-wide GPU utilization or infer utilization from GPU frequency.

The support contract is capability-based, not every product from each vendor.
Virtual GPUs, vendor-specific partitions and SR-IOV accounting are not promised;
unrecognized or incomplete accounting must be disclosed. Real-hardware smoke
tests are required before claiming a driver/device combination works.

Primary references: [NVIDIA Go NVML](https://github.com/NVIDIA/go-nvml),
[AMD monitoring interfaces](https://www.kernel.org/doc/html/latest/gpu/amdgpu/thermal.html),
[i915 PMU](https://github.com/torvalds/linux/blob/master/drivers/gpu/drm/i915/i915_pmu.c),
and [Xe PMU](https://github.com/torvalds/linux/blob/master/drivers/gpu/drm/xe/xe_pmu.c).

### Build and packaging consequence

The current Dockerfile uses an Alpine runtime and `CGO_ENABLED=0`. Official
go-nvml bindings require cgo, so full NVIDIA support cannot be added without a
packaging change. The proposed default OCI build uses a cgo-enabled Go builder
and a compatible glibc-based slim runtime. Preserve the non-root user, ownership,
restrictive data permissions, CA certificates, time zones, entrypoint, and
existing deployment contracts.

Keep a build-constrained non-cgo implementation so `CGO_ENABLED=0 go build` still
works; only the NVIDIA adapter is unsupported in that build. A cgo build must
start normally without an NVIDIA driver installed. Native documentation states
the build prerequisites and runtime library requirements. Compare binary/image
size and memory before accepting the dependency and image change; no CUDA toolkit
or compute stack is bundled merely to read metrics.

## Native and Docker deployment contract

Add startup-only monitoring configuration with `mode: native | host | disabled`.
Native is the ordinary binary default; the OCI image explicitly defaults to
host mode. Disabling monitoring stops collection and explains the disabled
dashboard state. Sampling and retention are fixed in this release.

In native mode use native `/proc`, `/sys` and filesystem paths. In host mode use
explicit `host_proc`, `host_sys`, and `host_root` locations, defaulting to
`/host/proc`, `/host/sys`, and `/host/root`. These are administrator startup
configuration, never request parameters. Provide YAML and corresponding
`PORTY_MONITORING_*` environment settings following existing config conventions.

Docker documentation must provide a tested recipe for read-only host procfs,
sysfs, and filesystem mounts. Host network counters and mount enumeration come
from the host's process-1 namespace views, not the container's `self` views.
Resolve reported host mount paths beneath the configured host root. Missing
mounts never fall back to container paths. Independently available host metric
families may still be displayed if another mount is missing.

Mounting the host root grants broad read visibility even when writes are blocked;
explain that concrete deployment consequence. Require recursively read-only
mounts, including submounts, and verify that guarantee in packaging tests. Do not
advertise a simple read-only parent bind as sufficient on systems where nested
mounts remain writable. If the host cannot provide the documented read-only
arrangement, leave the affected filesystem metrics unavailable.

Basic monitoring must not require privileged Docker mode, host PID sharing, or
host networking. GPU access is optional and documented separately:

- NVIDIA: host driver and NVIDIA Container Toolkit access with the `utility`
  capability; expose only the selected GPUs.
- AMD/Intel: only the relevant readable sysfs paths and device nodes/groups when
  required. Intel PMU access may additionally require `CAP_PERFMON` and a narrow
  Docker seccomp allowance for `perf_event_open`, or a native systemd override.
  Do not disable seccomp or grant `SYS_ADMIN` as the default solution.

Porty never changes host kernel settings, device permissions, or GPU power
configuration to obtain readings. Document rootless/container-runtime limits as
unavailable capabilities rather than weakening isolation automatically.
See [NVIDIA container capabilities](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/docker-specialized.html)
and [Linux perf access controls](https://docs.kernel.org/admin-guide/perf-security.html).

## API and frontend boundaries

Add a cached, authenticated `GET /api/v1/monitoring` endpoint in
`internal/http/routes_monitoring.go`, backed by a narrow reader interface on
`RouterOptions`. Use `authenticatedRoute`, not `readRoute`, because the latter
requires repository readiness. Return `Cache-Control: no-store`. No request may
trigger collection, accept filesystem paths, or alter monitoring configuration.

The response contains host metadata, a collector generation, monotonic sequence,
server time, actual history window, device metadata, samples, and source states.
Use an opaque bounded cursor containing generation and sequence for incremental
reads. Missing, expired, or previous-generation cursors return a reset plus the
available window; malformed cursors return a bounded validation error. Encode
sequences losslessly for JavaScript. Clients replace history on reset and dedupe
by generation/sequence otherwise.

Do not resend unchanged device metadata on every incremental request. Bound each
response to 8 MiB. If a full window would exceed that, remove the oldest samples
and expose the shorter window; never emit a truncated JSON document. An empty or
partially unavailable collector remains a successful structured response. Only
authentication and actual endpoint failures use error responses.

Keep API calls and polling in a feature-local module/hook under
`web/src/features/dashboard`. Presentation components receive typed data. Poll
every two seconds while visible, with one request in flight, request cancellation,
and a five-second client deadline. Pause on page hide or unmount; resume with the
cursor and fetch a reset window if needed. Authentication failures follow the
existing session flow. Retain previous readings as stale during transient errors.
Stop polling after sign-out and clear monitoring state.

Render small SVG charts using browser/Preact APIs; no new chart library is needed
for this fixed scope. Keep chart and metric-card components local until another
feature actually reuses them. Do not add monitoring samples to the operation
WebSocket hub's shared replay buffer.

## Navigation and repository independence

`#/` displays the new dashboard; `#/stacks` displays the current stack inventory.
Rename the existing stack feature's `Dashboard` component to `StackInventory`
through the repository's symbol-aware workflow, and retain its table behavior.
Keep `#/stacks/{id}` and container routes unchanged. Stack breadcrumbs and
inventory return actions target `#/stacks`; the Porty brand targets `#/`.

Render the dashboard before the workspace's stack-loading/error branches.
Stack, audit, operation, or repository failures must not replace host metrics
with a loading screen or unrelated page-wide error.

After administrator registration/sign-in, allow Dashboard independently of
repository setup. Move the current repository setup gate into repository-dependent
destinations, preserving the existing setup flow and all backend repository
guards. Reuse the workspace shell without inventing a second dashboard shell.
Do not relax authentication or make host readings available before sign-in.

## Security and resource boundaries

Preserve existing auth/session behavior, CSRF and origin guards, and no-store
handling. This feature introduces only an authenticated read endpoint. It does
not change stack/repository lock ordering or existing filesystem access policies.

Treat kernel-provided names and mount paths as data. Bound counts and label/error
lengths, escape them in the frontend, and avoid returning raw driver errors,
environment values or arbitrary file contents. Root filesystem operations under
configured mounts and reject traversal or symlink escape. Legitimate sysfs links
may resolve within the configured sysfs root, never outside it. Device nodes are
opened only from discovered, validated device identities for the relevant adapter.

Use fixed APIs and narrow syscalls, not constructed shell commands. Never probe
application URLs in this phase. When app checks are designed later, address URL
authorization, redirects, timeouts and request-target policy in that separate
design.

## Verification and acceptance

Follow repository regression-first testing. Tests use injected clocks, fixture
procfs/sysfs trees, fake GPU readers and controlled failures, not the test runner's
real CPU load.

- Verify counter deltas, CPU normalization, available-memory accounting, first
  samples, rollback, missing samples, restart and device replacement.
- Verify network selection, storage-layer deduplication, filesystem reserved
  space, primary sensor selection and Intel utilization labeling.
- Verify retention/eviction, cardinality/response limits, cursor reset/deduplication,
  long pauses, repeated failures, bounded workers and deadline-limited shutdown.
- Verify missing or malformed sources, no GPU, driver/library absence, partial
  fields, denied PMU access, hotplug and stalls without losing other metrics.
- Verify host/container namespace separation with intentionally different fixture
  values. Missing host roots must never disclose container-derived substitutes.
- Verify traversal, symlink escape, invalid device identifiers, redacted errors,
  bounded output, unauthenticated reads and repository-independent authenticated
  reads. Re-run existing auth/origin/CSRF suites for integration regressions.
- Verify the new home route, preserved stack/container deep links, inventory
  return navigation, setup flow, local selections, stale states, chart gaps,
  hidden-tab behavior, mobile disclosures and keyboard access.
- Run focused and affected Go/Vitest suites, frontend typecheck/build, Go vet/build,
  the non-cgo build and packaging tests. Use Playwright for desktop/mobile flows
  and capture screenshots. Run the full relevant repository checks before merge.
- Smoke-test a native installation and the documented Docker mount recipe.
  Exercise NVIDIA, AMD, i915 and Xe paths on available hardware; clearly record
  any untested combinations rather than treating fixture tests as hardware proof.
  Measure idle monitoring cost, multiple-client behavior, retained memory, image
  size and GPU sleep/power behavior; collection must not grow with browser count.

## Review notes

The specification includes two implementation consequences uncovered during
source review: repository-independent dashboard access and the cgo/glibc packaging
change for official NVIDIA bindings. These are included for review before an
implementation plan is written.

Repository examined: `/home/msoldin/porty`. Its GitNexus index was one commit
behind during exploration; refresh attempts failed because the sandbox could not
resolve the package registry. Relevant lifecycle, HTTP guards, routing, config and
Dockerfile source was checked directly. Refresh the graph before symbol edits;
the stale index is not proof of complete impact coverage.
