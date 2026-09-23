# Repository Guidelines

## Project Structure & Module Organization

Porty is a Go modular monolith with an embedded Preact frontend. The server entry point is `cmd/porty/`. Business rules and use cases live in `internal/domain/` and `internal/application/`; adapters for SQLite, Git, Docker Compose, process execution, authentication, and rooted filesystem access live under `internal/infrastructure/`. HTTP and WebSocket contracts are in `internal/httpapi/` and `internal/websocket/`.

Frontend source, component tests, and Playwright journeys are in `web/src/` and `web/e2e/`. Production assets are generated into `web/dist/` and embedded by `web/`. Deployment artifacts live in `deploy/`; project documentation belongs in `docs/`.

## Build, Test, and Development Commands

* `go test ./...` — run all backend and embedding tests.
* `go vet ./...` — perform Go static analysis.
* `go build ./cmd/porty` — build the server binary.
* `npm --prefix web test` — run Vitest component and unit tests.
* `npm --prefix web run typecheck` — check TypeScript without emitting files.
* `npm --prefix web run build` — produce the embedded frontend bundle.
* `npm --prefix web run dev` — start Vite for local UI development.
* `npm --prefix web run test:e2e` — run the desktop and mobile Playwright journey; build `/tmp/porty-e2e` first.
* `./deploy/package_test.sh` — validate packaging and the systemd unit.

## Coding Style & Naming Conventions

Format Go with `gofmt`; use standard Go naming (`MixedCaps`, short package names, `_test.go` tests). Keep domain and application packages independent of infrastructure implementations. Format frontend files with Prettier and use PascalCase component names, camelCase functions, and explicit TypeScript types at API boundaries. Do not construct shell commands; pass fixed executables and argument arrays.

## Testing Guidelines

Use Go’s `testing` package, Vitest with Testing Library, and Playwright. Add a failing regression test before implementing behavior, then run the focused and affected suites. Name Go tests `TestBehaviorUnderCondition` and frontend tests by user-visible behavior. Security changes must cover traversal, symlinks, stale writes, redaction, origins, and bounded output where applicable.

## Commit & Pull Request Guidelines

Follow the repository’s Conventional Commit style: `feat:`, `fix:`, and `chore:` with an imperative summary. Keep commits scoped and leave the worktree clean. Pull requests should explain intent, implementation impact, verification commands, environment gates, and linked issues. Include desktop/mobile screenshots for visible UI changes.

## Security & Configuration Tips

Never commit credentials, runtime environment values, databases, or backups. Preserve rooted-path checks, Git configuration hardening, CSRF/origin enforcement, restrictive file modes, secret redaction, and repository/stack lock ordering.

## Agent usage

Optimize for token efficiency.

Keep all output concise and task-relevant. Return only the information needed to understand progress, decisions, errors, and results. Avoid repeating known context, summarizing unchanged code, explaining obvious steps, or including unrelated command output.

When running commands, reduce output at the source where practical. Prefer targeted queries, filters, specific test packages, relevant log ranges, and concise status commands over commands that produce large outputs. When output is large, inspect or report only the relevant lines rather than reproducing it in full.

Do not sacrifice correctness or hide information required to diagnose a problem solely to save tokens. Expand context only when the additional information is necessary to make a decision, investigate a failure, or verify the result.

Do not use subagents.

Prefer established, well-maintained open-source libraries and official SDKs over implementing equivalent functionality from scratch or wrapping command-line tools when they have strong community adoption and fit the project architecture. For example, prefer the official Docker SDK over implementing a Docker command-line runner.

Do not introduce a dependency solely to avoid writing code. Evaluate dependencies for memory footprint, runtime efficiency, binary size, maintenance burden, security, transitive dependencies, API stability, and architectural fit. Prefer a small local implementation when an external dependency would materially worsen these properties or introduce unnecessary complexity.

@RTK.md
