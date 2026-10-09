# Porty development

On-demand activation architecture, deployment requirements, reproducible Docker
gates and measured limitations are documented in [On-demand groups](on-demand.md).
`internal/traffic` owns temporary sleeping listeners; `internal/ondemand` owns
bounded observation/decision loops; `internal/control` owns coordinated exact-ID
transitions. SQLite joins transition completion and alerts atomically. The
portable backend uses the existing Docker SDK and adds no runtime dependencies.

Porty requires Go 1.27.1+, Bun 1.4.2+, and Linux. The running service uses the Go Git and Docker Compose SDKs, so it needs access to a Docker daemon for stack actions but does not need `git` or `docker` executables. Docker is optional for contract tests and required for the live Compose and OCI gates.

SQLite migrations run automatically at startup through goose using the embedded SQL files in `internal/sqlite/migrations/`. This development version does not upgrade databases created by the previous custom migration runner. Before starting this version with an existing local installation, stop Porty and remove or recreate the local `porty.db` file and its `-wal`/`-shm` companions in the configured data directory. Porty never deletes the database automatically.

For a TLS reverse proxy, set `server.public_url` to the external origin (for example, `https://porty.example.com`). Porty uses that origin for CSRF checks and marks authentication cookies Secure. Direct TLS and local HTTP use the request origin when this setting is empty. Access cookies expire after 15 minutes; the browser refreshes them with a rotating token whose original login expires after seven days. Logout revokes the refresh family, while an access token already issued at logout can remain valid for at most 15 minutes. Password changes and resets rotate the signing key and revoke all refresh families immediately.

The frontend entry point is `web/src/main.tsx`. `web/src/app/` owns session boot, hash routing, workspace coordination, theme, and global styles. Each directory under `web/src/features/` owns its domain UI, API functions, and types. `web/src/lib/http.ts` owns shared HTTP/CSRF/refresh behavior, and `web/src/components/` contains UI used across features. Unit and component tests sit beside their source; cross-feature browser tests remain in `web/e2e/`.

```sh
(cd web && bun ci)
(cd web && bun run test)
(cd web && bun run typecheck)
(cd web && bun run build)
go test ./...
go test -race ./...
go vet ./...
```

SQLite read queries are generated with [sqlc v1.31.1](https://github.com/sqlc-dev/sqlc/releases/tag/v1.31.1) from `internal/sqlite/migrations/` and `internal/sqlite/queries/`. The migration directory is the only schema source. Install that exact version and run `sqlc generate` from the repository root. The generated files in `internal/sqlite/generated/` are committed; CI regenerates them and rejects any diff. Nullable database columns use sqlc's `sql.Null*` defaults so the SQLite stores can map them explicitly to domain values. Transactional writes remain handwritten where they enforce cross-table behavior.

The Go binary embeds `web/dist`; rebuild the frontend before building a release binary. Run the browser journey after building `/tmp/porty-e2e`:

```sh
go build -o /tmp/porty-e2e ./cmd/porty
(cd web && PORTY_E2E_BINARY=/tmp/porty-e2e bun run test:e2e)
```

The browser test starts a real Porty process and Git repository. It intercepts Docker-backed API responses in the browser, so no workload or daemon is touched. Run `PORTY_LIVE_DOCKER_CHECK=1 ./deploy/package_test.sh` on a disposable Docker-enabled builder to verify the OCI image.

The SDK uses a new normalized Compose digest. A stack deployed before this migration may show pending changes once; redeploying records the new digest and restores the current state.

Tests that open an HTTP listener, run processes, or use a browser may require permission in restricted sandboxes. Never commit generated Playwright reports, traces, or screenshots.

## UI overhaul verification

The UI browser suite covers reviewed deployment, stale review rejection, Save/Discard/Cancel, operation streaming, partial batch acceptance, keyboard focus, long names, and 1536/390/320 px layouts. It also captures both themes for inventory, stack detail, editor, repository, settings, operation details, registration/setup, and loading/error/empty states:

```sh
rtk bun run --cwd web build
rtk go build -o /tmp/porty-e2e ./cmd/porty
cd web
rtk env PORTY_E2E_BINARY=/tmp/porty-e2e bun run test:e2e -- --output=/tmp/porty-ui-review
```

Screenshots are under the chosen output directory, grouped by test and device. Authentication, local repository setup, file writes, commits, and password-change sign-out use the real server in the critical/setup journeys. Runtime scenarios use deterministic browser fixtures. Actual revision validation and continuous coordinator ownership are covered by the Go deployment-review tests; browser fixtures alone do not establish backend safety.

`GET /api/v1/stacks/{id}/deployment-review` returns an opaque process-local revision. The UI sends it as a quoted `If-Match` value on the existing deploy POST. A changed source returns 412 and requires fresh confirmation. Legacy deploy requests without that header retain their existing behavior. Revisions expire on process restart and do not promise an immutable snapshot of direct host edits or live bind mounts.

## Shared alerts

`internal/alert` owns the alert lifecycle contract. SQLite persists deduplication occurrences, revisions, episodes, and actor history. Operation completion and its alert changes commit together; WebSocket `alerts` events publish only after commit. HTTP reads are authoritative after reconnect. A stale recovery cannot resolve a newer failure, and stale user mutations return 409.

Control actions supply stable stack/problem/target keys and verify matching postconditions before resolving incidents. Alert HTTP routes require authentication and mutation origin/CSRF checks but remain available when repository readiness fails. Acknowledgment and manual resolution have separate audit entries.

`web/src/features/alerts` owns alert reads, mutations, views, and lifecycle history. `useTopicStream` supplies shared reconnect mechanics for operations and alerts. Browser fixtures exercise global/stack consistency and stale revisions independently of Docker; `TestManualAlertLifecycleSurvivesRestartAndRecurrence` exercises the control/runtime boundary with real SQLite.

Run `bun run test:e2e -- alerts.spec.ts --output=/tmp/porty-alert-playwright` from `web` after building `/tmp/porty-e2e`. Desktop/mobile screenshots are written to `/tmp/porty-alerts-desktop.png` and `/tmp/porty-alerts-mobile.png`.

## Scheduled updates

`internal/autoupdate` owns strict UTC cron parsing, revisioned policies, bounded scheduling, and run outcomes. It depends on narrow store/executor interfaces; `internal/control` coordinates that executor with existing manual operations. The Compose adapter owns snapshot inspection, public-registry digest preparation, constrained recreation, runtime verification, and self-protection. SQLite owns atomic run/operation/deployment/image/alert completion. No general workflow engine or action graph is introduced.

Image preparation releases the stack coordinator; mutation reacquires it and compares policy, source, environment, deployment provenance, and container evidence. Prepared intent is stored before the applying phase. Startup discards preparation and conservatively pauses interrupted applying/verifying executions without replaying Docker actions. Effective image selections remain separate from the source configuration digest. Keep these boundaries intact when adding future automation types.

Deterministic tests cover scheduling/clock rollback, stale policies, preparation races, partial failure pauses, intent persistence, restart phases, manual image selection, authentication/CSRF, and alert recovery. The browser fixtures cover policy editing and failure → acknowledgment → manual recovery → explicit resume on desktop/mobile:

```sh
go build -o /tmp/porty-e2e ./cmd/porty
(cd web && bun run test:e2e -- auto-update.spec.ts alerts.spec.ts --output=/tmp/porty-update-playwright)
```

Screenshots are written to `/tmp/porty-auto-update-desktop.png` and `/tmp/porty-auto-update-mobile.png`. Browser fixtures do not prove Docker behavior.

### Disposable Docker release gate

Do not run live tests against a daemon hosting user workloads. Set `DOCKER_HOST` to an isolated disposable daemon and explicitly attest that it is disposable. Supply two distinct immutable public Alpine-compatible images containing `sh`, `hostname`, and `sleep`:

```sh
DOCKER_HOST=unix:///path/to/disposable/docker.sock \
PORTY_LIVE_DOCKER_CHECK=1 PORTY_DISPOSABLE_DOCKER=1 \
PORTY_TEST_IMAGE_A='alpine@sha256:<first-manifest>' \
PORTY_TEST_IMAGE_B='alpine@sha256:<second-manifest>' \
go test ./internal/compose -run 'TestUpdateLive' -count=1 -v
```

`TestUpdateLivePreservesVolumesAndUnselectedService` creates a uniquely named fixture, checks named/anonymous volume markers and unselected container identity across recreation, and verifies a stopped service remains stopped. Cleanup removes only the fixture project and volumes; pulled images are left on the disposable daemon. The fixture bypasses registry discovery to isolate Compose recreation behavior.

**Release gate pending:** live tests were not run during this implementation because no daemon had been established as disposable. Record Docker version, API version, storage mode, fixture images and test output before release. The full matrix still needs live coverage on classic and containerd image stores, mutable-tag publication during pull, a multi-architecture index changing only another platform, shared-tag stacks, multi-replica/dependency fixtures, and marker ownership inside custom-hostname/shared-mount containers. Unit tests for these boundaries are not substitutes for the missing live scenarios. The OCI packaging test has its own explicit live gate.

## Host dashboard verification

Build the frontend before the executable. Monitoring is owned by
`internal/monitoring`; Linux adapters use bounded, rooted proc/sys reads and
the NVIDIA adapter uses optional runtime NVML. The only added Go dependency is
`github.com/NVIDIA/go-nvml v0.13.4-1`; the frontend adds no dependencies.

```sh
go test -race ./internal/monitoring ./internal/app ./internal/http ./internal/config
CGO_ENABLED=0 go test ./internal/monitoring
CGO_ENABLED=0 go build -o /tmp/porty-nocgo ./cmd/porty
(cd web && bun run test && bun run typecheck && bun run build)
go build -o /tmp/porty-e2e ./cmd/porty
(cd web && PORTY_E2E_BINARY=/tmp/porty-e2e bun run test:e2e -- dashboard.spec.ts --output=/tmp/porty-dashboard-validation/browser)
PORTY_LIVE_DOCKER_CHECK=1 ./deploy/package_test.sh
```

The OCI check creates and removes only its own container and anonymous data
volume. It verifies non-root startup, private data modes, dynamic libc,
startup without NVML and unavailable host metrics when mounts are omitted.
The pinned Go 1.27.1 Bookworm builder and Debian Bookworm runtime digests were
resolved from Docker Hub on 2026-10-09. NVIDIA's binding emits upstream
deprecated-declaration warnings; these do not require NVIDIA libraries at link
or startup time.

On a disposable local Linux builder with permission to create temporary
mounts, also run `./deploy/monitoring_test.sh --image porty:verify --nested-mounts`.
It creates a small tmpfs under a temporary directory, checks that both parent
and nested mounts reject writes from the container, then unmounts/removes its
own fixture. It does not mount host root, change host permissions or touch
workloads. Remote Docker daemons cannot see the local fixture path.

GPU fixture tests cover supported field decoding and error states. Record
actual driver/kernel/device results separately; no GPU hardware matrix is
implied by the unit suite. See the operator guide for optional permissions.

### Validation recorded on 2026-10-09

Verified on Linux amd64: 537 Go tests with the race detector, 239 frontend
tests, typecheck, production build, go vet, cgo/non-cgo builds, 38 desktop/mobile
browser workflows, and the live OCI smoke check. The final snapshot-allocation
fix was followed by the backend suite and 10 dashboard browser regressions.
Screenshots cover light/dark 1536×1024 and 390×844, plus 320 px with every
disclosure open; no page overflow or unexpected browser errors remained.

An isolated container using the documented host mounts matched host RAM
capacity exactly and returned CPU readings; all 71 exposed host mounts were
read-only. The final native collector reported 1.65% CPU versus 1.56% from an
independent /proc/stat interval and matched RAM capacity exactly. Availability
values can differ because they are sampled at different instants.

A 15-minute integration-build run used consecutive five-minute phases with
zero, one and three real dashboard tabs. A separate observer requested a full
snapshot every ten seconds. Results include that observer and concurrent
development/test activity; these are smoke measurements, not a benchmark SLA:

| Open tabs | CPU (% of one core) | RSS range (MiB) | Open descriptors |
| --- | ---: | ---: | ---: |
| 0 | 1.19 | 69.0–103.3 | 22–23 |
| 1 | 1.44 | 93.9–99.2 | 23–25 |
| 3 | 2.10 | 90.3–98.0 | 23–25 |

History plateaued at 150 samples and approximately 3.07 MB for full responses
on this host; sequence advanced at the same two-second cadence in every
phase. This run preceded the final hotplug, Alerts stream and oversized-history
allocation fixes. Final focused tests additionally verify that 1,000 snapshots
from 20 concurrent clients cause only one scheduled collection and retain no
extra goroutines (2 before and after). A 128 KiB response-budget stress case
allocated about 1.06 MB after the fix, versus 351 MB before it. The 64 MiB limit
applies to stored monitoring data, not total process RSS.

The uncompressed OCI image increased from 19,679,310 to approximately
47,532,123 bytes due mainly to the glibc runtime. Non-cgo unstripped native
binary size grew from 57,858,290 bytes (monitoring not yet reachable) to about
58.2 MB; the cgo build is about 58.8 MB. No frontend dependency was added.

Artifacts for this run are under `/tmp/porty-dashboard-validation/`:
`soak.json`, `native-host.json`, `docker-host.json`, `client-count.log`,
`allocation.log`, `final-packaging.log`, `final-browser/`, and
`final-dashboard/`. They are not tracked in Git.

Remaining environment checks: no AMD, NVIDIA, Intel i915 or Xe device was
exposed, so real driver compatibility and GPU sleep/power behavior remain
untested. The disposable newly-created nested-mount fixture requires mount
privileges unavailable to this user; existing nested host mounts were verified
read-only through Docker. Live on-demand workload/activation gates were not
run because this daemon was not established as disposable; their existing
browser and unit regressions passed. Other architectures and rootless Docker
remain unverified. For partially accessible Intel engines, restart Porty after
changing access to additional engines; fully denied PMU initialization retries
every ten seconds.
