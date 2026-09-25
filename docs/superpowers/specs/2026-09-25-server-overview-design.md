# Server Overview Design

## Intent

The landing page is a live inventory of everything Porty manages on this server. It shows every Porty stack in one table and every existing Docker container belonging to those stacks in a second table. A container is one row, so scaled service replicas remain distinct. Both running and stopped containers appear. Docker containers outside Porty stacks are outside scope.

## Approach

Use the current per-stack container endpoint with four-request concurrency and overview-only polling. It already enforces project membership and supplies the fields the table needs, so this design adds no new server contract. A new aggregate endpoint could reduce browser requests, but would still need to collect per-stack runtime data or introduce a separate Docker inventory mapping. Revisit that option if measured latency or daemon load makes bounded polling inadequate.

## Page and navigation

- Keep the existing `#/` route. Rename the sidebar item and page heading to **Overview**. The page has **Stacks** and **Services** sections, each with its own search and filters. Stack detail remains at `#/stacks/{id}`; its breadcrumb returns to Overview.
- The Stacks table keeps its current columns, row links, selection limit, and stack actions. Include archived stacks in the default list. Show an **Archived** badge beside their names without hiding the last observed runtime. Add an Active/Archived filter alongside the current Modified filter. Archived stacks cannot be selected for actions; active stacks keep their current action rules.
- The Services table columns are selection, Stack, Service, State, Image, Networks, and Ports. Stack links to its detail page. Service shows the Compose service and full container name; each replica has its own row. The existing state, health, image, network, and port presentation remains authoritative. An archived stack is marked beside its name.
- Show all existing containers, including created and exited containers, and sort by stack name, service, container name, then ID. A declared service with no container has no row. Search matches stack, service, container name, image, and network. A state filter offers All, Running, Stopped, and Unhealthy. The Stacks and Services search/filter controls do not affect each other.
- Counts above Services show total loaded containers and running containers. If any stack read fails, label the inventory **Partial** and identify failed stacks; never imply an empty or complete server inventory while reads are incomplete.

## Live data

- Reuse `GET /api/v1/stacks` and `GET /api/v1/stacks/{id}/containers`. No new backend route or dependency is needed. The existing container route includes stopped containers and validates project membership.
- Fetch containers for active and archived stacks with at most four requests in flight. Poll five seconds after the previous cycle completes while Overview is visible. Queue an immediate refresh when the tab regains visibility or focus, the stack ID set changes, or a relevant container operation finishes; if a cycle is still running, start the queued refresh as soon as it finishes. Do not overlap cycles.
- A failed stack read removes its old rows from the current snapshot and produces a named warning. Successful stacks remain visible. A later successful read clears that warning. Ignore late responses after navigation or a changed stack set. A zero-stack page shows the existing create-stack prompt and an empty Services state; a successful zero-container snapshot says no containers exist yet.

## Actions

- Services selection is limited to 20 visible container rows. Select-all is unavailable if more than 20 rows are visible. Filtering clears selection; sorting and polling preserve selected IDs only while their rows remain visible. A disappeared container, changed row that no longer matches the filter, or failed stack read removes its selection.
- Start is available only if every selected container is `created` or `exited`. Stop and Restart require every selected container to be `running`. An archived stack, an active operation for any selected stack, a pending submission, or an unknown state disables the action. Stop requires confirmation with container and stack counts.
- Group selected full container IDs by stack ID. Submit one existing `POST /api/v1/stacks/{id}/containers/actions/{action}` request per stack, with no more than 20 IDs in any request. Register every accepted operation in the existing operation list. Report each stack's immediate acceptance or error by name; keep failed groups selected for retry and clear accepted groups. Later failures remain in the operation stream. The server's existing stack lock, membership check, state validation, archived guard, origin/CSRF checks, redaction, and bounded output remain authoritative.
- The stack detail Services table retains its existing actions and polling. It and the overview share only the small display logic that would otherwise be duplicated; do not introduce a generic table framework.

## Layout and accessibility

- Preserve the current table-first visual language in light and dark themes. On narrow screens both tables stay in labelled horizontal scroll regions. Keep selection and the leading identity column usable while scrolling, with no page-level horizontal overflow at 320 px.
- Give each checkbox an accessible name containing both stack and container, and announce partial reads and action outcomes through status/notice text. Links, filters, and action controls work by keyboard. Unknown and unhealthy states use text as well as color.

## Verification

- Write failing Vitest tests for archived visibility and action gating, distinct cross-stack and replica rows, filters, partial reads, polling cancellation, selection limits, cross-stack request grouping, mixed-state/active-operation guards, and partial acceptance.
- Add Playwright coverage for desktop and mobile overview tables, cross-stack selection and actions, and narrow-screen table scrolling. Run frontend test, typecheck, build, and affected Go tests. Existing Go tests for the container endpoint and batch action must remain green; no backend behavior changes are planned.
