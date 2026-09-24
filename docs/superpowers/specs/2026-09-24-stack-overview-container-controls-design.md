# Stack Overview Container Controls Design

## Intent

Stack Overview should show every existing Docker container associated with the stack, including stopped containers and each replica of a scaled Compose service. A user can start a stopped container, or stop or restart a running container, without affecting its siblings. The Overview should no longer present stack-wide runtime buttons.

## Scope and behavior

- Replace the Overview's Runtime action row and status snapshot output with a container list. Remove its **Refresh status**, **Start stack**, **Pull images**, and **Recreate containers** buttons.
- Remove the stack-level **Stop** and **Restart** buttons from the page header. Keep **Validate** and **Deploy** there; Deploy creates containers when none exist.
- Show one row per existing container, identified by Docker container ID. Display container name, Compose service, state, and health when available. Replicas have separate rows and separate actions.
- A `running` container, including one with unhealthy health, offers **Stop** and **Restart**. A `created` or `exited` container offers **Start**. Other states, including paused, restarting, dead, and unknown, offer no action. Archived stacks show the list without action controls.
- If the stack has no existing containers, show an empty message that points to Deploy. Declared Compose services without an existing container do not appear as rows.
- Refresh the list automatically while Overview is visible, using the existing five-second stack-state cadence and refreshing when the page regains focus or visibility. Refresh after a container operation finishes. Stop polling when the tab or stack changes. A failed read shows an error and prevents actions based on stale data; a successful read clears the error.
- An action is unavailable while another operation is active for the stack or while its request is being submitted. The existing unsaved-editor-changes guard applies. The accepted operation opens the existing operation drawer; operation failures appear there, while request rejections appear in the page notice. If Docker changes state between display and click, the server reports a state conflict rather than acting on another container.
- Last deployment and Compose project information remain below the container list.

## Backend design

Add `GET /api/v1/stacks/{id}/containers`, returning a minimal array of `{id, name, service, state, health}`. Resolve the stack through its opaque ID and environment through the existing services. Use the Compose client's existing `Ps` call with `All: true` and include every returned container whose project matches the stack's Compose project. The response must not expose environment values or the full Compose summary.

Add `POST /api/v1/stacks/{id}/containers/{containerId}/actions/{action}` for `start`, `stop`, and `restart`. It returns the existing asynchronous operation response. The control plane uses the existing stack coordinator lock, resolves the stack, and checks the requested ID against the current `Ps(All: true)` result before accepting work. It rejects a missing or wrong-stack ID with 404, an unsupported action with 400, and an incompatible state, archived stack, or concurrent operation with 409. It queues a stack-scoped operation so the current operation stream and conflict behavior continue to work. Docker failures are reported through the operation with the existing bounded, redacted error handling.

Extend the existing Compose-backed runtime with targeted Docker SDK start, stop, and restart calls by container ID. The SDK client already backs Compose in this process; use that connection rather than adding a CLI command or a second Docker connection. The operation acts on exactly the validated ID. Existing stack-wide backend actions can continue to serve other callers; this feature removes their controls from Stack Overview.

The new read route uses the existing authenticated, repository-ready guard. The mutation route uses the existing authenticated, origin, and CSRF guards. Container IDs are treated as opaque path values and never interpolated into a command. Authorization and project membership are checked server-side; the frontend's state-based buttons are only a convenience.

## Frontend design

Keep API calls in the stacks feature API module. A focused container-list hook owns Overview-only polling and its loading/error state. A focused list component renders the rows and available actions; `StackDetail` supplies the stack, active operation state, dirty-editor guard, and existing `onAction` callback. Use accessible button names that include the container name and a layout that remains usable on narrow screens. Do not add a manual refresh control.

## Verification

- Go tests cover the complete list with running, exited, and scaled containers; exact-ID targeting; wrong-stack and missing IDs; incompatible states; operation conflicts; archived-stack behavior; authenticated reads; CSRF/origin protection; and redacted, bounded Docker errors.
- Frontend tests cover each action rule, distinct replica rows, empty and read-error states, archived and active-operation gating, polling updates, and removal of the six specified buttons and old status output.
- Run focused Go and frontend tests first, then `go test ./...`, `go vet ./...`, frontend typecheck, tests, and build. Check desktop and mobile Overview layout.
