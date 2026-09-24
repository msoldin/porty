# Stacks and Services Tables Design

## Intent

Make Docker Compose stacks and their containers readable at a glance and safe to control in groups. Preserve Porty's existing shell, repository controls, stack detail tabs, deployment history, and operation model. Use focused Preact feature code and the existing Go and Docker SDK boundaries; add no frontend or backend dependency.

The visual directions are [Stacks](../../design/stacks-table-overhaul-concept.png) and [Services](../../design/services-table-overhaul-concept.png). They define spacing, hierarchy, and component treatment. Sample dates, repositories, image names, and remote badge wording in the images are illustrative; the contracts and copy below prevail.

## Stacks view

- The page heading keeps **New stack** and adds one **Actions** menu for selected stacks. The menu contains Deploy, Restart, and Stop in that order. With no selection, its actions are unavailable. The selection summary only shows the number selected and a way to clear selection; it does not repeat the actions.
- The table columns, in order, are selection checkbox, Name, Runtime, Remote, Deployment, and Last deployment. Remove Working tree and Containers from this table. Keep the existing Modified filter and Working tree information in stack detail.
- Name uses the existing stack icon at 18–20 px and a 15–16 px link. The full name is available when it is visually truncated.
- Runtime labels: `running` → RUNNING, `stopped` → STOPPED, `partial` → PARTIALLY RUNNING, `unhealthy` → UNHEALTHY, absent or unrecognized → UNKNOWN. The unhealthy label is retained because the backend already distinguishes it. Use green, red, yellow, red, and neutral respectively, with text and an icon as well as color.
- Remote retains `remoteState(repo)` wording and repository-wide meaning. Deployment retains its existing freshness meaning. Runtime, Remote, and Deployment use the same badge dimensions, typography, and alignment; semantic tone varies.
- Last deployment means the latest recorded deployment **attempt**, including a failed attempt. Show its `startedAt` as a locale-formatted absolute timestamp with relative time secondary. Show `Never` if no attempt exists. A Docker state read failure must not erase this stored timestamp.
- Select-all applies to the currently visible filtered rows when there are at most 20. If more than 20 are visible, disable select-all with a 20-stack limit explanation; individual selection remains available up to 20. Search and filter changes clear selection; sorting does not. Selection also clears on navigation. No hidden row may be acted on accidentally.
- Deploy is available for selected non-archived stacks without an active operation. Restart and Stop are available only when **every** selected stack is non-archived, has a successful prior deployment, currently reports `running`, and has no active operation. Unknown, partial, unhealthy, and stale states do not permit Restart or Stop. Stop requires confirmation naming the count. The server remains authoritative if state changes after rendering.
- The stack action endpoint rechecks successful-deployment and running state under the existing stack lock for Stop and Restart, returning a conflict before an operation is created when either gate fails.
- A multi-stack command submits one existing stack action per selected stack. It reports each accepted operation and each immediate failure by stack name. Operation completion remains visible through Porty's operation stream. A browser reload does not undo accepted operations.

## Stack detail and Services view

- Rename the Overview heading **Runtime** to **Services**. Keep the existing Overview helper, Last deployment summary, and Compose project. Do not add a deployment-history table to Overview; the History tab remains authoritative.
- The detail page heading has one stack-wide **Actions** menu with Deploy, Restart, and Stop. These use the existing stack action API and the same conditional rules as today. Keep Validate accessible as a separate secondary action so the redesign does not remove it. Dirty editor changes and active operations continue to block mutations.
- The Services table has one row per existing Docker container, including each replica. Columns, in order: selection checkbox, Service, State, Image, Networks, Ports. Service name is primary; full container name is secondary and identifies replicas. Rows do not imply navigation.
- Container state labels: `running` → RUNNING; `created` and `exited` → STOPPED; unrecognized, paused, restarting, or unavailable → UNKNOWN. Docker health appears as secondary text; `unhealthy` is red even when the container state is running. Do not silently treat an unhealthy container as healthy.
- Image displays the exact image reference returned by Compose, including its tag when present. Do not fabricate `:latest`. Networks are the assigned runtime network names. Ports show target port and protocol, plus host and published port when present; absent publisher data displays `—`. Long values wrap or have a readable full-value affordance.
- The Services search filters by service name, container name, image, and network name within loaded rows. Filtering clears selection; polling preserves selected IDs still present and removes IDs that disappeared.
- Selected container rows expose a separate compact toolbar with Start, Restart, and Stop. Start is available only when every selected container is `created` or `exited`; Restart and Stop only when every selected container is `running`. Unknown, paused, and mixed-state selections have no mutation available. Archived stacks, dirty editor state, and active operations disable the toolbar. Select-all applies to visible rows when there are at most 20; otherwise it is disabled with a limit explanation. Confirm Stop with the selected count.
- Bulk container actions are a single asynchronous stack-scoped operation. The request contains one action and 1–20 unique full container IDs. Under the existing stack lock, reject archived stacks, IDs outside the stack, unsupported actions, and incompatible preflight states before starting the operation. Run the selected Docker SDK actions sequentially. Continue after an individual Docker failure, mark the operation failed if any item fails, and write a bounded, redacted per-container result to the operation output. Refresh the table when the operation completes. A later external Docker state change may produce a per-item failure and must never target another stack.

## Data and architecture

- Add image, networks, and structured ports to `GET /api/v1/stacks/{id}/containers` using fields already present in Compose `api.ContainerSummary`. Keep the existing ID, name, service, state, and health fields.
- Add optional `lastDeploymentAt` to each `GET /api/v1/stacks` item. Read latest attempt times for all active stacks in one SQL read; do not request deployment history per table row. The stack domain type stays independent of deployment history; assemble the additive HTTP list response from the stack list and deployment read model.
- Add `POST /api/v1/stacks/{id}/containers/actions/{action}` for the bulk container operation. Preserve the existing single-container route. Use the existing authentication, origin, CSRF, operation, coordinator, redaction, and bounded-output paths.
- Keep shared visual primitives small: an accessible Actions menu used by both screens, feature-local status mapping, and the existing badge base style. Do not introduce a generic data-table framework.

## Layout and accessibility

- Desktop rows have enough vertical space for two-line Services content and readable status labels. Maintain clear column alignment and restrained selected-row highlighting.
- On narrow screens, tables stay tables inside labelled horizontal scroll regions. Keep selection and identity visible while scrolling where practical; do not collapse them into unrelated cards. Menus and toolbars remain usable at 320 px without page-level horizontal overflow.
- Checkboxes have row-specific accessible names; select-all exposes checked and indeterminate states. The Actions menu supports keyboard open, Escape, focus return, and disabled-item explanations. Loading, empty, Docker-unavailable, and operation-error states are explicit.
- Both light and dark themes keep semantic contrast. Respect reduced-motion preferences.

## Verification

Write failing regression tests before code changes. Cover latest-attempt timestamps, Docker-unavailable behavior, replicas, image/network/port mapping, mixed and stale selection, lock conflicts, cross-stack IDs, archived stacks, CSRF/origin guards, redaction, and bounded output. Run focused Go and Vitest suites, then `go test ./...`, `go vet ./...`, frontend test/typecheck/build, and Playwright desktop/mobile interaction and screenshot checks. Compare the rendered tables with both concept images; document intentional copy/data differences rather than reproducing illustrative values.
