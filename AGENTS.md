# Repository Guidelines

## Architecture

Keep the codebase simple, idiomatic, and easy to navigate.

Prefer clear boundaries, explicit dependencies, focused interfaces, and straightforward control flow. Avoid unnecessary
abstractions, indirection, coupling, and architectural patterns.

Before completing a change, verify that it is placed in the right package or module and remains easy to understand,
test, and maintain.

## Project Structure

Porty is a Go modular monolith with an embedded Preact frontend.

Backend code lives under `cmd/porty/` and `internal/`. `internal/app/` wires concrete implementations. Feature behavior
lives in `internal/auth/`, `internal/stack/`, `internal/repository/`, `internal/operation/`, and `internal/control/`.
HTTP contracts and auth guards live in `internal/http/`; middleware in `internal/http/middleware/`; WebSocket streaming
in `internal/websocket/`.

Frontend source lives in `web/src/`, Playwright tests in `web/e2e/`, and generated assets in `web/dist/`.

Deployment files live in `deploy/`; documentation in `docs/`.

## Backend

Follow idiomatic Go and format with `gofmt`.

Feature packages own their behavior and types. Keep interfaces narrow and near their consumers. Use subpackages only for
meaningful API, dependency, or domain boundaries.

Split packages into focused files when they contain distinct responsibilities. Group code by responsibility or domain
concept, not line count. Keep related types, functions, and helpers together.

Do not use one-file-per-function or one-file-per-type organization. Do not keep unrelated responsibilities in one large
file when they can be separated clearly.

Package filenames should make the package's major responsibilities obvious.

Use standard Go naming conventions. Prefer explicit error handling and simple control flow. Do not construct shell
commands from strings; use fixed executables and argument arrays.

Goose runs embedded migrations. sqlc generates selected stable reads.

## Frontend

Use focused Preact components and modules with clear responsibilities.

Separate UI, state, API access, hooks, and utilities when they represent distinct concerns. Prefer local state unless
state is genuinely shared.

Split large files by coherent responsibility, not arbitrary size. Avoid unnecessary generic abstractions and premature
shared components.

Format with Prettier. Use PascalCase for components and camelCase for functions, variables, and hooks. Use explicit
TypeScript types at API and external boundaries.

Prefer Preact and browser APIs over unnecessary dependencies.

## Commands

### Backend

* `go test ./...`
* `go vet ./...`
* `go build ./cmd/porty`
* `sqlc generate`

### Frontend

* `npm --prefix web test`
* `npm --prefix web run typecheck`
* `npm --prefix web run build`
* `npm --prefix web run dev`
* `npm --prefix web run test:e2e`

### Packaging

* `./deploy/package_test.sh`

## Testing

Add a failing regression test before implementing behavior, then run the focused and affected suites.

Use Go's `testing` package, Vitest with Testing Library, and Playwright.

Name Go tests `TestBehaviorUnderCondition`. Name frontend tests by user-visible behavior. Prefer observable behavior
over
implementation details.

Security-sensitive changes must test relevant traversal, symlink, stale-write, redaction, origin, CSRF, authorization,
and bounded-output behavior.

## Commits & Pull Requests

Use Conventional Commits such as `feat:`, `fix:`, and `chore:` with imperative summaries.

Keep commits scoped and the worktree clean.

Pull requests should describe intent, implementation impact, verification, environment requirements, and linked issues.
Include desktop and mobile screenshots for visible UI changes.

## Security

Never commit credentials, runtime environment values, databases, or backups.

`internal/auth` owns access tokens, rotating refresh tokens, and signing-key rotation. `internal/http` owns cookies,
origin checks, and CSRF guards.

Preserve rooted-path checks, Git hardening, CSRF/origin enforcement, restrictive file modes, secret redaction, and
repository/stack lock ordering.

## Agent Usage

Optimize for token efficiency.

Keep all output concise and task-relevant. Return only the information needed to understand progress, decisions, errors,
and results. Avoid repeating known context, summarizing unchanged code, explaining obvious steps, or including unrelated
command output.

When running commands, reduce output at the source where practical. Prefer targeted queries, filters, specific test
packages, relevant log ranges, and concise status commands over commands that produce large outputs. When output is
large, inspect or report only the relevant lines rather than reproducing it in full.

Do not sacrifice correctness or hide information required to diagnose a problem solely to save tokens. Expand context
only when the additional information is necessary to make a decision, investigate a failure, or verify the result.

Do not use subagents.

Prefer established, well-maintained open-source libraries and official SDKs over implementing equivalent functionality
from scratch or wrapping command-line tools when they have strong community adoption and fit the project architecture.
For example, prefer the official Docker SDK over implementing a Docker command-line runner.

Do not introduce a dependency solely to avoid writing code. Evaluate dependencies for memory footprint, runtime
efficiency, binary size, maintenance burden, security, transitive dependencies, API stability, and architectural fit.
Prefer a small local implementation when an external dependency would materially worsen these properties or introduce
unnecessary complexity.

@GIT_NEXUS.md
@RTK.md
