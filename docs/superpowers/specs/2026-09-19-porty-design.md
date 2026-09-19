# Porty v1 Design

Porty is a single-process Go application for one administrator on a trusted local network. It manages Docker Compose stacks from one Git worktree on the same Linux server. The repository root is the stack root; each immediate, non-hidden child containing `docker-compose.yml` is one stack.

## Locked product decisions

- One administrator, first-visitor registration, optional built-in TLS, no RBAC.
- One repository, remote `origin`, one configured branch, stack-scoped commits, fetch plus fast-forward-only pulls.
- Versioned files live in Git. Runtime environment values and Porty metadata live in a mode-restricted data directory and SQLite database.
- One `docker-compose.yml` per stack. Nested directories are stack contents, not stacks.
- HTTP handles commands and queries. One authenticated, origin-checked WebSocket streams operations and logs.
- Compose definitions and the authenticated administrator are trusted with Docker-equivalent host power. Porty prevents path and command injection but does not sandbox valid Compose capabilities.
- Stack deletion runs `compose down --remove-orphans`, removes the directory without volumes, and archives external metadata until explicit purge.
- Host/systemd is the primary deployment; an OCI image is a convenience alternative, not a sandbox.

## Architecture

Dependencies point inward: `domain <- application <- interfaces`, while infrastructure implements application ports. The domain imports no HTTP, SQL, filesystem, Git, or Docker packages. The Preact frontend is built with Vite and embedded in the Go binary.

The UI presents repository, working-tree, desired-configuration, deployment, runtime, and active-operation state independently. A successful deployment records Git SHA, dirty flag, stack diff digest, normalized Compose digest, duration, result, and capped redacted output.

## Safety invariants

- All editor paths are relative and accessed through `os.Root`; `.git`, symlinks, special files, traversal, and oversized files are rejected.
- Git and Compose use `exec.CommandContext` with fixed executable names and explicit arguments, never a shell.
- Git permits HTTPS/SSH only and disables hooks, inherited config, external diffs, filters, submodules, prompts, pagers, and credential helpers.
- Passwords use Argon2id. Session tokens are random and only hashes are stored. Mutations require same-origin checks and a CSRF header.
- Environment values are write-only through the API and are materialized only to short-lived mode-0600 files for Compose.
- Lock order is repository lock, then stack lock. Conflicting operations return `409`; there is no hidden queue.
- Output is bounded and redacted. Rendered Compose configuration and container logs are never persisted.

## HTTP contract

All JSON endpoints use `/api/v1`. Main resources are setup/session, repository actions and history, stacks, stack files/diff/environment/Git/actions, operations, deployments, and audit. Long operations return `202` with an operation ID. Errors use `{error:{code,message,requestId,details?}}`. File updates require `If-Match`.

The WebSocket endpoint `/api/v1/stream` accepts subscribe/unsubscribe envelopes for operations and logs. Every server envelope contains type, subscription ID, sequence, timestamp, and payload. Reconnect gaps are explicit.

## Technology and UI

Use Go 1.27.1+, `net/http`, `modernc.org/sqlite`, `golang.org/x/crypto`, `github.com/coder/websocket`, and YAML configuration. Use Preact, TypeScript, Vite, and modular CodeMirror 6 packages without a global state or CSS framework.

The accepted visual references are the generated Porty dashboard and editor concepts from this session: a true-white, table-first dashboard with narrow navigation and operation drawer, plus a three-pane file/editor/diff screen. UI text remains code-native.

## Out of scope

Multiple users, multiple repositories, branch switching, nested stacks, merge/rebase conflict resolution, arbitrary shell execution, secret files, automatic deployments, image building, Kubernetes/Swarm, metrics stacks, plugins, and remote agents.

