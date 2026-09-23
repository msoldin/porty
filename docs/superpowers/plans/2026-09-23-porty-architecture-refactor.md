# Porty Architecture Refactor Implementation Plan

> **For agentic workers:** Use `superpowers:executing-plans` to implement this plan task by task. The repository instructions prohibit subagents. Check off each step after verification.

**Goal:** Simplify Porty's Go backend into cohesive packages with explicit wiring, HTTP middleware, short-lived JWT access tokens, revocable refresh tokens, and database tooling while preserving its HTTP, WebSocket, and security contracts.

**Architecture:** Keep `cmd/porty` as the executable and move construction into `internal/app`. Group business operations around authentication, stacks, repository setup, and operations. Put SQLite, Git, Compose, process execution, rooted filesystem, and HTTP transport code in direct `internal/` packages, as in the architecture brief; remove `internal/infrastructure`. Move `internal/httpapi` to `internal/http`, with `internal/http/middleware` for request IDs and safe access logging; keep authentication, origin/CSRF, and repository-readiness guards beside the HTTP routes they protect. Replace the old session model with a 15-minute JWT access cookie and a rotating, revocable refresh cookie. Store only refresh-token hashes and the current signing key in SQLite; access validation reads that key without a per-access-token session lookup.

**Tech Stack:** Go 1.27.1, `database/sql`, `modernc.org/sqlite`, SQLite, `goose`, `sqlc`, `github.com/golang-jwt/jwt/v5`, `log/slog`, embedded Preact frontend.

**Spec:** `Architecture_Plan.md` (the user-provided architecture brief). This plan covers planning only; implementation is a separate step.

## Global Constraints

- Preserve existing `/api/v1` routes, response shapes, cookie security attributes, status codes, WebSocket events, CLI flags, and the `cmd/porty` binary path. Add only the refresh endpoint and cookie required by Task 6. The existing `porty_session` cookie carries the access JWT; expiry and logout semantics are specified in Task 6.
- Treat the current SQLite database as disposable development data. Support fresh goose migrations and repeated opens; document that developers must recreate existing local databases during this change. The application must not silently delete a database.
- Remove `internal/infrastructure` entirely by the final gate. Direct internal packages should reflect their real responsibilities, not adapter layers.
- Preserve rooted filesystem checks, Git command hardening, CSRF and origin enforcement, file modes, secret redaction, bounded output, and repository/stack lock ordering.
- Keep access JWTs and refresh tokens in separate `HttpOnly` cookies, never browser storage or logs. Keep the separate CSRF cookie/header check, bind it to the active token pair, and require it on refresh.
- Configure the canonical external origin explicitly when Porty runs behind TLS termination. Derive `Secure` cookies and Origin checks from that validated origin, never from untrusted forwarding headers.
- Use concrete dependencies by default. Keep interfaces only at an actual consumer boundary, such as a command runner, runtime, event publisher, or testable external I/O seam.
- Pass `context.Context` through request I/O; keep operation execution independent of a disconnected HTTP request, as it is today.
- Add no general-purpose `utils`, `core`, or `shared` package, DI framework, or generic repository.
- Run focused tests after each change and the repository checks at the final gate. Use targeted `gofmt` paths; the brief's truncated `go ve` means `go vet ./...`, per `AGENTS.md` and CI.

## Current-State Findings

| Area | Evidence | Planned correction |
| --- | --- | --- |
| Wiring | `cmd/porty/main.go` builds the router, SQLite stores, Git, Compose, filesystem, operations, and WebSocket hub in `buildHandler`. | Move construction and readiness setup to `internal/app`; keep CLI and server lifecycle in `cmd/porty`. |
| Cross-layer import | `internal/infrastructure/gitcli/setup.go` imports `internal/application` for setup requests and errors, while application code depends on Git through interfaces. | Put repository setup types and errors with the repository feature and let the direct `internal/git` package implement its consumer-defined boundary. |
| Broad application package | `internal/application` owns auth, stack/workspace, repository setup, operations, deployment, and control orchestration. | Move cohesive operations in small, tested slices; retain the coordinator and control orchestration until their dependencies are explicit. |
| Abstractions | `RepositoryService` adds remote-state gating and client replacement; `WorkspaceService` adds locking and filesystem/DB coordination; `EnvironmentService` validates and hides values. | Preserve these behaviors. Remove only confirmed forwarding methods or unused interfaces after caller-by-caller review. |
| Persistence | `internal/infrastructure/sqlite/db.go` has a custom `schema_migrations` runner and one `001_initial.sql` migration; six stores contain handwritten SQL. | Move persistence to `internal/sqlite`, replace the custom runner with goose for fresh databases, then use sqlc where it simplifies query code. |
| Transactions | Auth registration/password change, repository config, stack purge, and deployment save use explicit SQL transactions. | Keep those operations atomic; use generated `WithTx` only where it makes the operation clearer. |
| HTTP | `internal/httpapi` already centralizes routes and safe error envelopes, but `api.go` and `auth.go` are large. | Retain transport behavior; split route registration by existing feature only if it reduces navigation cost. |
| Middleware | `requestid.go` has request ID middleware; `auth.go` and `api.go` contain authentication, CSRF, readiness, and mutation wrappers. There is no request access log despite `log_format` config. | Put request IDs and safe `slog` access logging in `internal/http/middleware`; keep authentication and feature policy in `internal/http` to avoid a generic package that depends on route-specific responses. |
| Sessions | `application/auth.go`, `sqlite/auth_store.go`, and `sessions` SQL implement opaque tokens with idle/absolute expiry and server-side revocation. | Replace them with short-lived signed access JWTs and hashed, rotating refresh tokens. Store the current signing key in SQLite so password change/reset can rotate it in the same transaction as refresh revocation. |
| Tests/build | Go unit/contract tests, frontend tests, Playwright, packaging, and CI exist. `web/dist` is embedded. | Use those as compatibility gates; rebuild assets before binary/e2e verification. |

## Review Focus

Each condition below needs an explicit regression test in the task that owns it.

1. A fresh database gets the complete schema and seed row through goose, and a second open applies no migration twice (Task 5).
2. A forged, expired, wrong-algorithm, wrong-issuer/audience, or key-rotated access JWT is rejected; refresh-token replay revokes its active family; a missing or mismatched CSRF token blocks refresh and mutations (Task 6).
3. Access logs omit JWTs, CSRF values, passwords, query strings, headers, and response bodies while preserving WebSocket upgrades; an authenticated WebSocket closes at access-token expiry or password/key rotation (Tasks 6 and 8).
4. A failed repository auth update leaves the old configuration and secret row together, with no partial write or response leak (Task 3).
5. A tampered ready repository remains unready, stack/repository lock conflicts remain enforced, and accepted operation output stays redacted and bounded (Tasks 2 and 4).

## Dependency and File Map

The intended direction is `cmd/porty -> app -> feature operations + HTTP + SQLite + Git/Compose/filesystem/WebSocket`; HTTP depends on feature behavior, and SQLite and Git may depend on feature-owned value types. No feature package may import `app` or the HTTP transport package. Candidate backend packages are `internal/app`, `auth`, `stack`, `repository`, `operation`, `control`, `sqlite`, `git`, `compose`, `process`, `filesystem`, `http` (with a small `middleware` child), `websocket`, and `config`. Keep each only if it has a clear role after the moves. Do not retain `internal/infrastructure`, `internal/domain`, or `internal/application` as organizational layers.

| Responsibility | Starting files | Intended owner |
| --- | --- | --- |
| Process entry, Git helper, password reset | `cmd/porty/main.go`, `git_helper.go` | `cmd/porty` (unchanged path) |
| Construction, readiness, route assembly | `cmd/porty/main.go:buildHandler` | New `internal/app/app.go` |
| Authentication, signing-key policy, refresh policy, and password hashing | `internal/application/auth.go`, `internal/infrastructure/auth/password.go` | `internal/auth`; `internal/sqlite` persists the signing key and refresh hashes, with no per-access-token session |
| Stacks, environment, rooted workspace operations | `internal/application/stack.go`, `workspace.go`; `internal/domain/stack.go` | `internal/stack` for behavior; `internal/filesystem` for path-safe I/O |
| Repository setup, remote policy, Git state | `internal/application/repository.go`, `repository_setup.go`; `internal/domain/repository*.go` | `internal/repository` for policy; `internal/git` for Git execution |
| Operation/deployment lifecycle and coordination | `internal/application/operation.go`, `deployment.go`, `control_plane.go`; related domain files | `internal/operation` for lifecycle; `internal/control` for cross-feature actions |
| Database and migrations | `internal/infrastructure/sqlite/*` | `internal/sqlite`, with `migrations/`, `queries/`, and generated sqlc code |
| External execution | `internal/infrastructure/composecli/*`, `process/*` | `internal/compose`, `internal/process` |
| HTTP contracts, middleware, and event stream | `internal/httpapi/*`, `internal/websocket/*` | `internal/http` for transport/auth/feature guards, `internal/http/middleware` for request ID and logging, `internal/websocket` for the event hub |

Do not bulk-move `internal/domain`. Move types only when their owning feature and all callers are being updated in the same tested slice. HTTP request/response types that truly differ from business values remain transport types.

## Tasks

### Task 1: Establish Compatibility Baseline

**Files:** `docs/development.md`, existing Go and browser tests; add tests only where an observed contract lacks coverage.

- [ ] Record the current package imports, routes, schema, and test commands in the implementation PR description. Record any failing baseline separately from new failures.
- [ ] Run `go test ./...`, `go vet ./...`, `npm --prefix web test`, and `npm --prefix web run typecheck`. If the local Go installation lacks standard-library test packages, fix the toolchain or use CI before interpreting test results.
- [ ] Verify the already-covered invariants: setup/readiness in `cmd/porty/main_test.go`, auth/CSRF in `internal/httpapi/auth_test.go`, stale writes in `internal/httpapi/api_test.go`, and SQLite rollback in `internal/infrastructure/sqlite/repository_store_test.go`. The last path is the current location; Task 3 moves it.
- [ ] Commit any new characterization tests alone as `test: lock architecture refactor contracts`.

### Task 2: Extract Explicit Application Construction

**Files:** Create `internal/app/app.go` and `internal/app/app_test.go`; modify `cmd/porty/main.go` and `cmd/porty/main_test.go`.

**Interface:** `app.New(ctx context.Context, db *sql.DB, cfg config.Config) (http.Handler, error)` owns the current `buildHandler` wiring and readiness state. The executable keeps `resetPassword`, Git helper dispatch, listen settings, and shutdown.

- [ ] Add a failing `internal/app` test for a fresh installation that serves `/healthz`, `/readyz`, and `/api/v1/setup/status` through the embedded frontend root; add a test for a tampered ready repository that remains unready.
- [ ] Move the existing constructor statements into `app.New` in their current order. Keep startup recovery of interrupted operations and setup reconciliation; preserve safe degraded readiness behavior where the current server starts but reports unready.
- [ ] Change `cmd/porty` to call `app.New`; add `signal.NotifyContext` and a bounded `http.Server.Shutdown` path, with a focused lifecycle test. Do not change the binary name or reset-password semantics.
- [ ] Run `go test ./internal/app ./cmd/porty ./internal/httpapi`, then `go vet ./...`; commit as `refactor: move application wiring out of main`.

### Task 3: Move Technical Packages and Give Repository Setup One Owner

**Files:** Move `internal/infrastructure/sqlite/*` to `internal/sqlite`, `gitcli/*` to `internal/git`, `composecli/*` to `internal/compose`, `process/*` to `internal/process`, and `filesystem/*` to `internal/filesystem`. Move the relevant code and tests from `internal/application/repository.go`, `repository_setup.go`, and `internal/domain/repository*.go` into `internal/repository`; update imports in `internal/httpapi`, `internal/app/app.go`, and tests.

**Interface:** `repository` owns setup request types, classification errors, mutable live-client policy, and the narrow provisioner contract. `git` implements that contract and imports only `repository` types where required; `repository` does not import `git`.

- [ ] First move SQLite, Git, Compose, process, and filesystem directories to their direct `internal/` paths, updating import paths and package declarations. Keep logic unchanged in this mechanical commit. Run `go test ./...`; commit as `refactor: move adapters into direct internal packages`.
- [ ] After the moves, inspect each new package's exported API and imports. Merge any package that only forwards calls, shares all callers with a neighbor, or exists solely to mirror the old `infrastructure` tree; retain focused Git, Compose, process, filesystem, and SQLite packages only where they encapsulate real behavior.
- [ ] Add or retain tests for setup retry after provisioning, persistence failure without live-client replacement, managed-remote gating, SSH material handling, and secret-free status/error responses.
- [ ] Move types/errors and update Git signatures without changing command arguments, timeouts, authentication handling, or cleanup ordering. Break the current `git -> application` import before moving any other feature.
- [ ] Preserve transactionality in `RepositoryStore.Save` and `SaveRemote`; add a failing test for auth-row rejection if existing coverage does not prove the caller still sees the previous live client.
- [ ] Run `go test ./internal/repository ./internal/git ./internal/sqlite ./internal/httpapi ./cmd/porty`; commit as `refactor: cohere repository setup boundary`.

### Task 4: Move Auth, Stack, and Operation Behavior in Small Slices

**Files:** Move auth code and tests to `internal/auth`; move stack/workspace/environment code and tests to `internal/stack`; move operation/deployment/coordinator code and tests to `internal/operation`; move `internal/application/control_plane.go` and its tests to `internal/control`; adapt `internal/app/app.go`, SQLite and HTTP imports. Keep `internal/filesystem`, `compose`, `process`, and `websocket` focused on their existing behavior unless a concrete issue requires edits.

- [ ] Move `AuthService`, password hashing, and their tests to `internal/auth` as one tested change. Keep initial registration atomic. Run `go test ./internal/auth ./internal/sqlite ./internal/httpapi ./cmd/porty`; commit as `refactor: group authentication rules`.
- [ ] Add failing or retained tests for stack deletion shutdown order, filesystem rollback on failed DB writes, stale file hashes, symlinks/traversal, and repository-versus-stack lock conflicts.
- [ ] Move `StackService`, `WorkspaceService`, and `EnvironmentService` together so the filesystem and metadata coordination remains in one feature. Keep the environment-values method inaccessible to HTTP callers except through its current key-only path.
- [ ] Move operation execution, deployment recording, and coordinator as a second change. Keep accepted jobs detached from request cancellation, but bound them by their own timeout; retain redaction, output limit, and recovery of interrupted jobs.
- [ ] Move cross-feature action orchestration to one concrete `internal/control` type only if it remains cohesive after the stack and operation moves; otherwise keep that sequencing in the nearest owning feature. Remove the constructor's optional variadic log publisher and post-construction `ConfigureState` only after all call sites can pass explicit dependencies together.
- [ ] Run focused stack/operation/control, filesystem, Compose, HTTP, and WebSocket tests after each move; commit each independently testable slice as `refactor: group stack operations` and `refactor: group operation lifecycle`.

### Task 5: Replace the Custom Migration Runner with goose

**Files:** `internal/sqlite/db.go`, `db_test.go`, `migrations/001_initial.sql`; replace the initial migration with a goose-compatible migration in `internal/sqlite/migrations/`. Update `go.mod`/`go.sum` for goose.

- [ ] Add failing tests for schema creation from an empty database, idempotent second open, and failure cleanup. Assert the current tables, foreign keys, and `app_state` seed row.
- [ ] Convert the existing initial SQL to a zero-padded, ordered goose migration with an explicit `Up` section and a `Down` section usable for development reset. Keep the current schema in this task so the existing auth code still works. Embed and run goose migrations during `Open`; remove the custom `schema_migrations` loop and table creation.
- [ ] Document in `docs/development.md` that existing local `porty.db` files created by the old runner must be removed or recreated by the developer before starting this version. Do not implement an upgrade bridge or automatic database deletion.
- [ ] Check `PRAGMA foreign_keys`, `busy_timeout`, WAL mode, single-connection assumption, restrictive modes, and cleanup on migration failure.
- [ ] Run `go test ./internal/sqlite ./cmd/porty` and the full Go suite; commit as `refactor: use goose for fresh SQLite databases`.

### Task 6: Replace Saved Sessions with JWT Access and Revocable Refresh

**Files:** Add `internal/auth/token.go`, `key.go`, and their tests; change `internal/auth/auth.go`, `internal/sqlite/auth_store.go`, auth tests, `internal/httpapi/auth.go`, `internal/websocket/handler.go`, `internal/config/config.go`, `cmd/porty/main.go` reset-password path, `deploy/porty.example.yaml`, `web/src/api.ts`, `web/src/App.tsx`, and `web/src/stream.ts`. Add a zero-padded goose migration under `internal/sqlite/migrations/` to create `auth_keys` and `refresh_tokens` and drop `sessions`. Update `go.mod`/`go.sum` for `github.com/golang-jwt/jwt/v5`.

- [ ] Add failing auth tests for access JWT signature, HS256-only algorithm, issuer, audience, subject, issued-at/expiry, 15-minute lifetime, and key rotation. Use `github.com/golang-jwt/jwt/v5` with explicit valid-method, issuer, audience, required-expiry, and issued-at parser options; reject `alg=none` and tokens with missing required claims. Keep only user ID, username, and a CSRF binding in claims; put no credentials or repository secrets in a JWT.
- [ ] Add `auth_keys` as a single-row SQLite table for the active signing key. On a truly fresh installation before administrator registration, initialize it once from at least 32 bytes of `crypto/rand` output. After registration, fail closed if the row is missing or invalid rather than silently creating a new key. Load the current key for each access-token verification, so password reset and refresh revocation can share SQLite transaction boundaries. Test restart persistence, concurrent first initialization, missing/corrupt key, and failed rotation; do not store a second key copy in a file or configuration value.
- [ ] Add a goose migration for `refresh_tokens` with hashed-token primary key, user foreign key, family ID, original login time, expiry, CSRF hash, consumption time, and revocation time; remove `sessions`. Keep consumed hashes until the family's seven-day expiry so reuse is detectable, and delete expired families during existing auth writes rather than adding a background job. Add tests proving the new tables exist, the old one does not, and expired or revoked hashes cannot be exchanged.
- [ ] Replace `SessionRecord` and `SessionCredentials` with access/refresh credentials and an authenticated principal. Login and registration issue a 15-minute access JWT and random opaque refresh token; store only the refresh hash, user ID, CSRF hash, expiry, and revocation metadata in SQLite. Keep `porty_session` as the `HttpOnly` access cookie; add `porty_refresh` as a separate `HttpOnly` cookie scoped to `/api/v1/session`; retain `porty_csrf` as the readable CSRF cookie.
- [ ] Add `POST /api/v1/session/refresh`. Require expected Origin and matching CSRF cookie/header, then compare the CSRF hash with the stored hash for that refresh-token family. Atomically consume the presented hash and insert its successor; preserve the original seven-day absolute expiry. Two concurrent uses yield one success. If a consumed hash is reused, revoke every active token in its family in the same transaction and return the safe authentication error envelope without issuing cookies.
- [ ] Change logout to revoke the current refresh-token family and clear all three cookies. An already-issued access JWT can remain valid until its 15-minute expiry; document that limit. For password change and offline reset, update the password hash, revoke all refresh families, and rotate `auth_keys` in one SQLite transaction. On any transaction error, leave all three unchanged and return an error. Clear all three cookies after successful browser password change; keep login rate limiting and safe error responses.
- [ ] Add `server.public_url` to configuration for TLS reverse-proxy deployment. Validate a canonical origin with scheme and host but no path, query, or credentials; use it for Origin checks and `Secure` cookie policy. Preserve direct TLS and loopback development behavior when absent. Ignore untrusted `Forwarded` and `X-Forwarded-*` headers. Test HTTPS external origin with HTTP backend, invalid origins, and cookie flags for login, refresh, and clearing.
- [ ] Bound each authenticated WebSocket connection by the access JWT's expiry. Have the composition root wire a narrow post-commit key-rotation callback from auth to the WebSocket hub; close active connections before a successful browser password change returns. The offline reset command runs while the service is stopped as documented. Test that expired or key-rotated access cannot keep streaming, including an otherwise idle connection.
- [ ] Update the frontend to read the existing `porty_csrf` cookie after reload, call the refresh endpoint when `/session` returns 401, and retry an API request at most once after a single shared refresh attempt. Refresh before reconnecting a WebSocket closed for access-token expiry. Never put either token in `localStorage` or JavaScript state. Test reload, concurrent 401s, expired refresh, WebSocket reconnection, logout, and password change/reset behavior in Go and frontend tests.
- [ ] Run `go test ./internal/auth ./internal/sqlite ./internal/httpapi ./cmd/porty`, `npm --prefix web test`, and `npm --prefix web run typecheck`; commit the token exchange and frontend update together as `feat: use JWT access and revocable refresh tokens`.

### Task 7: Introduce sqlc Selectively

**Files:** Create root `sqlc.yaml` and `internal/sqlite/queries/*.sql`; generate into `internal/sqlite/generated/`; modify only stores whose query boilerplate actually shrinks. Pin the sqlc tool version and document the exact generation command in `docs/development.md` and CI.

- [ ] Point `sqlc.yaml` at the zero-padded goose migration directory as the SQLite schema source; do not maintain a second schema file. Pin one sqlc version for local generation and CI so output is reproducible.
- [ ] Start with read-heavy, stable operation/deployment/audit/stack queries. Name parameters and nullable overrides deliberately. Keep hand-written transactional repository-auth logic until generated code makes its boundary clearer.
- [ ] Add or retain store tests for null timestamps, byte secrets, pagination, missing rows, cancelled contexts, and affected-row checks before switching each store.
- [ ] Generate code from the migration schema, then replace handwritten scans/query text one store at a time. Use generated `Queries` directly inside the SQLite package; keep small store methods when they enforce transaction or error semantics. Use `WithTx` within a multi-statement operation, not as a generic transaction manager.
- [ ] Run the pinned `sqlc generate` command twice and verify generated files are unchanged. Add a CI check that generation leaves no diff under `internal/sqlite/generated/`; run `go test ./internal/sqlite ./internal/httpapi`; commit each useful store conversion. Stop converting when generated types or SQL make a query less clear.

### Task 8: Move HTTP Middleware and Finish Package Cleanup

**Files:** Move `internal/httpapi/*` to `internal/http/*`; add `internal/http/middleware/requestid.go`, `logging.go`, and focused tests; add `internal/http/auth_middleware.go`; move remaining `internal/application` and `internal/domain` files; update `internal/app/app.go`, `AGENTS.md`, `docs/security.md`, `docs/operator-guide.md`, and tests importing old packages.

- [ ] Keep the present safe error envelope and stable error codes. Add focused route tests only for any uncovered changed mapping; verify malformed JSON, oversized bodies, origin/CSRF denial, and raw database errors never appear in a response.
- [ ] Move the HTTP package to `internal/http` and update imports. Use an explicit alias for `net/http` where needed. Split `api.go` registration by existing route groups if the move reduces file size and keeps common middleware in one place. Keep one router construction entry and one error translation path.
- [ ] Move request ID generation into `internal/http/middleware`. Keep authentication and origin/CSRF checks as HTTP middleware functions in `internal/http/auth_middleware.go`, where they can use the existing safe error writer without a cross-package callback or import cycle. Keep repository-readiness guards next to the routes they protect; put the authenticated principal in request context for handlers. Keep public setup, health, readiness, and login routes outside authentication middleware; require a valid access JWT for protected HTTP and WebSocket handshakes.
- [ ] Add `slog` access logging middleware using `cfg.LogFormat` for text or JSON output. Record request ID, method, route/path without query string, status, duration, and remote IP. Never log cookies, Authorization headers, CSRF values, request/response bodies, or raw errors. Test that a secret in a query, cookie, and JSON body never reaches logs. Ensure the response wrapper preserves WebSocket upgrade and streaming behavior through `http.ResponseController`/`Unwrap` or equivalent forwarding; run the existing WebSocket journey.
- [ ] Move any remaining meaningful types from `internal/domain` and `internal/application` to their owning feature or `internal/control`. Remove obsolete aliases, forwarding services, and interfaces after `rg` finds no production callers, then delete the empty old directories.
- [ ] Update `AGENTS.md` to describe the final `internal/app`, feature, SQLite, Git, Compose, process, filesystem, `internal/http` auth guards, `internal/http/middleware` logging/request IDs, and WebSocket packages. Replace its `domain/application/infrastructure` guidance with the new package dependency direction, explicit construction, concrete-dependency preference, and JWT access/refresh ownership. Add the pinned sqlc generation command to build commands and document goose as the migration runner. Keep the existing security, test, frontend, and agent-use rules.
- [ ] Update `docs/security.md` and `docs/operator-guide.md` to describe the 15-minute access JWT, seven-day revocable refresh family, logout's remaining access-token lifetime, atomic signing-key/password/refresh rotation, WebSocket closure, cookie/CSRF handling, `server.public_url` for TLS termination, and the reset command's effect on other devices.
- [ ] Verify `rg -n 'internal/infrastructure|internal/domain|internal/application|internal/httpapi' --glob '*.go' --glob 'AGENTS.md'` has no live imports or stale architecture guidance after the final moves. Delete the empty old directories.
- [ ] Run `go test ./internal/http ./internal/app ./...` and `go vet ./...`; commit as `refactor: simplify HTTP and obsolete layers`.

### Task 9: Final Compatibility Gate

**Files:** `docs/development.md`, relevant package documentation, and `.github/workflows/verify.yml` for the pinned generation check. Frontend auth changes are covered in Task 6.

- [ ] Run the pinned sqlc generation command, inspect `git diff --exit-code -- internal/sqlite/generated`, and format only modified Go files with `gofmt -w`.
- [ ] Run `go test ./...`, `go test -race ./...`, `go vet ./...`, `go build ./cmd/porty`, `npm --prefix web test`, `npm --prefix web run typecheck`, `npm --prefix web run build`, and `./deploy/package_test.sh`.
- [ ] Build `/tmp/porty-e2e` and run `PORTY_E2E_BINARY=/tmp/porty-e2e npm --prefix web run test:e2e`. Run the live Docker/OCI gate only in an environment with Docker access.
- [ ] Compare routes and dependencies with Task 1. Confirm `AGENTS.md` matches the final package tree, no `internal/infrastructure` directory remains, and each remaining feature package has a distinct reason to exist. Review changed paths for security regressions and generated-file edits. Record any environment-gated verification and leave the implementation worktree clean.

## Sequencing Notes

Tasks 2–4 change Go ownership and imports; complete them before deleting old package scaffolding. Task 5 establishes goose before Task 6 changes auth tables; Task 7 then generates sqlc from the resulting migration schema. Each task should be one reviewable commit or a short series of store conversions. Avoid a single repository-wide rename, since it would hide the behavior changes that deserve review.

## Security Rationale

JWT access tokens remain valid until expiry after logout revokes their refresh family; the 15-minute lifetime limits that window. Password change/reset rotates the SQLite-held signing key in the same transaction as password and refresh changes, invalidating already-issued access tokens; authenticated WebSockets close at token expiry or after rotation. This addresses [OWASP's JWT revocation limits](https://cheatsheetseries.owasp.org/cheatsheets/REST_Security_Cheat_Sheet.html). Keep refresh-token family relationships long enough to detect reuse and revoke the active family, as described in [RFC 9700's refresh-token rotation guidance](https://www.rfc-editor.org/rfc/rfc9700.html#section-4.14). Validate the expected algorithm and required claims using [golang-jwt parser options](https://pkg.go.dev/github.com/golang-jwt/jwt/v5) and [JWT best current practices](https://www.rfc-editor.org/rfc/rfc8725.html). Bind CSRF values to the authenticated token pair, consistent with the [OWASP CSRF guidance](https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html). Mark cookies `Secure` when the validated public origin is HTTPS, as recommended by [OWASP session guidance](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html). Exclude tokens and request bodies from access logs, consistent with the [OWASP logging guidance](https://cheatsheetseries.owasp.org/cheatsheets/Logging_Cheat_Sheet.html). Use zero-padded goose migration names because [sqlc parses ordered goose migrations as schema input](https://docs.sqlc.dev/en/latest/howto/ddl.html).

## Planning Verification

This document is grounded in the current source and tests. The local `go test ./...` baseline could not run: this checkout's `/usr/local/go` reports Go 1.27.1 but lacks standard-library `testing` and `net/http/httptest` sources. Resolve that toolchain problem before executing the refactor or treat CI as the baseline gate. `Architecture_Plan.md` is currently untracked and was not modified.
