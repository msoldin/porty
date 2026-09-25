# Server Overview Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Repository instructions prohibit subagents.

**Goal:** Make the landing page show all Porty stacks and all of their existing containers in two live tables, with safe cross-stack container actions.

**Architecture:** Keep `#/` and the current stack table. Reuse the authenticated per-stack container API through a bounded frontend reader and one overview-only polling hook. Group selected container IDs by stack and submit one existing batch action per stack; the Go server remains the authority for membership and state.

**Tech Stack:** Go, Preact, TypeScript, Vitest with Testing Library, Playwright, existing Docker Compose API. No new dependency or backend route.

**Spec:** `docs/superpowers/specs/2026-09-25-server-overview-design.md`

## Global Constraints

- Show active and archived Porty stacks and their existing containers; exclude unmanaged Docker containers.
- Show stopped containers and each scaled replica as distinct rows. Never turn a failed read into a false empty state.
- Limit overview reads to four concurrent stack requests and selection to 20 visible containers.
- Use existing authenticated container reads and stack-scoped batch mutations. Keep server validation, CSRF/origin checks, lock ordering, redaction, and bounded output intact.
- Keep API access in feature hooks or orchestration components, never in presentation-only table cells.
- Use `rtk` for shell commands. Run GitNexus impact before editing named symbols and `detect_changes --scope all` before each commit. Follow red-green tests and format with Prettier.

## Review Focus

- A read failure for one stack must leave other stacks visible, remove the failed stack's stale rows, and name the failed stack (Task 2 test).
- A container ID must be submitted only to its owning stack, even with replicas or identical service names in different stacks (Task 4 test).
- An archived stack must remain visible while its stack and container actions are unavailable (Tasks 1 and 4 tests).
- A state change or active operation between selection and click must disable unsafe actions; the server still rejects races (Task 4 test; existing Go control tests).
- A late poll after navigation or stack-set change must not restore removed rows or selections (Tasks 2 and 3 tests).

---

### Task 1: Make the existing Stacks table an inclusive Overview section

**Files:**
- Modify: `web/src/app/WorkspaceShell.tsx`, `web/src/features/stacks/StackDetail.tsx`
- Modify: `web/src/features/stacks/Dashboard.tsx`, `web/src/features/stacks/Dashboard.test.tsx`
- Test: `web/src/app/App.test.tsx`

**Interfaces:** Keep the existing `Dashboard` props and `#/` route. Archived rows use `stack.archivedAt` and cannot enter `selected`.

- [ ] **Step 1: Run GitNexus upstream impact for `Dashboard`, `WorkspaceShell`, and `StackDetail`.** Read their current callers and tests before editing.
- [ ] **Step 2: Add failing UI tests.** In `Dashboard.test.tsx`, add an archived fixture and assert that both active and archived names appear, the archived row has an `Archived` badge and disabled selection, and the existing stack actions never receive its ID. In `App.test.tsx`, assert that the main nav says `Overview` and the detail breadcrumb returns to it.

```tsx
const archived = { ...stacks[0], id: "old", directoryName: "old-app", archivedAt: "2026-09-01T00:00:00Z" };
expect(screen.getByRole("link", { name: "old-app" })).toBeInTheDocument();
expect(screen.getByRole("checkbox", { name: "Select old-app" })).toBeDisabled();
expect(screen.getByText("Archived")).toBeInTheDocument();
```

- [ ] **Step 3: Run the focused tests to see the expected failures.** `cd web && rtk bun run test -- src/features/stacks/Dashboard.test.tsx src/app/App.test.tsx`.
- [ ] **Step 4: Implement the section.** Make the page `<h1>Overview</h1>` and a Stacks `<h2>Stacks</h2>`. Remove the `!stack.archivedAt` row exclusion; add an All/Active/Archived filter separate from Modified. Keep the existing search, sort, New stack, and stack action menu. Render `Archived` beside the stack name; disable archived row selection and explicitly exclude archived IDs from `canDeploy` and `canRun`. Set the nav item's visible name to Overview while retaining its Stacks icon; change detail breadcrumb copy to Overview.

```tsx
const visible = stacks.filter((stack) =>
  archiveFilter === "active" ? !stack.archivedAt :
  archiveFilter === "archived" ? !!stack.archivedAt : true,
);
const actionable = selectedStacks.every((stack) => !stack.archivedAt);
```

- [ ] **Step 5: Run the focused tests and typecheck.** `cd web && rtk bun run test -- src/features/stacks/Dashboard.test.tsx src/app/App.test.tsx && rtk bun run typecheck`. Run GitNexus `detect_changes --scope all`, inspect affected flows, then commit `feat: show archived stacks in overview`.

### Task 2: Read all stack containers with bounded polling

**Files:**
- Create: `web/src/features/stacks/overviewContainers.ts`, `web/src/features/stacks/overviewContainers.test.ts`
- Create: `web/src/features/stacks/useOverviewContainers.ts`, `web/src/features/stacks/useOverviewContainers.test.tsx`
- Reuse: `web/src/features/stacks/api.ts` and its `listStackContainers(id)` contract; do not change the API.

**Interfaces:** `OverviewContainer = { stackId: string; container: Container }`; `OverviewReadError = { stackId: string; message: string }`; `OverviewSnapshot = { rows: OverviewContainer[]; errors: OverviewReadError[]; loading: boolean }`. Export `readOverviewContainers(ids: string[], read: (id: string) => Promise<Container[]>): Promise<Pick<OverviewSnapshot, "rows" | "errors">>` and `useOverviewContainers(ids: string[], refreshKey: string): OverviewSnapshot`.

- [ ] **Step 1: Add failing reader tests.** Stub six stack reads with deferred promises; assert no more than four are active, every replica becomes a separate `{stackId, container}` row, and one rejected read produces a named error while successful rows survive. Assert input order yields stable output order, independent of completion order.
- [ ] **Step 2: Run `cd web && rtk bun run test -- src/features/stacks/overviewContainers.test.ts` and confirm failure.**
- [ ] **Step 3: Implement the focused reader.** Use four async workers sharing a synchronous `next` index. Each worker writes its result into the original stack index, so completion order cannot reorder rows. Catch each stack failure, store `message(cause)`, and flatten only successful arrays. Do not add a generic concurrency utility.

```ts
export type OverviewContainer = { stackId: string; container: Container };
export type OverviewReadError = { stackId: string; message: string };
export type OverviewSnapshot = { rows: OverviewContainer[]; errors: OverviewReadError[]; loading: boolean };
```

- [ ] **Step 4: Add failing hook tests.** With fake timers, assert first read, next read five seconds after completion, no reads while `document.hidden`, immediate read on focus/visibility restoration and changed `refreshKey`, clearing removed-stack rows, and ignoring a late result after unmount or ID-set change. Hold one cycle open while changing `refreshKey`; assert the next cycle starts after it settles and never raises concurrent reads above four.
- [ ] **Step 5: Run `cd web && rtk bun run test -- src/features/stacks/useOverviewContainers.test.tsx` and confirm failure.**
- [ ] **Step 6: Implement the hook.** Derive a stable sorted ID key; queue a new cycle when that key or `refreshKey` changes. Serialize cycles through one in-flight promise so a changed key cannot launch four more requests while the previous cycle is open. Use a stopped/generation guard before every state update, keep a single timer scheduled after each completed read, and clean up focus/visibility listeners. On a failed stack read, publish only current successful rows and errors. Do not keep that stack's prior rows.
- [ ] **Step 7: Run both focused tests and typecheck.** Run GitNexus `detect_changes --scope all`, inspect affected flows, then commit `feat: poll overview containers across stacks`.

### Task 3: Render the all-services table with filtering and selection

**Files:**
- Create: `web/src/features/stacks/ServiceCells.tsx`
- Modify: `web/src/features/stacks/ServicesTable.tsx`, `web/src/features/stacks/ServicesTable.test.tsx`
- Create: `web/src/features/stacks/OverviewServices.tsx`, `web/src/features/stacks/OverviewServices.test.tsx`
- Modify: `web/src/app/styles.css`

**Interfaces:** `OverviewServices` accepts `{ stacks: Stack[]; operations: Operation[]; navigate: (path: string) => void; onOperationsAccepted: (operations: Operation[]) => void }`. It owns search/filter/selection and calls `useOverviewContainers`. `ServiceCells` accepts `{ container: Container }` and renders the five shared data cells; it makes no API call.

- [ ] **Step 1: Run GitNexus upstream impact for `ServicesTable` and `containerStatePresentation`.** Check current detail-table behavior before extracting shared cells.
- [ ] **Step 2: Add failing table tests.** Assert a selection checkbox column followed by `Stack, Service, State, Image, Networks, Ports`; two same-service replicas remain separate; running, stopped, and unhealthy text is visible; archived stack label appears; stack links target encoded stack IDs. Assert search matches stack/container/service/image/network and the state filter works. Assert independent selection, max-20 select-all behavior, selection removal when a row disappears or no longer matches the filter after polling, and explicit loading/empty/partial-error states.

```tsx
expect(within(screen.getByRole("region", { name: "All services table" })).getAllByRole("row")).toHaveLength(4);
expect(screen.getByRole("link", { name: "alpha" })).toHaveAttribute("href", "#/stacks/one");
expect(screen.getByText(/Partial inventory/)).toBeInTheDocument();
```

- [ ] **Step 3: Run `cd web && rtk bun run test -- src/features/stacks/OverviewServices.test.tsx` and confirm failure.**
- [ ] **Step 4: Extract the existing Service, State, Image, Networks, and Ports `<td>` rendering into `ServiceCells`.** Keep `ServicesTable` selection/actions unchanged and rerun its tests to prove detail behavior survives.
- [ ] **Step 5: Implement `OverviewServices`.** Map stack IDs to current stack names, sort rows by stack/service/container/ID, render the Stack link and `ServiceCells`, and show loaded/running counts. Keep the selected key as `stackId + ":" + container.id`; remove keys absent from the latest snapshot or no longer visible after polling. Search/filter changes clear selection. A partial read names failed stacks and avoids complete-inventory wording.
- [ ] **Step 6: Add responsive CSS.** Give the overview table a labelled scroll region and its own column widths; make its checkbox and Stack columns sticky at mobile widths. Check 320 px, light/dark contrast, keyboard focus, and no page-level horizontal overflow.
- [ ] **Step 7: Run focused tests, typecheck, and build.** Run GitNexus `detect_changes --scope all`, inspect affected flows, then commit `feat: show services from every stack`.

### Task 4: Submit safe cross-stack container actions

**Files:**
- Modify: `web/src/features/stacks/OverviewServices.tsx`, `web/src/features/stacks/OverviewServices.test.tsx`
- Use: `web/src/features/stacks/api.ts` `runContainerBatchAction(id, ids, action)`
- Use: `web/src/features/operations/types.ts` `Operation`

**Interfaces:** Keep the Task 3 `OverviewServices` props. The component calls `onOperationsAccepted(accepted)` once for the successfully accepted stack-scoped operations.

- [ ] **Step 1: Add failing action tests.** Select containers from two stacks and assert exactly two requests with only their own full IDs. Assert Start accepts only created/exited, Stop/Restart only running, archived and active-operation rows cannot be acted on, and unknown/mixed states disable the buttons. Assert Stop confirmation includes both counts. Make one request reject and one resolve; assert the accepted operation is registered, feedback names both outcomes, and only failed group's rows remain selected.

```tsx
expect(runContainerBatchAction).toHaveBeenCalledWith("one", ["id-a", "id-b"], "restart");
expect(runContainerBatchAction).toHaveBeenCalledWith("two", ["id-c"], "restart");
expect(onOperationsAccepted).toHaveBeenCalledWith([expect.objectContaining({ scopeId: "one" })]);
```

- [ ] **Step 2: Run the focused test and confirm failure.** `cd web && rtk bun run test -- src/features/stacks/OverviewServices.test.tsx`.
- [ ] **Step 3: Implement the selected-actions toolbar.** Compute eligibility from current rows and operations at click time. Group `{stackId, container.id}` by stack, call `runContainerBatchAction` once per group through `Promise.allSettled`, register fulfilled operations, clear accepted-group selection, retain rejected-group selection, and announce named outcomes. Disable submission while requests are pending. Keep server validation as the final guard.
- [ ] **Step 4: Run the focused tests and the existing Go container tests.** `cd web && rtk bun run test -- src/features/stacks/OverviewServices.test.tsx src/features/stacks/ServicesTable.test.tsx`; from root run `rtk go test ./internal/control ./internal/http`. Run GitNexus `detect_changes --scope all`, inspect affected flows, then commit `feat: control services across stacks from overview`.

### Task 5: Integrate and verify the landing page

**Files:**
- Modify: `web/src/features/stacks/Dashboard.tsx`, `web/src/features/stacks/Dashboard.test.tsx`
- Modify: `web/e2e/critical.spec.ts`
- Modify: `web/src/app/styles.css` only for final layout corrections

**Interfaces:** `Dashboard` passes all stacks and operations to `OverviewServices`; its `onOperationsAccepted` callback feeds the existing operation stream. `OverviewServices` builds `refreshKey` from completed container operation IDs/statuses so its hook refreshes immediately after actions.

- [ ] **Step 1: Add failing integration tests.** Assert both named tables appear at `#/`; stacks search does not hide services; services search does not hide stacks; completed container operations trigger a new services read; stack detail navigation and its Services table remain available.
- [ ] **Step 2: Run focused tests and confirm failure.** `cd web && rtk bun run test -- src/features/stacks/Dashboard.test.tsx`.
- [ ] **Step 3: Mount `OverviewServices` beneath the Stacks section.** Keep the current New stack and stack action flows, and pass the existing operation-registration callback. Update test mocks of `./api` to include `listStackContainers` where needed.
- [ ] **Step 4: Extend Playwright with two managed stacks and containers.** Route each stack's container GET to distinct full IDs, rather than reusing the existing test's common fixture. Check the two tables, cross-stack service grouping and accepted operations on desktop; check labelled horizontal scroll, sticky identity, and no document overflow on mobile. Capture desktop and mobile screenshots for the visible UI change.
- [ ] **Step 5: Run final gates.** From root: `rtk go test ./...`, `rtk go vet ./...`, `rtk go build ./cmd/porty`, `rtk ./deploy/package_test.sh`; from `web/`: `rtk bun run test`, `rtk bun run typecheck`, `rtk bun run build`, `rtk bun run test:e2e`. Run `rtk git diff --check` and GitNexus `detect_changes --scope all`; inspect any risks. Confirm generated `web/dist` matches the frontend build, then commit `feat: add server overview tables`.

## Completion Check

Review the design spec against the rendered page: all active and archived stacks, all existing managed containers, honest partial-read states, grouped actions, and safe mobile tables. Check that no new backend route or dependency was introduced. Record desktop/mobile screenshots and verification results in the pull request description.
