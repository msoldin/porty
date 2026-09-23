# Stack Improvements Design

## Intent and success criteria

Stack users can inspect saved environment values deliberately, receive clear input errors before submitting forms, see real overview data, and open Logs to an automatically updating stream. Stack actions reflect the known runtime and deployment state. Backend validation, authentication, and secret handling remain authoritative.

This spec covers the six Stack improvements agreed in chat. It does not change repository setup or add a general monitoring service.

## Existing behavior and boundaries

- `web/src/Settings.tsx` lists environment keys and blank replacement inputs. `GET /api/v1/stacks/{id}/environment` returns keys only. `internal/stack/EnvironmentService.Values` is reserved for deployment infrastructure.
- Stack create and rename use `internal/filesystem/manager.go:validStackName`: 1–63 ASCII bytes, first character lowercase letter or digit, later characters lowercase letter, digit, underscore, or hyphen. The create form has an incomplete browser pattern; rename lacks one.
- `internal/control/ControlPlane.StackState` already calls Compose `Ps` and the deployment store, but returns only aggregate runtime and deployment freshness. Dashboard's Containers and Last deployment cells are placeholders.
- Stop and Restart always render on the detail page. Start stack appears in the detail Overview actions and calls the existing stack action endpoint.
- The Logs tab subscribes to `logs:{stackId}` on WebSocket. The `logs` operation obtains and publishes one bounded snapshot only after Load logs is clicked. The existing WebSocket hub supplies subscriptions and replay; the Compose SDK supports `LogOptions.Follow`.

## Environment values

Keep the environment list response value-free. Add a per-key authenticated read endpoint backed by a narrow EnvironmentService method. The endpoint returns only that key's stored value, rejects an invalid or missing key, checks the stack, and sets `Cache-Control: no-store`. Do not place a value in a URL, error, audit target, server log, or operation record.

Settings renders every stored value masked at first. A row's accessible Show control fetches and displays its value; Hide masks it and clears the browser copy. Clear revealed values after update, deletion, stack change, or leaving Settings. The displayed current value and editable replacement field are separate. Empty strings are valid stored values and must be distinguishable from absent values.

The current store contains overrides only. Compose interpolation defaults and values in container service definitions are not Settings entries. The UI labels the source as a stored Stack value and never fabricates a default. Adding Compose defaults later requires a separate source and precedence design.

All environment values use deliberate reveal. Key-name guesses cannot reliably classify secrets, so non-password names do not receive weaker default protection. Browser masking is a display safeguard; an authenticated user who reveals a value receives it in the browser.

## Frontend validation

Use one focused frontend validation module for mutation forms. It provides field-specific messages and prevents API submission for invalid input. It mirrors stable backend checks while the backend continues to validate every request.

- Create and rename names: 1–63 ASCII bytes and `^[a-z0-9][a-z0-9_-]*$`.
- Environment keys: `^[A-Za-z_][A-Za-z0-9_]*$`. Environment values may be empty but may not contain NUL. Align the Add form with that backend behavior.
- File create and move paths: reject empty, absolute, traversal, and `.git` components. Existing files, symlinks, and directory state remain server checks. Retain `If-Match` on file writes.
- Commit message: nonblank after trimming, no NUL, and at most 4096 UTF-8 bytes. Align the editor's current 200-character cap with this backend rule.
- File contents: the editable byte limit is configured at runtime. Expose that limit to the UI before enforcing it client-side; continue server enforcement. Compose/YAML validity remains the Validate action's responsibility.

The search, filter, and sort controls do not submit Stack mutations and need no new validation.

## Overview and action state

Extend the existing Stack state response with counts from the Compose `Ps` result and latest deployment summary from the deployment store. Show actual container counts and latest deployment status/time in Dashboard. For zero containers or no deployment, use explicit empty states. For a failed state fetch, show unavailable rather than a fabricated zero. Reuse the existing per-stack state refresh; do not add a second history request per row.

Stop and Restart appear only when the runtime is exactly `running` and at least one successful deployment for that Stack exists. A later failed deployment attempt must not erase that historical fact. Add a store query and an explicit state boolean if the current latest-deployment query cannot establish it. Unknown, stopped, partial, and unhealthy states hide both controls. Active operations and archived stacks retain their existing disabling behavior. The server remains the authority when browser state is stale.

Remove the Start stack button from detail Overview. Keep the existing `start` action API for existing clients; removing a UI entry does not change API permissions or the CSRF and origin checks.

## Live logs

Opening Logs starts an authenticated, Stack-scoped follow session through the existing WebSocket route and hub. The Compose client sends a bounded initial tail of 500 lines, then emits new lines using `LogOptions.Follow`. A subscription owns its follower; unsubscribe, disconnect, tab change, or Stack change cancels it. Do not hold the Stack operation coordinator for the whole viewing session. Bound concurrent followers and per-client queued output so slow viewers cannot consume unbounded resources.

The Logs UI shows connecting, loading, empty, streaming, reconnecting, and error states. It reconnects with sequence tracking while the tab remains open. If replay reports a gap, it requests a fresh bounded tail and tells the user that lines may have been missed. Closing Logs clears timers, socket handlers, and the active subscription. Remove Load logs and snapshot-oriented copy from the page.

Redact saved environment values before log events enter the hub. Redaction must work when a secret spans SDK callbacks; buffer complete lines or use an equivalent boundary-safe method. Bound each emitted event and retained UI output. Never persist live log contents in operation records. Preserve WebSocket session, repository-ready, origin, and expiry behavior.

## Testing and release checks

Add failing focused regression tests before each behavior change. Cover environment authorization, missing and invalid keys, no-store and non-leakage; masked/revealed/cleared UI values; valid and invalid boundary inputs with no request on failure; state counts and deployment history; action visibility; and log tail, follow, redaction, gaps, reconnect, cancellation, and resource bounds. Update existing App and Playwright expectations affected by removed controls or changed Settings copy.

Run focused Go and Vitest suites during each task, then `go test ./...`, `go vet ./...`, `go build ./cmd/porty`, `(cd web && bun run test)`, `(cd web && bun run typecheck)`, and `(cd web && bun run build)`. Run relevant Playwright flows for the changed UI. Before editing symbols, use GitNexus impact; before any implementation commit, use `detect_changes` and resolve partial or truncated results.
