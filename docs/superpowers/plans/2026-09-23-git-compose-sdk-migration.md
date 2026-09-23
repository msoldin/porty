# Git and Compose SDK Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Repository instructions prohibit subagents.

**Goal:** Replace Porty's Git and Compose subprocess adapters with the three pinned Go SDKs while preserving the current HTTP behavior and eliminating executable launches from Git and stack operations.

**Architecture:** `internal/git` owns go-git transport, repository operations, and safe setup. `internal/compose` owns explicit-environment project loading and the Compose v5 lifecycle, using a go-sdk client behind Compose's Docker CLI Go interface. `internal/repository`, `internal/operation`, and `internal/control` use typed SDK values where useful; HTTP types remain Porty-owned.

**Tech Stack:** Go 1.27.1; Compose v5.5.1; go-sdk/client v0.1.0-alpha013; go-git/v6 v6.0.0-alpha.5; Go testing; Playwright.

**Spec:** `docs/superpowers/specs/2026-09-23-git-compose-sdk-migration-design.md`

## Global Constraints

- Keep the three requested versions pinned exactly. No `git`, `docker`, credential-helper, Buildx, provider, or model subprocess may run during Porty operations.
- Preserve init, clone, adopt, managed remote changes, HTTPS/SSH auth, stack lifecycle, HTTP JSON, error codes, audit fields, and lock ordering.
- Reject remote Git Compose resources and SDK features that require helper processes before Docker mutation.
- Preserve rooted paths, symlink rejection, safe file modes, secret redaction, timeouts, and bounded output.
- The new `sha256:` Compose digest may make prior deployments stale once; subsequent loads must be stable.
- Use failing behavior tests before implementation. Keep daemon-free tests in the default suite; use a disposable daemon for the opt-in integration suite.
- Before each symbol edit, run GitNexus impact analysis; before each commit, run GitNexus `detect_changes`. A zero on an untracked file is not proof of safety. Do not use subagents.

## File Structure

- `internal/git/client.go`: repository operations and public validation, then split focused auth/diff/history helpers as needed.
- `internal/git/setup.go`: provisioning orchestration; extract path inspection, remote/auth, and safe checkout/rollback into focused files in the same package.
- `internal/compose/project.go`: explicit-environment local loading, process-launch policy, digest.
- `internal/compose/client.go`: Compose lifecycle adapter, bounded logs/errors.
- `internal/compose/docker.go`: SDK client and Docker CLI Go-interface bridge with explicit ownership.
- `internal/repository/repository.go`, `internal/operation/deployment.go`, `internal/control/control.go`: narrow typed boundaries and unchanged HTTP mapping.
- `internal/app/app.go`: construct and close SDK dependencies.
- `web/e2e/critical.spec.ts`, `docs/development.md`, `docs/security.md`: replace binary fixtures and describe behavior.

## Review Focus

1. A malicious repository has a symlinked `.git`, stack path, or checkout target: reject it before reading or writing outside the root. Pin in Task 3.
2. A remote URL or SDK error contains a credential: no HTTP, audit, operation, or test log exposes it. Pin in Tasks 2, 3, and 5.
3. A Compose file interpolates an ambient `PORTY_SECRET` or references a remote Git include: reject or ignore it before a daemon mutation. Pin in Task 4.
4. A Git pull sees local modifications or divergent history: leave the worktree and branch intact, and report a non-fast-forward error. Pin in Task 2.
5. SDK status/log streams are malformed or unbounded: return typed status and bounded, redacted logs without hanging or panicking. Pin in Task 5.

---

### Task 1: Pin dependencies and prove the Docker bridge

**Files:** Modify `go.mod`, `go.sum`; create `internal/compose/docker.go`, `internal/compose/docker_test.go`.

**Interfaces:** Produce `newDockerService(ctx context.Context) (api.Compose, io.Closer, error)` for Task 5. The bridge embeds `command.Cli` and overrides `Client() client.APIClient` to return the `go-sdk/client.SDKClient`. It never executes a command.

- [ ] **Step 1: Record the baseline.** Run `rtk go build -o /tmp/porty-before ./cmd/porty`, `rtk wc -c /tmp/porty-before`, and `rtk go list -m all | rtk wc -l`; save numbers in this plan's execution notes.
- [ ] **Step 2: Write a failing bridge test.** In `docker_test.go`, use a fake `command.Cli` and fake Moby API client. Assert `bridge.Client()` returns the supplied SDK API client and `compose.NewComposeService(bridge)` succeeds without contacting a daemon. Also test that constructing an unavailable daemon returns an error rather than exiting the process.

  ```go
  type dockerBridge struct {
      command.Cli
      api client.APIClient
  }
  func (b dockerBridge) Client() client.APIClient { return b.api }
  func (b dockerBridge) BuildKitEnabled() (bool, error) { return false, nil }
  ```
- [ ] **Step 3: Run `rtk go test ./internal/compose -run TestDockerBridge -count=1`; expect the missing bridge test to fail.**
- [ ] **Step 4: Add the exact module pins and implement the bridge.** Use `sdkclient.New(ctx)`, `command.NewDockerCli()`, and `Initialize(&flags.ClientOptions{})`; wrap the initialized Go CLI interface, overriding `Client()` with the SDK client and `BuildKitEnabled()` with false so Compose uses its in-process classic build path. The SDK constructor normally probes the daemon; configure a no-op construction health check so Porty still starts while Docker is unavailable, then let actual operations report connection errors. Close the SDK client on application context cancellation. Keep the bridge's concrete types inside `internal/compose`.

  ```go
  sdk, err := sdkclient.New(ctx, sdkclient.WithHealthCheck(
      func(context.Context) func(sdkclient.SDKClient) error {
          return func(sdkclient.SDKClient) error { return nil }
      },
  ))
  // Initialize command.NewDockerCli(), then pass dockerBridge{Cli: cli, api: sdk}
  // to compose.NewComposeService. Return sdk as the closer.
  ```
- [ ] **Step 5: Run the focused test and `rtk go build ./cmd/porty`.** If Moby API or client versions are incompatible, capture the exact compiler failure and stop this plan for a version decision; do not add a CLI fallback. Run a disposable-daemon smoke check if Docker is available. Run `rtk go build -o /tmp/porty-after ./cmd/porty` and record size and module-count changes.
- [ ] **Step 6: Run GitNexus change analysis and commit** with `feat: add Docker SDK bridge`.

### Task 2: Replace Git repository operations

**Files:** Modify `internal/git/client.go`, `internal/git/client_test.go`, `internal/repository/repository.go`, `internal/repository/repository_test.go`; create focused `internal/git/diff.go` and `internal/git/auth.go` only where responsibilities warrant them.

**Interfaces:** Preserve `repository.GitRepository` behavior: `Status`, `Head`, `Diff`, `Commit`, `History`, `HistoryPage`, `Fetch`, `PullFastForward`, `Push`. Accept go-git repository/transport values behind the adapter. Keep `ValidateRemoteURL`, `ValidateBranch`, and `ErrUnsafeRepository`.

- [ ] **Step 1: Add failing behavior tests using temporary go-git repositories.** Assert unborn status/history, dirty and untracked paths, ahead/behind counts, a stack-scoped commit that excludes a sibling stack, bounded text/binary diff, and history pagination. Use `TestBehaviorUnderCondition` names and assert outputs, not command arguments.
- [ ] **Step 2: Run `rtk go test ./internal/git ./internal/repository -count=1`; expect the new tests to fail against the command adapter.**
- [ ] **Step 3: Implement repository operations with go-git v6.** Open repositories via `git.PlainOpen`; use worktree status/add/commit, repository log/fetch/push, and reference ancestry for fast-forward and ahead/behind. Check the selected branch and preserve the existing path and output caps. Before checkout or branch ref updates, prove clean worktree and ancestor relationship; a failed pull must not modify files.

  ```go
  repo, err := git.PlainOpen(root)
  if err != nil { return err }
  worktree, err := repo.Worktree()
  if err != nil { return err }
  status, err := worktree.Status()
  // Require status.IsClean() and an ancestor check before fast-forwarding.
  ```
- [ ] **Step 4: Run focused tests, including hostile input and secret-redaction cases.** Add a local in-process remote test for fetch/push, then run `rtk go test ./internal/git ./internal/repository -count=1`.
- [ ] **Step 5: Run GitNexus change analysis and commit** with `feat: use go-git for repository operations`.

### Task 3: Replace Git setup and remote authentication

**Files:** Modify `internal/git/setup.go`, `internal/git/setup_test.go`, `internal/git/setup_cleanup_test.go`, `internal/app/app.go`; create focused `internal/git/setup_path.go`, `internal/git/setup_remote.go`, `internal/git/setup_checkout.go` where extracted behavior belongs; remove executable helper routing from `cmd/porty` after callers are gone.

**Interfaces:** Preserve `repository.RepositoryProvisioner` (`InspectPath`, `InspectRemote`, `Provision`, `ConfigureRemote`, `RemoveRemote`, `Open`) and its domain errors. Construct go-git v6 `plumbing/client` with in-memory HTTPS auth or key plus pinned `known_hosts` for SSH. No helper executable argument remains in `NewProvisioner`.

- [ ] **Step 1: Replace runner-argument tests with failing real-behavior tests.** Cover empty remote, symbolic HEAD and sole branch, local setup, clone into a clean root, adopt, remote replacement and rollback, unsafe config, invalid remote tree paths, symlinks, stale SSH material, and error redaction. Use local HTTP/SSH Git servers, never a public remote.
- [ ] **Step 2: Run `rtk go test ./internal/git -count=1`; verify the new tests fail.**
- [ ] **Step 3: Port setup to go-git and split its responsibilities.** Inspect local config without activating hooks or filters; preserve `repositoryAttempt` staging and `os.Root` cleanup. Validate all tree entries before publishing. Use go-git transport options for HTTPS and SSH, with host-key verification from Porty's stored file. Preserve timeouts and remote error classification.

  ```go
  // Credentials never enter a remote URL or process environment.
  options := &git.FetchOptions{RemoteName: "origin", Tags: git.NoTags,
      ClientOptions: []gitclient.Option{gitclient.WithHTTPAuth(httpAuth)}}
  err := repo.FetchContext(ctx, options)
  // Use gitclient.WithSSHAuth(sshAuth) for an SSH remote instead.
  ```
- [ ] **Step 4: Remove `git` helper dispatch and process-runner wiring only after the replacement tests pass.** Run `rtk rg -n 'runAt|exec.Command|GIT_ASKPASS|GIT_SSH|PORTY_GIT_' internal/git cmd/porty`; no production Git execution path should remain. Run `rtk go test ./internal/git ./internal/repository ./internal/app -count=1`.
- [ ] **Step 5: Run GitNexus change analysis and commit** with `feat: use go-git for repository setup`.

### Task 4: Load and validate local Compose projects

**Files:** Create `internal/compose/project.go`, `internal/compose/project_test.go`; modify `internal/compose/types.go`, `internal/compose/client_test.go`.

**Interfaces:** Produce `Load(ctx context.Context, request Request) (*types.Project, error)` and `Digest(project *types.Project, environment map[string]string) (string, error)`. `Request` retains stack directory, project name, and stored environment. Task 5 consumes `*types.Project`.

- [ ] **Step 1: Write failing loader tests.** Test exact `docker-compose.yml` selection; missing/invalid file; stable digest; digest changes after an env or service change; no ambient env interpolation; rejected remote Git include, provider/model service, and Buildx-only configuration; symlinked paths; redacted validation errors. Use a sentinel executable on `PATH` and assert no sentinel invocation.
- [ ] **Step 2: Run `rtk go test ./internal/compose -run 'TestLoad|TestDigest|TestReject' -count=1`; expect failures.**
- [ ] **Step 3: Load with compose-go project options and an explicit environment map.** Do not call Compose v5 `LoadProject`, which adds `os.Environ`. Normalize and resolve service environment as needed, validate the local resource policy before daemon calls, and use deterministic `project.MarshalJSON()` plus `filesystem.SerializeEnvironment` for `sha256:` digests. Keep the old digest only as historical data; one-time stale state is expected.

  ```go
  opts, err := cli.NewProjectOptions([]string{filepath.Join(req.StackDir, "docker-compose.yml")},
      cli.WithWorkingDirectory(req.StackDir), cli.WithName(req.ProjectName),
      cli.WithEnv(serializedKeyValuePairs(req.Environment)))
  if err != nil { return nil, err }
  project, err := opts.LoadProject(ctx)
  // Validate local-only resources before passing project to Compose.
  ```
- [ ] **Step 4: Run focused Compose tests and `rtk go test ./internal/control ./internal/operation -count=1`.**
- [ ] **Step 5: Run GitNexus change analysis and commit** with `feat: load Compose projects without process environment`.

### Task 5: Replace Compose lifecycle and typed status

**Files:** Modify `internal/compose/client.go`, `internal/compose/client_test.go`, `internal/control/control.go`, `internal/control/control_test.go`, `internal/operation/deployment.go`, `internal/operation/deployment_test.go`, `internal/app/app.go`.

**Interfaces:** `internal/compose.Client` accepts `api.Compose`, returns typed `[]api.ContainerSummary` for status, and uses `*types.Project` from Task 4 for mutating actions. `internal/control` maps summaries to existing `StackState`; HTTP JSON does not change. Preserve the `Validate`, `Digest`, `Start`, `Stop`, `Restart`, `Deploy`, `Pull`, `Down`, `Logs` user flows.

- [ ] **Step 1: Write failing fake-Compose tests.** Assert `Up` recreate/orphan options, pull/down/stop/restart dispatch, typed status with running/stopped/unhealthy containers, bounded/redacted log streaming, cancellation, and a malformed SDK error containing a secret. Assert no daemon action after project validation fails.
- [ ] **Step 2: Run `rtk go test ./internal/compose ./internal/control ./internal/operation -count=1`; verify failures.**
- [ ] **Step 3: Implement SDK lifecycle mapping.** Use Compose v5 `Up`, `Stop`, `Restart`, `Pull`, `Down`, `Ps`, and `Logs`; apply the existing five-minute action timeout and `maxCommandOutput` cap. Translate typed state directly in control, and redact before producing operation errors. Do not call Compose `LoadProject` or any CLI command.

  ```go
  project, err := c.Load(ctx, request)
  if err != nil { return err }
  recreateMode := api.RecreateDiverged
  if recreate { recreateMode = api.RecreateForce }
  return c.service.Up(ctx, project, api.UpOptions{
      Create: api.CreateOptions{Build: &api.BuildOptions{}, Recreate: recreateMode, RemoveOrphans: true},
      Start: api.StartOptions{},
  })
  ```
- [ ] **Step 4: Run focused tests and an opt-in disposable-daemon integration test for load, up, ps, logs, stop, restart, pull, and down.** Include a local build and confirm the process sentinel stays untouched; reject any feature requiring a helper process before mutation.
- [ ] **Step 5: Run GitNexus change analysis and commit** with `feat: run Compose stacks through SDK`.

### Task 6: Remove binary assumptions and verify the application

**Files:** Modify `web/e2e/critical.spec.ts`, `docs/development.md`, `docs/security.md`, deployment packaging files as required; delete unused `internal/process` only after all callers are gone.

**Interfaces:** Existing `/api/v1` contracts, operation events, and browser labels remain unchanged. Playwright uses an API fixture for Docker-backed calls while its local Git flow runs against real Porty.

- [ ] **Step 1: Write a failing Playwright journey without the fake `docker` binary.** Intercept Docker-backed state/deploy/operation responses at the browser API boundary while leaving registration, repository setup, file editing, and commit calls real. Assert the same visible success and responsive behavior.

  ```ts
  await page.route("**/api/v1/stacks/*/state", async (route) => {
    await route.fulfill({ json: { runtime: "stopped", freshness: "never_deployed" } });
  });
  ```
- [ ] **Step 2: Remove binary fixtures and unused helper/runner code.** Update deployment docs and package contents so runtime `git` and `docker` executables are not required; keep Docker socket access and group requirements. Update security docs with local-only Compose resource and process-free policy.
- [ ] **Step 3: Run verification.** `rtk go test ./...`, `rtk go vet ./...`, `rtk go build ./cmd/porty`, `rtk npm --prefix web test`, `rtk npm --prefix web run typecheck`, `rtk npm --prefix web run build`, `rtk npm --prefix web run test:e2e`, and `rtk ./deploy/package_test.sh`. Run `gofmt` and Prettier on touched files. Record the opt-in daemon result separately if unavailable on this host.
- [ ] **Step 4: Search for executable paths.** Run `rtk rg -n 'exec.Command|Name: "(git|docker)"|GIT_ASKPASS|GIT_SSH|fakeDocker' internal cmd web/e2e`; inspect every remaining hit and prove none is reachable by production Git or Compose operations. Check `rtk git diff --check` and the worktree.
- [ ] **Step 5: Run GitNexus change analysis and commit** with `chore: remove Git and Docker command assumptions`.

## Execution notes

- Baseline focused tests before planning: 126 passed across `internal/git`, `internal/compose`, `internal/control`, `internal/operation`, and `internal/repository`.
- Baseline/final binary size, module count, exact SDK compatibility result, and live-daemon result are recorded during execution.
