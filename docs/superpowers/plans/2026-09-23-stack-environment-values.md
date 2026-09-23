# Stack Environment Values Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` to implement this plan task by task. Steps use checkbox (`- [ ]`) syntax. Repository instructions prohibit subagents.

**Goal:** Let an authenticated Stack administrator inspect each saved environment value through an explicit Show/Hide control while keeping every value concealed until requested.

**Architecture:** Keep the existing key-only list response. Add an authenticated per-key read that returns only a saved override and is never cached. In the Preact row, keep the fetched current value separate from the editable replacement, fetch on Show, and clear it on Hide or when the row ceases to be current.

**Tech Stack:** Go, SQLite, Preact/TypeScript, Vitest with Testing Library.

**Spec:** `docs/superpowers/specs/2026-09-23-stack-improvements-design.md`, **Environment values** section. Its other sections are outside this plan.

## Global Constraints

- `stack_environment` contains saved Stack overrides only. Compose interpolation defaults and service environment declarations are outside this feature; do not label them as current saved values.
- No stored secret classification exists. Mask every saved value by default, regardless of key name. A password input's bullets are a visual safeguard; fetched values are available to that authenticated browser.
- Preserve session and repository-ready guards, existing origin/CSRF protection for mutations, secret redaction, and the key-only list contract. Never place a value in a URL, audit event, log, or error.
- Add a failing regression test before each behavior change. Run focused and affected suites. Format Go with `gofmt` and frontend files with Prettier.
- Before editing each symbol, run GitNexus upstream `impact`; investigate HIGH/CRITICAL risks and confirm `UNKNOWN` with source search. Before any commit, run `detect_changes --scope all` and resolve partial or truncated results.
- Do not add dependencies or use subagents. Keep commits scoped with Conventional Commit messages.

## File Structure

- `internal/stack/stack.go`: Validate a requested key and read one value from the existing repository map, preserving the distinction between empty and absent.
- `internal/stack/workspace.go`: Expose that narrow read to the HTTP interface; the store already checks that the Stack exists.
- `internal/http/auth.go`, `internal/http/routes_environment.go`: Extend the consumer interface and register an authenticated, no-store per-key GET. The existing error mapper already maps `sql.ErrNoRows` to 404 and invalid keys to 400.
- `web/src/features/stacks/api.ts`: Typed per-key read client.
- `web/src/features/stacks/EnvironmentRow.tsx`: Current-value display, per-row Show/Hide state, and replacement editing.
- `web/src/features/stacks/StackSettings.tsx`, `web/src/app/styles.css`: Accurate copy and minimal row layout changes if needed.
- Colocated Go tests and `web/src/app/App.test.tsx`: Service, route, and user-visible state coverage.

## Review Focus

1. Empty saved value remains distinguishable from a missing key; Task 1 tests both.
2. Invalid key and absent Stack return safe errors without exposing any value; Task 1 tests both.
3. List, error, audit, and URL data remain value-free; Task 1 tests the route and response headers.
4. A late Show response cannot redisplay a value after Hide, update, deletion, Stack switch, or unmount; Task 2 tests the race.
5. A failed reveal or failed update cannot leave an old secret visible; Task 2 tests both.

---

### Task 1: Read One Saved Value Through the Authenticated API

**Files:** Modify `internal/stack/stack.go`, `internal/stack/workspace.go`, `internal/http/auth.go`, `internal/http/routes_environment.go`; test `internal/stack/stack_test.go`, `internal/stack/workspace_test.go`, `internal/http/api_test.go`.

**Interfaces:** Produce `(*EnvironmentService).Value(ctx context.Context, id StackID, key string) (string, error)` and `(*WorkspaceService).EnvironmentValue(ctx context.Context, id StackID, key string) (string, error)`. Extend `http.EnvironmentAPI` with `EnvironmentValue(context.Context, stack.StackID, string) (string, error)`. `GET /api/v1/stacks/{id}/environment/{key}` returns `{"value":"..."}` for an existing key, 400 for an invalid key, and 404 for an absent key or Stack. Its response includes `Cache-Control: no-store`. The list still returns keys only.

- [ ] **Step 1: Check impact.** Run upstream GitNexus `impact` for `EnvironmentService.Values`, `EnvironmentService.Keys`, `WorkspaceService.EnvironmentKeys`, `registerEnvironmentRoutes`, and `EnvironmentAPI`; record callers and risks. Resolve any `UNKNOWN` with `rg` before editing.
- [ ] **Step 2: Write failing service and workspace tests.** In `stack_test.go`, create a Stack and save `TOKEN=secret` and `EMPTY=`. Assert `Value` returns both exact strings, returns `sql.ErrNoRows` for `MISSING` and an absent Stack, and returns `ErrInvalidEnvironment` for `BAD-KEY`. In `workspace_test.go`, extend `TestWorkspaceResolvesOpaqueStackIDForFileAndEnvironmentOperations` to read back `TOKEN` via `EnvironmentValue`. These tests should fail to compile until the methods exist.
- [ ] **Step 3: Run red tests.** `rtk go test ./internal/stack -run 'TestEnvironmentValue|TestWorkspaceResolvesOpaque' -count=1`; expect missing-method failures.
- [ ] **Step 4: Implement the narrow read.** Use `environmentKey.MatchString(key)` before repository access. Call `s.store.Environment(ctx, id)`, then `value, ok := values[key]`; return `sql.ErrNoRows` when `!ok`, allowing `""` when `ok`. Have `WorkspaceService.EnvironmentValue` delegate to the service. Do not reuse `Values` in the HTTP handler or add a bulk-values endpoint.
- [ ] **Step 5: Run green service tests.** Repeat Step 3; expect pass.
- [ ] **Step 6: Write failing HTTP tests.** Use `authenticatedAPIRouter` with a focused fake `EnvironmentAPI` in `api_test.go`. Assert unauthenticated GET is 401; authenticated GET for `TOKEN` is `200` with exactly `{"value":"secret"}` and `Cache-Control: no-store`; `EMPTY` is `200` with `{"value":""}`; missing key/Stack is 404; invalid key is 400. Assert list output contains keys but no saved value. With the fake audit recorder, assert the secret is absent from audit action/target/outcome and error bodies. Assert GET requires no CSRF token, while existing PUT/DELETE guards remain covered by the HTTP suite.
- [ ] **Step 7: Run red HTTP test.** `rtk go test ./internal/http -run 'TestEnvironmentRead|TestStackEndpoints' -count=1`; expect the new route/contract assertions to fail.
- [ ] **Step 8: Add the route.** Extend `EnvironmentAPI`; register the per-key GET with `readRoute`, set `Cache-Control: no-store` before calling the service or writing JSON/error, and use `writeResult` with `{value: value}`. The existing `writeAPIError` maps `sql.ErrNoRows` and `ErrInvalidEnvironment`; do not include raw errors in the response. Keep the current list route unchanged.
- [ ] **Step 9: Run green HTTP and affected backend tests.** Repeat Step 7, then `rtk go test ./internal/stack ./internal/http ./internal/app -count=1` and `rtk go vet ./internal/stack ./internal/http ./internal/app`.
- [ ] **Step 10: Review and commit.** Run `rtk gofmt -w` on changed Go files, `rtk git diff --check`, and GitNexus `detect_changes --scope all`. Resolve findings, then commit only Task 1 files with `feat: add authenticated Stack environment value read`.

### Task 2: Show and Hide Saved Values in Stack Settings

**Files:** Modify `web/src/features/stacks/api.ts`, `web/src/features/stacks/EnvironmentRow.tsx`, `web/src/features/stacks/StackSettings.tsx`, and `web/src/app/styles.css` only if layout needs it; test `web/src/app/App.test.tsx`.

**Interfaces:** Consume Task 1's `GET /api/v1/stacks/{id}/environment/{key}`. Export `getEnvironmentValue(id: string, key: string): Promise<string>` from `api.ts`; parse `{value: string}` without treating an empty string as missing. `EnvironmentRow` continues to receive `name`, `stackId`, and `reload`.

- [ ] **Step 1: Check impact.** Run upstream GitNexus `impact` for `listEnvironmentKeys`, `EnvironmentRow`, and `StackSettings`. Confirm any `UNKNOWN` callers with `rg` before editing.
- [ ] **Step 2: Write failing user-visible tests.** In `App.test.tsx`, mock the per-key GET separately from the key-only list. After opening Settings, assert no GET has fetched `DATABASE_PASSWORD`, the current-value control is masked/empty, and the replacement input is blank. Click `Show DATABASE_PASSWORD`; assert one GET and a read-only current-value control containing the saved value. Click `Hide DATABASE_PASSWORD`; assert the value disappears and a second Show refetches. Test an empty saved value displays an explicit empty-state label, while a missing key shows an error. Keep the existing replacement-save assertion.
- [ ] **Step 3: Add lifecycle/race tests.** Use a deferred GET promise: click Show then Hide, resolve the promise, and assert no value appears. Repeat with update, delete, Stack change, and leaving Settings. Assert failed reveal leaves no value; failed update clears any previously revealed value while preserving an error message. Assert Show/Hide buttons never submit the replacement form. These tests should fail under the current UI.
- [ ] **Step 4: Run red frontend tests.** From `web`, run `rtk bun run test -- src/app/App.test.tsx`; expect the new assertions to fail.
- [ ] **Step 5: Implement the client and row.** `getEnvironmentValue` URL-encodes the key and returns the response's `value` directly. Store a row-local revealed value as `string | null` (`null` means concealed; `""` means revealed empty), plus loading/error state and a request generation guard. Show fetches on demand; Hide increments the generation and clears the value. Ignore late responses after a generation change or unmount. Clear/invalidate on save attempt, delete attempt, `name`/`stackId` change, and Settings unmount. Use `type="button"`, accessible Show/Hide labels, and a read-only current-value control with `type="password"` while concealed and `type="text"` while revealed. Keep the editable replacement field separate. Render the revealed empty string as an explicit `Empty value` cue. Render values as text or an input value, never HTML.
- [ ] **Step 6: Correct Settings copy and layout.** Replace the write-only claim with copy explaining that saved values are hidden until Show is selected. Label the display as a saved Stack value; do not claim to show Compose defaults. Add only the CSS needed to keep the display, toggle, replacement field, and actions usable on desktop and mobile.
- [ ] **Step 7: Run green frontend gates.** From `web`, run `rtk bun run test -- src/app/App.test.tsx`, `rtk bun run typecheck`, and `rtk bun run build`. Run Prettier on changed frontend files using the repository's configured command; inspect the rendered desktop and mobile Settings row if browser tooling is available.
- [ ] **Step 8: Review and commit.** Run `rtk git diff --check` and GitNexus `detect_changes --scope all`; resolve findings. Commit only Task 2 files with `feat: reveal saved Stack environment values on demand`.

### Task 3: Final Integration Check

**Files:** Change only tests or documentation proven stale by verification.

**Interfaces:** The key-only list, per-key reveal, and existing PUT/DELETE behavior work together.

- [ ] **Step 1: Run project gates.** `rtk go test ./...`, `rtk go vet ./...`, `rtk go build ./cmd/porty`; from `web`, `rtk bun run test`, `rtk bun run typecheck`, and `rtk bun run build`. Run relevant Playwright tests if the local browser/server setup is available.
- [ ] **Step 2: Review security behavior.** Confirm the list response has no values, per-key responses have `Cache-Control: no-store`, and no value reaches URLs, audit/error output, or rendered HTML. Confirm Hide clears UI state and a later Show refetches. Check that the replacement input stays blank until edited and empty saved values remain valid.
- [ ] **Step 3: Finish scoped review.** Run `rtk git diff --check`, `rtk git status --short`, and GitNexus `detect_changes --scope all`. Resolve any remaining finding before reporting completion.

## Execution Handoff

The requested deliverable is this plan. Implementation begins after the user reviews it. Use `superpowers:executing-plans` for inline execution; repository instructions prohibit subagents.
