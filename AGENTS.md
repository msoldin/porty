# Repository Guidelines

## Architecture

Keep the codebase simple, idiomatic, and easy to navigate.

Prefer clear boundaries, explicit dependencies, focused interfaces, and straightforward control flow. Avoid unnecessary
abstractions, indirection, coupling, and architectural patterns.

Before completing a change, verify that it is placed in the right package or module and remains easy to understand,
test, and maintain.

## Project Structure

Porty is a Go modular monolith with an embedded Preact frontend.

Backend code lives in `cmd/porty/` and `internal/`; frontend code in `web/`; deployment files in `deploy/`; documentation
in `docs/`.

## Backend

Follow idiomatic Go and format with `gofmt`.

Organize backend code by domain and responsibility:

```
cmd/
└── porty/                  # Application entry point
internal/
├── app/                    # Dependency wiring
├── auth/                   # Authentication
├── stack/                  # Stack management
├── repository/             # Repository management
├── operation/              # Operations
├── control/                # Control behavior
├── http/                   # HTTP contracts and auth guards
│   └── middleware/         # HTTP middleware
└── websocket/              # WebSocket streaming
```

Feature packages own their behavior and types. Keep interfaces narrow and near their consumers. Use subpackages only for
meaningful API, dependency, or domain boundaries.

Group files by responsibility or domain concept, not line count. Avoid one-file-per-function/type organization and
large files containing unrelated responsibilities. Filenames should make their responsibility obvious.

Use standard Go naming, explicit error handling, and simple control flow. Do not construct shell commands from strings;
use fixed executables and argument arrays.

Goose runs embedded migrations. sqlc generates selected stable reads.


## Frontend

Use focused Preact components and modules with clear responsibilities.

Organize `web/src/` primarily by feature:

```
src/
├── app/ # Bootstrap, routing, global setup
├── features/ # Feature/domain code
├── components/ # Shared UI components
├── hooks/ # Shared hooks
├── lib/ # API client and shared infrastructure
├── utils/ # Shared pure utilities
├── types/ # Shared types
└── main.tsx
```

Keep feature-specific components, hooks, API calls, types, and utilities together under `features/<feature>/`. Move code
to shared directories only when it is genuinely reused across features. Do not create directories or abstractions just
to match the structure.

Prefer local state unless state is genuinely shared. Keep API access out of presentation components. Colocate unit and
component tests with the code they test; keep cross-feature browser tests in `web/e2e/`.

Split large files by coherent responsibility, not arbitrary size. When multiple components or functions implement the
same logic or behavior, extract that logic into an appropriate reusable component, hook, or utility rather than
duplicating it. Avoid unnecessary generic abstractions, barrel files, deep nesting, and premature shared components.

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

* `(cd web && bun run test)`
* `(cd web && bun run typecheck)`
* `(cd web && bun run build)`
* `(cd web && bun run dev)`
* `(cd web && bun run test:e2e)`

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