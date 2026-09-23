# Stack Improvements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Repository instructions prohibit subagents.

**Goal:** Make Stack settings, validation, overview state, action visibility, and logs accurately reflect stored data and live runtime behavior.

**Architecture:** Keep Stack input rules in a focused frontend module while Go remains authoritative. Extend the existing environment and Stack state contracts narrowly. Use the existing authenticated WebSocket route and hub for a bounded, reference-counted Compose log follower; keep UI connection lifecycle in a focused hook.

**Tech Stack:** Go, SQLite/sqlc, Docker Compose v5 SDK, Preact/TypeScript, Vitest with Testing Library, Playwright.

**Spec:** `docs/superpowers/specs/2026-09-23-stack-improvements-design.md`

## Global Constraints

- No product code is changed while writing this plan. During execution, add a failing regression test before each behavior change and run focused and affected suites.
- Preserve rooted-path and symlink checks, `If-Match`, stack/repository lock ordering, authentication, origin/CSRF enforcement, restrictive file modes, secret redaction, and bounded output.
- Backend validation remains authoritative. Do not introduce a dependency solely for frontend validation or log following.
- Before each symbol edit, run GitNexus upstream `impact` and report HIGH/CRITICAL or resolve UNKNOWN with text search. Before each commit, run `detect_changes --scope all`; partial or truncated results are unresolved. Do not use subagents.
- Format Go with `gofmt` and frontend files with Prettier. Keep commits scoped and the worktree clean.

## File Structure

- `internal/stack/stack.go`, `internal/stack/workspace.go`: narrow stored environment value read.
- `internal/http/routes_environment.go`, `internal/http/auth.go`, `internal/http/api.go`: value-read and active file-limit contracts; `internal/app/app.go` wires the limit.
- `web/src/stackValidation.ts`: Stack form input rules and messages; `Dashboard.tsx`, `Settings.tsx`, and `Editor.tsx` consume them.
- `internal/sqlite/queries/deployments.sql`, generated sqlc output, `deployment_store.go`: latest successful deployment query. `internal/control/control.go` and `types.go` own state aggregation; `web/src/api.ts`, `App.tsx`, `Dashboard.tsx`, `StackDetail.tsx` render it.
- `internal/compose/client.go`: Compose follow adapter; `internal/control/control.go`: Stack-scoped follow setup; `internal/websocket/logs.go`, `handler.go`, `hub.go`: one bounded follower per watched Stack, subscription ownership, and event delivery.
- `web/src/useStackLogs.ts`: Logs connection, replay, reconnect, and cleanup; `StackDetail.tsx` renders it.

## Review Focus

1. An empty stored environment value is distinguishable from a missing key, and neither appears in an audit/error URL payload. Test in Task 1.
2. A 64-byte or Unicode Stack name and an invalid file path never submit; the backend still rejects a direct request. Test in Task 2.
3. A successful deployment followed by a failed attempt still permits Stop/Restart only when runtime is running. Test in Tasks 3 and 4.
4. A secret split across log callbacks never reaches the hub or browser in raw form. Test in Task 5.
5. A slow, disconnected, or reconnecting log viewer cannot retain an unbounded follower or stale UI update. Test in Tasks 6 and 7.

---

### Task 1: Deliberate environment value reveal

**Files:** Modify `internal/stack/stack.go`, `internal/stack/workspace.go`, `internal/http/auth.go`, `internal/http/routes_environment.go`, `internal/http/api.go`, `web/src/Settings.tsx`, `web/src/ui.tsx`, `web/src/styles.css`; test `internal/stack/stack_test.go`, `internal/stack/workspace_test.go`, `internal/http/api_test.go`, `web/src/App.test.tsx`.

**Interfaces:** Add `EnvironmentService.Value(ctx context.Context, id StackID, key string) (string, error)` and `WorkspaceService.EnvironmentValue` with the same signature. Add `EnvironmentValue` to `http.EnvironmentAPI`. `GET /api/v1/stacks/{id}/environment/{key}` returns `{ "value": string }` and `Cache-Control: no-store`; the list endpoint remains `{ "keys": string[] }`.

- [ ] **Step 1: Run upstream impact** for `EnvironmentService.Values`, `WorkspaceService.EnvironmentKeys`, `EnvironmentAPI.EnvironmentKeys`, `registerEnvironmentRoutes`, and `StackSettings`. Record callers and risk before editing.
- [ ] **Step 2: Write failing Go tests.** `TestEnvironmentValueReturnsExistingAndEmptyValues` stores `TOKEN=secret` and `EMPTY=` and checks both reads; `TestEnvironmentValueRejectsInvalidAndMissingKeys` checks `../TOKEN`, absent key, and absent Stack. `TestEnvironmentReadRequiresSessionAndNeverCachesValue` checks 401, 200, 404, no-store, and absence of the value from list, audit, and error bodies.

  ```go
  got, err := service.Value(ctx, id, "EMPTY")
  if err != nil || got != "" { t.Fatalf("empty value: %q, %v", got, err) }
  _, err = service.Value(ctx, id, "MISSING")
  if !errors.Is(err, stack.ErrNotFound) { t.Fatalf("missing: %v", err) }
  ```

- [ ] **Step 3: Run** `rtk go test ./internal/stack ./internal/http -run 'TestEnvironmentValue|TestEnvironmentRead' -count=1`; expect failures for missing read behavior.
- [ ] **Step 4: Implement the narrow backend read.** Validate key with the existing expression, check Stack existence in workspace, look up the key with map membership (so empty differs from missing), and map `stack.ErrNotFound` to HTTP 404. Return only the selected value. Set `Cache-Control: no-store` before JSON or error writing; audit only the key and outcome, never the value.
- [ ] **Step 5: Write a failing Settings test.** Mock the keys list and per-key read. Assert the secret is absent initially, Show fetches and displays it, Hide clears it, and update/delete/tab change clear it. Include an empty value and a failed reveal without leaking prior data.
- [ ] **Step 6: Run** `rtk bun run test -- App.test.tsx` in `web`; expect the new test to fail. Implement a per-row displayed value state, a `type="button"` Show/Hide control with an accessible label and eye icon, separate replacement state, and request/error/cleanup guards. Replace the write-only explanatory copy with stored-value copy. Run the focused Go and frontend tests until green.
- [ ] **Step 7: Run `detect_changes --scope all` and commit** `feat: reveal stored Stack environment values`.

### Task 2: Match frontend input validation to backend rules

**Files:** Create `web/src/stackValidation.ts`, `web/src/stackValidation.test.ts`; modify `web/src/Dashboard.tsx`, `web/src/Settings.tsx`, `web/src/Editor.tsx`, `web/src/App.test.tsx`, `internal/http/auth.go`, `internal/http/api.go`, `internal/app/app.go`; test `internal/http/api_test.go`.

**Interfaces:** Export `validateStackName(name: string): string | null`, `validateEnvironmentKey(key: string): string | null`, `validateEnvironmentValue(value: string): string | null`, `validateStackPath(path: string): string | null`, `validateCommitMessage(message: string): string | null`, and `validateFileBytes(content: string, maxBytes: number): string | null`. Add `GET /api/v1/limits` returning `{ "maxEditableFileBytes": number }` from the active config.

- [ ] **Step 1: Run upstream impact** for `validStackName`, `cleanLocalPath`, `SerializeEnvironment`, `sdkCommit`, `Dashboard`, `StackSettings`, `Editor`, and `registerAPIRoutes`. Record risks and verify UNKNOWN references by text search.
- [ ] **Step 2: Write failing validator tests** for name length 1/63/64, uppercase, Unicode, slash, dot; environment key and empty/NUL value; empty/absolute/traversal/`.git` paths; whitespace/NUL/4096-byte/4097-byte commit messages; and UTF-8 file byte limits.

  ```ts
  expect(validateStackName("a".repeat(63))).toBeNull();
  expect(validateStackName("a".repeat(64))).toMatch(/63/);
  expect(validateEnvironmentValue("")).toBeNull();
  expect(validateStackPath("../outside")).toMatch(/relative/);
  expect(validateCommitMessage(" ")).toMatch(/message/);
  ```

- [ ] **Step 3: Run** `rtk bun run test -- stackValidation.test.ts` in `web`; expect failure for missing module/functions.
- [ ] **Step 4: Implement the small validators.** Use `TextEncoder` for byte limits, no path library dependency, and the exact backend name/key expressions. Reject `.` and `..` path components before normalization; reject `.git` at every level. Keep the backend's filesystem existence, symlink, and stale-write decisions server-side.
- [ ] **Step 5: Write failing form/API tests.** Create, rename, add/update environment, file create/move, commit, and file save must show specific errors and send no mutation when invalid. Test that `GET /limits` requires a session and returns the configured byte limit; use the returned value in Editor. Preserve an empty environment value; remove the Add form's `required` value constraint. Align the commit field with 4096 UTF-8 bytes instead of its current 200-character cap.
- [ ] **Step 6: Run focused tests to confirm failure, wire validators into submit handlers, and rerun.** Use `rtk go test ./internal/http -run TestLimits -count=1`; in `web`, use `rtk bun run test -- stackValidation.test.ts App.test.tsx`. Keep API errors visible for races or server-only checks.
- [ ] **Step 7: Run `detect_changes --scope all` and commit** `feat: validate Stack inputs before submission`.

### Task 3: Populate overview state from existing sources

**Files:** Modify `internal/sqlite/queries/deployments.sql`, generated `internal/sqlite/generated/deployments.sql.go` via `sqlc generate`, `internal/sqlite/deployment_store.go`, `internal/control/control.go`, `internal/control/types.go`, `web/src/api.ts`, `web/src/Dashboard.tsx`, `web/src/App.tsx`; test `internal/sqlite/deployment_store_test.go`, `internal/control/control_test.go`, `internal/http/api_test.go`, `web/src/App.test.tsx`.

**Interfaces:** Extend `StackState` JSON with `containers: {running: number, total: number}` and `lastDeployment?: {status: string, startedAt: string}`, plus `hasSuccessfulDeployment: boolean` for Task 4. Add `LatestSuccessfulDeployment(ctx context.Context, id stack.StackID) (operation.Deployment, error)` to `DeploymentStateStore` and `DeploymentStore`. Missing history is `sql.ErrNoRows`.

- [ ] **Step 1: Run upstream impact** for `ControlPlane.StackState`, `DeploymentStateStore.LatestDeployment`, `DeploymentStore.LatestDeployment`, `StackState`, `Dashboard`, and `Workspace.refresh`. Record callers and risk.
- [ ] **Step 2: Write failing store/control tests.** Save a successful deployment followed by a failed one; assert latest summary is failed while `hasSuccessfulDeployment` is true. Assert zero, all-running, mixed, and unhealthy Compose rows produce the right counts and runtime. Assert no deployment returns no summary and false; a real store error remains an error.

  ```sql
  -- name: GetLatestSuccessfulDeployment :one
  SELECT id, stack_id, operation_id, git_commit, dirty, diff_digest,
         compose_digest, status, started_at, completed_at, duration_ms, error_code
  FROM deployments WHERE stack_id = sqlc.arg(stack_id) AND status = 'succeeded'
  ORDER BY started_at DESC LIMIT 1;
  ```

- [ ] **Step 3: Run** `rtk go test ./internal/sqlite ./internal/control -run 'TestLatestSuccessful|TestStackState' -count=1`; expect failures. Add the query, run `rtk sqlc generate`, implement store and state mapping, then rerun.
- [ ] **Step 4: Write a failing Dashboard test** asserting actual `2/3 running`, latest failed deployment time/status, `No deployments` for absent history, and `Unavailable` when `/state` fails. Preserve the existing overview refresh path without additional per-row history requests.
- [ ] **Step 5: Run** `rtk bun run test -- App.test.tsx` in `web`; expect failure. Update types, state fetch handling, and cells; rerun frontend and affected Go tests.
- [ ] **Step 6: Run `detect_changes --scope all` and commit** `feat: show Stack deployment and container state`.

### Task 4: Gate Stop/Restart and remove Start Stack

**Files:** Modify `web/src/StackDetail.tsx`; test `web/src/App.test.tsx`, `web/e2e/critical.spec.ts`.

**Interfaces:** Consume Task 3's `stack.state.runtime` and `stack.state.hasSuccessfulDeployment`. Keep backend `POST /api/v1/stacks/{id}/actions/start` unchanged for API clients.

- [ ] **Step 1: Run upstream impact** for `StackDetail` and its `action` function. Record the Workspace caller and risk.
- [ ] **Step 2: Write failing visibility tests.** For `running + true`, Stop and Restart appear; for `running + false`, `stopped`, `partial`, `unhealthy`, and missing state they do not. Assert Deploy remains, Start stack is absent, and active operations disable visible actions.

  ```ts
  expect(screen.queryByRole("button", { name: "Start stack" })).toBeNull();
  expect(screen.queryByRole("button", { name: "Stop", exact: true })).toBeNull();
  // After a running state with a successful deployment, both appear.
  ```

- [ ] **Step 3: Run** `rtk bun run test -- App.test.tsx` in `web`; expect failures. Render Stop/Restart only under the exact state predicate and remove `start` from Overview actions. Keep current busy/archived/active disabling and dirty-editor protection.
- [ ] **Step 4: Rerun focused tests and update Playwright assertions** for the removed control. Verify direct API `start` remains authenticated and CSRF protected with existing HTTP tests.
- [ ] **Step 5: Run `detect_changes --scope all` and commit** `fix: show Stack actions for running deployments`.

### Task 5: Follow and redact Compose logs

**Files:** Modify `internal/compose/client.go`, `internal/control/control.go`, `internal/app/app.go`; create `internal/compose/log_follow_test.go`, `internal/control/log_follow_test.go`.

**Interfaces:** Add `Client.FollowLogs(ctx context.Context, request Request, tail int, emit func(string) error) error` and `ControlPlane.FollowStackLogs(ctx context.Context, id stack.StackID, emit func(string) error) error`. The latter resolves the Stack and stored environment, then invokes the Compose adapter. The callback receives bounded, redacted output only.

- [ ] **Step 1: Run upstream impact** for `Client.Logs`, `boundedComposeOutput`, `ControlPlane.StartAction`, `RuntimeController.Logs`, and `NewControlPlane`. Record callers and risk before edits.
- [ ] **Step 2: Write failing fake-Compose tests.** Assert `LogOptions{Follow:true,Tail:"500"}`, initial and later output, context cancellation, daemon error, oversized lines, and a secret split across callbacks or line boundaries. Include a multiline saved value. Assert no callback receives any complete raw secret and no callback output exceeds the event cap.

  ```go
  err := client.FollowLogs(ctx, request, 500, func(chunk string) error {
      if strings.Contains(chunk, "secret") { t.Fatal("unredacted log") }
      return nil
  })
  ```

- [ ] **Step 3: Run** `rtk go test ./internal/compose ./internal/control -run 'TestFollowLogs|TestFollowStackLogs' -count=1`; expect failure. Implement a streaming `api.LogConsumer` with per-container redaction windows long enough to cover the longest saved secret across arbitrary callback and line boundaries; only release text outside those windows after replacement. Bound buffered and emitted data, including final partial chunks. Cancellation must stop the SDK call promptly. Do not hold the operation coordinator during viewing or persist output as an operation.
- [ ] **Step 4: Rerun focused tests** and add a test proving Stack lookup errors never invoke Compose. Confirm the existing one-shot Logs action still works for API clients.
- [ ] **Step 5: Run `detect_changes --scope all` and commit** `feat: follow bounded redacted Stack logs`.

### Task 6: Own live log subscriptions on the WebSocket route

**Files:** Create `internal/websocket/logs.go`, `internal/websocket/logs_test.go`; modify `internal/websocket/handler.go`, `internal/websocket/hub.go`, `internal/app/app.go`, `internal/http/stream_test.go`.

**Interfaces:** Add `LogSource` with `FollowStackLogs(context.Context, stack.StackID, func(string) error) error`. Add a `LogManager` that acquires/releases one follower per Stack, caps active followers, retains the last 65,536 characters per Stack, and publishes bounded `log` events on `logs:{id}` through `Hub`. Each `log` payload contains the current bounded output window, so the UI replaces output rather than appending. `Handler` accepts the manager. Keep the existing `subscribe`/`unsubscribe` command shape and `sequence`/`gap` envelopes; send each new subscriber a `log-ready` envelope with the manager's current window and sequence after acquisition.

- [ ] **Step 1: Run upstream impact** for `websocket.Handler.ServeHTTP`, `Hub.Subscribe`, `Hub.PublishLog`, `Hub.CloseConnections`, and `app` stream construction. Record callers and risk.
- [ ] **Step 2: Write failing WebSocket tests.** Two viewers of one Stack share a follower; the last unsubscribe cancels it. A viewer of another Stack has separate output. Reject malformed/unsupported log topics; a slow viewer cannot grow a queue indefinitely. Reconnect replay returns ordered events or a gap, and auth/repository-ready/session-expiry checks remain in force.

  ```go
  // After two subscriptions to logs:stk_1:
  if source.starts != 1 { t.Fatalf("followers = %d", source.starts) }
  // After both unsubscribes:
  select { case <-source.cancelled: case <-time.After(time.Second): t.Fatal("leak") }
  ```

- [ ] **Step 3: Run** `rtk go test ./internal/websocket ./internal/http -run 'TestLogSubscription|TestAuthenticatedWebSocket' -count=1`; expect failures. Implement reference-counted acquisition, bounded active followers and hub replay, and cancellation on unsubscribe/disconnect. Acquire only after validating the `logs:{stackId}` topic and authenticated route; ensure a failed acquisition sends a safe error event. For a new subscriber, send `log-ready` with the current bounded output and sequence, then ignore replay events at or before that sequence; later `log` events replace the window. A reconnect after a hub gap receives the current window again and keeps a visible missed-lines warning. Keep operation subscriptions unchanged.
- [ ] **Step 4: Rerun focused tests** including race testing for subscriber cancellation: `rtk go test -race ./internal/websocket -run TestLogSubscription -count=1`.
- [ ] **Step 5: Run `detect_changes --scope all` and commit** `feat: manage live Stack log subscriptions`.

### Task 7: Open Logs automatically and recover connection state

**Files:** Create `web/src/useStackLogs.ts`, `web/src/useStackLogs.test.tsx`; modify `web/src/StackDetail.tsx`, `web/src/styles.css`, `web/src/App.test.tsx`, `web/e2e/critical.spec.ts`.

**Interfaces:** `useStackLogs(stackId: string, enabled: boolean)` returns `{ output: string, status: "connecting" | "loading" | "streaming" | "empty" | "reconnecting" | "error", gap: boolean, error: string }`. The hook owns its socket, sequence, timer, and cleanup. It retains at most the last 65,536 characters.

- [ ] **Step 1: Run upstream impact** for `StackDetail`, `useOperationStream`, `reduceStream`, and `api.refreshSession`. Record callers and risk.
- [ ] **Step 2: Write failing hook/page tests.** Opening Logs subscribes without a click; `log-ready` supplies the current output window or marks it empty; later `log` events replace the window; socket close enters reconnect with bounded backoff and session refresh; gap restores the current bounded window with a missed-lines warning; malformed/error events show an error; leaving Logs or changing Stack closes the socket and cancels timers. Test that late events cannot update the old Stack.

  ```ts
  fireEvent.click(screen.getByRole("tab", { name: "Logs" }));
  expect(socket.sent[0]).toContain('"topic":"logs:s1"');
  expect(screen.queryByRole("button", { name: "Load logs" })).toBeNull();
  ```

- [ ] **Step 3: Run** `rtk bun run test -- useStackLogs.test.tsx App.test.tsx` in `web`; expect failures. Implement the hook using the existing WebSocket envelope and session refresh pattern. Subscribe from sequence zero initially; on reconnect use last seen sequence. `log-ready` establishes the current bounded window and its sequence; ignore older replay, and replace the window on later `log` events. On `gap`, retain a warning and accept the manager's next current window. `log-ready` distinguishes empty from still loading.
- [ ] **Step 4: Replace the Logs snapshot UI** with status/output rendering and no Load logs button. Keep output as text in `<pre>`, not HTML. Update the Playwright journey with a mocked stream event and assert automatic output and cleanup.
- [ ] **Step 5: Run focused frontend and E2E tests.** `rtk bun run test -- useStackLogs.test.tsx App.test.tsx`; `rtk bun run test:e2e` in `web` where the local test server is available.
- [ ] **Step 6: Run `detect_changes --scope all` and commit** `feat: stream Stack logs on page open`.

### Task 8: Final integration verification

**Files:** Update only tests or docs that the final verification proves stale; do not broaden product scope.

**Interfaces:** All six requested changes are observable together. Existing direct API clients retain `start` and snapshot `logs` actions.

- [ ] **Step 1: Run formatting and focused verification.** `rtk gofmt -w` on changed Go files, `rtk bun run format` only if that script exists or run the project's Prettier command on changed frontend files. Run each task's focused suites once after integration.
- [ ] **Step 2: Run project gates:** `rtk go test ./...`, `rtk go vet ./...`, `rtk go build ./cmd/porty`, `rtk sqlc generate` followed by a clean generated diff, `(cd web && rtk bun run test)`, `(cd web && rtk bun run typecheck)`, `(cd web && rtk bun run build)`, and relevant `(cd web && rtk bun run test:e2e)` tests.
- [ ] **Step 3: Review security and lifecycle cases** from Review Focus against test names and actual output. Inspect browser network responses for environment value caching and raw log leaks. Check `rtk git diff --check` and the scoped worktree status.
- [ ] **Step 4: Run `detect_changes --scope all`** and resolve every HIGH/CRITICAL, UNKNOWN, partial, or truncated finding before the final commit. Commit any integration-only fixes with a scoped Conventional Commit message.

## Execution handoff

Use `superpowers:executing-plans` for inline implementation. Repository instructions prohibit subagents. The initial task remains plan-only; implementation begins only after a separate user request.
