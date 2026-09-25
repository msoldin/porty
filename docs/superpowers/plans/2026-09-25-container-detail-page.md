# Container Detail Page Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Repository instructions prohibit subagents.

**Goal:** Open a page for one existing Docker container with its current metadata, logs, full inspect JSON, and start/stop/restart actions.

**Architecture:** Extend the existing stacks feature and hash router. Reuse the container list for summary polling and the existing single-container action endpoint. Add two authenticated, exact-ID read endpoints backed by the current Docker SDK connection.

**Tech Stack:** Go, Docker Compose v5, Moby Docker SDK, Preact, TypeScript, Vitest/Testing Library, Playwright.

**Spec:** `docs/superpowers/specs/2026-09-25-container-detail-page-design.md`

## Global Constraints

- A page represents one Docker container ID, including one replica of a scaled service.
- Logs return the latest 500 lines with a 1 MiB snapshot limit and existing Compose-environment redaction.
- Inspect returns the full Docker JSON, including environment values, on explicit tab load/Refresh; reject payloads above 2 MiB and set `Cache-Control: no-store`.
- Reads verify the ID belongs to the requested stack's Compose project on every request. Mutations retain existing CSRF/origin checks and operation locking.
- Use the existing Docker SDK connection. Add no dependency and build no shell command.
- Before each symbol edit, run GitNexus `impact` as required by `GIT_NEXUS.md`; before each commit, run `detect_changes --scope all`. Treat `UNKNOWN` as unresolved and check text references.
- Follow repository TDD guidance: add a failing regression test first, then focused and affected suites.

## Review Focus

1. A sibling replica or a container from another Compose project must never supply logs or inspect data for the selected ID. Task 2 tests both cases.
2. An unreadable log driver, non-TTY multiplexing, TTY plain output, and an oversized log line must produce a bounded, intelligible result. Task 1 tests each case.
3. Inspect may contain secrets and JSON numbers larger than JavaScript's safe integer range; only authenticated users receive it, the browser shows/copies the original JSON text, and responses are not cached. Tasks 2 and 5 test this.
4. A stale detail URL after redeploy must show a not-found state and disable actions rather than control a replacement container. Tasks 2 and 4 test this.
5. A completed operation, tab switch, or hidden browser tab must not leave polling active or stale action state. Tasks 4 and 5 test this.

## File Map

| File | Responsibility |
| --- | --- |
| `internal/compose/container_details.go` | Docker SDK inspect/log reads, stream decoding, bounds, safe errors |
| `internal/compose/client.go` | Extend the existing SDK-facing client interface/constructor only |
| `internal/compose/sdk_client_test.go` | SDK read behavior with fake Docker client |
| `internal/control/containers.go` | Verify stack/project membership for both read methods |
| `internal/control/control.go` | Extend runtime interface for the two reads |
| `internal/control/control_test.go` | Membership and exact-ID tests |
| `internal/http/routes_containers.go` | Two GET routes and no-store response |
| `internal/http/auth.go`, `internal/http/api.go`, `internal/http/api_test.go` | Narrow API interface, safe error/status handling, route tests |
| `web/src/app/Workspace.tsx`, `web/src/app/routes.ts` | Distinguish stack and container hash routes |
| `web/src/features/stacks/containerRoute.ts`, `ServiceCells.tsx`, `ServicesTable.tsx`, `OverviewServices.tsx` | Container links in both tables |
| `web/src/features/stacks/ContainerDetail.tsx`, `useContainerLogs.ts`, `api.ts`, `types.ts` | Detail page, actions, log polling, inspect text retrieval |
| `web/src/lib/http.ts` | Reuse authenticated fetch/refresh flow for text responses |
| `web/src/app/styles.css` | Detail layout and mobile overflow |
| `web/e2e/critical.spec.ts` | Click-through journey and action/log/inspect smoke test |

---

### Task 1: Read one container through the Docker SDK

**Files:** Create `internal/compose/container_details.go`; modify `internal/compose/client.go`; test `internal/compose/sdk_client_test.go`.

**Interfaces:** Consume the existing `Client.containers` Docker connection and `Request.Environment`. Produce `Client.ContainerLogs(ctx, request, id) (ContainerLogSnapshot, error)` and `Client.ContainerInspect(ctx, request, id) (json.RawMessage, error)`. `ContainerLogSnapshot` has JSON fields `Output string` and `Truncated bool`.

```go
type ContainerLogSnapshot struct {
	Output    string `json:"output"`
	Truncated bool   `json:"truncated"`
}
// Docker read methods use client.ContainerLogsOptions and client.ContainerInspectOptions.
```

- [ ] **Step 1: Impact and failing tests.** Run GitNexus impact for `Client` and `newWithContainerActions`. Add fake SDK methods `ContainerLogs` and `ContainerInspect`, then tests named `TestContainerLogsReadsOnlySelectedID`, `TestContainerLogsDemultiplexesAndBoundsOutput`, `TestContainerLogsHandlesTTYAndRedactsSecrets`, `TestContainerInspectReturnsFullRawJSON`, and `TestContainerInspectRejectsOversizedJSON`. Assert exact ID, `Tail: "500"`, both streams, `Follow: false`, close of log reader, `truncated`, no exposed configured secret in logs/errors, unchanged inspect `Config.Env`, and size rejection.
- [ ] **Step 2: Run the focused test red.** `rtk go test ./internal/compose -run 'TestContainer(Logs|Inspect)' -count=1`; expect failure from missing methods/behavior.
- [ ] **Step 3: Implement SDK reads.** Extend the Docker-facing interface with the SDK's `ContainerLogs` and `ContainerInspect` signatures. Add a focused file with these methods; use the existing timeout, `safeError`, and `boundedComposeOutput`. Inspect with `client.ContainerInspectOptions{Size:false}`; use `result.Raw`, reject invalid or `len(raw) > 2<<20` with a sentinel error. Logs use `client.ContainerLogsOptions{ShowStdout:true, ShowStderr:true, Tail:"500"}`, inspect `result.Container.Config.Tty` to select plain copy or `stdcopy.StdCopy`, close the reader, and cap decoded output at 1 MiB. Return `Truncated:true` when output was capped. Never return a partial inspect document.
- [ ] **Step 4: Run focused and affected tests.** `rtk go test ./internal/compose -count=1`; verify all new cases pass. Run `rtk gofmt -w` on changed Go files.
- [ ] **Step 5: Review and commit.** `rtk node .gitnexus/run.cjs detect-changes --scope all --repo .`; inspect all affected flows, then commit `feat: read individual container logs and inspect via docker sdk`.

### Task 2: Enforce membership and expose read endpoints

**Files:** Modify `internal/control/containers.go`, `internal/control/control.go`, `internal/http/routes_containers.go`, `internal/http/auth.go`, `internal/http/api.go`; test `internal/control/control_test.go`, `internal/http/api_test.go`.

**Interfaces:** Consume Task 1's SDK methods. Produce `ControlPlane.ContainerLogs(ctx, stackID, containerID)` and `ControlPlane.ContainerInspect(ctx, stackID, containerID)`; expose `GET /api/v1/stacks/{id}/containers/{containerId}/logs` and `/inspect`.

```go
ContainerLogs(context.Context, portystack.StackID, string) (portycompose.ContainerLogSnapshot, error)
ContainerInspect(context.Context, portystack.StackID, string) (json.RawMessage, error)
```

- [ ] **Step 1: Impact and failing control tests.** Run impact for `ControlPlane.Containers`, `RuntimeController`, and `registerContainerRoutes`. Add `TestContainerDetailsRejectWrongProjectAndMissingID` and `TestContainerDetailsReadSelectedReplica`: seed two replicas plus a foreign project row, request each ID for logs and inspect, assert only the exact owned ID reaches the fake runtime and foreign/missing IDs return `ErrContainerNotFound`.
- [ ] **Step 2: Add failing route tests.** Extend `fakeContainerAPI`. `TestContainerDetailsRoutesRequireSessionAndNoStore` asserts 401 without a session, 200 with one, `Cache-Control: no-store` on success and error, JSON logs shape, and exact inspect JSON including a fixture `"Config":{"Env":["TOKEN=secret"]}`. `TestContainerDetailsRoutesMapNotFoundAndLimit` asserts 404/413 and no inspect data in error bodies. A malformed/unknown container path must not invoke the control layer.
- [ ] **Step 3: Run focused tests red.** `rtk go test ./internal/control ./internal/http -run 'TestContainerDetails' -count=1`; expect missing method/route failures.
- [ ] **Step 4: Implement common membership lookup.** In `containers.go`, resolve the stack by opaque ID, load its environment, call `Status(All:true)`, and select only a row whose full `ID` and `Project` exactly match. Reuse this helper for both reads; do not rely on the frontend's selected ID. Extend `RuntimeController` and `ContainerAPI` with the two narrow read methods. Map the SDK's inspect-size sentinel to a control error usable by the HTTP layer.
- [ ] **Step 5: Implement routes.** Register both GET paths with `readRoute`, wrapped so `Cache-Control: no-store` is set before auth and every response. For inspect, indent the raw JSON with `json.Indent` so the text displayed/copied by the browser preserves numeric lexemes, then write it as `application/json`; map missing ID to 404 and oversize to 413. For logs, use existing `writeResult` with `{output,truncated}`. Do not put raw inspect JSON in errors, audit records, or logs.
- [ ] **Step 6: Verify and commit.** Run `rtk gofmt -w` on changed Go files, `rtk go test ./internal/control ./internal/http -count=1`, and the GitNexus change analysis. Commit `feat: expose owned container logs and inspect`.

### Task 3: Build overview and single-container actions

**Files:** Create `web/src/features/stacks/ContainerDetail.tsx` and `ContainerDetail.test.tsx`; modify `web/src/features/stacks/types.ts` and `web/src/app/styles.css`.

**Interfaces:** Consume `useStackContainers(stack.id, true, refreshKey)`, `runContainerAction`, `containerStatePresentation`, `formatContainerPort`, and the parent `onAction` callback. Produce the Overview tab and action header for the exact ID.

```ts
const canStart = container && ["created", "exited"].includes(container.state);
const canRun = container?.state === "running";
// Also require !busy, !activeOperation, !stack.archivedAt, !dirty, and no read error.
```

- [ ] **Step 1: Failing component tests.** Run impact for `useStackContainers`, `runContainerAction`, and the presentation helpers. Test the exact replica's ID/name/service/state/health/image/networks/ports; a sibling never appears in the detail; `created`/`exited` offer Start, `running` offers Stop/Restart, and paused/unknown offer none. Assert archived, dirty, active operation, pending request, loading/error, and vanished ID disable actions. Test Stop confirmation, request error notice, accepted operation passed to `onAction`, and refresh after a completed operation.
- [ ] **Step 2: Run component tests red.** `rtk bun run test -- ContainerDetail` from `web/`; expect missing page behavior.
- [ ] **Step 3: Implement summary/actions.** Render breadcrumbs and an Overview summary using current presentation helpers; Task 5 adds the Logs and Inspect tabs. Derive current container from the polled stack list by exact ID. Call `runContainerAction(stack.id, container.id, action)`; use the same operation, archived, dirty, and state guards as `StackDetail`. Confirm Stop and show request errors. Keep accepted operations in the existing drawer through `onAction`. Add responsive styles scoped to the detail page.
- [ ] **Step 4: Verify and commit.** Run component tests and `rtk bun run typecheck`, then GitNexus change analysis. Commit `feat: show container overview and actions`.

### Task 4: Make container rows navigable

**Files:** Create `web/src/app/routes.ts`, `web/src/features/stacks/containerRoute.ts`; modify `web/src/app/Workspace.tsx`, `web/src/features/stacks/ServiceCells.tsx`, `ServicesTable.tsx`, `OverviewServices.tsx`, `StackDetail.tsx`; test colocated table tests and `web/src/app/App.test.tsx`.

**Interfaces:** Consume Task 3's `ContainerDetail`. Produce `containerRoute(stackId: string, containerId: string): string` and `parseStackRoute(route: string): {stackId:string; containerId?:string} | null`. `ServiceCells` receives an optional container-detail href/click callback. The new route is `#/stacks/{stackId}/containers/{containerId}` with both IDs encoded separately.

```ts
const containerRoute = (stackId: string, containerId: string) =>
  `/stacks/${encodeURIComponent(stackId)}/containers/${encodeURIComponent(containerId)}`;
```

- [ ] **Step 1: Impact and failing tests.** Run impact for `Workspace`, `ServiceCells`, `ServicesTable`, and `OverviewServices`. Test a service-name link in both tables, checkbox selection remaining independent, the parent stack link remaining intact, back/forward/reload of a detail hash, malformed percent escapes, and the existing `#/stacks/{id}` route. For a test ID containing an encoded slash, route parsing must reject it as unknown rather than silently selecting a different stack.
- [ ] **Step 2: Run focused tests red.** `rtk bun run test -- ServiceCells ServicesTable OverviewServices App` from `web/`; expect missing link and route failures.
- [ ] **Step 3: Implement route parsing and links.** Match only `^/stacks/([^/]+)(?:/containers/([^/]+))?$`; decode segments individually and reject empty IDs, malformed escapes, or decoded `/`. Build hrefs with `encodeURIComponent`. Put the anchor around the existing service/name identity in `ServiceCells`; pass `navigate` from both table parents, and stop its click from toggling selection. In `Workspace`, render `ContainerDetail` for a valid nested route and keep `StackDetail` for the exact stack route.
- [ ] **Step 4: Verify and commit.** Run focused frontend tests, `rtk bun run typecheck`, and GitNexus change analysis. Commit `feat: link service rows to container detail route`.

### Task 5: Add Logs and Inspect tabs and finish the browser journey

**Files:** Create `web/src/features/stacks/useContainerLogs.ts` and its test; modify `ContainerDetail.tsx`, `ContainerDetail.test.tsx`, `web/src/features/stacks/api.ts`, `types.ts`, `web/src/lib/http.ts`, `web/src/app/styles.css`, and `web/e2e/critical.spec.ts`.

**Interfaces:** Produce `getContainerLogs(stackId, containerId): Promise<{output:string; truncated:boolean}>`, `getContainerInspect(stackId, containerId): Promise<string>`, and an authenticated `apiText` sibling to `api` that shares session-refresh/error behavior. `useContainerLogs` returns output, loading/error, and truncated state.

```ts
export type ContainerLogs = { output: string; truncated: boolean };
export const getContainerLogs = (stackId: string, containerId: string) =>
  api<ContainerLogs>(`${stackPath(stackId)}/containers/${encodeURIComponent(containerId)}/logs`);
export const getContainerInspect = (stackId: string, containerId: string) =>
  apiText(`${stackPath(stackId)}/containers/${encodeURIComponent(containerId)}/inspect`);
```

- [ ] **Step 1: Failing API/hook/component tests.** Test `apiText` retries a 401 through the same refresh path, returns the inspect response as text, and surfaces the existing `APIError`. Test logs poll every five seconds only on the active visible tab, refresh on focus, ignore late responses after ID/tab changes, and stop timers on unmount. Test inspect fetches once on opening its tab, refreshes only on explicit click, displays a full JSON fixture including `TOKEN=secret` and `9007199254740993` unchanged, and copies the original text. Test log truncation and read-error notices.
- [ ] **Step 2: Run focused tests red.** `rtk bun run test -- useContainerLogs ContainerDetail http` from `web/`; expect missing tab/API behavior.
- [ ] **Step 3: Implement response retrieval.** Refactor `web/src/lib/http.ts` only enough for `api` and `apiText` to share credentials, refresh retry, and error decoding. Use the existing stack API path builder and encode the container ID. Keep inspect as text end to end; do not parse/stringify it in JavaScript. Add Logs and Inspect tab states to `ContainerDetail`, a visible-tab-only polling hook for logs, an explicit inspect Refresh button, selectable preformatted output, and Copy via `navigator.clipboard.writeText` with user-visible failure feedback.
- [ ] **Step 4: Browser test and visual check.** In `web/e2e/critical.spec.ts`, stub the two new endpoints, click a container from the dashboard, verify the selected ID and action, visit Logs and Inspect, then click a second container from the stack table. Check desktop and 390px mobile widths for wrapping actions and usable scroll panes; capture screenshots for the PR.
- [ ] **Step 5: Full verification and commit.** Run `rtk go test ./...`, `rtk go vet ./...`, `rtk go build ./cmd/porty`, `(cd web && rtk bun run test)`, `(cd web && rtk bun run typecheck)`, `(cd web && rtk bun run build)`, and `(cd web && rtk bun run test:e2e)`. Run GitNexus change analysis, review its affected flows and the final diff, then commit `feat: show container logs and inspect`.

## Completion Criteria

Both Services tables link to a stable, exact-container page. Overview matches the existing displayed fields. Logs never mix replicas and remain bounded/redacted. Inspect shows the full JSON to an authenticated user without browser caching or precision loss. Start/Stop/Restart keep the existing action and operation behavior. Focused and full checks pass, and desktop/mobile screenshots are attached to the PR if one is opened.
