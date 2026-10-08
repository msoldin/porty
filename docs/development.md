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
