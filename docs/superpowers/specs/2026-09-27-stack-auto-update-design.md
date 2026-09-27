# Scheduled stack image updates and shared alerts

Status: reviewed and approved on 2026-09-27; implementation has not started.

Implementation plans: [shared alerts](../plans/2026-09-27-shared-alerts.md), then
[scheduled stack updates](../plans/2026-09-27-stack-auto-update.md). Plans require
review before execution.

## Intent and agreed scope

Automatically check opted-in Docker Compose stacks for changed image content and
update eligible running services safely. Keep scheduling separate from execution
so future automation can invoke the same operation without requiring a procedure
engine now.

Provide one persistent Alerts area for actionable problems from manual and
automatic stack operations. Stack status describes current runtime state,
operation history records attempts, and alerts track problems needing attention.

Decisions agreed during discussion:

- Opt-in per stack, using cron syntax. Default: `0 0 * * *` in UTC.
- Skip missed schedules after downtime; do not deploy immediately on startup.
- Only fully running, healthy stacks qualify. Skip the whole stack if an expected
  service or replica is stopped or missing.
- Follow existing image tags. Do not discover version tags or modify Compose files.
  Digest-pinned images remain unchanged.
- Skip stacks with configuration or environment changes since their last
  successful deployment.
- Update eligible registry images in mixed stacks; leave built and pinned services
  untouched. Recreate only changed services, without starting dependencies.
- Public registries only. Private registry credentials and management are outside
  v1 scope.
- Failures before recreation may retry with backoff. Failures during recreation or
  health verification pause automatic updates until manual recovery.
- Existing healthchecks must pass. Containers without a healthcheck must stay
  running for 30 seconds. Verification has a five-minute overall timeout.
- The stack hosting Porty must never be updated through this feature.
- Shared alerts cover failed manual deployments/runtime actions, automatic update
  failures and interrupted operations requiring recovery. Acknowledgment is
  separate from resolution; neither action resumes automatic updates.

The detailed policies below make these decisions concrete for review.

## Existing architecture

Architecture inspection used the `/home/msoldin/porty` source and matching
GitNexus index at commit `16e70c14d11761a7f6ffdfaa04e20c0be524cad9`.

- `internal/control/control.go` coordinates runtime actions and deployment
  provenance through the operation and deployment services.
- `internal/operation/deployment.go` provides the shared in-process coordinator.
  Stack actions exclude conflicting actions on that stack; repository operations
  exclude stack operations. Workspace writes and container actions use it too.
- `internal/operation/operation.go` persists operation lifecycle and publishes
  updates. Its workers currently use independent background contexts.
- `internal/compose/client.go` wraps the Compose SDK. Ordinary deployment permits
  builds, starts services and removes orphans, so it is not the automatic update
  execution path.
- `internal/compose/project.go` loads rooted Compose projects and computes a
  configuration/environment digest. This is not a deployed image digest.
- `internal/control/state.go` aggregates existing containers. Automatic update
  eligibility additionally needs expected service and replica inventory.
- `internal/app/app.go` wires services and marks interrupted operations failed
  at startup. Automatic updates require additional reconciliation of actual Docker
  state before a later run.

## Boundaries

| Package | Responsibility |
| --- | --- |
| `internal/autoupdate` | Policy, cron scheduling, due-run admission, latest check state and pause state |
| `internal/alert` | Shared alert types, deduplication, acknowledgment and resolution lifecycle |
| `internal/control` | Stack eligibility, coordinator ownership and the controlled image-update operation |
| `internal/compose` | Public registry resolution, runtime image inspection, exact-digest pulls, constrained recreation and verification |
| `internal/operation` | Existing operation/deployment tracking, extended with update provenance and recovery information |
| `internal/sqlite` | Goose migrations and persistence implementations |
| `internal/app` | Wiring, scheduler lifetime, startup reconciliation and shutdown |
| `internal/http` | Authenticated policy/status contracts using existing guards |
| `web/src/features/stacks` | Auto-update settings and current result, linked to existing operation history |
| `web/src/features/alerts` | Global alert list, badge and reusable stack-scoped alert presentation |

Keep interfaces narrow and at their consumers. The scheduler invokes an explicit
stack image-update entry point; it does not call HTTP handlers or Docker directly.
Future triggers can call that entry point. Do not introduce generic actions,
user-defined conditions, scripts, workflow graphs or a separate generic job queue.

Use the existing Docker/Compose SDK integration. Prefer an established small cron
parser after dependency review; do not implement a cron parser from scratch.

## Scheduling and persistence

Accept standard five-field cron expressions with UTC evaluation. Do not support
seconds, per-expression timezone overrides or interval aliases in v1. Validate on
the server and display the next scheduled UTC time in settings.

Persist per-stack policy: enabled, expression, revision, next scheduled time,
latest check outcome and pause reason. Existing stacks default to disabled.
Persist a unique identity for each admitted stack/scheduled-time occurrence so an
occurrence cannot be admitted twice. Keep operation history as the execution
record rather than introducing a competing history system.

At startup, after reconciliation, calculate the next occurrence strictly after
startup time. Enabling or changing a schedule likewise selects a future occurrence.
Do not backfill missed runs or shift successful checks to an interval schedule.

Use bounded concurrency across checks and updates, with at most one run per stack.
Process stacks due at the same time as a bounded batch; admission must be fair so
the common midnight schedule does not starve stacks. A stack busy with another
operation is recorded as skipped and reconsidered at its next scheduled run. Do
not retain a deployment retry queue beyond the current scheduled batch.

Transient registry inspection/pull failures may retry with bounded backoff within
the current check deadline. Exhausted attempts wait for the next cron occurrence.
Authentication failures are unsupported, not transient retries. No retry after
recreation begins, including after a timeout. Paused stacks require an explicit
resume after manual recovery and renewed eligibility validation.

Disablement or policy revision invalidates pending work before recreation. If
recreation has already begun, finish verification and record the result; disabling
the schedule must not abruptly abandon a deployment.

## Eligibility and image selection

Require an active, previously successfully deployed stack with verifiable current
configuration. Its loaded configuration/environment digest must match the latest
successful deployment, and unresolved failed/interrupted deployment state blocks
automatic execution. A matching Git commit alone is insufficient.

Inspect the expected active service set, replicas, Compose ownership labels and
actual containers. Require every expected replica to exist and be running, with
healthy healthchecks where present. Ignore unrelated one-off containers, but do
not treat stopped expected job services as healthy long-running services. Skip
unsupported job/profile/dependency arrangements with a visible reason. Never
infer completeness solely from the containers returned by Docker.

Identify the running Porty container and its Compose project using runtime
identity and ownership metadata, not an image-name heuristic. Block its whole
stack at policy enablement and again immediately before execution. If Porty is
containerized and its identity cannot be established reliably, automatic
deployment must remain unavailable until identity is established. A verified
native host process has no hosting Compose stack. The implementation plan must
specify and test this identity mechanism before enabling automatic execution.

Eligible images have a registry tag and no build definition. Exclude digest-pinned
references, build-defined services (including `build` plus `image`), local-only
images and services with `pull_policy: never`. Show exclusions. If no eligible
services remain, report a skip.

Resolve public images without using configured private credentials or executable
credential helpers. Public anonymous bearer-token exchanges remain supported.
If a candidate requires authentication, report it as unsupported and skip the
stack's update attempt rather than performing a partially resolved update.

Resolve the effective platform, including an explicit service platform when
present. Compare the selected platform image with the image actually used by each
running replica. Image IDs, manifest digests and multi-platform index digests are
different identities and must not be directly compared as interchangeable values.
Unknown identity or divergent replicas blocks automatic execution until resolved.

A changed tag means different content, not proof of a newer or compatible release.
Freeze selected image references to immutable digests for this operation. Pull by
digest so checks do not retag shared local images and affect unrelated stacks.

## Execution protocol

1. Under the coordinator, capture policy revision, stack/project identity, loaded
   configuration digest, expected inventory and running container/image identities.
   Validate eligibility, then release the lock for registry work.
2. Resolve candidates and pull every required digest with bounded timeouts. If any
   required resolution or pull fails, do not mutate containers. An unchanged check
   does not create a deployment record.
3. Reacquire the existing stack coordinator. If busy, skip. Revalidate the policy,
   stack identity, configuration, runtime inventory, dependency safety and Porty
   exclusion. Discard stale candidates rather than silently applying a new plan.
4. Durably record operation intent, configuration provenance, selected services,
   old container/image identities and target digests before runtime mutation. A
   failed persistence write blocks execution.
5. Use one validated project snapshot and in-memory digest overrides. Recreate
   only changed services while preserving service scale. Disable builds, implicit
   pulls, dependency starts/restarts, orphan removal and volume renewal. Do not
   run down or prune. Do not rewrite tracked Compose files or environment values.
6. Verify all affected replicas and inspect the rest of the stack for unexpected
   changes. Defined healthchecks must pass; other updated containers must remain
   running for 30 continuous seconds. Apply a five-minute verification timeout
   after recreation, within an operation deadline that accommodates this phase.
7. Persist per-service outcome and actual resulting image identities. Mark success
   only after verification. On any failure after recreation begins, pause the
   stack and record recovery required, even if some services succeeded.

Hold the coordinator through recreation, verification and outcome persistence.
Retain the existing repository/stack lock ordering and conflict response semantics.
Manual operations cannot preempt an update that already holds the lock.

Dependency handling must account for more than `depends_on`: shared service
namespaces, dependency restart propagation and Compose lifecycle hooks can also
create side effects. If constrained execution cannot preserve the untouched
services, skip with an explicit unsupported reason. Validate SDK behavior through
real Docker integration tests before supporting an arrangement.

External Docker commands and direct filesystem changes bypass Porty's coordinator.
Revalidate observable state before mutation and use the captured project, but do
not claim atomicity against an external actor. Reliable operation assumes exclusive
management of these stacks through Porty during updates.

## Failure, shutdown and data protection

Compose updates are not transactions. Pulling all candidates first reduces failure
exposure but cannot guarantee an all-or-nothing recreation. Do not automatically
rollback: an application may already have migrated persistent data.

Stop admitting runs during shutdown. Give active updates a bounded drain period
before closing Docker/database resources. Persist execution phase so a forced
shutdown can be distinguished from a check that never mutated containers.
Reconcile interrupted attempts against Docker on startup without starting,
recreating or rolling back containers. Ambiguous or partially applied state pauses
the stack; merely marking the operation failed is not sufficient recovery.

Preserve bind mounts, named volumes and existing anonymous volumes. Retain old
images for manual recovery; automatic pruning is outside scope. Container writable
layer data is lost during recreation, and retaining volumes cannot prevent an
application from making destructive changes. Explain this when enabling updates;
this feature provides neither backups nor a zero-data-loss guarantee.

If final history writes fail, block further automatic updates in memory and
attempt to persist recovery-required state when storage becomes available. The
intent recorded before mutation remains the durable recovery marker. On restart,
any nonterminal execution intent is reconciled before more automatic work. Never
report a successful deployment solely because the SDK call returned successfully.

The coordinator is process-local. V1 assumes one Porty process manages a Docker
daemon/workspace; distributed scheduling and multiple independent writers are
outside scope.

## Shared alerts

Persist alerts in SQLite and expose one consistent lifecycle for manual and
automatic operations. Feature services report typed failures and verified recovery
through a narrow alert interface; the alert package does not interpret arbitrary
logs, poll Docker or own deployment policy. Continuous health monitoring, external
delivery channels and alerts for unrelated features are outside v1. Future
features can use the same interface and lifecycle.

Create alerts for:

- Failed manual deployments and accepted mutating runtime actions, including
  image pulls and partially failed container batches.
- Automatic registry checks or pulls that fail after bounded retries, deployment
  failures, and health verification failures.
- Interrupted mutating operations where reconciliation requires manual recovery.

Expected eligibility skips, operation conflicts and invalid form input remain
contextual messages. An auth-required image encountered in an enabled automatic
check creates an unsupported-image alert after that check, since the user must
change the configuration or disable the policy. Other documented exclusions stay
visible in stack settings. Do not generate an alert for each internal retry.

Group recurring failures by stable stack ID, problem type and affected target
where needed (for example service and intended action). Manual and scheduled
attempts describing the same problem share the key; trigger source is provenance,
not a separate alert category. Distinct failures on different targets must not be
merged solely because their text matches. Store a bounded redacted summary, first
and latest occurrence, count, latest operation link when available, acknowledgment
actor/time and resolution actor/time/reason. Use stable occurrence identifiers so
replayed reports cannot inflate counts.

Acknowledgment and resolution are independent:

| Event | Result |
| --- | --- |
| First failure | Open and unacknowledged |
| Repeated unresolved failure | Update latest occurrence/count; retain acknowledgment |
| Acknowledge | Record actor/time; leave problem open |
| Verified equivalent recovery | Resolve automatically; retain acknowledgment state |
| Mark resolved | Resolve manually with actor/time and optional note |
| Failure after resolution | Reopen, clear acknowledgment and record a new episode |

Retain lifecycle history across reopening, including previous acknowledgments and
resolutions. A failure which resolves before anyone sees it remains unacknowledged
until acknowledged, so unattended incidents do not disappear silently. The global
badge counts unacknowledged alerts, including these resolved incidents. The default
list includes open or unacknowledged alerts; filters expose full history.

Feature producers determine recovery evidence for the same target and action.
A successful registry check can resolve a registry-check failure; a successful
log request cannot resolve a deployment failure. Failed manual deployments require
a subsequent successful deployment and verified runtime recovery, not merely a
successful SDK return. Stale success reports must not resolve a newer failure;
use occurrence/revision checks for both producer and user lifecycle writes.

Offer Mark resolved for conditions Porty cannot verify, with an optional note.
If the same failure is observed again, reopen it unacknowledged. Acknowledgment
and manual resolution never start containers, retry operations, change historical
operation outcomes or clear auto-update pause/recovery guards. Resume remains a
separate action requiring recovery and eligibility verification.

Save an operation's terminal failure and its corresponding alert atomically where
they share SQLite persistence. Scheduled checks without a deployment still need a
durable failure result and alert. Startup reconciliation must report interrupted
work idempotently. If storage is unavailable, retain the existing execution intent
as the recovery marker and report persistence failure; do not silently treat an
in-memory alert as durable.

## API and UI

Add focused stack settings for enablement, cron expression, explicit UTC display,
next scheduled time, last checked time/result and pause/resume. Show skip and
unsupported reasons, including Porty self-update exclusion. Link executions to the
existing operation UI, with service-level old/target/observed image information.

Add a global Alerts entry and unacknowledged count. Stack details show the same
alerts filtered by stack, with links to operation history and clear recovery
guidance. Provide acknowledge and, where applicable, mark-resolved actions; show
who acted and when. Keep auto-update pause/resume separate from alert controls.
Publish alert changes through the existing WebSocket infrastructure and reload
authoritative state on reconnect. Preserve historical context when a stack is
archived or deleted; unavailable stack links must not break the alert history.

Keep policy revision checks at the API boundary to reject stale writes. Reuse
authentication, CSRF/origin guards, secret redaction and bounded output. Record
scheduled provenance and policy edits without impersonating an interactive user.
Registry errors must never expose credentials or be reported as up to date.
Alert reads and mutations use the same authorization boundaries as their affected
resources. Alert acknowledgment/resolution records the authenticated actor and
rejects stale revisions; alerts never contain environment or credential values.

## Verification required before implementation completion

Add failing regression tests before behavior changes, then focused and affected
suites. Cover:

- Cron validation, midnight UTC, next occurrence, duplicate admission, schedule
  edits, disablement, missed runs, busy skips and restart without catch-up.
- Stopped/missing/partial/unhealthy replicas, archived and never-deployed stacks,
  completed jobs, pending configuration/environment and excluded Porty stacks.
- Public anonymous resolution, auth-required rejection, rate limits/timeouts,
  mutable-tag races, platform selection, shared tags, pinned/build/local images.
- Pre-pull failure without recreation; exact-digest execution despite tag changes;
  stale policy/configuration/runtime snapshots; manual and repository conflicts.
- Partial recreation, health failures, process restarts during every phase,
  persistence failure, no automatic rollback/replay and explicit recovery/resume.
- Docker integration proving unchanged services stay untouched, no dependency or
  stopped-container starts, preserved replicas and named/anonymous volume data,
  no builds/orphan removal/pruning, and health verification behavior.
- Authorized settings changes, CSRF/origin enforcement, stale writes, rooted paths,
  symlinks, redaction and bounded registry/error output.
- UI schedule validation, UTC/next-run display, disabled/paused/unsupported states
  and operation links, with desktop/mobile browser coverage for visible changes.
- Shared manual/automatic alert production, target-aware grouping, retry/replay
  deduplication, independent acknowledgment/resolution, retained history, reopening
  and stale success rejection.
- Atomic failure/alert persistence, restart recovery, storage failure, stack
  deletion, and acknowledgment or resolution never resuming updates or altering
  operation outcomes.
- Global badge and stack-list consistency, resolved-but-unacknowledged visibility,
  authenticated actor attribution, stale mutations and WebSocket reconnect.

## Alternatives and future extension

A timer inside ControlPlane would mix scheduling with runtime coordination. A
general workflow engine would add persistence and configuration concepts without
v1 consumers. A focused auto-update service with an explicit controlled operation
provides a later extension point for conditional starts or procedures while keeping
current responsibilities small.

Private registries, semantic-version selection, dependency restarts, automatic
rollback, backup hooks, self-updates and general procedures remain outside v1.
Alerts add no continuous health polling, email, webhook or other external delivery
in v1; later delivery channels can consume the same alert lifecycle.
