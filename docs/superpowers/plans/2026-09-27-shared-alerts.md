# Shared Alerts Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Repository instructions prohibit subagents; execute and review inline.

**Goal:** Give Porty one persistent place to acknowledge and resolve actionable failures from manual operations and, subsequently, automatic updates.

**Architecture:** `internal/alert` owns lifecycle rules and narrow persistence interfaces. Feature services emit typed results; SQLite commits terminal operation results and alert changes together. The existing HTTP and WebSocket infrastructure serves global and stack-scoped views of the same records.

**Tech Stack:** Existing Go, SQLite/Goose, Preact, Vitest, Playwright and WebSocket libraries. No new dependency for alerts.

**Spec:** [Approved design](../specs/2026-09-27-stack-auto-update-design.md), especially Shared alerts and API and UI.

## Global Constraints

- Implement this plan first; [scheduled updates](2026-09-27-stack-auto-update.md) consumes its contracts.
- No implementation has started. New symbols and files below are proposed contracts, not claims about existing code.
- Acknowledgment, resolution and auto-update resume are separate actions.
- Resolved but unacknowledged incidents remain in the badge and default list.
- Repeated unresolved failures retain acknowledgment; recurrence after resolution clears it.
- No continuous health monitoring, log scraping, external notifications or repository-operation alerts in v1.
- Preserve existing authentication, CSRF/origin, redaction, output bounds and lock ordering.
- Run GitNexus impact before each existing-symbol edit and detect-changes before every scoped Conventional Commit. Do not use subagents.
- For every task: write failing regression tests, demonstrate failure, implement, run affected tests, review diff, then commit. Do not batch all tests until the end.

## Review Focus

1. A stale successful operation must not resolve a newer failure (Tasks 1–2).
2. A database error must not publish a terminal result whose alert was never saved (Task 2).
3. Repeated reports after restart must not inflate counts or lose acknowledgment (Tasks 1–2).
4. A resolved incident must remain visible until acknowledged, including after reconnect (Tasks 3–4).
5. Archived/deleted stacks and operations outside the first history page must retain usable alert context (Tasks 3–4).

## Evidence and file boundaries

Source baseline: `fc9c509`, initially clean worktree. Prior graph inspection and current source confirm:

- `internal/operation/operation.go`: `Start` persists queued work; `execute` currently ignores running/final write errors and publishes results anyway.
- `internal/sqlite/operation_store.go`: `FailInterrupted` blindly fails queued/running rows. `internal/sqlite/db.go` sets one database connection: transaction helpers must use their transaction, never reenter the outer DB.
- `internal/control/control.go` and `containers.go`: producers know which operation is mutating and own coordinator releases; the operation layer cannot infer recovery from generic output.
- `internal/http/auth.go` owns RouterOptions/interfaces; `api.go` registers APIs; `auth_middleware.go` exposes authenticated principal through `principalFrom`.
- `internal/websocket/hub.go` already publishes typed topics; `handler.go` handles generic subscriptions. `web/src/features/operations/useOperationStream.ts` contains reusable reconnect/gap behavior.
- `web/src/app/Workspace.tsx` owns operation selection; `WorkspaceShell.tsx` owns navigation. Operation detail links need an ID fetch when a record is not already loaded.

GitNexus MCP transport was unavailable during planning; local CLI fallback succeeded. Upstream impact on `OperationService.execute` reports **CRITICAL**: `Start` is its direct caller, then stack/container/repository actions and their HTTP routes. Preserve repository/read-only operation behavior and include those regressions. Process indexing has traversal caps, so graph results are navigation evidence, not exhaustive coverage.

New files stay grouped by responsibility: domain lifecycle in `alert.go`/`types.go`, database adapters in `alert_store.go`, HTTP in `routes_alerts.go`, frontend under `features/alerts`. Avoid an event bus or workflow abstraction.

## Task 1: Persist the alert lifecycle

**Files:** Create `internal/alert/types.go`, `alert.go`, `alert_test.go`; `internal/sqlite/alert_store.go`, `alert_store_test.go`, `migrations/00005_alerts.sql`.

**Interfaces (new):**

```go
// package alert
type Key struct { StackID, Problem, Target string }
type Change struct {
    Kind string // "failure" or "recovery"
    Key Key
    OccurrenceID, OperationID, Summary string
    ExpectedRevision int64 // recovery requires the captured revision
    ObservedAt time.Time
}
type Mutation struct { ID string; ExpectedRevision int64; ActorID, Note string }
type Filter struct { StackID, View string; Limit, Offset int }
type Page struct { Items []Alert; UnacknowledgedCount int; Total int }
type Store interface {
    Apply(context.Context, []Change) ([]Alert, error)
    List(context.Context, Filter) (Page, error)
    Get(context.Context, string) (Alert, error)
    Acknowledge(context.Context, Mutation) (Alert, error)
    Resolve(context.Context, Mutation) (Alert, error)
}
```

Define `Alert` with ID, Key, stack-name snapshot, revision, episode number, first/latest occurrence, count, bounded summary, latest operation ID, acknowledged actor/time, resolved actor/time/reason and `CanResolveManually`. Use explicit JSON names. Define `ErrConflict`, `ErrNotFound`, `ErrManualResolutionUnavailable`. `Service` delegates validated user mutations and publishes only committed records; feature-result transactions use the same lifecycle helpers via SQLite.

- [ ] Write `TestAlertReopensUnacknowledgedAfterRecurrence`, `TestAlertPreservesAcknowledgmentWhileOpen`, `TestRecoveryRejectsStaleRevision`, `TestAlertIgnoresReplayedOccurrence`, `TestAlertSurvivesStackRemoval` in the new tests. Assertions: same key yields one alert; duplicate occurrence leaves count unchanged; failure/ack/recover/failure gives a new episode with no acknowledgment; a stale recovery changes nothing; resolved-unacknowledged count remains 1; deleting stack metadata retains its saved display name.
- [ ] Run `rtk go test ./internal/alert ./internal/sqlite -run 'TestAlert|TestRecovery'`; expect failure until contracts exist.
- [ ] Implement `alerts`, `alert_occurrences`, and `alert_events` tables. Unique Key and occurrence ID; integer revisions; lifecycle events retain prior acknowledgment/resolution across episodes. Keep history references without cascade deletion. Add indexes for stack/view and ordering. Store counts per episode and historical occurrences; use UTC timestamps. Bound summary to 4 KiB and note to 1 KiB; producer summaries are controlled/redacted text, not raw error output.
- [ ] Implement transaction-local apply/read helpers; all compare-and-set mutations and event recording are atomic. `View="attention"` means unresolved OR unacknowledged; other views are `open`, `unacknowledged`, `all`. Default page size 50, maximum 200. Pagination uses latest occurrence and ID as stable tie-breaker. Empty lists encode as `[]`.
- [ ] Run focused tests plus `rtk go test ./internal/sqlite`; verify migration from a pre-alert DB, rollback of partial writes and no deadlock with one connection.
- [ ] Review and commit: `feat: persist shared alert lifecycle`.

## Task 2: Connect manual operation outcomes atomically

**Files:** Modify `internal/operation/operation.go`, `types.go`, `operation_test.go`; `internal/sqlite/operation_store.go`, `operation_store_test.go`; `internal/control/control.go`, `containers.go`, `control_test.go`; `internal/app/app.go`. Create `internal/control/operation_alerts.go`, `operation_alerts_test.go`.

**Interfaces (new/additive):**

```go
// package operation; imports alert, which does not import operation
type Result struct { Output string; Err error; Alerts []alert.Change }
func (s *OperationService) StartTracked(ctx context.Context, req OperationRequest,
    run func(context.Context) Result, release func()) (Operation, error)
// OperationRepository gains:
CompleteOperation(context.Context, Operation, Result) ([]alert.Alert, error)
```

Extend OperationRequest/persisted operation metadata with stable `AlertTargets []alert.Key` and trigger (`manual`/`scheduled`) for restart recovery. Add these columns in migration 00005 while this plan is unshipped. Keep existing `Start` as a small adapter to StartTracked with no alert changes; callers without alerts keep current interfaces. Preserve `initiated_by` as a nullable user FK: do not put `"scheduler"` in it. Scheduled origin goes in trigger metadata.

- [ ] Add `TestOperationDoesNotRunWhenRunningWriteFails`, `TestOperationCommitsFailureAndAlertTogether`, `TestOperationDoesNotPublishUncommittedCompletion`, `TestInterruptedMutationRaisesAlertOnce`, `TestReadOnlyFailureDoesNotRaiseAlert`, `TestManualRecoveryDoesNotResolveUnrelatedFailure`. Check release runs exactly once after accepted work even on persistence failure; failed acceptance leaves release ownership with the caller. Preserve `TestOperationContinuesAfterRequestDisconnectAndRedactsOutput`.
- [ ] Run `rtk go test ./internal/operation ./internal/sqlite ./internal/control -run 'TestOperation|TestInterrupted|TestReadOnly|TestManual'`; confirm new cases fail.
- [ ] Implement CompleteOperation as one SQLite transaction using Task 1 transaction helpers. Persist the already-redacted Operation and typed Result.Alerts; never serialize Result.Err or unredacted Output. Stop discarding persistence errors: do not mutate runtime unless the running state is saved, and do not publish terminal success/failure before commit. Log a controlled persistence error and retain a nonterminal durable marker for reconciliation. Call release after finalization attempts. Publish alert changes only after commit through an injected publisher.
- [ ] In control producers, explicitly attach alert keys for deploy/recreate, start/stop/restart/pull and container actions. Use stable service+replica identity, not replaceable container ID, for grouped container failures; store the actual container ID as occurrence context. Return per-target changes for batch results. Validation/status/log operations, request rejection and repository operations produce none.
- [ ] Resolve only matching target/action problems after verifying the requested postcondition. For deployment recovery inspect expected inventory and health; a successful Up return alone produces no recovery change. Successful stop verifies stopped; start/restart verifies expected runtime; pull verifies completed transfer. Capture the alert revision before work, so late results cannot resolve a later failure. Failed operation status remains immutable when an alert resolves.
- [ ] Extend FailInterrupted to atomically mark interrupted mutations and create recovery-required alerts from saved targets, idempotently. Read-only interruptions can fail without alerts. Subsequent auto-update reconciliation (Plan 2) runs before this generic fallback and owns its richer recovery markers. Add fault-injection tests for transaction failure and restart after commit but before publish.
- [ ] Run `rtk go test ./internal/operation ./internal/sqlite ./internal/control ./internal/http ./internal/app`; run `rtk go test -race ./internal/operation ./internal/control ./internal/sqlite` where supported.
- [ ] Review and commit: `feat: report manual operation failures as alerts`.

## Task 3: Expose authenticated alerts and live invalidation

**Files:** Create `internal/http/routes_alerts.go`, `alerts_test.go`; modify `internal/http/auth.go`, `api.go`, `errors.go`, `internal/websocket/hub.go`, `hub_test.go`, `internal/app/app.go`.

**Interfaces:** RouterOptions gains Alerts interface with `List`, `Get`, `Acknowledge`, `Resolve` from Task 1. `Hub.PublishAlert(alert.Alert)` emits type `alert`, topic `alerts`. Service publisher has that one method.

**HTTP contracts:**

| Method/path | Contract |
| --- | --- |
| GET `/api/v1/alerts?stackId=&view=attention&limit=50&offset=0` | Task 1 Page; badge count is global, unaffected by stack/view/page filters |
| GET `/api/v1/alerts/{id}` | Alert plus paginated lifecycle history via `?limit=&offset=` |
| POST `/api/v1/alerts/{id}/acknowledge` | `{expectedRevision}`; updated Alert |
| POST `/api/v1/alerts/{id}/resolve` | `{expectedRevision,note?}`; updated Alert; manual resolution permitted only where producer marks it available |

Add `History(ctx,id,limit,offset) ([]Event,error)` to the alert Store/Service, defining Event with ID, alert ID, episode, kind, actor, timestamp, note, operation ID. Missing resource 404; stale revision 409; forbidden mutation 403; invalid filter/body 400. Actor always comes from `principalFrom`, never request JSON.

- [ ] Add HTTP tests `TestAlertMutationRequiresAuthenticatedOriginAndCSRF`, `TestAlertMutationRejectsStaleRevision`, `TestAlertActorComesFromSession`, `TestAlertBadgeCountIgnoresPagination`; hub test `TestAlertPublishesOnlyCommittedRevision`. Assertions include unknown JSON fields, foreign origin, no cookie, duplicate acknowledge on stale revision, and no side-effect calls to stack operations.
- [ ] Run `rtk go test ./internal/http ./internal/websocket -run 'TestAlert'`; observe failures.
- [ ] Wire read/mutation guards and audit acknowledgment/resolution. Keep alert access available even if repository readiness is broken by using authenticated guards, not repository-ready guards. The current model is a single administrator; do not invent per-stack roles. Validate bounded pagination and reject unsupported views.
- [ ] Implement authoritative HTTP reads and committed WebSocket events. Reconnect recovery reads HTTP; no new broker or durable delivery queue. On persisted-but-unpublished events, reconnect still recovers correct state.
- [ ] Run `rtk go test ./internal/alert ./internal/sqlite ./internal/http ./internal/websocket ./internal/app`; verify archived stack context and absent operation links remain readable.
- [ ] Review and commit: `feat: expose alerts with authenticated lifecycle actions`.

## Task 4: Add global and stack-scoped alert UI

**Files:** Create `web/src/features/alerts/{types.ts,api.ts,useAlerts.ts,Alerts.tsx,AlertList.tsx,Alerts.test.tsx,useAlerts.test.ts}`; modify `web/src/app/{Workspace.tsx,WorkspaceShell.tsx,styles.css}`, `web/src/components/Icon.tsx`, `web/src/features/stacks/StackDetail.tsx`, `web/src/features/operations/{api.ts,useOperationStream.ts}`. Create `web/src/hooks/useTopicStream.ts`, `useTopicStream.test.ts`; retain/update operation stream regression tests.

**Interfaces:** `useAlerts(stackId?: string)` exposes Page/loading/error and async acknowledge/resolve/reload. `AlertList` consumes records and callbacks; no direct fetching. `Alerts` is the `/alerts` page. `WorkspaceShell` receives `unacknowledgedAlerts: number`. Extract only the duplicated reconnect/subscription mechanics into `useTopicStream(topic,onEvent,onRefresh)`; keep operation-specific handling in its existing hook. `getOperation(id): Promise<Operation>` loads linked history outside the first page.

- [ ] Add tests named “keeps recovered incidents visible until acknowledged”, “shows the same alert revision globally and on its stack”, “reopens a resolved problem as unacknowledged”, “refreshes after a stream gap”, “does not overwrite a newer response with a stale request”, and “opens an older linked operation”. Verify ack changes badge but never calls resume/start APIs; unresolved acknowledged remains listed.
- [ ] From `web`, run `rtk bun run test src/features/alerts src/hooks/useTopicStream.test.ts src/features/operations`; expect new failures.
- [ ] Implement global navigation badge, attention/open/unacknowledged/history filters, pagination, stack links, operation drawer links, acknowledgment and optional resolution note. Disable stale mutation controls during requests; on 409 reload and explain that the alert changed. Show acknowledged/resolved actors and timestamps without treating resolution as a successful operation.
- [ ] Reuse AlertList on stack details; scope independent fetches and cancel/ignore stale requests on stack navigation. Use server counts, not visible-row counts. Alert features own their API calls/types. Add accessible labels and keyboard controls; reuse current styles rather than redesigning the app.
- [ ] Run `rtk bun run test`, `rtk bun run typecheck`, `rtk bun run build` from `web`; verify existing operation reconnect behavior remains intact.
- [ ] Review and commit: `feat: add unified alerts view and stack context`.

## Task 5: Exercise the complete manual alert lifecycle

**Files:** Create `web/e2e/alerts.spec.ts`; extend `internal/app/app_test.go`; update `docs/operator-guide.md`, `docs/development.md`.

**Interfaces:** Use the HTTP contracts from Task 3 and current Playwright desktop/mobile projects. No production API added solely for tests.

- [ ] Write an app integration test driving a controlled failing runtime through action acceptance, failed operation+alert persistence, acknowledgment, verified recovery and recurrence. Use a real temporary SQLite DB and stub Docker boundary. Restart wiring against the same DB and assert no duplicated occurrence.
- [ ] Write browser cases for global count, stack filtering, history, acknowledgment, stale writes and operation links. Use deterministic API/WebSocket fixtures for UI cases; app integration above proves persistence separately. Assert mobile actions remain usable and capture desktop/mobile screenshots.
- [ ] Run focused integration and browser cases; demonstrate meaningful initial failure before adding any missing behavior.
- [ ] Complete integration/wiring fixes only; document distinctions between status/history/alerts, recovery expectations and no automatic resume. Record screenshot paths for eventual PR.
- [ ] Verify `rtk go test ./...`, `rtk go vet ./...`, `rtk go build ./cmd/porty`; from `web`, `rtk bun run test`, `rtk bun run typecheck`, `rtk bun run build`, `rtk bun run test:e2e -- alerts.spec.ts`. Run `rtk ./deploy/package_test.sh` for the completed deliverable. Remove only generated build artifacts produced by verification.
- [ ] Run graph change analysis, inspect all intended changes, commit `test: verify shared alert lifecycle end to end`.

## Handoff

This plan delivers manual stack-operation alerts independently. Scheduled checks and deployments are added by the linked update plan. All task names/tests/interfaces here are planned additions unless explicitly identified as existing evidence. Plan review precedes execution; execute inline per AGENTS.md.
