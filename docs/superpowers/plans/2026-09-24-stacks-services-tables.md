# Stacks and Services Tables Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (- [ ]) syntax for tracking. Repository instructions prohibit subagents.

**Goal:** Give Porty clear, selectable Stacks and Services tables with accurate Docker metadata, unified state labels, last-deployment times, and safe bulk actions.

**Architecture:** Keep stack and container behavior in the existing stacks feature and control plane. Add a batched deployment-time read to the stack-list HTTP response and expose Compose container metadata already available in memory. Multi-stack commands submit existing independent stack operations; selected-container commands become one validated, stack-locked operation. Reuse the existing operation stream, badge style, Docker SDK, and guarded HTTP routes.

**Tech Stack:** Go 1.27.1, SQLite and sqlc, Docker Compose v5.5.1, Preact 10, TypeScript, Vitest with Testing Library, Playwright.

**Spec:** docs/superpowers/specs/2026-09-24-stacks-services-tables-design.md

## Global Constraints

- Follow the spec's exact table columns and action scopes. Stacks Actions target selected stacks; detail-page Actions target the whole stack; the Services selection toolbar targets selected containers.
- Keep Validate accessible on stack detail. Keep the Modified filter and Working tree information outside the Stacks table. Preserve the History tab and existing single-container API.
- Preserve repository-wide Remote semantics and its existing copy from remoteState. Display the backend's unhealthy runtime state explicitly.
- Limit one stack command to 20 selected stacks and one container batch to 20 unique full IDs. A mixed or unknown state never silently skips an item.
- Use one SQL read for latest deployment times. A Docker outage must not erase a stored timestamp. Do not fabricate image tags, networks, or port bindings.
- Preserve stack/repository lock ordering, full-ID project membership checks, auth, origin, CSRF, secret redaction, bounded output, and restrictive error responses. Use the existing Docker SDK connection; add no dependency or shell runner.
- Use gofmt and Prettier. Write a failing regression test before each behavior change. Before editing each named symbol, run GitNexus upstream impact and inspect HIGH/CRITICAL and UNKNOWN risk; refresh its currently stale index first. Before each commit, run detect_changes --scope all and resolve truncated or partial reports. If reindex still fails, do not treat stale graph output as complete; inspect current source and report the limitation.
- Keep screenshots and mock data in docs/design as design references only. Rendered app UI remains code-native.

## File Structure

| File | Responsibility |
| --- | --- |
| internal/sqlite/queries/deployments.sql, internal/sqlite/generated/deployments.sql.go, internal/sqlite/deployment_store.go, internal/sqlite/deployment_store_test.go | One read of latest deployment attempt time per active stack. |
| internal/http/auth.go, internal/http/api.go, internal/http/api_test.go | Additive stack-list timestamp contract and batch-action HTTP interface/error mapping. |
| internal/control/containers.go, internal/control/control.go, internal/control/control_test.go | Container metadata mapping, validated batch action, and authoritative stack runtime-action gates. |
| internal/http/routes_containers.go, internal/http/router_test.go | Guarded batch route and auth/origin/CSRF regression checks. |
| web/src/features/stacks/types.ts, api.ts, statusPresentation.ts, statusPresentation.test.ts | Typed response and action calls; feature-local state labels and tones. |
| web/src/components/ActionMenu.tsx, ActionMenu.test.tsx, Feedback.tsx | One accessible action menu and consistent badge geometry. |
| web/src/features/stacks/Dashboard.tsx, Dashboard.test.tsx | Exact Stacks columns, selection, conditional batch actions, timestamp. |
| web/src/features/stacks/ServicesTable.tsx, ServicesTable.test.tsx, serviceDisplay.ts, serviceDisplay.test.ts | Exact Services columns, formatting, search, selection, and container batch toolbar; replace ContainerList. |
| web/src/features/stacks/StackDetail.tsx, StackDetail.test.tsx | Services integration, whole-stack menu, Validate preservation, refresh and dirty-state behavior. |
| web/src/app/Workspace.tsx, useWorkspaceData.ts, styles.css, web/e2e/critical.spec.ts | Register accepted multi-stack operations, responsive design, and browser workflow. |

## Review Focus

1. Two replicas with the same service must remain separate; selecting one must send only its full container ID. Pin in Tasks 2, 3, and 6.
2. A selected ID from another project, duplicate ID, stale state, or archived stack must produce no Docker mutation during batch preflight. Pin in Task 3.
3. An unavailable Docker state read must show UNKNOWN and disable Stop/Restart while preserving the database-backed Last deployment value; a stale direct action request must fail on the server. Pin in Tasks 1, 4, and 5.
4. Multi-stack partial acceptance and per-container partial execution must identify successes and failures without implying atomicity or leaking secrets. Pin in Tasks 3 and 5.
5. Long image refs, many networks, IPv6 host bindings, and a 320 px viewport must preserve readable content and keyboard access without page-level overflow. Pin in Tasks 4, 6, and final browser verification.

---

### Task 1: Expose latest deployment time on the stack list

**Files:** Modify internal/sqlite/queries/deployments.sql, generated/deployments.sql.go via sqlc, deployment_store.go, deployment_store_test.go, internal/http/auth.go, api.go, api_test.go, and web/src/features/stacks/types.ts.

**Interfaces:** Produce DeploymentStore.LatestDeploymentTimes(context.Context) (map[stack.StackID]time.Time, error). Extend DeploymentQueryAPI with that method. GET /api/v1/stacks returns existing stack fields plus optional lastDeploymentAt, assembled in HTTP without changing stack.Stack.

- [ ] **Step 1: Check impact.** Refresh GitNexus, then inspect upstream impact for DeploymentStore, DeploymentQueryAPI, registerAPIRoutes, and Stack. Confirm stack.Stack remains unchanged.
- [ ] **Step 2: Write failing store and route tests.** In deployment_store_test.go, create two attempts for one active stack, the later one failed, one stack with no attempts, and an archived stack. Assert the returned map holds the failed attempt's startedAt and excludes the other two. In api_test.go, use a fake deployment reader returning one timestamp; assert GET /api/v1/stacks includes lastDeploymentAt for that stack and omits it for a never-deployed stack. Assert a deployment-read error returns an error response rather than a misleading empty timestamp.

  ~~~go
  func TestLatestDeploymentTimesUsesLatestAttemptForActiveStacks(t *testing.T) {
      got, err := store.LatestDeploymentTimes(ctx)
      if err != nil { t.Fatal(err) }
      if !got[activeID].Equal(failedLater.StartedAt) { t.Fatalf("latest = %v", got[activeID]) }
      if _, ok := got[neverID]; ok { t.Fatal("invented deployment time") }
      if _, ok := got[archivedID]; ok { t.Fatal("archived stack included") }
  }
  ~~~

- [ ] **Step 3: Run rtk go test ./internal/sqlite ./internal/http -run 'TestLatestDeploymentTimes|TestStackListIncludesLastDeployment' -count=1.** Expect failures for the absent read method and response field.
- [ ] **Step 4: Add the single SQL read and HTTP projection.** Add a sqlc query returning stack_id and started_at for only the latest attempt of each active stack, ordered by started_at then id to settle ties. Run rtk sqlc generate. Parse each stored timestamp once and return an error for malformed stored data. In the GET route, call ListStacks, then LatestDeploymentTimes when options.Deployments is present, and map each stack into this response type:

  ~~~sql
  -- name: ListLatestDeploymentTimes :many
  SELECT s.id AS stack_id, d.started_at
  FROM stacks AS s
  JOIN deployments AS d ON d.id = (
      SELECT latest.id
      FROM deployments AS latest
      WHERE latest.stack_id = s.id
      ORDER BY latest.started_at DESC, latest.id DESC
      LIMIT 1
  )
  WHERE s.archived_at IS NULL;
  ~~~

  ~~~go
  type stackListItem struct {
      portystack.Stack
      LastDeploymentAt *time.Time `json:"lastDeploymentAt,omitempty"`
  }
  ~~~

  Use a nil pointer for no recorded attempt or a test router without options.Deployments. Do not call Docker or the per-stack deployments endpoint for this column. Add lastDeploymentAt?: string to the frontend Stack type.
- [ ] **Step 5: Run rtk gofmt -w on changed Go files, rtk go test ./internal/sqlite ./internal/http -count=1, and rtk sqlc generate.** Inspect generated-file diff and confirm the list still works when there are no deployments.
- [ ] **Step 6: Run GitNexus detect_changes --scope all; inspect the report and commit feat: show latest deployment time in stack list.**

### Task 2: Return real image, network, and port data for every container

**Files:** Modify internal/control/containers.go, internal/control/control_test.go, web/src/features/stacks/types.ts.

**Interfaces:** Extend control.Container and frontend Container with image: string, networks: string[], ports: ContainerPort[]. ContainerPort has host: string, targetPort: number, publishedPort: number, protocol: string. Preserve all existing fields and ordering by service, name, then ID.

- [ ] **Step 1: Check impact.** Inspect upstream impact for Container, containerFromSummary, Containers, and frontend Container; verify the current Compose v5.5.1 ContainerSummary fields in the local module.
- [ ] **Step 2: Write a failing mapping test.** Use two same-service replicas and a foreign-project row. Give one replica an image with an explicit tag, two networks, one IPv4 and one IPv6 PortPublisher; give the other no publishers. Assert only owned rows return, all metadata stays attached to the correct ID, and empty arrays serialize as arrays rather than null.

  ~~~go
  row := api.ContainerSummary{
      ID: "full-id-b", Project: "porty-demo", Service: "web", Name: "web-2",
      Image: "example/web:2.1", Networks: []string{"front", "back"},
      Publishers: api.PortPublishers{
          {URL: "127.0.0.1", PublishedPort: 8080, TargetPort: 80, Protocol: "tcp"},
          {URL: "::1", PublishedPort: 8443, TargetPort: 443, Protocol: "tcp"},
      },
  }
  ~~~

- [ ] **Step 3: Run rtk go test ./internal/control -run 'TestContainersIncludeMetadata' -count=1.** Expect failure for missing fields.
- [ ] **Step 4: Map fields without parsing or inventing tags.** Copy row.Image and row.Networks, sort a copied network slice, and map each PortPublisher to a typed port. Sort ports by target port, published port, protocol, then host for stable rendering. Keep the existing project filter and replica ordering.

  ~~~go
  type ContainerPort struct {
      Host          string `json:"host"`
      TargetPort    int    `json:"targetPort"`
      PublishedPort int    `json:"publishedPort"`
      Protocol      string `json:"protocol"`
  }
  ~~~

- [ ] **Step 5: Run rtk gofmt -w internal/control/containers.go internal/control/control_test.go and rtk go test ./internal/control -count=1.**
- [ ] **Step 6: Run GitNexus detect_changes --scope all; inspect the report and commit feat: expose container image networks and ports.**

### Task 3: Add one safe operation for selected containers

**Files:** Modify internal/control/containers.go, control_test.go, internal/http/auth.go, routes_containers.go, api.go, api_test.go, router_test.go, web/src/features/stacks/api.ts.

**Interfaces:** Produce ControlPlane.StartContainerBatchAction(context.Context, stack.StackID, []string, string) (operation.Operation, error). POST /api/v1/stacks/{id}/containers/actions/{action} accepts {"containerIds":["full-id-a","full-id-b"]} and returns a normal 202 Operation. Add runContainerBatchAction(stackId: string, containerIds: string[], action: ContainerAction): Promise<Operation>. Preserve the existing single-container route.

- [ ] **Step 1: Check impact.** Inspect upstream impact for ControlPlane.StartContainerAction, containerActionAllowed, ContainerAPI, registerContainerRoutes, and writeAPIError. Keep the existing lock and single-container behavior.
- [ ] **Step 2: Write failing control tests.** Cover empty, duplicate, and 21-ID input; wrong project and unknown IDs; archived stack; mixed running/exited selection; coordinator conflict; two replicas with only selected IDs acted on; one Docker failure after one success. Assert invalid preflight creates no operation and no SDK call. Assert partial execution produces a failed operation with a per-ID result. Include a secret-bearing >256 KiB Docker error and assert operation output is redacted and bounded.

  ~~~go
  _, err := control.StartContainerBatchAction(ctx, stackID, []string{"full-id-a", "foreign-id"}, "stop")
  if !errors.Is(err, ErrContainerNotFound) { t.Fatalf("error = %v", err) }
  if runtime.actionCalls != 0 || operationStore.created != 0 { t.Fatal("mutated before preflight passed") }
  ~~~

- [ ] **Step 3: Run rtk go test ./internal/control -run 'TestContainerBatch' -count=1.** Expect missing-method failures.
- [ ] **Step 4: Implement the batch under one stack lock.** Validate action and 1–20 unique nonempty IDs before locking. After Try(false, stackID), resolve stack and archive flag, environment, and Compose Status. Match every full ID and project; require every state to allow the action. Start one operation with kind container_batch_start, container_batch_restart, or container_batch_stop, stack scope, and mapValues(values) secrets. In the job, run the selected IDs sequentially through runtime.ContainerAction; collect one success or safe failure line per ID, continue after failures, and return a non-nil summary error if any failed. Release the coordinator lock only when the job ends or Start fails.

  ~~~go
  type batchResult struct {
      ID      string
      Name    string
      Success bool
      Error   string
  }
  ~~~

  Format batchResult values into bounded operation output using the existing OperationService redaction and output cap; do not add a new persistent result type or store raw environment values.
- [ ] **Step 5: Add the guarded HTTP route and tests.** Decode containerIds through decodeBody. Map invalid selection to 400, foreign or missing ID to 404, incompatible state or archived stack to 409, and coordinator conflict to 409. Verify unauthenticated, wrong-origin, and missing-CSRF requests cannot create an operation. Register the static /containers/actions/{action} path alongside the existing per-container path and assert both routes dispatch correctly.

  ~~~go
  var input struct {
      ContainerIDs []string `json:"containerIds"`
  }
  if decodeBody(w, r, &input) != nil { return }
  operation, err := options.Containers.StartContainerBatchAction(
      r.Context(), portystack.StackID(r.PathValue("id")), input.ContainerIDs, r.PathValue("action"))
  writeResult(w, r, operation, err, stdhttp.StatusAccepted)
  ~~~

- [ ] **Step 6: Run rtk gofmt -w on changed Go files, rtk go test ./internal/control ./internal/http -count=1, and frontend typecheck after adding the API function.**
- [ ] **Step 7: Run GitNexus detect_changes --scope all; inspect the report and commit feat: control selected containers in one operation.**

### Task 4: Establish shared table status and menu behavior

**Files:** Create web/src/features/stacks/statusPresentation.ts, statusPresentation.test.ts, web/src/components/ActionMenu.tsx, ActionMenu.test.tsx. Modify web/src/components/Feedback.tsx and web/src/app/styles.css.

**Interfaces:** Export stackRuntimePresentation(value?: string): {label: string; tone: "neutral"|"success"|"warning"|"danger"}; containerStatePresentation(state?: string, health?: string) with the same base type and optional health label. ActionMenu accepts label, items with id/label/disabled/reason, and onSelect(id). Extend Badge with an optional visible dot while keeping its existing callers valid.

- [ ] **Step 1: Check impact.** Inspect upstream impact for Badge and Icon. Read all badge and runtime CSS rules before changing them.
- [ ] **Step 2: Write failing presentation and keyboard tests.** Assert exact mapping for running, stopped, partial, unhealthy, undefined, and unknown values; assert running plus unhealthy health retains RUNNING but exposes red Unhealthy secondary text in Services. Assert Remote tone is neutral when repository status is unavailable, green when current, yellow when ahead, and red when behind or diverged; assert Deployment tone is green for current, yellow for changes pending, blue for deploying, and neutral for never deployed or unverifiable. For ActionMenu, test opening by keyboard, moving between enabled items, Escape closure and focus return, and disabled item non-activation.

  ~~~ts
  expect(stackRuntimePresentation("partial")).toEqual({
    label: "PARTIALLY RUNNING",
    tone: "warning",
  });
  expect(stackRuntimePresentation(undefined).label).toBe("UNKNOWN");
  ~~~

- [ ] **Step 3: Run rtk bun run test -- statusPresentation ActionMenu from web/.** Expect missing-module failures.
- [ ] **Step 4: Implement small primitives.** Use one existing badge class for common height, padding, type, and radius; add a dot element only for runtime/state. Keep remoteState(repo) wording and add pure remoteTone(repo) and deploymentTone(freshness) helpers for color. Make ActionMenu a button with aria-expanded and a keyboard-operable menu; disabled items expose their reason. Do not add a UI library. Use existing Icon paths or add one matching SVG path for the menu caret.

  ~~~tsx
  <button type="button" aria-haspopup="menu" aria-expanded={open} onClick={toggle}>
    {label}<Icon name="Chevron" />
  </button>
  ~~~

- [ ] **Step 5: Run rtk bun run test -- statusPresentation ActionMenu, rtk bun run typecheck, and rtk bunx prettier --check on changed frontend files from web/.**
- [ ] **Step 6: Run GitNexus detect_changes --scope all; inspect the report and commit feat: unify table status and action controls.**

### Task 5: Rebuild the Stacks table and selected-stack commands

**Files:** Modify web/src/features/stacks/Dashboard.tsx, Dashboard.test.tsx, web/src/app/Workspace.tsx, useWorkspaceData.ts, styles.css, internal/control/control.go, control_test.go, and internal/http/api.go; create a focused timestamp formatter beside Dashboard if needed.

**Interfaces:** Dashboard gains onOperationsAccepted: (operations: Operation[]) => void. useWorkspaceData gains addOperations(operations: Operation[]): void. StackRow reports its latest StackState | undefined to Dashboard for aggregate eligibility. ControlPlane.StartAction returns ErrStackRuntimeActionUnavailable for a direct Stop/Restart call when running plus hasDeployed gates fail. Use runStackAction for each selected stack; do not create a stack-batch endpoint.

- [ ] **Step 1: Check impact.** Inspect upstream impact for Dashboard, StackRow, Workspace, useWorkspaceData, runStackAction, ControlPlane.StartAction, and writeAPIError.
- [ ] **Step 2: Write failing user-visible and server tests.** Assert exact six-column header sequence, removal of Working tree and Containers, larger linked name with stack icon, consistent Runtime/Remote/Deployment badges, latest attempt timestamp and Never fallback, UNKNOWN after a failed state poll without losing timestamp, select-all of visible rows, selection clearing on search/filter, sorting preserving selection, strict mixed-state gating, 20-stack limit, Stop confirmation, and per-stack accepted/error feedback. Assert accepted operations are registered together without opening an arbitrary last operation drawer. In control_test.go, assert direct Stop/Restart of a stopped, unhealthy, or never-deployed stack returns ErrStackRuntimeActionUnavailable before an operation is created; a running previously deployed stack remains allowed. In api_test.go, assert that error maps to HTTP 409.

  ~~~tsx
  fireEvent.click(screen.getByRole("checkbox", { name: "Select alpha" }));
  fireEvent.click(screen.getByRole("checkbox", { name: "Select beta" }));
  fireEvent.click(screen.getByRole("button", { name: "Actions" }));
  fireEvent.click(screen.getByRole("menuitem", { name: "Deploy" }));
  await waitFor(() => expect(runStackAction).toHaveBeenCalledTimes(2));
  expect(onOperationsAccepted).toHaveBeenCalledWith([
    expect.objectContaining({ scopeId: "one" }),
    expect.objectContaining({ scopeId: "two" }),
  ]);
  ~~~

- [ ] **Step 3: Run rtk bun run test -- Dashboard from web/ and rtk go test ./internal/control ./internal/http -run 'TestStackRuntimeAction|TestStackActionConflict' -count=1.** Expect new behavior assertions to fail.
- [ ] **Step 4: Implement selection and action dispatch.** Keep Set<string> selected IDs in Dashboard, derive visible rows from existing search/filter/sort, cap one command at 20, and clear on search/filter or unmount. Let StackRow report state changes to a Dashboard map keyed by stack ID; report undefined on poll failure and prune removed stack IDs. Keep the callback stable and update the map only when that row's state actually changes. Compute menu eligibility from this map, hasDeployed, archived flag, and active operation. Disable select-all with an explanation if more than 20 rows are visible. Launch eligible selected stack IDs using Promise.allSettled with runStackAction; feed fulfilled operations to onOperationsAccepted and show a concise per-stack immediate result summary. The operation stream owns later completion status. Keep New stack, Modified filtering, and navigation intact.
- [ ] **Step 5: Recheck stack Stop/Restart on the server.** After the stack lock is acquired and before operations.Start, call runtime.Status and stateStore.HasSuccessfulDeployment; permit only AggregateRuntime(parseContainerStates(rows)) == RuntimeRunning with a successful prior deployment. Return ErrStackRuntimeActionUnavailable otherwise and release the lock. Map that error to 409 without leaking runtime details. Keep Deploy and Validate behavior unchanged.
- [ ] **Step 6: Implement timestamp display.** Use the optional lastDeploymentAt from GET /stacks, Intl.DateTimeFormat for the absolute value and Intl.RelativeTimeFormat for the secondary value. Use time element dateTime; render Never for missing or invalid input. Do not derive deployment time from stack.updatedAt or Docker state.
- [ ] **Step 7: Run rtk gofmt -w on changed Go files, rtk go test ./internal/control ./internal/http -count=1, rtk bun run test -- Dashboard, rtk bun run typecheck, and rtk bun run build from web/.** Inspect the mobile table width and selected-row styles in a browser.
- [ ] **Step 8: Run GitNexus detect_changes --scope all; inspect the report and commit feat: add selectable stacks table and bulk actions.**

### Task 6: Replace the Runtime list with the Services table

**Files:** Create web/src/features/stacks/ServicesTable.tsx, ServicesTable.test.tsx, serviceDisplay.ts, serviceDisplay.test.ts. Modify StackDetail.tsx, StackDetail.test.tsx, api.ts, types.ts, app/styles.css, and web/e2e/critical.spec.ts. Remove ContainerList.tsx and ContainerList.test.tsx after equivalent behavior coverage passes.

**Interfaces:** ServicesTable receives containers, error, busy, archived, dirty, and onBatchAction: (ids: string[], action: ContainerAction) => void. Export formatContainerPort(port: ContainerPort): string. StackDetail retains its onAction: (operation: Operation) => void contract and uses runContainerBatchAction.

- [ ] **Step 1: Check impact.** Inspect upstream impact for ContainerList, StackDetail, useStackContainers, runContainerAction, and the existing StackDetail.action function. Preserve the 5-second Overview-only poll and operation-completion refresh key.
- [ ] **Step 2: Write failing formatter and component tests.** Assert one row per replica with service primary and container name secondary, exact column names/order, image reference unchanged, sorted networks, IPv4 and bracketed IPv6 bindings, multiple ports, absent data as em dash, search matching service/name/image/network, select-all of visible rows, filtering clearing selection, polling pruning removed IDs, 20-ID cap, and Start versus Stop/Restart gates. Assert dirty, archived, busy, unknown, paused, and mixed selections never invoke onBatchAction.

  ~~~ts
  expect(formatContainerPort({
    host: "::1", publishedPort: 8443, targetPort: 443, protocol: "tcp",
  })).toBe("[::1]:8443 → 443/tcp");
  ~~~

- [ ] **Step 3: Run rtk bun run test -- ServicesTable serviceDisplay StackDetail from web/.** Expect the new table and formatter assertions to fail.
- [ ] **Step 4: Implement the Services table.** Render semantic table headers and row-named checkboxes, with no row-navigation chevron. Show state badge plus health text, exact image string, assigned network names, and formatted ports; a target port without a published port renders as target/protocol without a host prefix. Keep selection by full ID across successful polls, prune IDs absent from the newest rows, and clear selection on search change or stack change. Disable select-all with an explanation if more than 20 rows are visible. Confirm Stop with the selected count. On successful batch API acceptance, call StackDetail.onAction; on immediate failure, show a Notice without leaving stale controls.
- [ ] **Step 5: Replace the detail header action group with ActionMenu.** Deploy, Restart, and Stop act on the whole stack; Restart and Stop use the existing running plus hasDeployed gate. Keep Validate as a secondary button. Keep dirty-change guard, busy/active disablement, Stop confirmation, and archive read-only behavior. Rename the Overview heading to Services; retain Last deployment and Compose project below it.
- [ ] **Step 6: Update existing StackDetail and e2e assertions.** Replace per-row Stop/Start button expectations with checkbox and selected-toolbar flows. Preserve single-container route tests at the API/control layer. Add one browser test that selects only web-2 and confirms the request body contains only full-id-b. Add one test for stack-wide Actions remaining separate from selected-container actions.
- [ ] **Step 7: Run rtk bun run test, rtk bun run typecheck, rtk bun run build from web/.** Run Prettier on modified files. Remove ContainerList files only after the replacement tests cover their observable behavior.
- [ ] **Step 8: Run GitNexus detect_changes --scope all; inspect the report and commit feat: show services table with selected container actions.**

### Task 7: Browser review and full verification

**Files:** Modify only focused CSS/components or e2e tests where the review finds a concrete mismatch.

**Interfaces:** No new API. Verify the two implemented surfaces against docs/design/stacks-table-overhaul-concept.png and docs/design/services-table-overhaul-concept.png. Use the written spec when illustrative image copy differs from real data.

- [ ] **Step 1: Check whether the Browser plugin is available.** Use it first for visual and interaction review; if unavailable or unreliable, use Playwright Chromium and record that reason. Capture desktop and 320 px mobile views in light and dark themes, with selected rows and an open Actions menu.
- [ ] **Step 2: Inspect concept and rendered screenshots with view_image.** Compare at least five concrete points: table columns/order, name hierarchy, badge geometry/tones, selection/action separation, timestamp typography, and mobile overflow. Write down each mismatch and fix it before sign-off.
- [ ] **Step 3: Exercise the core workflows.** Select visible stacks and Deploy; verify mixed-state Restart/Stop gating and immediate partial-acceptance feedback. On Services, select one replica, then multiple compatible containers; verify exact IDs, action confirmation, operation progress, refresh after completion, and an error state. Verify keyboard menu, checkboxes, focus return, and long image/network/port data.
- [ ] **Step 4: Run final gates from the repository root:** rtk go test ./..., rtk go vet ./..., rtk go build -o /tmp/porty-e2e ./cmd/porty, and rtk sqlc generate. From web/: rtk bun run test, rtk bun run typecheck, rtk bun run build, and PORTY_E2E_BINARY=/tmp/porty-e2e rtk bun run test:e2e. Inspect only failed or changed output and resolve concrete failures.
- [ ] **Step 5: Run GitNexus detect_changes --scope all before any final commit.** Check git diff --check and git status --short. If a QA fix was required, commit it with a scoped Conventional Commit. Report screenshots, tests, any intentional visual deviations, and the final worktree state.

## Spec and Plan Self-Review

- The Stacks and Services columns, labels, action scopes, selection rules, data contracts, and preserved Validate/History behavior each map to a task above.
- No task adds a dependency, a shell command runner, or a generic table framework.
- The five Review Focus cases have explicit tests in Tasks 1–6 and browser checks in Task 7.
- The named endpoint, response keys, and component contracts above match the companion spec. All implementation tasks specify tests, code changes, and verification.
