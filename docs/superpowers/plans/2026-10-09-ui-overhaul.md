# Porty UI Overhaul Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` to implement this plan task by task. Implement and review inline; the repository explicitly prohibits subagents. Steps use checkbox syntax for tracking. Read the approved specification before execution.

**Goal:** Deliver the approved Soft contrast interface with clear runtime/Git/deployment states, scoped actions, reliable feedback, and usable desktop/mobile layouts.

**Architecture:** Retain the Preact feature structure and existing Go monolith. Centralize visual tokens and a small set of genuinely shared controls; feature hooks own requests and workflow state. Add one bounded deployment-review contract because existing endpoints cannot bind confirmation to reviewed configuration.

**Tech stack:** Existing Preact, TypeScript, CSS, CodeMirror, Vitest/Testing Library, Playwright, Go, Docker Compose SDK, and standard-library cryptography. No additional package dependency or database migration is planned.

**Spec:** [Approved UI overhaul design](../specs/2026-10-09-ui-overhaul-design.md). User approved the written specification on 2026-10-09. Implementation has not started.

## Global constraints

- “Preserve existing capabilities, routes, authentication, action eligibility, batch limits, and backend security behavior.”
- “The primary destinations are **Stacks, Repository, Operations, Alerts, Audit log, Settings**.” Keep `#/` and stack/container route identities; preserve the cross-stack container inventory and inclusive initial archive filter.
- “Retain System, Light, and Dark preferences.” Primary fill is `#1769b4` light / `#256bb7` dark, with white text; links have separate tokens.
- “Use at least 4.5:1 contrast for ordinary text and 3:1 for meaningful control boundaries and focus indicators.” Aim for 44 px mobile targets and no page-level horizontal overflow at 320 px.
- “Keep these independent actions with distinct success messages.” Save writes files, Commit records Git history, Deploy applies saved configuration. Never infer deployment changes from the Git diff.
- “No new metrics subsystem, server fleet management, roles, repositories, command palette, custom shortcut system, automatic recovery, exact deployment change planner, or backend rewrite is part of this design.”
- Preserve coordinator ordering, rooted/symlink checks, `If-Match` file writes, origin/CSRF/authentication, secret masking/redaction, bounded output, and existing automation semantics.
- Prefix commands with `rtk`. Format touched frontend files with Prettier and Go files with `gofmt`. Do not format unrelated files or commit runtime data, screenshots, or credentials.
- Before execution, use the worktree skill to establish an isolated checkout. Before each existing symbol edit, run GitNexus impact and report HIGH/CRITICAL risk; UNKNOWN requires source verification. Run complete change analysis before every scoped commit. No subagents.

## Review focus

1. Filters must not stop observing hidden stacks or turn unknown/partial data into a healthy summary — Task 3.
2. Save success followed by a failed diff refresh must not be reported as a failed save; a stale-write conflict must retain the edited buffer — Task 4.
3. A second browser tab changes configuration/environment while review is open; the original review must not deploy it — Task 5 and Task 6.
4. Late async results after switching stacks, closing details, or cancelling review must not act on the new resource — Tasks 4, 6, and 9.
5. At 320 px and with long names, keyboard focus, selection identity, and menu/dialog content must remain reachable — Tasks 2, 7, and 12.

## Verified starting point and contract decision

Baseline: `a6ae587dd679e081ac7bd717a85e3bf3d2bcbcf6`, clean worktree when planning began. Index refreshed at that commit with GitNexus 1.6.12. MCP calls returned `Transport closed`; CLI query/impact and source reads were used instead. A CLI context request failed once during package resolution; subsequent graph calls succeeded. The index reports bounded process coverage and interface dispatch limits; graph results are navigation evidence, not exhaustive proof.

Verified source anchors:

- `web/src/app/Workspace.tsx`: renders repository controls globally, owns operation selection, and passes a shared dirty flag to stack detail. `useWorkspaceData.ts` retains prior values on failed refresh and currently omits audit failures from its error choice.
- `Dashboard.tsx`: row-driven state updates and a separate cross-stack `OverviewServices` inventory; current bulk stack acceptance clears all selection. Filters include Modified and archive visibility.
- `Editor.tsx`: owns buffer, file hash, save, diff, file mutation, and commit internally. Save/diff refresh share one error path. `saveStackFile` already uses `If-Match`.
- `StackDetail.tsx`: Deploy is inside Actions; unsaved edits disable it; tabs use local state. `Deployment` in frontend types omits `operationId`, although the Go JSON already supplies it.
- `ActionMenu.tsx`: disabled reasons appear only in `title`; keyboard traversal excludes those entries. `OperationDrawer.tsx` displays an unavailable cancellation button without an accessible explanation outside its tooltip.
- `internal/http/routes_repository.go`: stack action POST calls `StartAction` with no reviewed source precondition. `ControlPlane.StartAction` captures environment before acquiring the stack coordinator. `DeployLocked` validates and digests configuration but does not compare it to an earlier review.
- `internal/compose/project.go`: existing `Digest` includes normalized Compose configuration and serialized managed environment. `WorkspaceService` file/environment writes use the coordinator. Repository Pull is fast-forward-only.

**Graph risk:** `ControlPlane.StartAction` is **HIGH** risk, with `registerRepositoryRoutes` as its direct indexed caller and API/router flows upstream; interface dispatch makes this a lower bound. Preserve `ActionAPI.StartAction` and existing calls. `Editor` is **LOW** risk in the graph, with `StackDetail → Workspace → App` upstream. Re-run impact against the execution checkout before changes; plan-time results are not edit authorization.

**Bounded contract extension proposed for plan approval:** add a read-only deployment review returning an opaque revision, and an optional `If-Match` precondition on the existing deploy action. The new UI always supplies it; existing API clients retain their current behavior. No new authentication capability is created by the revision. This is necessary to meet the spec's stale-review requirement, not an exact deployment change planner.

The revision covers stack/project identity, normalized Compose configuration including managed environment, Git HEAD, and the digest of the stack-scoped Git diff. Use a per-process HMAC key so neither secrets nor raw configuration fingerprints are exposed. Reviews become invalid after restart. An unchanged source may be reviewed/deployed again; the revision is not an idempotency key. Arbitrary host edits and live bind-mount contents are not an immutable filesystem snapshot: preserve the existing trust boundary and do not promise otherwise.

## File responsibilities and task order

All newly named files below are proposed additions; all other paths are existing files inspected or discovered during planning.

| Owner | Changes |
| --- | --- |
| `web/src/app/` | Tokens/shell styles, mobile navigation, resource freshness, repository placement; retain workspace orchestration. |
| `web/src/components/` | Add `Dialog.tsx` and `ConfirmDialog.tsx`; improve existing Feedback/ActionMenu. Reuse CSS control classes rather than introduce a generic component framework. |
| `web/src/features/stacks/` | Add inventory state/filter helpers, editor controller, review API/controller/dialog, stack-specific styles; adapt existing inventory/detail/settings components. |
| `internal/control/` | Add deployment-review behavior next to the deployment orchestration it protects. |
| `internal/http/` | Add narrow review interface/route and precondition error mapping; keep authentication guards here. |
| Other frontend features | Own their styling and requests; share only common controls and status presentation. |
| `web/e2e/` | Add overhaul workflow coverage using the existing real-server harness and deterministic API fixtures. |

Execute sequentially: 1–4 establish UI/controller interfaces; 5 adds the review contract; 6–8 integrate workflows; 9–11 cover supporting screens; 12 verifies the whole application. Each task ends in a working, scoped commit.

## Task 1: Theme and responsive application shell

**Files:** Modify `web/src/app/styles.css`, `theme.ts`, `WorkspaceShell.tsx`, `Workspace.tsx`, `web/src/components/Icon.tsx`; create `web/src/app/tokens.css`, `shell.css`, `WorkspaceShell.test.tsx`; update `web/e2e/critical.spec.ts` and existing `theme.test.ts` only where expectations change.

**Interfaces:** Keep existing `WorkspaceShell` props. Mobile navigation state belongs inside that component; use a real button with `aria-expanded` and `aria-controls`. Tokens use CSS custom properties; no JS theme copy.

- [ ] Add failing shell tests: `it("opens every destination from mobile navigation")` asserts Stacks/Repository/Operations/Alerts/Audit log/Settings are reachable; `it("marks the current stack section and retains account access")` asserts `aria-current="page"`, server identity, and sign-out access. Keep theme preference tests.

  ```ts
  expect(screen.getByRole("link", { name: "Stacks" })).toHaveAttribute("href", "#/");
  expect(screen.getByRole("link", { name: "Stacks" })).toHaveAttribute("aria-current", "page");
  ```
- [ ] Run `rtk bun run test -- src/app/WorkspaceShell.test.tsx src/app/theme.test.ts` in `web/`; confirm new behavior fails before implementation.
- [ ] Extract shared tokens and shell rules, then implement direction B. Rename visible Overview navigation to Stacks without changing route paths; group monitor items; show connection state. At widths below 768 px use a compact header and expandable navigation; navigation closes after choosing a destination and restores focus appropriately. Extract only styles touched by this task.
- [ ] Run the focused tests and `rtk bun run typecheck`. Verify both themes with the existing browser workflow at 1536×1024 and 390×844; update old literal color assertions to the approved palette. Record screenshots outside tracked source.
- [ ] Run change analysis, stage only this task's files, and commit `feat: establish soft contrast shell and themes`.

## Task 2: Accessible feedback, menus, and confirmation primitives

**Files:** Create `web/src/components/Dialog.tsx`, `Dialog.test.tsx`, `ConfirmDialog.tsx`, `ConfirmDialog.test.tsx`; modify `Feedback.tsx`, `ActionMenu.tsx`, `ActionMenu.test.tsx`, and shared control styles in `web/src/app/styles.css`.

**Interfaces:**

```ts
type DialogProps = { open: boolean; title: string; children: ComponentChildren;
  onClose: () => void; initialFocusRef?: RefObject<HTMLElement> };
type ConfirmDialogProps = { open: boolean; title: string; description: ComponentChildren;
  confirmLabel: string; destructive?: boolean; busy?: boolean;
  error?: string; onConfirm: () => void; onCancel: () => void };
// Existing Notice gains optional tone and role; existing callers remain valid.
```

- [ ] Add tests `it("names a confirmation and returns focus after Escape")`, `it("prevents duplicate confirmation while submitting")`, and `it("makes unavailable action reasons reachable by keyboard")`. Assert callback counts, accessible descriptions, and disabled action non-execution. Test actual focus containment in Playwright, not only jsdom's dialog stub.

  ```ts
  expect(screen.getByRole("dialog", { name: "Stop monitoring?" })).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Stop stack" })).toBeDisabled(); // submitting fixture
  expect(onConfirm).toHaveBeenCalledTimes(1); // two activation attempts
  ```
- [ ] Run `rtk bun run test -- src/components` in `web/` and observe the new failures.
- [ ] Implement native `<dialog>` lifecycle with cleanup, cancel handling, focus restoration, and action-specific labels. Make disabled menu entries focusable via roving focus with `aria-disabled`, visible explanatory copy and `aria-describedby`; guard activation. Do not disable a whole trigger when its explanation would become unreachable. Notices distinguish persistent errors from non-interrupting status information.
- [ ] Re-run component tests and typecheck; browser-check Tab/Shift+Tab/Escape, long confirmation titles, and menus at 320 px. Use CSS for primary/secondary/destructive controls; avoid one component per decorative wrapper.
- [ ] Analyze changes and commit `feat: add accessible confirmations and action feedback`.

## Task 3: Honest stack state, inventory filtering, and freshness

**Files:** Modify `statusPresentation.ts`, its tests, `stackStatus.ts`, `Dashboard.tsx`, `Dashboard.test.tsx`, `api.ts`, `useStackState.ts`, `web/src/app/useWorkspaceData.ts`, `Workspace.tsx`; create `stackInventory.ts`, `stackInventory.test.ts`, `useStackInventoryState.ts`, `useStackInventoryState.test.tsx`, `web/src/app/useWorkspaceData.test.tsx`. All unqualified files are under `web/src/features/stacks/`.

**Interfaces:** Add `listStacks(): Promise<Stack[]>` for the plain existing stack-list endpoint; migrate workspace use from eager `listStacksWithState` after confirming callers. Add `useStackInventoryState(ids: string[], refreshKey: string): { states: Record<string, StackState | undefined>; errors: string[]; loading: boolean }`. Add pure `filterStacks(stacks: Stack[], states: Record<string, StackState | undefined>, repo: Repository | null, filters: StackFilters): Stack[]`, with explicit search/runtime/deployment/git/archive/name-sort fields in `StackFilters`.

- [ ] Add regression assertions: a running/current stack may still have uncommitted files; unknown state is never counted as healthy; sleeping does not match Stopped; archived stacks remain visible initially. Filter out an unhealthy stack and prove the attention count still observes it. Reject one state read and assert the summary reports incomplete data. Test 25 stacks with max four simultaneous reads, no overlapping cycles, unmount cancellation, and focus/visibility refresh.

  ```ts
  expect(filterStacks(stacks, states, repo, { ...filters, runtime: "stopped" })
    .some(stack => states[stack.id]?.runtime === "sleeping")).toBe(false);
  expect(maxConcurrentReads).toBeLessThanOrEqual(4); // counted in the deferred API fake
  ```
- [ ] Run `rtk bun run test -- src/features/stacks/stackInventory.test.ts src/features/stacks/useStackInventoryState.test.tsx src/features/stacks/statusPresentation.test.ts src/app/useWorkspaceData.test.tsx`; observe new tests fail.
- [ ] Move inventory observation above filtered rows. Follow `useOverviewContainers`' bounded cycle pattern: four concurrent reads, five seconds after completion, hidden-document pause, queued refresh, and stale-response rejection. Remove duplicate row polling and eager workspace state fetching. Keep the detail page's independent `useStackState` observer. Runtime labels use sentence case; No health check is explicit. Compute attention counts from the complete available snapshot, not selected/visible rows.
- [ ] Add resource-specific read status to `useWorkspaceData`: distinguish initial loading/error from retained-but-stale data for stacks, repository, operations, and audit. Do not claim an audit failure is an empty audit log. Re-run the focused tests plus Dashboard and overview-container suites; typecheck.
- [ ] Analyze changes and commit `feat: clarify stack status and incomplete inventory`.

## Task 4: Extract the editor controller and preserve user edits

**Files:** Create `web/src/features/stacks/useStackEditor.ts`, `useStackEditor.test.tsx`, `Editor.test.tsx`; modify `Editor.tsx`, `StackDetail.tsx`, `StackDetail.test.tsx`. Keep `CodeEditor.tsx`'s existing contract unless a behavioral test proves a change necessary.

**Interfaces:**

```ts
type StackEditor = {
  tree: FileEntry[]; file?: FileContent; content: string; diff: string;
  dirty: boolean; busy: boolean; error: string; notice: string;
  commitMessage: string; setCommitMessage: (value: string) => void;
  setContent: (value: string) => void; open: (path: string) => Promise<boolean>;
  save: () => Promise<boolean>; discard: () => void;
  mutateFile: (kind: "create" | "directory" | "move" | "delete", path: string) => Promise<boolean>;
  commit: () => Promise<boolean>;
};
useStackEditor({ stackId, enabled, onDirtyChange, onSaved }): StackEditor;
// Options types: string, boolean, (dirty: boolean) => void, () => void.
Editor({ stack, model }: { stack: Stack; model: StackEditor }): VNode;
```

- [ ] Add tests `it("keeps the buffer and old hash after a stale-write rejection")`, `it("reports a successful save when refreshing the diff fails")`, `it("ignores a file response from the previous stack")`, and `it("commits saved files without deploying")`. Assert the saved snapshot/hash, dirty flag, persistent error, and zero deployment calls. A blank/unloaded editor's Save must return false.

  ```ts
  expect(await result.current.save()).toBe(false); // write rejects with 412
  expect(result.current.content).toBe(editedContent);
  expect(result.current.file?.hash).toBe(original.hash);
  expect(result.current.dirty).toBe(true);
  // In the separate successful-write/failed-diff fixture:
  expect(await result.current.save()).toBe(true);
  ```
- [ ] Run `rtk bun run test -- src/features/stacks/useStackEditor.test.tsx src/features/stacks/Editor.test.tsx` and confirm failures.
- [ ] Move requests and editor state into the feature hook. `StackDetail` owns the model so header actions can call Save/Discard. Enable initial loading on the first editor visit and retain the model across local tab changes; do not fetch files for every unvisited stack detail. Save returns true only after the write succeeds; diff-refresh errors remain a separate notice. Keep writes read-only while submitting and do not overwrite later user input with a stale response.
- [ ] Keep dirty state synchronized with the existing route guard; discard restores the last loaded/saved buffer. Use the existing `If-Match` API, never retry an overwrite after a conflict. Re-run new tests, `StackDetail.test.tsx`, `App.test.tsx`, and typecheck. Existing editor actions remain available at task completion.
- [ ] Analyze changes and commit `refactor: expose stack editor save and discard workflow`.

## Task 5: Bind deployment review to saved configuration

**Files:** Create `internal/control/deployment_review.go`, `deployment_review_test.go`, `internal/http/routes_deployment_review.go`, `deployment_review_test.go`; modify `internal/control/control.go`, `internal/http/auth.go`, `routes_repository.go`, `api.go`, `internal/app/app.go`; extend existing `internal/control/control_test.go` and `internal/http/api_test.go` where fixtures need it. Do not alter database schema or the existing `ActionAPI` interface.

**Interfaces:**

```go
// internal/control/deployment_review.go
type DeploymentReview struct {
    StackID string `json:"stackId"`
    SourceRevision string `json:"sourceRevision"`
    UncommittedChanges bool `json:"uncommittedChanges"`
}
func (c *ControlPlane) ReviewDeployment(ctx context.Context, id stack.StackID) (DeploymentReview, error)
func (c *ControlPlane) StartReviewedDeployment(ctx context.Context, id stack.StackID, expected string) (operation.Operation, error)
var ErrDeploymentReviewChanged = errors.New("deployment source changed; review again")
```

HTTP exposes `GET /api/v1/stacks/{id}/deployment-review`. For `POST /api/v1/stacks/{id}/actions/deploy`, a quoted `If-Match` revision invokes the narrow `DeploymentReviewAPI` in `RouterOptions`; absent header keeps the existing `StartAction` behavior. Malformed revisions return 400; stale revisions return 412 with `DeploymentReviewChanged`; archive/conflict rules retain meaningful 409 responses. Never silently fall back to unchecked deployment when the header is present but the review implementation is unavailable.

- [ ] Add failing Go tests `TestReviewedDeploymentRejectsChangedConfiguration`, `TestReviewedDeploymentRejectsChangedEnvironment`, `TestReviewedDeploymentRejectsRevisionFromAnotherStack`, `TestReviewedDeploymentRejectsArchivedStack`, `TestReviewedDeploymentKeepsSourceLockedUntilCompletion`, and `TestReviewRevisionDoesNotExposeSecrets`. Assert no accepted operation or Docker mutation for a stale review, and lock release on every failed admission path. Also cover Git HEAD/diff changes, restart-invalidated revisions, invalid configuration, and entropy-source failure.

  ```go
  // After a successful review and a coordinated configuration change:
  _, err = control.StartReviewedDeployment(ctx, id, review.SourceRevision)
  if !errors.Is(err, ErrDeploymentReviewChanged) {
      t.Fatalf("stale review error = %v", err)
  }
  ```
- [ ] Add HTTP tests for unchanged legacy deployment, a valid reviewed deployment, stale/malformed revisions, missing backend capability, unauthenticated access, origin/CSRF rejection, and bounded input/errors. Run `rtk go test ./internal/control ./internal/http -run 'ReviewedDeployment|DeploymentReview|ReviewRevision'`; confirm failures.
- [ ] Implement source capture under the existing coordinator before reading environment/configuration. Use existing rooted Compose loading and Digest; HMAC a deterministically encoded tuple of stack/project identity, Compose digest, HEAD, and stack diff digest. Lazily initialize one 32-byte process-local secret with `crypto/rand`, cache initialization errors, and fail closed. Emit only the opaque hex revision and minimal review metadata. Never reuse auth signing keys, persist this key, or log source material. Match revisions with constant-time comparison.
- [ ] Keep coordinator ownership continuous from successful comparison through accepted operation completion. Reuse existing deployment orchestration through a private helper with an optional expected revision; keep public `StartAction` unchanged. Do not recursively acquire the coordinator. Re-read relevant metadata inside the lock, apply existing manual image overrides, and preserve deployment/alert records, output bounds, and release-on-error semantics. Direct host filesystem mutation remains outside the application lock boundary.
- [ ] Run `rtk go test ./internal/control ./internal/http ./internal/operation ./internal/stack ./internal/compose ./internal/repository`, `rtk go vet ./internal/control ./internal/http`, and existing filesystem/auth security tests. Re-run graph impact on touched symbols, review HIGH risk explicitly, then commit `feat: reject stale deployment reviews` after full change analysis.

## Task 6: Stack detail and deployment review interaction

**Files:** Create `web/src/features/stacks/deploymentReviewApi.ts`, `useDeploymentReview.ts`, `DeploymentReviewDialog.tsx`, `useDeploymentReview.test.tsx`, `stackDetail.css`; modify `StackDetail.tsx`, its tests, `types.ts`, and `Editor.tsx` integration from Task 4.

**Interfaces:** Frontend `DeploymentReview` matches Task 5. `getDeploymentReview(id: string): Promise<DeploymentReview>` and `deployReviewedStack(id: string, revision: string): Promise<Operation>` use the existing HTTP client and quoted `If-Match`. `useDeploymentReview({ stack, operations, editor, onAccepted })` returns `{ phase, review, error, request, saveAndContinue, discardAndContinue, cancel, submit }`, with async actions returning `Promise<void>`, cancel synchronous, and phase in `idle | unsaved | loading | reviewing | submitting | error`. `editor` is `StackEditor`; `onAccepted` is `(operation: Operation) => void`.

- [ ] Add tests `it("saves before opening review but does not deploy automatically")`, `it("keeps edits after cancelling review preparation")`, `it("discards only after explicit choice")`, `it("requires fresh confirmation after a 412 response")`, and `it("ignores a review response after changing stacks")`. Assert request ordering, no mutation before explicit confirmation, one POST on double click, and the captured stack ID/revision. Failures remain visible.

  ```ts
  expect(editor.save).toHaveBeenCalledTimes(1);
  expect(getDeploymentReview).toHaveBeenCalledWith(stack.id);
  expect(deployReviewedStack).not.toHaveBeenCalled(); // after Save and continue
  expect(result.current.phase).toBe("reviewing");
  ```
- [ ] Run `rtk bun run test -- src/features/stacks/useDeploymentReview.test.tsx src/features/stacks/StackDetail.test.tsx`; confirm new failures.
- [ ] Implement the approved detail structure and primary labels: Deploy changes… for `changes_pending`, Deploy stack… otherwise, with applicable gating. Dirty edits open the three-choice Save and continue / Discard and continue / Cancel dialog instead of disabling Deploy. Use Task 5 review metadata; after a stale response reload review but require another deliberate confirmation. Cancel invalidates pending reads. Never automatically retry uncertain POST failures; retain operation context.
- [ ] Use Task 2 dialogs for whole-stack Stop/Restart; preserve runtime guards and revalidate active/archive state at confirmation. Add accessible tab panels and keyboard tab navigation. Use `operationId` already present in deployment JSON to open actual operation details; extend the TS `Deployment` type, not the backend response. Keep Compose project, logs, alerts, and settings reachable. Run focused suites, typecheck, and a browser review in both themes.
- [ ] Analyze changes and commit `feat: clarify stack detail and deployment review`.

## Task 7: Stack inventory, bulk actions, and container workflows

**Files:** Modify `Dashboard.tsx`, `OverviewServices.tsx`, `ServicesTable.tsx`, `ServiceCells.tsx`, `ContainerDetail.tsx`, their existing tests, and `serviceDisplay.ts` where needed; create `useStackBatchActions.ts`, `useStackBatchActions.test.tsx`, `useContainerBatchActions.ts`, `useContainerBatchActions.test.tsx`, `stackInventory.css`, `containerDetail.css` under `web/src/features/stacks/`.

**Interfaces:** `useStackBatchActions({ stacks, states, operations, onAccepted })` returns `{ selectedIds, setSelectedIds, pending, error, outcomes, review, request, confirm, cancel }`; selection is `Set<string>`, kind is `deploy | restart | stop`, outcomes identify stack ID and accepted operation or error. Deploy review stores Task 5 revisions per selected stack. `useContainerBatchActions({ targets, onAccepted })` accepts explicit `{stackId: string; stackName: string; container: Container; archived: boolean; busy: boolean}[]`, exposes pending review/confirm/cancel/error/outcomes, and owns grouping/submission. Reuse it only between existing container tables, retaining feature ownership.

- [ ] Extend inventory tests for attention links, independent stack/container filters, visible archive markers, contextual bulk controls, and max 20 selections. Add `it("keeps rejected stacks selected after partial acceptance")` and `it("never submits a replacement selection after confirmation opens")`. Container tests cover same-service replicas, changed membership, state changes, stale reads, archived parents, and scoped Stop/Restart confirmations.

  ```ts
  expect(result.current.selectedIds).toEqual(new Set([rejectedStack.id]));
  expect(onAccepted).toHaveBeenCalledWith([acceptedOperation]);
  expect(runContainerBatchAction).not.toHaveBeenCalled(); // invalidated selection fixture
  ```
- [ ] Run `rtk bun run test -- src/features/stacks/Dashboard.test.tsx src/features/stacks/OverviewServices.test.tsx src/features/stacks/ServicesTable.test.tsx src/features/stacks/ContainerDetail.test.tsx src/features/stacks/useStackBatchActions.test.tsx src/features/stacks/useContainerBatchActions.test.tsx`; observe new failures.
- [ ] Build the approved stack table and attention strip using Task 3 state. Remove repeated Remote cells, preserve uncommitted-change filtering and last deployment, and retain the secondary cross-stack container section. Put requests in the hooks above; leave rendering, filters, and local selection in their appropriate feature owners.
- [ ] Freeze explicit target IDs/names in confirmations and invalidate reviews when relevant resources or selection change. Acquire reviewed-deploy revisions with at most four concurrent reads; require explicit refreshed review on changes. Keep existing 20-item limits and per-stack container grouping. Accepted groups clear, rejected groups remain selected. Containers retain logs/inspection/image/network/port detail, no-health-check copy, sleeping semantics, and eligible start/stop/restart behavior. Keep actions off unknown/archived states as today.
- [ ] Run affected suites and typecheck; verify long names, published ports, selection identity, and menu positioning at desktop/390/320 px. Analyze changes and commit `feat: polish inventories and scoped container actions`.

## Task 8: Editor presentation, navigation protection, and repository page

**Files:** Modify `Editor.tsx`, `Editor.test.tsx`, `web/src/app/Workspace.tsx`, `useHashRoute.ts`, `App.test.tsx`, `web/src/features/repository/RepositoryHeader.tsx`, `RepositoryHistory.tsx`; create `web/src/app/useHashRoute.test.tsx`, `web/src/features/repository/RepositoryPage.tsx`, `RepositoryPage.test.tsx`, `repository.css`, `web/src/features/stacks/editor.css`.

**Interfaces:** `RepositoryPage` owns existing repository-action requests; receives repo, commits, remoteEnabled, dirty, and `(Operation) => void` onAccepted from Workspace. Preserve existing HTTP API functions. Extend `useHashRoute()` with `pendingRoute?: string`, `confirmNavigation(): void`, and `cancelNavigation(): void`; keep `navigate(path: string): void`, `route`, `dirty`, `setDirty`. Browser unload still uses the native browser warning.

- [ ] Add tests `it("saving and committing do not deploy")`, `it("keeps repository controls on the repository page")`, `it("cancels hash navigation without losing the editor buffer")`, and `it("explains fast-forward pull and repository scope")`. Assert one navigation/confirmation per internal or browser-back transition, restored hash on cancellation, scoped commit copy, and retained commit message on rejection.

  ```ts
  expect(screen.queryByRole("button", { name: "Fetch" })).not.toBeInTheDocument(); // Stacks route
  expect(location.hash).toBe(originalHash); // cancelled navigation
  expect(runStackAction).not.toHaveBeenCalled(); // Save / Commit flow
  ```
- [ ] Run editor, repository-page, route-hook, and App tests to observe failures.
- [ ] Present Files / Editor / Uncommitted changes as coherent desktop areas and accessible narrow-screen sections. Replace in-app file mutation prompts with labeled dialogs; preserve rooted paths, overwrite behavior, protected compose-file deletion, stale-write handling, and secret restrictions. Retain full CodeMirror keyboard functionality. Use Task 4 operations rather than duplicate requests.
- [ ] Move Fetch/Pull/Push into RepositoryPage. Explain Fetch updates remote knowledge, Pull fast-forwards local files, and Push publishes commits; none implies deployment. Use scoped Push confirmation and existing dirty/remote guards. Show one repository summary/link on Stacks with “last fetched” semantics and no invented timestamp. Internal navigation and sign-out use consistent dialogs; cancelled navigation restores context without double prompting. Never prevent the real browser unload safeguard.
- [ ] Run focused tests and typecheck; verify editor, diff, navigation, and repository flows in the browser. Analyze changes and commit `feat: distinguish editing git and deployment workflows`.

## Task 9: Operations, alerts, audit, and persistent recovery feedback

**Files:** Modify `web/src/features/operations/Operations.tsx`, `OperationDrawer.tsx`, `types.ts`, `web/src/features/alerts/AlertList.tsx`, `Alerts.tsx`, `web/src/features/audit/Audit.tsx`, `web/src/app/Workspace.tsx`; create `web/src/features/operations/operationPresentation.ts`, `OperationDrawer.test.tsx`, `operations.css`, `web/src/features/audit/Audit.test.tsx`; extend `Alerts.test.tsx` and App tests.

**Interfaces:** Add `operationPresentation(operation: Operation): { label: string; tone: StatusTone; terminal: boolean }`. Map only actual backend statuses; failed operations with an interruption error code get explanatory copy, not a fabricated wire status. Keep `openOperation(id)` and existing request-generation protection in Workspace.

- [ ] Add tests `it("shows accepted work as queued rather than succeeded")`, `it("keeps failed output after navigating away and reopening")`, `it("does not reopen details from a late response after close")`, `it("acknowledging an alert does not start recovery")`, and `it("shows failed audit loading instead of an empty log")`. Cover cancelled, unknown, truncated-output, and terminal-empty-output states.

  ```ts
  expect(operationPresentation({ ...operation, status: "queued" }).terminal).toBe(false);
  expect(screen.queryByRole("region", { name: "Operation details" })).not.toBeInTheDocument(); // closed late response
  ```
- [ ] Run operations, alerts, audit, and App focused tests; confirm new failures.
- [ ] Group active operations above completed history; show scope, trigger, available times, persistent error, output, and existing service-update details. Desktop uses a nonmodal labeled details region with deliberate focus and close restoration; mobile uses a modal full-width dialog with focus containment. Preserve page mounting/filter/scroll context. Replace unavailable cancellation's tooltip-only explanation with readable text; do not add cancellation behavior.
- [ ] Alerts distinguish acknowledgement, manual resolution, and verified recovery, with existing links and eligibility reasons. Apply quieter audit styling. Use Task 3 resource status to distinguish partial/error/empty states. Re-run tests, typecheck, and operation stream/alerts e2e suites; inspect both detail layouts by keyboard.
- [ ] Analyze changes and commit `feat: make operation and recovery feedback explicit`.

## Task 10: Stack settings and automation clarity

**Files:** Modify `web/src/features/stacks/StackSettings.tsx`, `AutoUpdateSettings.tsx`, `OnDemandSettings.tsx`, `OnDemandForm.tsx`, `EnvironmentRow.tsx`, existing automation CSS/tests; create `StackSettings.test.tsx`, `EnvironmentRow.test.tsx`, `stackSettings.css` in the same feature.

**Interfaces:** Retain existing settings APIs/hooks/types. Forms own field state; request behavior stays in existing feature hooks or a focused handler extracted only when necessary. Shared confirmation behavior consumes Task 2.

- [ ] Add tests `it("distinguishes enabled policy from paused execution")`, `it("explains ineligibility without implying the policy is disabled")`, `it("keeps secret values masked until deliberately revealed")`, and `it("names removed files and retained volumes before deletion")`. Reject a settings save and assert fields remain populated; assert no recovery/enable call occurs from acknowledgement alone.

  ```ts
  expect(screen.getByText(/volumes.*retained/i)).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Delete stack" })).toBeInTheDocument();
  expect(screen.queryByText(secretValue)).not.toBeInTheDocument();
  ```
- [ ] Run `rtk bun run test -- src/features/stacks/StackSettings.test.tsx src/features/stacks/EnvironmentRow.test.tsx src/features/stacks/AutoUpdateSettings.test.tsx src/features/stacks/OnDemandSettings.test.tsx`; observe failures.
- [ ] Organize automation, environment, identity, and destructive actions into clear sections. Show policy state separately from current eligibility, pause reason, last outcome, and existing resume/recovery action. Preserve on-demand group choices, host capability explanations, and sleeping/waking semantics. Replace destructive native confirmation with named, consequence-specific dialog; do not hide or expose secrets through the new UI.
- [ ] Re-run focused tests, typecheck, and `auto-update.spec.ts`, `on-demand.spec.ts`, `alerts.spec.ts` after the e2e build prerequisites in Task 12. Review narrow forms and both themes.
- [ ] Analyze changes and commit `feat: clarify stack settings and automation state`.

## Task 11: Login, setup, and application settings consistency

**Files:** Modify `web/src/features/auth/Auth.tsx`, `AccountSettings.tsx`, `web/src/features/repository/RepositorySetup.tsx`, `RepositoryRemoteFields.tsx`, `RepositorySettings.tsx`; create `web/src/features/auth/Auth.test.tsx`, `AccountSettings.test.tsx`, `auth.css`; update existing repository setup/settings tests.

**Interfaces:** Keep existing public props and API types. System/Light/Dark still use `getThemePreference` / `setThemePreference`. Preserve managed-remote/authentication configuration contracts and mandatory initial repository setup.

- [ ] Add tests `it("retains nonsecret setup fields after a rejected submission")`, `it("explains that password changes sign out all sessions")`, and `it("respects a saved theme before login")`. Keep existing tests for local/remote setup, Git author identity, remote removal, and authentication methods.

  ```ts
  expect(screen.getByLabel("Git author name")).toHaveValue("Porty"); // after rejected setup
  expect(screen.getByText(/signs out every session/i)).toBeInTheDocument();
  ```
- [ ] Run auth, repository setup/settings, and App tests; confirm new behavior fails.
- [ ] Apply the shared visual language, labels, inline errors, and clear next actions. Group account, appearance, and repository settings. Preserve password-manager autocomplete and secret-handling behavior; do not persist secrets or move API access into new presentational field components. Do not add a new onboarding wizard.
- [ ] Run focused tests and typecheck; browser-check registration→repository setup→Stacks, login, theme changes, and password-change session effects using existing fixtures.
- [ ] Analyze changes and commit `feat: unify setup and application settings`.

## Task 12: Cross-feature usability and regression verification

**Files:** Create `web/e2e/ui-overhaul.spec.ts`; modify `web/e2e/critical.spec.ts`, `alerts.spec.ts`, `auto-update.spec.ts`, `on-demand.spec.ts` only for changed labels/flows; update `docs/development.md` with reproducible screenshot/check commands if needed. Keep Playwright's existing desktop/mobile projects.

**Interfaces:** Use the existing `PORTY_E2E_BINARY` real-server harness. Use API fixtures for deterministic runtime scenarios; use real HTTP/control tests for stale-review locking, rather than claiming mocked browser responses prove server safety. Shared test setup may be extracted to `web/e2e/helpers/server.ts` if two suites genuinely need identical lifecycle code.

- [ ] Add browser flows for unhealthy stack→container logs, sleeping/no-health-check states, undeployed versus uncommitted files, Save/Discard/Cancel→review→queued/running/succeeded/failed, changed revision→new confirmation, and partial bulk acceptance. Assert page state/call effects, not arbitrary timeouts or exact internal class names.

  ```ts
  await page.getByRole("button", { name: "Deploy changes…", exact: true }).click();
  await expect(page.getByRole("dialog", { name: "Deploy monitoring?" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Deploy stack", exact: true })).toBeEnabled();
  ```
- [ ] Add keyboard/focus and long-name scenarios at 1536×1024, 390×844, and an explicit 320 px check. Capture both-theme screenshots for the shell, stack list/detail, editor, repository, settings, operation details, auth/setup, and representative empty/loading/error states. Check dialogs and menus by interaction; verify no page overflow, no relevant console errors, accessible disabled reasons, and the corrected Deploy contrast. Avoid assertions that simply restate CSS.
- [ ] Build current embedded assets and binary, then run the suites in order:

```bash
# From web/
rtk bun run test
rtk bun run typecheck
rtk bun run build
# From repository root (after web build)
rtk go test ./...
rtk go vet ./...
rtk go build -o /tmp/porty-e2e ./cmd/porty
# From web/
rtk env PORTY_E2E_BINARY=/tmp/porty-e2e bun run test:e2e
# From repository root
rtk ./deploy/package_test.sh
```

- [ ] Inspect screenshots and perform an inline end-to-end review against every spec section. Verify production build embeds the new UI; distinguish browser fixtures from real backend coverage. Fix only observed failures and rerun affected checks; retain meaningful security regressions. Document any environment-blocked checks honestly rather than claiming success.
- [ ] Run full GitNexus change analysis and `rtk git diff --check`; commit scoped verification changes as `test: verify polished UI workflows across devices`. Follow the branch-finishing skill without subagents. Include desktop/mobile screenshots, checks, the review-contract addition, and environment requirements in the eventual PR.

## Coverage and handoff

| Specification area | Tasks |
| --- | --- |
| Visual system, themes, navigation, responsive shell | 1, 2, 12 |
| Inventory, archive compatibility, state vocabulary, partial freshness | 3, 7 |
| Editor model, save/commit/deploy separation, conflicts | 4, 6, 8 |
| Source revalidation and action scope | 5, 6, 7 |
| Stack/container details and logs | 6, 7, 9 |
| Repository controls and navigation protection | 8 |
| Operations, alerts, audit, persistent feedback | 9 |
| Automation, on-demand, environment and destructive settings | 10 |
| Authentication, onboarding, application settings | 11 |
| Accessibility, real user tasks, security and packaging | 2, 5, 12 |

**Plan review decision:** confirm this plan, including the bounded backend revision check in Task 5, before implementation. Execution is inline with no subagents, as required by the repository. The approved visual direction and original spec remain authoritative; the plan adds implementation detail, not unrelated product features.
