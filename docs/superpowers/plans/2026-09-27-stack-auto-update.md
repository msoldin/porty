# Scheduled Stack Updates Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Repository instructions prohibit subagents; execute and review inline.

**Goal:** Automatically update opted-in, fully running Compose stacks on UTC cron schedules while preserving stopped state, data mounts and manual-operation coordination.

**Architecture:** `internal/autoupdate` owns policy, scheduled admission and outcomes, calling a narrow executor implemented by `internal/control`. The existing Compose adapter implements snapshot inspection and constrained recreation. Shared alerts and operation history persist failures and recovery, with durable intent before mutation.

**Tech Stack:** Existing Go/SQLite/Compose v5/Moby SDK and Preact stack; proposed `github.com/robfig/cron/v3` for parsing/next occurrence only.

**Spec:** [Approved design](../specs/2026-09-27-stack-auto-update-design.md).

**Dependency:** Complete [Shared Alerts](2026-09-27-shared-alerts.md) first. It defines `alert.Change`, `operation.Result`, `StartTracked`, atomic operation/alert completion and the global UI.

## Global Constraints

- Opt-in per stack. Default `0 0 * * *`, five fields, UTC only; no missed-run replay.
- Public registry tags only; no tag/version discovery, private credentials or automatic builds. Pinned/build/local/never-pull services stay untouched.
- Skip stopped, missing, partially running, unhealthy, archived, never-deployed or configuration-drifted stacks.
- Never automatically update the stack hosting Porty. Failure to establish protection blocks automatic execution.
- Only changed services may be recreated. Preserve scale and mounted volumes; no dependency starts/restarts, orphan removal, down, prune or rollback.
- Healthchecks must pass; other updated containers run continuously for 30 seconds. Verification timeout is five minutes.
- A failure after mutation starts pauses automatic updates; acknowledgment/resolution cannot clear this guard.
- Current code has process-local locks: one Porty instance manages a workspace/daemon. No distributed scheduler.
- Existing paths/types named as evidence are verified; new filenames, methods and tests below are proposed implementation contracts.
- Every task follows failing regression test → failure observed → implementation → affected tests → graph analysis → scoped Conventional Commit. Existing-symbol edits require GitNexus impact first. No subagents.

## Review Focus

1. A shared mutable tag changes during a check, or only another platform changes: no wrong image or unrelated retag (Task 3).
2. A later manual deploy accidentally restores an old cached tag after an automatic update (Task 5).
3. All schedules default to midnight; capacity limits and backward clock changes must not cause starvation or duplicates (Task 7).
4. Porty uses a custom image/hostname, shared mounts or a remote daemon: identification must not depend on image-name guesses (Task 2).
5. SDK service filters, anonymous volumes or dependency behavior cause unrequested changes despite green mocks (Tasks 4 and 9).

## Evidence and file map

Planning baseline: `fc9c509`; prior index/source exploration is recorded in the spec and linked alerts plan. Existing code needs these specific adaptations:

- `internal/compose/client.go` `up` reloads the project, allows builds and uses default StartOptions. Do not reuse it for updates.
- Installed Compose `v5.5.1/pkg/api/api.go` explicitly states CreateOptions.Services does not constrain project convergence, and StartOptions.Services does not constrain Start. Narrow the project model.
- Installed compose-go `v2.15.0/types/project.go` provides `WithSelectedServices(names, types.IgnoreDependencies)`, deep-copying and removing dependencies outside the selection. Compose CLI uses this for no-deps behavior. `CreateOptions.Inherit=true` preserves anonymous volume inheritance.
- Installed Moby client v0.5.1 exposes DistributionInspect, ImagePull with Platforms, ImageInspectWithPlatform and ContainerStatPath/CopyFromContainer. ImageInspectWithPlatform requires API 1.49; fail closed where image identity cannot be established on an older daemon.
- `internal/operation/deployment.go` computes the source Compose digest and saves deployment provenance; keep it separate from effective execution-image overrides.
- `internal/sqlite/deployment_store.go` has LatestDeployment and HasSuccessfulDeployment, but needs a latest-successful lookup with image provenance.
- `internal/app/app.go` closes Docker on root context cancellation, while operations use background contexts. `cmd/porty/main.go` only drains HTTP. Change ordering before starting a scheduler.
- `operations.initiated_by` is a user FK: scheduled trigger is separate metadata, never a synthetic user ID.

Source documentation: [cron parser](https://pkg.go.dev/github.com/robfig/cron/v3), [Compose up](https://docs.docker.com/reference/cli/docker/compose/up/), [dependency behavior](https://docs.docker.com/compose/how-tos/startup-order/). The installed SDK source determines Go options; CLI flags are not assumed to map directly.

Proposed files by responsibility: `autoupdate/{types,policy,schedule,scheduler}.go`; `control/auto_update.go`; `compose/{update_types,update_snapshot,update_images,update_apply,update_verify,self_guard}.go`; SQLite policy/run/image adapters; focused HTTP routes; a stack settings panel. Colocate tests. Do not split each method into its own file.

## Task 1: Store policy and scheduled-run state

**Files:** Create `internal/autoupdate/{types.go,policy.go,schedule.go,schedule_test.go}`, `internal/sqlite/{auto_update_store.go,auto_update_store_test.go,migrations/00006_auto_updates.sql}`; modify `go.mod`, `go.sum` during implementation only.

**Interfaces (new):**

```go
// package autoupdate; StackID fields use stack.StackID
type Policy struct {
    StackID stack.StackID
    Enabled bool
    Expression string
    Revision int64
    NextRunAt time.Time
    PausedReason string
}
type Run struct {
    ID string; StackID stack.StackID; PolicyRevision int64
    ScheduledAt time.Time; Phase, Outcome, Reason, OperationID string
}
type PolicyUpdate struct { Enabled bool; Expression string; ExpectedRevision int64 }
func NextRun(expression string, after time.Time) (time.Time, error)
type Store interface {
    GetPolicy(context.Context, stack.StackID) (Policy, error)
    SavePolicy(context.Context, stack.StackID, PolicyUpdate, time.Time) (Policy, error)
    ListDue(context.Context, time.Time, int) ([]Policy, error)
    Admit(context.Context, Policy, time.Time) (Run, bool, error)
    ResetAfterStartup(context.Context, time.Time) error
    PendingRuns(context.Context) ([]Run, error)
}
```

- [ ] Add `TestNextRunUsesMidnightUTC`, `TestNextRunRejectsUnsupportedExpression`, `TestAdmissionIsUniquePerStackAndOccurrence`, `TestPolicyRejectsStaleWrite`, `TestStartupSkipsMissedRuns`. Example assertions: after `2026-09-27T00:00:00Z`, default next is `2026-09-28T00:00:00Z`; six fields, `@daily`, `CRON_TZ=...`, zero-step and impossible dates fail; host timezone never changes results.
- [ ] Run `rtk go test ./internal/autoupdate ./internal/sqlite -run 'TestNextRun|TestAdmission|TestPolicy|TestStartup'`; expect new failures.
- [ ] Add reviewed/pinned cron v3 dependency; use `NewParser(Minute|Hour|Dom|Month|Dow)` after explicitly requiring five whitespace fields and rejecting timezone prefixes. Evaluate Next from UTC; reject zero/no-next results. Use standard cron day-of-month/day-of-week OR semantics. Check dependency size/license/transitives and record the selected version in the commit.
- [ ] Store policies and runs with unique `(stack_id,scheduled_at)`; atomic policy-revision admission and next-time advancement. No policy row means disabled/default expression. Admission when paused/disabled/stale returns false. Track latest result separately from deployment history. Persist queued/checking/prepared/applying/verifying/terminal phases; only applying/verifying imply possible mutation.
- [ ] Define `FinishRun(ctx,runID,outcome,reason,changes []alert.Change) error` and `Pause(ctx,stackID,reason) error` on the concrete store/consumer interfaces. Finish a check result and its alerts in one transaction. Policy deletion may follow stack purge; run/alert history retains stack-name snapshots.
- [ ] Verify focused tests and full `internal/sqlite` tests, including transaction failure and defaults on existing stacks; commit `feat: persist UTC cron update policies`.

## Task 2: Capture eligibility and protect the hosting stack

**Files:** Create `internal/compose/{update_types.go,update_snapshot.go,update_snapshot_test.go,self_guard.go,self_guard_test.go}`; extend `internal/compose/docker.go` and `internal/sqlite/deployment_store.go`/tests.

**Interfaces (new):**

```go
// package compose
type UpdateSnapshot struct {
    Project *types.Project // owned loaded snapshot; never serialized with secrets
    SourceDigest string
    Containers []UpdateContainer
    Excluded map[string]string
}
type UpdateContainer struct {
    ID, Service, ImageID, State, Health string
    Replica int; RestartCount int; StartedAt time.Time
}
func (c *Client) SnapshotUpdate(context.Context, Request) (UpdateSnapshot, error)
type SelfGuard interface { CheckProject(context.Context, string) error }
// package sqlite
func (s *DeploymentStore) LatestSuccessfulDeployment(context.Context, stack.StackID) (operation.Deployment, error)
```

- [ ] Add table-driven `TestSnapshotRejectsIncompleteRuntime` for no containers, missing replica, stopped, restarting, unhealthy, health-starting, paused, wrong project ownership and mixed replica images. Add `TestSnapshotExcludesUnsupportedImages` for build+image, local-only, digest pin, never-pull. Test expected replicas from Compose rather than actual list length; scale=0 is deliberately absent and never started. Record one-off containers separately and reject service_completed_successfully jobs/profile selections unsupported by the current loader.
- [ ] Add `TestSelfGuardRejectsHostingProject`, `TestSelfGuardFailsClosedWhenUnverifiable`, `TestSelfGuardDoesNotTrustImageOrHostname`, `TestSelfGuardProtectsAllMatchingProjects`. Run `rtk go test ./internal/compose ./internal/sqlite -run 'TestSnapshot|TestSelfGuard|TestLatestSuccessful'` to confirm failures.
- [ ] Load through existing rooted `Load`, then inspect owned runtime containers and their image identities. Preserve environment redaction. Reject malformed ownership, unknown runtime state and uncertain image identity. Compare source digest with latest successful deployment in the control task; do not overwrite or reinterpret this digest as an image fingerprint.
- [ ] Implement a runtime ownership proof that works with custom hostname/image and remote daemons: create a cryptographically random, mode-0600 marker in a private temporary directory for this Porty process, resolving its canonical absolute path. Through the Docker SDK, inspect that exact path in running containers, verifying marker content with a bounded archive read. Matching containers identify protected Compose projects through ownership labels. Never extract archives or read arbitrary paths. Multiple matches (shared mounts) protect all matched projects conservatively. A complete successful scan with no match proves this process is not inside a container on the target daemon; an unreadable/ambiguous scan blocks automation. Treat container disappearance during probing as a rescan, not evidence of safety. Remove marker on clean shutdown.
- [ ] Guard checks at enablement and immediately before mutation refresh container membership; confirm marker still exists and daemon identity has not changed. Cache only established protected IDs within the same process/daemon, never cache absence across execution checks. Missing marker, excessive/oversized response, unauthorized probe, missing project labels on a matching container or unknown daemon fails closed. Manual operations remain available.
- [ ] Test the marker proof on a disposable daemon with Porty running inside a custom-hostname container and from the host; do not replace failed proof with a name heuristic. If the daemon cannot support this proof, expose automation-unavailable rather than bypassing protection.
- [ ] Run focused tests, existing rooted-path/symlink tests and `rtk go test ./internal/compose ./internal/sqlite`; commit `feat: inspect update eligibility and protect Porty runtime`.

## Task 3: Resolve and stage public images immutably

**Files:** Create `internal/compose/update_images.go`, `update_images_test.go`; extend `update_types.go`, `docker.go`.

**Interfaces (new):**

```go
type ImageChange struct {
    Service, SourceReference, TargetReference, Platform string
    BeforeImageID, AfterImageID string
    ManifestDigest string
}
type PreparedUpdate struct { Snapshot UpdateSnapshot; Changes []ImageChange }
func (c *Client) PrepareUpdate(context.Context, UpdateSnapshot) (PreparedUpdate, error)
```

- [ ] Write `TestPreparePinsTagBeforePull`, `TestPrepareDoesNotRetagSharedImages`, `TestPrepareIgnoresOtherPlatformOnlyChange`, `TestPrepareRejectsPrivateAuth`, `TestPrepareDoesNotMutateContainersWhenPullFails`. Assert zero create/start/stop calls; a tag moving A→B after resolution still pulls A; same selected image yields no change; all candidate pulls finish before any recreation; configured inline credentials/helpers are unused.
- [ ] Run `rtk go test ./internal/compose -run 'TestPrepare'`; confirm failure.
- [ ] Use Moby DistributionInspect with empty auth to resolve immutable descriptor digest, then ImagePull by `repository@digest` with explicit platform and empty RegistryAuth/no PrivilegeFunc. Read/close the pull response to completion and recognize streamed errors. Reuse existing distribution/reference and platform packages already in the module graph. Normalize omitted tags to latest. No custom registry-auth client or shell commands.
- [ ] Inspect the pulled immutable reference for the selected platform and compare its verified runnable image/config identity against each actual container image. Keep index digest, platform manifest and runtime image ID distinct. If the daemon reports an index ID rather than selected-image identity, use the platform manifest data; reject unverifiable identity. A changed index for another architecture must not restart this service. Digest+platform fixes execution even when only an index reference is exposed.
- [ ] Bound each registry attempt by the caller deadline; at most three transient attempts with 1s/2s backoff. Respect bounded Retry-After; if it exceeds remaining check time, finish failed and wait for next schedule. Retry only transport/429/5xx; auth-required is unsupported, not retryable. Public anonymous token exchange through Docker is allowed. Stage by digest without changing local tags; no cache cleanup/pruning.
- [ ] Run focused tests and `rtk go test ./internal/compose`; include classic/containerd image stores in the live gate (Task 9), and commit `feat: prepare public image updates by digest`.

## Task 4: Recreate only selected services and verify outcomes

**Files:** Create `internal/compose/{update_apply.go,update_apply_test.go,update_verify.go,update_verify_test.go,update_integration_test.go}`; extend `update_types.go`.

**Interfaces (new):**

```go
type UpdateResult struct { Services []ServiceUpdateResult; RecoveryRequired bool }
type ServiceUpdateResult struct { Service, BeforeImageID, TargetImageID, ActualImageID, Outcome string }
func (c *Client) ApplyUpdate(context.Context, PreparedUpdate) error
func (c *Client) VerifyUpdate(context.Context, PreparedUpdate) (UpdateResult, error)
```

- [ ] Write `TestApplyNarrowsProjectBeforeUp`, `TestApplyPreservesAnonymousVolumes`, `TestApplyRejectsDependencySideEffects`, `TestVerifyRequiresThirtyContinuousSeconds`, `TestVerifyUsesFiveMinuteDeadline`, `TestVerifyChecksActualTargetImage`. Inspect SDK project contents, not just Services options. RestartCount/StartedAt changes reset the 30-second observation; unhealthy/exit/wrong digest fails; checks use injected clock/timer.
- [ ] Run `rtk go test ./internal/compose -run 'TestApply|TestVerify'`; confirm failures.
- [ ] Reject lifecycle hooks, links, volumes_from, shared service/container namespaces and restart-propagating dependency arrangements involving updated services until explicitly proven safe. Allow plain running dependencies without propagation after snapshot verification. Clone/narrow the project with `WithSelectedServices(changedNames, types.IgnoreDependencies)`; reject empty selection before calling SDK because empty means all services.
- [ ] Apply immutable image overrides only to the cloned selected project. Set no build definitions, PullPolicyNever, Create.Build=nil, RecreateDiverged, RecreateDependencies=RecreateNever, Inherit=true, RemoveOrphans=false and IgnoreOrphans=true. Preserve original scale and volume definitions. Start detached with the narrowed project; never activate disabled/profile services. Do not use existing Client.Deploy or reload mutable disk state.
- [ ] Verify actual selected service replicas, image identities, restart/health state and unchanged nonselected container IDs. Use a five-minute verification context after Up; no-healthcheck services need 30 continuous seconds. Return per-service results even on error. The executor, not this adapter, decides persistent pause/alerts.
- [ ] Add a gated disposable-Docker integration case using `PORTY_LIVE_DOCKER_CHECK=1`: start two services with named and anonymous marker data, update one, verify the other ID unchanged and both data markers retained. Stop a required service before execution and assert no new start. Include dependency and multi-replica fixtures. Cleanup only uniquely named test resources, never general prune.
- [ ] Run unit suites and the live test on a disposable daemon; if live validation is unavailable, keep this as an explicit unsatisfied release gate. Commit `feat: constrain and verify automatic service recreation`.

## Task 5: Coordinate durable update execution and manual redeploys

**Files:** Create `internal/control/auto_update.go`, `auto_update_test.go`, `internal/sqlite/update_execution_store.go`, `update_execution_store_test.go`; modify `internal/operation/{deployment.go,deployment_types.go}`, `internal/control/control.go`, `internal/compose/client.go`; extend migration 00006 while unshipped.

**Interfaces (new):**

```go
// package autoupdate
type Executor interface { CheckAndUpdate(context.Context, Run) error }
// package control
func (c *ControlPlane) CheckAndUpdate(context.Context, autoupdate.Run) error
type UpdateExecutionStore interface {
    SavePrepared(context.Context, autoupdate.Run, operation.Operation, compose.PreparedUpdate) error
    MarkApplying(context.Context, string, int64) error
    MarkVerifying(context.Context, string) error
}
```

PreparedUpdate includes a project with secrets: the SQLite adapter must persist only allowlisted intent fields (source digest/provenance, identities, selections), never serialize the project/environment. Persist selected images in `deployment_images`, and durable `update_executions` phase with source policy revision and operation ID. Extend `operation.Result` with `Update *UpdateCompletion`; define UpdateCompletion in operation with RunID, Deployment, Services (`[]compose.ServiceUpdateResult`) and PauseReason. Extend the existing CompleteOperation transaction to save optional update completion, deployment/image results, pause, run outcome, terminal operation and alerts together. SavePrepared links the already-created operation; it does not create a duplicate. A crash before final commit leaves durable execution intent for reconciliation.

- [ ] Add `TestAutoUpdateSkipsPendingConfiguration`, `TestAutoUpdateRevalidatesAfterPrepare`, `TestAutoUpdateDefersBusyStack`, `TestAutoUpdatePersistsIntentBeforeMutation`, `TestAutoUpdatePausesOnPartialFailure`, `TestAutoUpdateDisableDoesNotAbandonApply`, `TestManualRedeployKeepsEffectiveImage`. Assert stop/rename/delete/environment/policy changes during prepare cause discard; failed initial DB writes yield zero runtime calls; locks remain held through final outcome persistence.
- [ ] Run `rtk go test ./internal/control ./internal/sqlite ./internal/operation -run 'TestAutoUpdate|TestManualRedeploy'`; confirm failure.
- [ ] Capture policy, stack, environment, repository provenance, latest deployment and runtime snapshot while holding Coordinator.Try(false,stackID); check all eligibility, self-protection and latest-success guards. Release for image preparation. Reacquire and reload/revalidate all admission evidence. Compare container IDs/image IDs/state/restart identity and source digest; reject stale plans. Expected skips save a result without alerts/deployments. Registry exhaustion uses shared check alerts.
- [ ] After StartTracked accepts the operation and its callback begins, save intent and move prepared→applying by policy-revision compare-and-set immediately before SDK mutation; MarkVerifying follows successful apply. Add an optional bounded Timeout to OperationRequest so this operation receives the remaining scheduled-run budget (maximum 20 minutes) instead of silently inheriting the existing ten-minute timeout; other operations retain their default. Policy edits use the same revision gate so disable winning the race prevents apply. Once applying, disable affects future runs but execution completes verification. Use the release callback to retain the coordinator through the CompleteOperation transaction. Any error/timeout after applying pauses the stack and reports per-service state. No automatic rollback/retry.
- [ ] Store source Compose digest before overrides, effective per-service image references separately, and scheduled trigger provenance. Do not write `scheduler` into InitiatedBy. Resolve prior check alerts after a fully verified check, and deployment alerts only after verified equivalent runtime recovery. Unchanged checks must not overwrite deployment history.
- [ ] Prevent cached-tag rollback in subsequent manual deploy/recreate: use successful effective-image selections for unchanged tagged source references, including when other configuration changes. Explicit successful manual pull invalidates those selections for that stack; a changed source reference also invalidates its selection. Add an explicit `ImageOverrides map[string]string` field to DeployRequest and compose.Request, used only for execution, never in source Digest. Preserve selections through manual successful deployments; failed pull/deploy must not lose the last verified selection. Do not retag shared daemon references to solve this.
- [ ] Run `rtk go test ./internal/control ./internal/operation ./internal/compose ./internal/sqlite` and affected HTTP tests; include failure after Docker success but before DB commit. Commit `feat: coordinate durable automatic stack updates`.

## Task 6: Reconcile restarts and drain operations before closing resources

**Files:** Create `internal/control/update_recovery.go`, `update_recovery_test.go`; modify `internal/operation/operation.go`/tests, `internal/app/app.go`/tests, `cmd/porty/main.go`/tests, `deploy/systemd/porty.service`, `docs/operator-guide.md`.

**Interfaces (new):** `ControlPlane.ReconcileUpdates(ctx) error`; `OperationService.Shutdown(ctx) error`; `app.Application` implements `http.Handler` and `Shutdown(ctx) error`; `app.New` returns `*Application,error` (existing handler consumers still work).

- [ ] Add `TestRecoveryNeverReplaysInterruptedMutation`, `TestRecoveryPausesAmbiguousOutcome`, `TestRecoveryFinalizesCommittedSuccessOnce`, `TestShutdownDrainsBeforeDockerClose`, `TestShutdownRejectsNewWork`. Crash matrix: before intent, prepared, applying, verifying, result committed/operation nonterminal, alert committed/not published. Assert zero start/recreate/rollback calls during reconciliation and no duplicated alert counts.
- [ ] Run `rtk go test ./internal/control ./internal/operation ./internal/app ./cmd/porty -run 'TestRecovery|TestShutdown'`; confirm failure.
- [ ] Reconcile durable update executions before generic FailInterrupted. Prepared-only work ends interrupted without a deployment pause; applying/verifying becomes recovery-required unless persisted verified completion proves success. Inspect Docker to describe actual service state, but never infer historical five-minute verification from a one-time healthy snapshot. Keep ambiguity paused.
- [ ] Persist/propagate failure to reconcile as automation-unavailable; keep manual authenticated inspection available. On storage failure after mutation, stop automatic admission in memory, retain intent, and reconcile before reopening admission when storage recovers. Resume requires explicit request plus matching source/configuration and complete healthy runtime, and new verified baseline if the last attempt failed.
- [ ] Move Docker cleanup out of the immediate root-context goroutine. Stop scheduler admission, drain accepted operations up to 30 seconds, cancel outstanding workers, preserve recovery intent, then close Docker and finally database. Main invokes Application.Shutdown on every server exit path before DB close. Request cancellation still does not cancel accepted manual work. Guard WaitGroup/admission races with one lifecycle mutex. Document/set systemd stop timeout greater than the combined HTTP/drain budget; forced kill remains covered by recovery.
- [ ] Run `rtk go test -race ./internal/operation ./internal/control ./internal/app ./cmd/porty`, plus normal affected suites; commit `fix: reconcile updates and drain background operations`.

## Task 7: Run fair UTC schedules with no replay

**Files:** Create `internal/autoupdate/scheduler.go`, `scheduler_test.go`; extend `auto_update_store.go`/tests and app wiring.

**Interfaces (new):** `NewScheduler(store Store, executor Executor, clock Clock) *Scheduler`; `Run(ctx) error`; `Clock` supplies Now and cancellable timers for deterministic tests. `CheckAndUpdate` returns only after its run is terminal; await StartTracked completion through an explicit operation-service wait method `Wait(ctx,id) (Operation,error)`, never by spawning unbounded background work or sleeping on a database poll loop.

- [ ] Write `TestSchedulerRunsMidnightBatchFairly`, `TestSchedulerSkipsMissedRunOnRestart`, `TestSchedulerDoesNotDuplicateAfterClockRollback`, `TestSchedulerSkipsOverlap`, `TestSchedulerRejectsStalePolicyRevision`. Use five stacks, two workers; each due stack progresses, no stack has overlapping runs, no early/jittered launch, backward time never repeats an admitted occurrence. Forward clock jumps coalesce missed slots rather than replaying them.
- [ ] Run `rtk go test ./internal/autoupdate ./internal/sqlite -run 'TestScheduler'`; confirm failure.
- [ ] Start only after repository readiness and recovery. Reset next times strictly after startup, then wake for earliest due time or policy-change signal. Use two workers, stable due ordering and paginated reads. Persist admission before execution; advance next occurrence from current UTC time, not from repeatedly replaying missed slots. If prior same-stack work remains active, record skipped occurrence and advance.
- [ ] Bound each admitted run to 20 minutes: up to five minutes registry preparation, five minutes apply, five minutes verify and remaining time for coordination/persistence. A current due batch may wait for a worker within that deadline; expired slots become visible capacity skips, never late catch-up deployments. Rotate admission ordering fairly across batches so large repeated midnight batches cannot permanently starve the same stacks. Busy coordinator skips wait until next cron time. Persist failures/alerts after exhausted checks using a fresh bounded storage context when the execution context expires.
- [ ] Policy disable, revision changes and archive/purge invalidate queued work; scheduler teardown stops admission before OperationService shutdown. Do not start it for broken/unconfigured repository state or failed self-protection.
- [ ] Run scheduler tests with fake time, app startup/restart tests and race detector; commit `feat: schedule stack updates with UTC cron`.

## Task 8: Expose policy, results and explicit resume

**Files:** Create `internal/http/routes_auto_update.go`, `auto_update_test.go`; modify `auth.go`, `api.go`, `app.go`. Create `web/src/features/stacks/{AutoUpdateSettings.tsx,AutoUpdateSettings.test.tsx,useAutoUpdate.ts,autoUpdateApi.ts,autoUpdateTypes.ts}`; modify `StackSettings.tsx` and operation/deployment UI types/presentation as needed.

**Interfaces:** `PolicyService.Get(ctx,stackID) (Status,error)`, `Update(ctx,stackID,PolicyUpdate,actorID) (Status,error)`, `Resume(ctx,stackID,expectedRevision,actorID) (Status,error)`. Status includes Policy, latest Run, eligibility/exclusion reasons and auto-update availability. Resume delegates recovery validation to control through a narrow interface, and never deploys immediately.

| Method/path | Body/result |
| --- | --- |
| GET `/api/v1/stacks/{id}/auto-update` | Status; default disabled if no row |
| PUT `/api/v1/stacks/{id}/auto-update` | `{enabled,expression,expectedRevision}` → Status |
| POST `/api/v1/stacks/{id}/auto-update/resume` | `{expectedRevision}` → Status |

- [ ] Add `TestAutoUpdatePolicyRequiresAuthAndCSRF`, `TestAutoUpdateCannotEnableHostingStack`, `TestResumeRequiresVerifiedRecovery`, `TestAlertResolutionCannotResumeUpdate`; frontend tests “defaults to midnight UTC”, “shows next UTC run and skipped reason”, “keeps pause separate from acknowledgment”, “does not enable after stale policy response”. Verify malformed/impossible cron, unsupported timezone, missing/archived stack and 409 revision handling.
- [ ] Run focused backend and frontend tests; confirm new failures.
- [ ] Wire existing HTTP guards and actor audit. Reads never mutate Docker; policy enablement validates self-protection but may allow currently stopped stacks, showing they will skip until eligible. Enable/disable does not clear a pause. Resume with disabled policy can clear a verified recovery guard but must remain disabled; neither action launches an immediate check.
- [ ] Add the focused panel with enable checkbox, cron input, explicit UTC, next run, last outcome, exclusions, paused state and operation/alert links. Keep API calls in hook/module. Show a concise opt-in explanation: recreation loses writable-layer data; volumes are retained but migrations are not rolled back. No registry-credential or external-notification UI. Preserve current stack settings layout.
- [ ] Show per-service before/target/actual images for update operations from safe persisted history. Keep generic operation history functioning for manual/repository operations. Reuse alert UI from Plan 1; no second notification state.
- [ ] Run `rtk go test ./internal/http ./internal/control ./internal/app`; from `web`, run tests, typecheck and build. Commit `feat: add scheduled update settings and recovery controls`.

## Task 9: Prove end-to-end update safety and document operation

**Files:** Extend `internal/compose/update_integration_test.go`, `internal/app/app_test.go`; create `web/e2e/auto-update.spec.ts`; update `docs/operator-guide.md`, `docs/development.md`, `deploy/porty.example.yaml` only where actual new settings require documentation.

**Interfaces:** Use disposable Docker and an anonymous local registry fixture through the SDK, the production HTTP contracts and an injected scheduler clock. Never exercise these tests against user stacks.

- [ ] Add integrated scenarios: publish tag A and deploy; move to B; scheduled update changes only selected IDs; advance same tag again mid-pull and confirm frozen digest; fail one candidate pull before mutation; fail health verification after partial recreation; stop a service during preparation; edit environment/policy during preparation; restart at each persisted phase. Assert corresponding alert lifecycle and pause, source digest stability and no unintended starts.
- [ ] Add classic and containerd-store image identity cases, an index whose nonhost architecture changes, two stacks sharing a tag, built/pinned services, dependency propagation exclusions, named/anonymous volume markers, root/symlink rejection and Porty hosting protection with custom hostname. Test manual redeploy preserves B and successful explicit pull invalidates its effective selection. Verify image/volume cleanup only touches fixture resources.
- [ ] Add desktop/mobile browser coverage for policy editing, UTC next run, failed update alert → acknowledgment → manual recovery → explicit resume. Use API fixtures for browser timing; real app/Docker integration proves runtime behavior. Save desktop/mobile screenshots for the PR.
- [ ] Run new tests to expose missing behavior, then implement only corrections needed for the approved contract. Document five-field cron OR semantics, missed/capacity/busy skips, daemon capability requirements, public-registry exclusions, shared-mount self-protection limitations, pause/recovery, effective-image selection on manual deploy and deployment downtime/data limitations.
- [ ] Run `rtk go test ./...`, `rtk go vet ./...`, `rtk go build ./cmd/porty`; affected backend race suites; from `web`, `rtk bun run test`, `rtk bun run typecheck`, `rtk bun run build`, `rtk bun run test:e2e -- auto-update.spec.ts alerts.spec.ts`; `rtk ./deploy/package_test.sh`.
- [ ] On a verified disposable daemon run `rtk env PORTY_LIVE_DOCKER_CHECK=1 go test ./internal/compose -run 'TestUpdate|TestSelfGuard|TestPrepare' -count=1` including all newly named live cases. Record daemon API/storage mode, tests and result. Skipped live tests are not evidence of safety; this is a release gate.
- [ ] Review spec coverage, inspect scoped diff and GitNexus detect-changes output; commit `test: verify scheduled update safety end to end`.

## Implementation sequence and review

Execute the alerts plan first, then Tasks 1–9 here, inline. Do not enable scheduling before self-protection, constrained recreation, durable outcomes and restart recovery are verified. The marker-based self-protection and preserved effective-image selections are concrete implementation choices for the approved safety requirements; review them with the rest of this plan before coding. If live SDK behavior contradicts the constraints, stop that capability rather than relaxing stopped-state/data guarantees.

No product code, dependency installation or runtime mutation was performed while writing these plans. Detailed task execution, tests and per-symbol impact checks occur after plan approval.
