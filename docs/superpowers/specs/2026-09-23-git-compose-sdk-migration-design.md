# Git and Compose SDK migration

## Goal and constraints

Replace Porty's Git and Docker command runners with in-process Go libraries while preserving the existing HTTP contracts and ordinary repository and stack workflows. Pin `github.com/docker/compose/v5` to `v5.5.1`, `github.com/docker/go-sdk/client` to `v0.1.0-alpha013`, and `github.com/go-git/go-git/v6` to `v6.0.0-alpha.5`. No Porty Git or Compose operation may launch an executable, including the current Git credential helpers. Keep existing repository setup modes, managed remotes, HTTPS and SSH authentication, and stack lifecycle actions.

Porty remains a modular monolith. SDK types may appear in backend feature interfaces where they make the flow clearer; HTTP request and response types remain Porty-owned. Keep interfaces focused at the call sites and concrete SDK construction in `internal/app` and the Git and Compose packages.

## Dependency and API feasibility gate

Before the larger migration, add the pinned modules in an isolated branch and compile a minimal bridge through the public APIs. Compose v5.5.1 asks for `github.com/moby/moby/api v1.55.0` and `client v0.5.1`; go-sdk/client alpha013 asks for `api v1.52.0` and `client v0.1.0`. Go module resolution will select one version of each, so compilation and a disposable-daemon smoke check must establish that the SDK client works through Compose's `command.Cli` interface. If the exact pins are incompatible, stop the migration and bring the concrete conflict back for a version decision; do not hide it behind a command fallback.

Measure the resulting binary size and dependency growth against the current build, and record the result in the implementation plan or PR. The Go toolchain in this repository is 1.27.1; the requested module Go versions are lower.

## Git design

`internal/git` owns go-git repository and transport use. Split its current large setup implementation by responsibility: path and adopted-repository inspection, remote authentication and inspection, provisioning and rollback, and worktree operations. `internal/repository` continues to own setup policy and remote enablement. The backend may carry go-git references and hashes through narrow repository interfaces; HTTP still returns Porty's `GitStatus`, `GitCommit`, setup status, and error codes.

Use go-git v6 for init, open, clone, remote reference inspection, fetch, fast-forward update, push, status, history, and commit. Preserve the selected branch, unborn-branch behavior, ahead/behind counts, stack-scoped commits, history pagination, and bounded diffs including untracked files. Keep `Diff` output in the existing user-facing patch format or update only the renderer behind the same HTTP string contract. Preserve the current staged checkout and rollback rules for remote setup, including validation of every remote tree path before publication.

Supply HTTPS credentials in memory through go-git's v6 transport client. Supply SSH keys and host verification using the stored key and `known_hosts`, with no agent, prompt, helper program, or ambient Git configuration. Revalidate key files before each remote operation. Reject command-bearing local Git configuration in adopted repositories even if go-git would currently ignore it, so unsafe repositories cannot be silently accepted. Keep existing rooted-path, symlink, permission, URL, branch, and secret-redaction checks.

## Compose and Docker design

`internal/compose` loads the fixed local `docker-compose.yml` under a stack directory with an explicit interpolation environment built from Porty's stored stack values. Use compose-go's project loader directly where needed; Compose v5's `LoadProject` currently imports the Porty process environment, which would weaken the existing clean-environment behavior. Use the resulting typed project for validation, digesting, and lifecycle operations.

`internal/app` creates the requested go-sdk Docker client and a Docker CLI Go interface for Compose v5's `NewComposeService`; the bridge must pass the SDK client for daemon calls without starting the `docker` executable. Keep ownership and closing of the daemon client explicit. `internal/compose` calls Compose's `Up`, `Stop`, `Restart`, `Pull`, `Down`, `Ps`, and `Logs` APIs with the current timeout, orphan-removal, recreate, tail, and output limits. Backend control and operation interfaces use typed Compose projects and container summaries instead of command output and JSON parsing. HTTP response fields, operation states, and WebSocket events stay stable.

Reject remote Git Compose resources and other Compose features that would cause the SDK to launch a helper process. In particular, reject provider and model services and Buildx-only build configurations. Keep ordinary local image-based stacks and in-process supported builds. The validator must fail before any Docker mutation; a process-free regression test guards this policy. The UI should receive the existing validation failure path with a useful, redacted message.

Compute the desired-state digest from the normalized project model plus Porty's serialized environment, retaining the `sha256:` format and stable ordering. Successful deployments continue to record the digest. Because the previous digest hashed CLI-rendered JSON, existing deployments may show stale once after migration until redeployed. Document this and test that the new digest is stable across repeated loads and changes when relevant configuration or environment changes.

## Failure behavior and tests

Keep authentication and remote errors mapped to the current domain errors and HTTP codes. Bound network calls, Docker calls, diff/history/log output, and error strings. Redact stack environment values and Git credentials before they enter logs, audit records, operation events, or HTTP errors. Preserve repository and stack lock ordering. A failed setup or deploy must leave the last valid repository and deployment records intact according to current behavior.

Replace command-argument unit tests with behavior tests using real temporary Git repositories, local in-process HTTP and SSH remotes, and fake Compose/API interfaces. Cover init, clone, adopt, remote replacement and rollback, empty remotes, fast-forward-only pull, scoped commits, untracked diffs, credentials, hostile config, traversal, symlinks, and secret redaction. Cover Compose interpolation isolation, local resource validation, digest stability, lifecycle options, typed status, logs, output limits, and no-process policy. Run a disposable Docker daemon integration scenario for load, up, status, logs, stop, restart, pull, and down when available; keep the normal unit suite daemon-free.

Update the Playwright journey so its Git flow uses the real in-process implementation and Docker-backed browser calls use an API fixture. The disposable-daemon integration scenario covers the actual Compose-to-daemon path. Remove fake `docker` and Git helper fixtures, update development and security documentation, then run the affected Go tests, `go test ./...`, `go vet ./...`, `go build ./cmd/porty`, frontend tests and typecheck, Playwright, and packaging checks. The current focused baseline is 126 passing tests across `internal/git`, `internal/compose`, `internal/control`, `internal/operation`, and `internal/repository`.

## Sources checked

- [Compose v5.5.1 API](https://pkg.go.dev/github.com/docker/compose/v5@v5.5.1/pkg/api) and [service constructor](https://pkg.go.dev/github.com/docker/compose/v5@v5.5.1/pkg/compose)
- [Docker go-sdk/client alpha013](https://pkg.go.dev/github.com/docker/go-sdk/client@v0.1.0-alpha013)
- [go-git v6 alpha.5](https://pkg.go.dev/github.com/go-git/go-git/v6@v6.0.0-alpha.5)
