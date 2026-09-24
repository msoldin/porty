# Stack Overview Container Controls Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Repository instructions prohibit subagents.

**Goal:** List every existing container in Stack Overview and allow actions on one container at a time while retaining the header's whole-stack actions.

**Architecture:** The Compose adapter continues to list all project containers and gains targeted start, stop, and restart calls through its existing Docker SDK connection. The control plane checks stack membership and state under the stack operation lock before accepting an asynchronous action. Dedicated HTTP routes and a stacks-feature polling hook feed a container list in Overview.

**Tech Stack:** Go 1.27.1; Docker Compose v5.5.1; Moby client v0.5.1; Preact 10; TypeScript; Vitest with Testing Library; Playwright.

**Spec:** `docs/superpowers/specs/2026-09-24-stack-overview-container-controls-design.md`

## Global Constraints

- Keep the page header's existing whole-stack Stop, Restart, Validate, and Deploy actions and their current runtime gates. Remove only Refresh status, Start stack, Pull images, and Recreate containers from the Overview panel.
- Show one row per existing Docker container, including stopped instances and separate replicas. Do not manufacture rows for declared services without a container.
- Running containers allow Stop and Restart; created and exited containers allow Start; all other states allow none. Archived stacks are read-only.
- Poll only while Overview is visible, every five seconds and on focus or visibility return; refresh after a container operation completes. Clear stale actions on read failure.
- Reuse the existing Compose SDK connection, stack operation lock, auth/origin/CSRF guards, bounded secret redaction, and operation stream. Add no dependency or shell command.
- Use `gofmt` and Prettier. Write failing behavior tests before production changes. Before each symbol edit run GitNexus `impact`; before every commit run `detect_changes --scope all` and inspect the report. `ControlPlane` has HIGH upstream impact through `internal/app`, so preserve its constructor and the existing stack-action interface.

## File Structure

| File | Responsibility |
| --- | --- |
| `internal/compose/client.go`, `docker.go`, `sdk_client_test.go` | Target one validated Docker container through the SDK client already used by Compose. |
| `internal/control/containers.go`, `control.go`, `control_test.go` | Container response, state rules, stack membership, archive check, and queued action. |
| `internal/http/auth.go`, `routes_containers.go`, `api.go`, `api_test.go` | Narrow HTTP interface, guarded routes, and typed error mapping. |
| `internal/app/app.go` | Wire the existing control plane to the new HTTP interface. |
| `web/src/features/stacks/types.ts`, `api.ts`, `useStackContainers.ts`, `useStackContainers.test.tsx` | Typed container contract, calls, and Overview-only polling. |
| `web/src/features/stacks/ContainerList.tsx`, `ContainerList.test.tsx`, `StackDetail.tsx`, `StackDetail.test.tsx` | Per-instance rows and actions, Overview integration, and header regression coverage. |
| `web/src/app/styles.css` | Readable desktop and narrow-screen list layout. |

## Review Focus

1. Two replicas of one service must have separate rows; clicking one must send only that full container ID to Docker. Pin in Tasks 1, 2, and 5.
2. A stale, guessed, or other-project ID must get 404 before an operation is created; no SDK mutation may run. Pin in Tasks 2 and 3.
3. A state change between poll and click, a paused container, or an archived stack must not start an invalid action. Pin in Tasks 2 and 5.
4. A Docker read failure after a successful poll must remove stale controls and recover automatically when Docker returns. Pin in Tasks 4 and 5.
5. Auth/origin/CSRF failure or a secret-bearing Docker error must never cause an unauthorized mutation or leak a secret. Pin in Tasks 1 and 3.

---

### Task 1: Target one container through the existing Docker SDK client

**Files:** Modify `internal/compose/client.go`, `internal/compose/docker.go`, `internal/compose/sdk_client_test.go`, and `internal/compose/docker_test.go` only if its return-type assertions need adjustment.

**Interfaces:** Produce `Client.ContainerAction(context.Context, Request, string, string) error`. Keep `New(service api.Compose, timeout time.Duration) *Client` for existing tests and callers. Add a private constructor accepting a narrow `containerActions` interface. `NewDockerClient` still returns `(*Client, io.Closer, error)`.

- [ ] **Step 1: Check impact.** Run GitNexus upstream impact on `Struct:internal/compose/client.go:Client`, `NewDockerClient`, and `newDockerService`; inspect direct callers before editing.
- [ ] **Step 2: Write failing targeted-action tests.** In `sdk_client_test.go`, create a fake with the three Moby methods and record the ID and action. Test `start`, `stop`, and `restart` against ID `full-id-b` while another replica `full-id-a` exists in the fixture. Test an error containing `TOKEN=secret` plus over 1 MiB of text.

  ```go
  type recordingContainers struct {
      client.APIClient
      calls []string
      err error
  }
  func (r *recordingContainers) ContainerStart(_ context.Context, id string, _ client.ContainerStartOptions) (client.ContainerStartResult, error) {
      r.calls = append(r.calls, "start:"+id)
      return client.ContainerStartResult{}, r.err
  }
  func (r *recordingContainers) ContainerStop(_ context.Context, id string, _ client.ContainerStopOptions) (client.ContainerStopResult, error) {
      r.calls = append(r.calls, "stop:"+id)
      return client.ContainerStopResult{}, r.err
  }
  func (r *recordingContainers) ContainerRestart(_ context.Context, id string, _ client.ContainerRestartOptions) (client.ContainerRestartResult, error) {
      r.calls = append(r.calls, "restart:"+id)
      return client.ContainerRestartResult{}, r.err
  }
  func TestContainerActionTargetsOnlySelectedID(t *testing.T) {
      docker := &recordingContainers{}
      runtime := newWithContainerActions(&recordingCompose{}, docker, time.Minute)
      for _, action := range []string{"start", "stop", "restart"} {
          if err := runtime.ContainerAction(context.Background(), Request{}, "full-id-b", action); err != nil { t.Fatal(err) }
      }
      if got := strings.Join(docker.calls, ","); got != "start:full-id-b,stop:full-id-b,restart:full-id-b" { t.Fatal(got) }
  }
  func TestContainerActionRedactsAndBoundsDockerError(t *testing.T) {
      docker := &recordingContainers{err: errors.New("secret" + strings.Repeat("x", maxCommandOutput))}
      runtime := newWithContainerActions(&recordingCompose{}, docker, time.Minute)
      err := runtime.ContainerAction(context.Background(), Request{Environment: map[string]string{"TOKEN": "secret"}}, "full-id-b", "stop")
      if err == nil || strings.Contains(err.Error(), "secret") || len(err.Error()) > maxCommandOutput+len("Compose: ") { t.Fatalf("unsafe error: %v", err) }
  }
  ```
- [ ] **Step 3: Run `rtk go test ./internal/compose -run 'TestContainerAction' -count=1`; expect failure because the method is absent.**
- [ ] **Step 4: Implement the narrow SDK seam and dispatch.** Change `newDockerService` to return `sdkclient.SDKClient` as its second value; it already returns the SDK object dynamically as `io.Closer`. Pass it into the private constructor from `NewDockerClient`. Keep the existing exported constructor unchanged. Apply `c.timeout` and `c.safeError(err, request)` around each SDK call.

  ```go
  type containerActions interface {
      ContainerStart(context.Context, string, client.ContainerStartOptions) (client.ContainerStartResult, error)
      ContainerStop(context.Context, string, client.ContainerStopOptions) (client.ContainerStopResult, error)
      ContainerRestart(context.Context, string, client.ContainerRestartOptions) (client.ContainerRestartResult, error)
  }
  func newWithContainerActions(service api.Compose, docker containerActions, timeout time.Duration) *Client {
      result := New(service, timeout)
      result.containers = docker
      return result
  }
  func (c *Client) ContainerAction(parent context.Context, request Request, id, action string) error {
      if c.containers == nil { return errors.New("Docker client unavailable") }
      ctx, cancel := context.WithTimeout(parent, c.timeout)
      defer cancel()
      var err error
      switch action {
      case "start": _, err = c.containers.ContainerStart(ctx, id, client.ContainerStartOptions{})
      case "stop": _, err = c.containers.ContainerStop(ctx, id, client.ContainerStopOptions{})
      case "restart": _, err = c.containers.ContainerRestart(ctx, id, client.ContainerRestartOptions{})
      default: return errors.New("unsupported container action")
      }
      return c.safeError(err, request)
  }
  ```
- [ ] **Step 5: Run `rtk gofmt -w internal/compose/client.go internal/compose/docker.go internal/compose/sdk_client_test.go`, `rtk go test ./internal/compose -count=1`, and `rtk go test ./internal/app -count=1`.** Confirm stack-wide Compose Stop and Restart tests still pass.
- [ ] **Step 6: Run GitNexus `detect_changes --scope all`; inspect non-low risk and commit `feat: target individual containers through Docker SDK`.**

### Task 2: List and validate containers in the control plane

**Files:** Create `internal/control/containers.go`; modify `internal/control/control.go`, `internal/control/control_test.go`.

**Interfaces:** Produce `control.Container{ID, Name, Service, State, Health string}`, `(*ControlPlane).Containers(context.Context, stack.StackID) ([]Container, error)`, and `(*ControlPlane).StartContainerAction(context.Context, stack.StackID, string, string) (operation.Operation, error)`. Export `ErrContainerNotFound`, `ErrContainerStateConflict`, `ErrContainerArchived`, and `ErrUnsupportedContainerAction` for Task 3. Add `ContainerAction(context.Context, compose.Request, string, string) error` to `RuntimeController`; preserve `NewControlPlane`'s signature.

- [ ] **Step 1: Check impact.** Run upstream impact on `RuntimeController`, `ControlPlane`, `NewControlPlane`, and `StartAction`. Record the HIGH `ControlPlane` risk and confirm no constructor or stack action change is needed.
- [ ] **Step 2: Write failing control tests.** Extend the existing `controlRuntime` fake with `status []api.ContainerSummary`, `calledID`, and `calledAction`; keep the old empty-status default. Add `TestContainersIncludeStoppedAndSeparateReplicas`, `TestContainerActionRejectsWrongProjectAndMissingID`, `TestContainerActionRejectsIncompatibleStateAndArchivedStack`, and `TestContainerActionTargetsOnlySelectedReplicaAndRespectsLock`.

  ```go
  rows := []api.ContainerSummary{
      {ID: "id-a", Name: "app-1", Project: "porty-gateway", Service: "app", State: "running"},
      {ID: "id-b", Name: "app-2", Project: "porty-gateway", Service: "app", State: "exited"},
      {ID: "id-c", Name: "other-1", Project: "other", Service: "app", State: "running"},
  }
  runtime := &controlRuntime{status: rows}
  operations := &countingOperationStore{}
  coordinator := portyop.NewCoordinator()
  control := portycontrol.NewControlPlane("/srv/repository", controlLookup{},
      portystack.NewEnvironmentService(controlEnvironmentStore{}), nil, runtime,
      portyop.NewOperationService(operations, nil, time.Second, 1024), nil,
      coordinator, nil, nil)
  items, err := control.Containers(context.Background(), "stk_gateway")
  if err != nil || len(items) != 2 || items[0].ID != "id-a" || items[1].ID != "id-b" { t.Fatalf("containers = %#v, %v", items, err) }
  _, err = control.StartContainerAction(context.Background(), "stk_gateway", "id-c", "stop")
  if !errors.Is(err, portycontrol.ErrContainerNotFound) || operations.created != 0 { t.Fatalf("wrong-project action = %v, created = %d", err, operations.created) }
  ```
- [ ] **Step 3: Run `rtk go test ./internal/control -run 'TestContainer' -count=1`; expect the new API/tests to fail.**
- [ ] **Step 4: Implement mapping and validation.** In `containers.go`, map only rows where `row.Project == stack.ComposeProjectName`, return `[]Container{}` for none, and sort by service, name, then ID for stable rows. Accept `start` only for `created`/`exited`; accept `stop`/`restart` only for `running`. In `StartContainerAction`, reject invalid action, acquire `coordinator.Try(false, string(id))`, resolve stack and archived state, load stored environment, call runtime `Status`, match the exact full ID and project, check state, then call `operations.Start` with kind `container_` plus action, scope type `stack`, scope ID `string(id)`, and `mapValues(values)` as secrets. Keep the lock until the job ends; release it immediately if `operations.Start` fails.

  ```go
  func containerActionAllowed(state, action string) bool {
      switch action {
      case "start": return state == "created" || state == "exited"
      case "stop", "restart": return state == "running"
      default: return false
      }
  }
  operation, startErr := c.operations.Start(ctx, portyop.OperationRequest{
      Kind: "container_" + action, ScopeType: "stack", ScopeID: string(id), Secrets: mapValues(values),
  }, func(jobCtx context.Context) (string, error) {
      defer release()
      return "", c.runtime.ContainerAction(jobCtx, request, containerID, action)
  })
  if startErr != nil { release() }
  return operation, startErr
  ```
- [ ] **Step 5: Run `rtk gofmt -w internal/control/containers.go internal/control/control.go internal/control/control_test.go`, `rtk go test ./internal/control ./internal/operation -count=1`, and `rtk go build ./cmd/porty`.** Check that existing StackState aggregation and whole-stack action tests still pass.
- [ ] **Step 6: Run GitNexus `detect_changes --scope all`; inspect affected app/control flows and commit `feat: validate per-container stack actions`.**

### Task 3: Expose guarded container HTTP routes

**Files:** Create `internal/http/routes_containers.go`; modify `internal/http/auth.go`, `internal/http/api.go`, `internal/http/api_test.go`, `internal/app/app.go`.

**Interfaces:** Add `RouterOptions.Containers ContainerAPI` with the two Task 2 methods. Register `GET /api/v1/stacks/{id}/containers` and `POST /api/v1/stacks/{id}/containers/{containerId}/actions/{action}`. Keep the existing stack action route and its interface.

- [ ] **Step 1: Check impact.** Run upstream impact on `RouterOptions`, `registerAPIRoutes`, `writeAPIError`, and `app.New`; inspect HTTP and application wiring.
- [ ] **Step 2: Write failing route tests.** Add a `fakeContainerAPI` returning two minimal containers and recording the exact ID/action. Use `authenticatedAPIRouter` from `api_test.go`. Assert an unauthenticated GET is 401; authenticated GET returns only `{id,name,service,state,health}`; valid POST with session, same-origin Origin, CSRF header and cookie is 202; missing CSRF or foreign Origin is rejected without calling the fake; wrong-ID/state/archived errors map to 404/409; unsupported action maps to 400.

  ```go
  type fakeContainerAPI struct {
      items []portycontrol.Container
      calledID, calledAction string
      err error
  }
  func (f *fakeContainerAPI) Containers(context.Context, portystack.StackID) ([]portycontrol.Container, error) {
      return f.items, f.err
  }
  func (f *fakeContainerAPI) StartContainerAction(_ context.Context, _ portystack.StackID, id, action string) (portyop.Operation, error) {
      f.calledID, f.calledAction = id, action
      return portyop.Operation{Kind: "container_" + action}, f.err
  }
  func TestContainerMutationRequiresOriginAndCSRF(t *testing.T) {
      fake := &fakeContainerAPI{}
      handler, session, csrf := authenticatedAPIRouter(t, RouterOptions{Containers: fake})
      path := "http://porty.local/api/v1/stacks/stk_gateway/containers/full-id-b/actions/stop"
      request := httptest.NewRequest(stdhttp.MethodPost, path, nil)
      request.AddCookie(session)
      request.Header.Set("Origin", "http://evil.local")
      request.Header.Set("X-CSRF-Token", csrf)
      request.AddCookie(&stdhttp.Cookie{Name: csrfCookieName, Value: csrf})
      response := httptest.NewRecorder()
      handler.ServeHTTP(response, request)
      if response.Code == stdhttp.StatusAccepted || fake.calledID != "" { t.Fatalf("foreign origin acted: %d, %q", response.Code, fake.calledID) }
  }
  ```
- [ ] **Step 3: Run `rtk go test ./internal/http -run 'TestContainer' -count=1`; expect route-not-found failures.**
- [ ] **Step 4: Add `ContainerAPI` near the HTTP consumer and register guarded routes.** Call `registerContainerRoutes(mux, options)` from `registerAPIRoutes`. Use `readRoute` and `mutationRoute`, `r.PathValue("id")`, `r.PathValue("containerId")`, and `r.PathValue("action")`; pass results to `writeResult` with 200 and 202. Map the four typed control errors in `writeAPIError` to generic messages without IDs or environment values. Set `options.Containers = control` beside `options.State = control` in `app.New`.

  ```go
  type ContainerAPI interface {
      Containers(context.Context, portystack.StackID) ([]portycontrol.Container, error)
      StartContainerAction(context.Context, portystack.StackID, string, string) (portyop.Operation, error)
  }
  func registerContainerRoutes(mux *stdhttp.ServeMux, options RouterOptions) {
      if options.Containers == nil { return }
      mux.HandleFunc("GET /api/v1/stacks/{id}/containers", readRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
          value, err := options.Containers.Containers(r.Context(), portystack.StackID(r.PathValue("id")))
          writeResult(w, r, value, err, stdhttp.StatusOK)
      }))
      mux.HandleFunc("POST /api/v1/stacks/{id}/containers/{containerId}/actions/{action}", mutationRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
          value, err := options.Containers.StartContainerAction(r.Context(), portystack.StackID(r.PathValue("id")), r.PathValue("containerId"), r.PathValue("action"))
          writeResult(w, r, value, err, stdhttp.StatusAccepted)
      }))
  }
  ```

  Add these cases to `writeAPIError`'s switch before its default case:

  ```go
  case errors.Is(err, portycontrol.ErrContainerNotFound):
      WriteError(w, r, stdhttp.StatusNotFound, "ContainerNotFound", "Container not found in this stack", nil)
  case errors.Is(err, portycontrol.ErrUnsupportedContainerAction):
      WriteError(w, r, stdhttp.StatusBadRequest, "InvalidContainerAction", "Unsupported container action", nil)
  case errors.Is(err, portycontrol.ErrContainerStateConflict), errors.Is(err, portycontrol.ErrContainerArchived):
      WriteError(w, r, stdhttp.StatusConflict, "ContainerStateConflict", "Container action is unavailable", nil)
  ```
- [ ] **Step 5: Run `rtk gofmt -w internal/http/auth.go internal/http/routes_containers.go internal/http/api.go internal/http/api_test.go internal/app/app.go`, `rtk go test ./internal/http ./internal/app -count=1`, and `rtk go vet ./internal/http ./internal/app`.** Check that existing stack-action auth tests pass.
- [ ] **Step 6: Run GitNexus `detect_changes --scope all`; inspect route/app flows and commit `feat: expose guarded container controls`.**

### Task 4: Fetch container state only while Overview is visible

**Files:** Modify `web/src/features/stacks/types.ts`, `web/src/features/stacks/api.ts`; create `web/src/features/stacks/useStackContainers.ts`, `web/src/features/stacks/useStackContainers.test.tsx`.

**Interfaces:** Export `Container` and `ContainerAction` types; `listStackContainers(id: string): Promise<Container[]>`; `runContainerAction(id: string, containerId: string, action: ContainerAction): Promise<Operation>`; `useStackContainers(stackId: string, enabled: boolean, refreshKey: string): {containers: Container[] | undefined; error: string}` for Task 5.

- [ ] **Step 1: Check impact.** Run upstream impact on `stackPath`, `getStackState`, and `useStackState`; keep the dashboard's state polling contract untouched.
- [ ] **Step 2: Write failing hook tests.** Render a small Preact consumer of `useStackContainers`. Mock `listStackContainers`. Assert no request while disabled; one request on enabling; a new request after five seconds; no request while `document.hidden`; a request on visibility return; `containers` becomes undefined and an error appears after a failed read; a later success clears the error; changing `stackId` does not render previous-stack rows; changing `refreshKey` triggers a request; unmount prevents late results from updating state.

  ```tsx
  function Probe({ id, enabled, keyValue }: { id: string; enabled: boolean; keyValue: string }) {
    const { containers, error } = useStackContainers(id, enabled, keyValue);
    return <div>{error || containers?.map((item) => item.name).join(",") || "empty"}</div>;
  }
  it("drops stale containers after a failed poll", async () => {
    vi.useFakeTimers();
    vi.mocked(listStackContainers)
      .mockResolvedValueOnce([{ id: "id-a", name: "app-1", service: "app", state: "running", health: "" }])
      .mockRejectedValueOnce(new Error("Docker unavailable"));
    render(<Probe id="one" enabled={true} keyValue="" />);
    await act(async () => { await Promise.resolve(); });
    expect(screen.getByText("app-1")).toBeInTheDocument();
    await act(async () => { await vi.advanceTimersByTimeAsync(5000); });
    expect(screen.getByText("Docker unavailable")).toBeInTheDocument();
    expect(screen.queryByText("app-1")).toBeNull();
  });
  ```
- [ ] **Step 3: Run `rtk bun run test -- src/features/stacks/useStackContainers.test.tsx` in `web/`; expect the missing hook/API to fail.**
- [ ] **Step 4: Add typed API calls and the polling hook.** Normalize a null list to `[]`. Encode both IDs in the action path. Follow `useStackState`'s timer, focus, visibility, and cleanup pattern, but enable it only for Overview. Reset `containers` to undefined on a failed read and on stack change; store `message(error)` for display. Include `refreshKey` in the effect dependencies so a completed container operation starts a fresh read.

  ```ts
  export type Container = { id: string; name: string; service: string; state: string; health: string };
  export type ContainerAction = "start" | "stop" | "restart";
  export async function listStackContainers(id: string): Promise<Container[]> {
    return (await api<Container[] | null>(`${stackPath(id)}/containers`)) || [];
  }
  export function runContainerAction(id: string, containerId: string, action: ContainerAction): Promise<Operation> {
    return api(`${stackPath(id)}/containers/${encodeURIComponent(containerId)}/actions/${action}`, "POST");
  }
  ```
- [ ] **Step 5: Format with `rtk bunx prettier --write src/features/stacks/types.ts src/features/stacks/api.ts src/features/stacks/useStackContainers.ts src/features/stacks/useStackContainers.test.tsx` in `web/`; run the focused test and `rtk bun run typecheck`.**
- [ ] **Step 6: Run GitNexus `detect_changes --scope all`; inspect web consumers and commit `feat: poll stack containers in overview`.**

### Task 5: Render per-container controls and keep whole-stack header actions

**Files:** Create `web/src/features/stacks/ContainerList.tsx`, `web/src/features/stacks/ContainerList.test.tsx`; modify `web/src/features/stacks/StackDetail.tsx`, `web/src/features/stacks/StackDetail.test.tsx`, `web/src/app/styles.css`.

**Interfaces:** `ContainerList` receives `{containers, error, busy, archived, onAction}`; `onAction(containerId: string, action: ContainerAction): void`. `StackDetail` consumes Task 4's hook/API and retains its existing props and whole-stack `action(kind)` function.

- [ ] **Step 1: Check impact.** Run upstream impact on `StackDetail`, `useStackState`, and `Workspace`; preserve the header action mapping and current props.
- [ ] **Step 2: Write failing visible-behavior tests.** In `ContainerList.test.tsx`, render two replicas and an exited container: both running replicas must have separate `Stop app-1`, `Restart app-1`, `Stop app-2`, and `Restart app-2` button names; the exited container gets only `Start worker-1`. Add paused and archived cases with no buttons, plus empty and error messages. In `StackDetail.test.tsx`, mock `listStackContainers` and `runContainerAction`; assert the four Overview buttons and old status output are absent; the header's exact `Stop` and `Restart` still appear for a running deployed stack; clicking `Stop app-2` passes only its ID; dirty editor changes block the request; an active stack operation disables all container actions.

  ```tsx
  expect(screen.getByRole("button", { name: "Stop app-2" })).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Stop app-2" }));
  expect(runContainerAction).toHaveBeenCalledWith("one", "id-b", "stop");
  expect(screen.getByRole("button", { name: "Stop" })).toBeInTheDocument(); // whole-stack header
  for (const label of ["Refresh status", "Start stack", "Pull images", "Recreate containers"]) {
    expect(screen.queryByRole("button", { name: label })).toBeNull();
  }
  ```
- [ ] **Step 3: Run `rtk bun run test -- src/features/stacks/ContainerList.test.tsx src/features/stacks/StackDetail.test.tsx` in `web/`; expect the new behavior tests to fail.**
- [ ] **Step 4: Implement the list and integrate it into Overview.** Use a stable `key={container.id}` and accessible per-container button names. Put the list under Runtime; remove the old four-button action group, `status` operation lookup, and status `<pre>`. Keep header `stop`, `restart`, `validate`, and `deploy` logic as it is. Compute `refreshKey` from completed `container_` operations for this stack, then call `useStackContainers(stack.id, tab === "Overview", refreshKey)`. Implement `containerAction` using the existing dirty guard, busy/error state, `runContainerAction`, and `onAction`. A read error renders `Notice` with no actionable stale rows; no containers renders `Empty` pointing to header Deploy. Add narrow-screen CSS so names, status, and buttons remain readable without page-width overflow.

  ```tsx
  const refreshKey = operations
    .filter((item) => item.scopeId === stack.id && item.kind.startsWith("container_") && ["succeeded", "failed"].includes(item.status))
    .map((item) => `${item.id}:${item.status}`)
    .join("|");
  const containerState = useStackContainers(stack.id, tab === "Overview", refreshKey);
  <ContainerList
    containers={containerState.containers}
    error={containerState.error}
    busy={busy || !!active}
    archived={!!stack.archivedAt}
    onAction={containerAction}
  />
  ```
- [ ] **Step 5: Format with `rtk bunx prettier --write src/features/stacks/ContainerList.tsx src/features/stacks/ContainerList.test.tsx src/features/stacks/StackDetail.tsx src/features/stacks/StackDetail.test.tsx src/app/styles.css` in `web/`; run the focused tests, `rtk bun run typecheck`, and `rtk bun run build`.** Check desktop and mobile layouts using the available browser tool or Playwright; capture screenshots for the PR.
- [ ] **Step 6: Run `rtk go test ./...`, `rtk go vet ./...`, `rtk go build ./cmd/porty`, and `rtk ./deploy/package_test.sh` from the repository root. From `web/`, run `rtk bun run test`, `rtk bun run typecheck`, `rtk bun run build`, and `rtk bun run test:e2e`.** Investigate any failures relevant to this change. Run `rtk git diff --check` and GitNexus `detect_changes --scope all`, inspect all affected flows, then commit `feat: control stack containers from overview`.

## Execution Handoff

Implement natively in this session with `superpowers:executing-plans`; repository instructions prohibit subagents. Review this plan before starting implementation.
