# Container Detail Page Design

## Intent

Clicking an existing container in either Services table opens a page for that exact Docker container instance. The page shows the information already visible in the tables, its own logs, its Docker inspect JSON, and the same start, stop, and restart actions. A scaled service therefore has one page per replica.

## User experience

- Link the service and container name in each row of the dashboard and stack Services tables to `#/stacks/{stackId}/containers/{containerId}`. Keep stack links and selection checkboxes independent. The URL supports reload, back, and forward navigation.
- Show breadcrumbs to Overview and the parent stack, followed by the Compose service and container name. The Overview tab is selected on entry.
- Use **Overview**, **Logs**, and **Inspect** tabs. Overview displays the exact container ID, name, Compose service, state, health, image, networks, and published ports. Reuse existing state and port presentation. Show an em dash for unavailable values.
- Put **Start**, **Stop**, and **Restart** in the page header. Enable Start only for `created` or `exited`; enable Stop and Restart only for `running`. Archived stacks, an active stack operation, or a pending request disable actions. Confirm Stop. Accepted actions open the existing operation drawer. Request errors appear on the page; completed operation errors remain in the drawer.
- Refresh the summary every five seconds while the page is visible and when focus or visibility returns. Refresh after an action completes. A vanished or wrong-stack container shows a clear not-found state with a link back to the parent stack. A failed read does not leave stale action buttons enabled.
- Logs shows the latest 500 lines from this ID's stdout and stderr, refreshed every five seconds only while the Logs tab is active and visible. Cap a returned snapshot at 1 MiB, show a truncation notice, and display a read error without losing the other tabs. Use the current Compose-environment redaction for log text and errors. An unavailable Docker log driver gets a clear error.
- Inspect fetches the Docker daemon's full JSON for this ID when the tab opens and when the user chooses Refresh. Render it as formatted, selectable text with horizontal scrolling and a Copy button. No automatic polling. The user explicitly chose full JSON, including environment values. The response is authenticated, marked `Cache-Control: no-store`, and never persisted or sent to telemetry. A payload above 2 MiB returns a clear size error rather than a partial JSON document.
- The layout uses the existing stack page and table styles; the header actions wrap and the JSON/log panes remain usable on narrow screens.

## Approach

Use the existing container list and five-second polling hook for the summary; find the exact ID in the current stack's list. Add two authenticated GET routes for logs and inspect. The control layer resolves the stack, checks that the requested full ID is in the current Compose `Ps(All: true)` result for the stack's project, then calls the existing Docker SDK connection. This exact-ID check applies to every read, not just action requests. The existing single-container POST action route handles page actions.

This approach keeps the page within `web/src/features/stacks/` and requires no new dependency or WebSocket topic. Filtering stack-wide logs in the browser would mix replicas; a new per-container WebSocket stream would add more moving parts for a five-second snapshot requirement.

| Option | Trade-off |
| --- | --- |
| Existing container list plus exact-ID Docker SDK read endpoints (chosen) | Reuses polling and action infrastructure; adds only two small read contracts. |
| Filter the current stack log stream in the browser | Fewer backend routes, but output from scaled replicas can be mixed or mislabeled and inspect still needs a route. |
| Dedicated per-container WebSocket stream | Lower log latency, but adds topic lifecycle, replay, and reconnection code for one page. |

## Backend contract and safeguards

- `GET /api/v1/stacks/{id}/containers/{containerId}/logs` returns `{ "output": string, "truncated": boolean }`. Server chooses 500 lines and the 1 MiB bound; there are no caller-controlled tail or follow parameters. Demultiplex Docker's non-TTY log stream and preserve plain TTY output.
- `GET /api/v1/stacks/{id}/containers/{containerId}/inspect` returns the Docker SDK `ContainerInspectResult.Raw` JSON object, with `Size: false`. Reject invalid or over-2-MiB JSON before writing a response. Return `404 ContainerNotFound` for an ID absent from the stack and `413 LimitExceeded` for excessive inspect size.
- Both routes use `readRoute`, exact stack/project membership validation, a bounded Docker request context, safe redaction of Docker errors, and `Cache-Control: no-store`. The inspect body deliberately remains unredacted per the user's choice; other API responses and logs keep existing redaction behavior.
- No shell command is built or executed. The Docker SDK already used for container actions performs logs and inspect reads.

## File boundaries

- `internal/compose/client.go` and a focused `internal/compose/container_details.go` own SDK reads, stream decoding, limits, and safe errors.
- `internal/control/containers.go` owns stack/project membership and maps the SDK response into the HTTP contract.
- `internal/http/routes_containers.go` owns routes; `internal/http/auth.go` owns the narrow container API interface; `internal/http/api.go` owns existing error mapping.
- `web/src/app/Workspace.tsx` selects the detail route. `web/src/features/stacks/ContainerDetail.tsx` owns page presentation and actions; a focused hook owns logs polling. Existing `ServiceCells` and both Services tables gain container links.

## Verification

Add a failing regression test before each behavior change. Cover exact replica targeting, wrong-stack and missing IDs, authentication and no-store headers, log demultiplexing, TTY logs, output bounds and redaction, inspect's full JSON and size limit, action state and operation conflicts, route reload/navigation, polling cleanup, and desktop/mobile layout. Run focused suites, then the repository's Go and frontend checks and a Playwright journey from each Services table.
